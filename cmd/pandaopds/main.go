package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"pandaopds/internal/app"
	"pandaopds/internal/archive"
	"pandaopds/internal/config"
)

func main() {
	_ = godotenv.Load()
	settings := config.Load()
	if (len(os.Args) == 3 || len(os.Args) == 4) && os.Args[1] == "archive" && os.Args[2] == "import" {
		sidecarSource := settings.ArchiveDir
		if len(os.Args) == 4 {
			sidecarSource = os.Args[3]
		}
		report, err := archive.ImportArchive(sidecarSource, settings.ArchiveStateDir, settings.ArchiveDir)
		if err != nil {
			log.Fatalf("import archive sidecars: %v", err)
		}
		if err := archive.CheckpointArchiveIndex(settings.ArchiveDir, settings.ArchiveStateDir); err != nil {
			log.Fatalf("save imported archive index: %v", err)
		}
		fmt.Printf("Archive import: imported_state=%d imported_gallery=%d imported_cover=%d migrated_payloads=%d skipped=%d conflicts=%d\n", report.State, report.Gallery, report.Cover, report.Payloads, report.Skipped, report.Conflicts)
		return
	}
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
