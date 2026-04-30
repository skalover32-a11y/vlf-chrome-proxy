package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/app"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/redact"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtime, err := app.New(ctx)
	if err != nil {
		log.Fatalf("start runtime: %v", err)
	}
	defer runtime.Close()

	proxy := &httpsProxy{runtime: runtime}
	server := &http.Server{
		Addr:              runtime.Config.HTTPSProxyListenAddr,
		Handler:           proxy,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       90 * time.Second,
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){},
		ErrorLog:          log.New(tlsErrorWriter{logger: runtime.Logger}, "", 0),
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	runtime.Logger.Info("https proxy listening", "addr", runtime.Config.HTTPSProxyListenAddr)
	if err := server.ListenAndServeTLS(runtime.Config.HTTPSProxyTLSCertPath, runtime.Config.HTTPSProxyTLSKeyPath); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen https proxy: %v", err)
	}
}

type httpsProxy struct {
	runtime *app.Runtime
}

type tlsErrorWriter struct {
	logger *slog.Logger
}

func (w tlsErrorWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if strings.Contains(message, "TLS handshake error") && strings.HasSuffix(message, ": EOF") {
		return len(p), nil
	}

	w.logger.Warn("https proxy server error", "message", message)
	return len(p), nil
}

func (p *httpsProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ok, username, reason := p.authenticate(r)
	if !ok {
		p.runtime.Logger.Warn(
			"proxy auth rejected",
			"reason", reason,
			"username", redact.String(username),
			"method", r.Method,
			"target", r.Host,
			"remote_addr", r.RemoteAddr,
		)
		w.Header().Set("Proxy-Authenticate", `Basic realm="VLF Browser Proxy"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}

	if r.Method == http.MethodConnect {
		p.handleConnect(w, r, username)
		return
	}

	p.handleForward(w, r, username)
}

func (p *httpsProxy) authenticate(r *http.Request) (bool, string, string) {
	username, password, ok, reason := parseProxyAuthorization(r.Header.Get("Proxy-Authorization"))
	if !ok {
		return false, username, reason
	}

	valid, err := p.runtime.Service.ValidateProxyCredentials(r.Context(), username, password)
	if err != nil {
		p.runtime.Logger.Error("proxy credential validation failed", "error", err)
		return false, username, "validation_error"
	}
	if !valid {
		return false, username, "invalid_credentials"
	}
	return true, username, "valid"
}

func (p *httpsProxy) handleConnect(w http.ResponseWriter, r *http.Request, username string) {
	target := r.Host
	if !p.allowTarget(target) {
		p.runtime.Logger.Warn("proxy target denied", "username", redact.String(username), "target", target, "remote_addr", r.RemoteAddr)
		http.Error(w, "proxy target denied", http.StatusForbidden)
		return
	}

	upstream, err := p.dialContext(r.Context(), "tcp", target)
	if err != nil {
		if errors.Is(err, errNoIPv4Address) {
			p.runtime.Logger.Info("proxy connect skipped ipv6-only target", "username", redact.String(username), "target", target, "remote_addr", r.RemoteAddr)
			http.Error(w, "target has no ipv4 address", http.StatusBadGateway)
			return
		}
		p.runtime.Logger.Warn("proxy connect target failed", "username", redact.String(username), "target", target, "remote_addr", r.RemoteAddr, "error", err)
		http.Error(w, "connect target failed", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		p.runtime.Logger.Warn("proxy connect hijack unsupported", "username", redact.String(username), "target", target, "proto", r.Proto, "remote_addr", r.RemoteAddr)
		http.Error(w, "hijacking is not supported", http.StatusInternalServerError)
		return
	}

	client, _, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		p.runtime.Logger.Warn("proxy connect hijack failed", "username", redact.String(username), "target", target, "remote_addr", r.RemoteAddr, "error", err)
		return
	}

	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	p.runtime.Logger.Info("proxy tunnel opened", "username", redact.String(username), "target", target)
	pipe(client, upstream)
}

func (p *httpsProxy) handleForward(w http.ResponseWriter, r *http.Request, username string) {
	if r.URL == nil || r.URL.Scheme == "" || r.URL.Host == "" {
		p.runtime.Logger.Warn("proxy forward rejected invalid url", "username", redact.String(username), "method", r.Method, "remote_addr", r.RemoteAddr)
		http.Error(w, "absolute proxy request url is required", http.StatusBadRequest)
		return
	}

	if !p.allowTarget(r.URL.Host) {
		p.runtime.Logger.Warn("proxy forward target denied", "username", redact.String(username), "target", r.URL.Host, "remote_addr", r.RemoteAddr)
		http.Error(w, "proxy target denied", http.StatusForbidden)
		return
	}

	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	outbound.Header = r.Header.Clone()
	outbound.Header.Del("Proxy-Authorization")
	outbound.Header.Del("Proxy-Connection")

	transport := &http.Transport{
		Proxy:               nil,
		ForceAttemptHTTP2:   true,
		DialContext:         p.dialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	defer transport.CloseIdleConnections()

	response, err := transport.RoundTrip(outbound)
	if err != nil {
		if errors.Is(err, errNoIPv4Address) {
			p.runtime.Logger.Info("proxy forward skipped ipv6-only target", "username", redact.String(username), "target", r.URL.Host, "remote_addr", r.RemoteAddr)
			http.Error(w, "target has no ipv4 address", http.StatusBadGateway)
			return
		}
		p.runtime.Logger.Warn("proxy forward request failed", "username", redact.String(username), "target", r.URL.Host, "remote_addr", r.RemoteAddr, "error", err)
		http.Error(w, "forward request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	p.runtime.Logger.Info("proxy request forwarded", "username", redact.String(username), "target", r.URL.Host)
}

var errNoIPv4Address = errors.New("target has no ipv4 address")

func (p *httpsProxy) dialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if ip.To4() != nil {
			return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), port))
		}
		if !p.runtime.Config.ProxyEnableIPv6 {
			return nil, errNoIPv4Address
		}
		return dialer.DialContext(ctx, "tcp6", net.JoinHostPort(ip.String(), port))
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	var firstErr error
	for _, addr := range addrs {
		ipv4 := addr.IP.To4()
		if ipv4 == nil {
			continue
		}
		conn, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ipv4.String(), port))
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	if p.runtime.Config.ProxyEnableIPv6 {
		for _, addr := range addrs {
			if addr.IP.To4() != nil {
				continue
			}
			conn, err := dialer.DialContext(ctx, "tcp6", net.JoinHostPort(addr.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("%w: %s", errNoIPv4Address, host)
}

func (p *httpsProxy) allowTarget(target string) bool {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		host = target
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	return p.runtime.Service.AllowProxyDestination(host)
}

func parseProxyAuthorization(value string) (string, string, bool, string) {
	if strings.TrimSpace(value) == "" {
		return "", "", false, "missing_proxy_authorization"
	}

	scheme, encoded, ok := strings.Cut(strings.TrimSpace(value), " ")
	if !ok || !equalFoldASCII(scheme, "basic") {
		return "", "", false, "unsupported_proxy_authorization"
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", "", false, "invalid_proxy_authorization"
	}

	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" || password == "" {
		return username, "", false, "invalid_proxy_authorization"
	}
	return username, password, true, "valid"
}

func pipe(a net.Conn, b net.Conn) {
	done := make(chan struct{}, 2)
	go copyAndClose(a, b, done)
	go copyAndClose(b, a, done)
	<-done
	_ = a.Close()
	_ = b.Close()
}

func copyAndClose(dst net.Conn, src net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	done <- struct{}{}
}

func copyHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(a)), []byte(strings.ToLower(b))) == 1
}
