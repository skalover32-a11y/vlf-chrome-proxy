package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/things-go/go-socks5"

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

	server := socks5.NewServer(
		socks5.WithCredential(credentialStore{runtime: runtime}),
		socks5.WithRule(proxyRuleSet{runtime: runtime}),
		socks5.WithLogger(socks5.NewLogger(log.New(os.Stdout, "socks5: ", log.LstdFlags))),
	)

	runtime.Logger.Info("socks5 listening", "addr", runtime.Config.Socks5ListenAddr)
	if err := server.ListenAndServe("tcp", runtime.Config.Socks5ListenAddr); err != nil {
		log.Fatalf("listen socks5: %v", err)
	}
}

type credentialStore struct {
	runtime *app.Runtime
}

func (c credentialStore) Valid(username, password, userAddr string) bool {
	ok, err := c.runtime.Service.ValidateProxyCredentials(context.Background(), username, password)
	if err != nil {
		c.runtime.Logger.Error("proxy credential validation failed", "error", err)
		return false
	}
	return ok
}

type proxyRuleSet struct {
	runtime *app.Runtime
}

func (p proxyRuleSet) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	if req.DestAddr == nil {
		return ctx, false
	}
	host := req.DestAddr.FQDN
	if host == "" && req.DestAddr.IP != nil {
		host = req.DestAddr.IP.String()
	}
	if host == "" {
		return ctx, false
	}
	if ip := net.ParseIP(host); ip != nil && !p.runtime.Service.AllowProxyDestination(ip.String()) {
		p.runtime.Logger.Warn("proxy destination denied", "host", host)
		return ctx, false
	}
	return ctx, true
}
