package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"pandaopds/internal/app"
	"pandaopds/internal/config"
	"pandaopds/internal/document"
	"pandaopds/internal/migration"
	"pandaopds/internal/storage"
)

func main() {
	_ = godotenv.Load()
	settings := config.Load()
	if (len(os.Args) == 3 || len(os.Args) == 4) && os.Args[1] == "archive" && os.Args[2] == "import" {
		sidecarSource := settings.ArchiveDir
		if len(os.Args) == 4 {
			sidecarSource = os.Args[3]
		}
		databasePath := settings.DatabasePath
		if databasePath == "" {
			databasePath = filepath.Join(filepath.Dir(settings.ArchiveStateDir), "pandaopds.db")
		}
		db, err := storage.Open(databasePath)
		if err != nil {
			log.Fatalf("open SQLite database: %v", err)
		}
		defer db.Close()
		if err = migration.ImportLegacyArchive(db, sidecarSource, settings.ArchiveStateDir); err != nil {
			log.Fatalf("import archive sidecars: %v", err)
		}
		docs, err := document.New(db, settings.ArchiveDir)
		if err != nil {
			log.Fatalf("open document scanner: %v", err)
		}
		report, err := docs.Scan(context.Background())
		if err != nil {
			log.Fatalf("scan local archive documents: %v", err)
		}
		linked, err := migration.LinkLegacyDocuments(db, settings.ArchiveDir)
		if err != nil {
			log.Fatalf("link imported legacy documents: %v", err)
		}
		fmt.Printf("SQLite archive migration complete: documents_found=%v legacy_documents_linked=%d source=%s database=%s\n", report["documents_found"], linked, sidecarSource, databasePath)
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
