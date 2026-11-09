// Package api serves the versioned API contract and its documentation.
package api

import (
	"embed"
	"net/http"
)

//go:embed openapi.json swagger/*
var assets embed.FS

// Register mounts the docs on the same mux and behind the same authentication
// middleware as the application routes.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs", serve("swagger/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /openapi.json", serve("openapi.json", "application/json"))
	mux.HandleFunc("GET /docs/assets/swagger-ui.css", serve("swagger/swagger-ui.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /docs/assets/swagger-ui-bundle.js", serve("swagger/swagger-ui-bundle.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("GET /docs/assets/init.js", serve("swagger/init.js", "text/javascript; charset=utf-8"))
}

func serve(path, contentType string) http.HandlerFunc {
	data, err := assets.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	}
}
