package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/app"
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

func (p *httpsProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ok, username := p.authenticate(r)
	if !ok {
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

func (p *httpsProxy) authenticate(r *http.Request) (bool, string) {
	username, password, ok := parseProxyAuthorization(r.Header.Get("Proxy-Authorization"))
	if !ok {
		return false, ""
	}

	valid, err := p.runtime.Service.ValidateProxyCredentials(r.Context(), username, password)
	if err != nil {
		p.runtime.Logger.Error("proxy credential validation failed", "error", err)
		return false, ""
	}
	return valid, username
}

func (p *httpsProxy) handleConnect(w http.ResponseWriter, r *http.Request, username string) {
	target := r.Host
	if !p.allowTarget(target) {
		http.Error(w, "proxy target denied", http.StatusForbidden)
		return
	}

	upstream, err := net.DialTimeout("tcp", target, 20*time.Second)
	if err != nil {
		http.Error(w, "connect target failed", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "hijacking is not supported", http.StatusInternalServerError)
		return
	}

	client, _, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}

	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	p.runtime.Logger.Info("proxy tunnel opened", "username", username, "target", target)
	pipe(client, upstream)
}

func (p *httpsProxy) handleForward(w http.ResponseWriter, r *http.Request, username string) {
	if r.URL == nil || r.URL.Scheme == "" || r.URL.Host == "" {
		http.Error(w, "absolute proxy request url is required", http.StatusBadRequest)
		return
	}

	if !p.allowTarget(r.URL.Host) {
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
		DialContext:         (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	defer transport.CloseIdleConnections()

	response, err := transport.RoundTrip(outbound)
	if err != nil {
		http.Error(w, "forward request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	p.runtime.Logger.Info("proxy request forwarded", "username", username, "target", r.URL.Host)
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

func parseProxyAuthorization(value string) (string, string, bool) {
	scheme, encoded, ok := strings.Cut(strings.TrimSpace(value), " ")
	if !ok || !equalFoldASCII(scheme, "basic") {
		return "", "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", "", false
	}

	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" || password == "" {
		return "", "", false
	}
	return username, password, true
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
