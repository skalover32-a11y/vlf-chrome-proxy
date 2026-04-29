package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/app"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/httpapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtime, err := app.New(ctx)
	if err != nil {
		log.Fatalf("start runtime: %v", err)
	}
	defer runtime.Close()

	server := &http.Server{
		Addr:    runtime.Config.HTTPListenAddr,
		Handler: httpapi.New(runtime.Service, runtime.Logger, runtime.Config).Handler(),
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	runtime.Logger.Info("api listening", "addr", runtime.Config.HTTPListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen api: %v", err)
	}
}
