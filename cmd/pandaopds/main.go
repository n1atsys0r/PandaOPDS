package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"pandaopds/internal/app"
	"pandaopds/internal/config"
)

func main() {
	_ = godotenv.Load()
	settings := config.Load()
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	server, err := app.New(settings, logger)
	if err != nil {
		logger.Fatalf("initialize PandaOPDS: %v", err)
	}
	defer server.Close()
	addr := ":" + env("PORT", "8000")
	logger.Printf("PandaOPDS Go starting on %s (site=%s)", addr, settings.SiteHost())
	httpServer := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	stopped := make(chan error, 1)
	go func() { stopped <- httpServer.ListenAndServe() }()
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signals.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Printf("HTTP shutdown timed out: %v", err)
			_ = httpServer.Close()
		}
	case err := <-stopped:
		if err != nil && err != http.ErrServerClosed {
			logger.Fatal(err)
		}
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
