package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsServeLocalAssetsAndOpenAPI(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)
	for _, target := range []struct {
		path, mime, contains string
	}{
		{"/docs", "text/html", "./docs/assets/init.js"},
		{"/docs/assets/swagger-ui.css", "text/css", ".swagger-ui"},
		{"/docs/assets/swagger-ui-bundle.js", "text/javascript", "SwaggerUIBundle"},
		{"/docs/assets/init.js", "text/javascript", `supportedSubmitMethods: ["get", "head"]`},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target.path, nil))
		if response.Code != http.StatusOK ||
			!strings.HasPrefix(response.Header().Get("Content-Type"), target.mime) ||
			!strings.Contains(response.Body.String(), target.contains) {
			t.Errorf("invalid documentation response at %s, status %d", target.path, response.Code)
		}
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	var spec struct {
		Version string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &spec) != nil {
		t.Fatal("invalid OpenAPI response")
	}
	if !strings.HasPrefix(spec.Version, "3.1.") {
		t.Fatalf("unexpected OpenAPI version %q", spec.Version)
	}
	if len(spec.Paths["/api/archive/{gid}/{token}/start"]["post"]) == 0 {
		t.Fatal("archive write operation is missing from the document")
	}
}
