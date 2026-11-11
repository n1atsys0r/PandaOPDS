package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"pandaopds/api"
	"pandaopds/app/webui"
	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/favorites"
)

type Server struct {
	Settings     config.Settings
	Service      *eh.Service
	Store        *archive.Store
	Archives     *archive.Manager
	Favorites    *favorites.Syncer
	Logger       *log.Logger
	mux          *http.ServeMux
	configOK     bool
	configError  any
	settingsPath string
	editMu       sync.Mutex
	editKey      []byte
}

func New(settings config.Settings, logger *log.Logger) (*Server, error) {
	editKey:=make([]byte,32);if _,err:=rand.Read(editKey);err!=nil{return nil,fmt.Errorf("initialize editor ETag key: %w",err)}
	s := &Server{Settings: settings, Logger: logger, mux: http.NewServeMux(), settingsPath: config.EditablePath(),editKey:editKey}
	if err := settings.Validate(); err != nil {
		s.configOK = false
		s.configError = err.Error()
	} else {
		s.configOK = true
	}
	service := eh.NewService(settings, nil)
	s.Service = service
	service.StartTagTranslation()
	store, err := archive.NewStore(settings.ArchiveDir)
	if err != nil {
		return nil, fmt.Errorf("open archive store: %w", err)
	}
	s.Store = store
	service.AttachArchive(store)
	s.Archives = archive.NewManager(settings, service.Client, service, store)
	rolled := s.Archives.RecoverInterruptedUpgrades()
	if rolled > 0 && logger != nil {
		logger.Printf("archive startup: rolled back %d interrupted tier upgrade(s)", rolled)
	}
	state := favorites.NewState(settings.FavoritesSyncState)
	s.Favorites = favorites.NewSyncer(settings, service, s.Archives, state)
	s.Favorites.Start()
	s.register()
	return s, nil
}
func (s *Server) Handler() http.Handler { return recoverMiddleware(s.basicAuth(s.mux)) }
func (s *Server) Close() error {
	if s.Favorites != nil {
		s.Favorites.Stop()
	}
	if s.Archives != nil {
		s.Archives.Close()
	}
	return s.Service.Close()
}
func (s *Server) register() {
	m := s.mux
	// Public API contract and Swagger assets share the same auth gate.
	api.Register(m)
	m.HandleFunc("GET /health", s.health)
	m.HandleFunc("GET /opds/v1.2/{$}", s.redirectOPDS("/opds/v1.2"))
	m.HandleFunc("GET /opds/v1.2", s.rootV12)
	m.HandleFunc("GET /opds/v1.2/search.xml", s.searchXML12)
	m.HandleFunc("GET /opds/v1.2/gallery", s.gallery12)
	m.HandleFunc("GET /opds/v1.2/toplist", s.toplist12)
	m.HandleFunc("GET /opds/v1.2/archives", s.archives12)
	m.HandleFunc("GET /opds/v1.2/gallery/{gid}/{token}/chapters", s.chapter12)
	m.HandleFunc("GET /opds/v2", s.redirectOPDS("/opds/v2.0"))
	m.HandleFunc("GET /opds/v2/{$}", s.redirectOPDS("/opds/v2.0"))
	m.HandleFunc("GET /opds/v2.0/{$}", s.redirectOPDS("/opds/v2.0"))
	m.HandleFunc("GET /opds/v2.0", s.rootV20)
	m.HandleFunc("GET /opds/v2.0/search.xml", s.searchXML20)
	m.HandleFunc("GET /opds/v2.0/gallery", s.gallery20)
	m.HandleFunc("GET /opds/v2.0/toplist", s.toplist20)
	m.HandleFunc("GET /opds/v2.0/archives", s.archives20)
	m.HandleFunc("GET /opds/v2.0/gallery/{gid}/{token}", s.detail20)
	m.HandleFunc("GET /opds/v2.0/gallery/{gid}/{token}/publication", s.publication20)
	m.HandleFunc("GET /stream/{gid}/{token}/page/{page}", s.streamPage)
	m.HandleFunc("GET /image/{gid}/{token}/thumb", s.thumb)
	m.HandleFunc("GET /image/{gid}/{token}/page/{page}/thumb", s.pageThumb)
	m.HandleFunc("GET /image/fetch", s.imageFetch)
	m.HandleFunc("GET /api/archive", s.archiveList)
	m.HandleFunc("GET /api/archive/{gid}/{token}/quote", s.archiveQuote)
	m.HandleFunc("POST /api/archive/{gid}/{token}/start", s.archiveStart)
	m.HandleFunc("GET /api/archive/{gid}/{token}/metadata", s.archiveMetadata)
	m.HandleFunc("POST /api/archive/{gid}/{token}/metadata/refresh", s.archiveMetadataRefresh)
	m.HandleFunc("POST /api/archive/{gid}/{token}/refresh", s.archiveRefresh)
	m.HandleFunc("DELETE /api/archive/{gid}/{token}", s.archiveDelete)
	m.HandleFunc("GET /api/archive/{gid}/{token}", s.archiveStatus)
	m.HandleFunc("GET /api/archive/{gid}/{token}/cover", s.archiveCover)
	m.HandleFunc("POST /api/favorites", s.favoriteAction)
	m.HandleFunc("GET /api/favorites/categories", s.favoriteCategories)
	m.HandleFunc("GET /api/favorites/favcats", s.favoriteFavcats)
	m.HandleFunc("GET /api/favorites/sync", s.favoriteSyncStatus)
	m.HandleFunc("POST /api/favorites/sync/run", s.favoriteSyncRun)
	m.HandleFunc("POST /api/rating", s.rateGallery)
	m.HandleFunc("GET /api/status", s.apiStatus)
	m.HandleFunc("GET /api/config", s.apiConfig)
	m.HandleFunc("GET /api/config/edit", s.configEdit)
	m.HandleFunc("PATCH /api/config/edit", s.configEdit)
	m.HandleFunc("GET /api/home", s.apiHome)
	m.HandleFunc("GET /api/home/edit", s.homeEdit)
	m.HandleFunc("PUT /api/home/edit", s.homeEdit)
	m.HandleFunc("POST /api/cache/clear", s.cacheClear)
	m.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Handle("GET /webui-assets/assets/{name}", http.StripPrefix("/webui-assets/", http.FileServerFS(webui.Assets)))
	m.HandleFunc("GET /{$}", s.webPage)
}
func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.Settings
		editRoute := r.URL.Path == "/api/config/edit" || r.URL.Path == "/api/home/edit"
		if editRoute && !cfg.AuthEnabled() {
			writeJSON(w, 403, map[string]any{"detail": "admin Basic Auth is required for editing"})
			return
		}
		isWebUI := r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/webui-assets/") || r.URL.Path == "/favicon.ico"
		if !editRoute && (!cfg.AuthEnabled() || r.URL.Path == "/health" || isWebUI || contains(cfg.AuthExemptPaths, r.URL.Path) || prefixMatch(cfg.AuthExemptPrefixes, r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		u, p, ok := parseBasic(r.Header.Get("Authorization"))
		if ok && constantEqual(u, cfg.AuthUsername) && constantEqual(p, cfg.AuthPassword) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" && !strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("WWW-Authenticate", `Basic realm="PandaOPDS"`)
		}
		writeJSON(w, 401, map[string]any{"error": "unauthorized", "detail": "valid PandaOPDS credentials required"})
	})
}
func parseBasic(header string) (string, string, bool) {
	if !strings.HasPrefix(header, "Basic ") {
		return "", "", false
	}
	raw := strings.TrimSpace(header[6:])
	if raw == "" || !regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`).MatchString(raw) {
		return "", "", false
	}
	b, e := base64.StdEncoding.Strict().DecodeString(raw)
	if e != nil || !utf8.Valid(b) {
		return "", "", false
	}
	i := strings.IndexByte(string(b), ':')
	if i < 0 {
		return "", "", false
	}
	return string(b[:i]), string(b[i+1:]), true
}
func constantEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func contains(a []string, v string) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}
func prefixMatch(a []string, v string) bool {
	for _, x := range a {
		if strings.HasPrefix(v, x) {
			return true
		}
	}
	return false
}
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				writeJSON(w, 500, map[string]any{"detail": "Internal Server Error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeBytes(w http.ResponseWriter, status int, contentType string, headers map[string]string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func writeError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var ae *archive.ArchiverUnavailableError
	if errors.As(err, &ae) {
		writeJSON(w, 403, map[string]any{"error": "ArchiverUnavailableError", "detail": ae.Detail})
		return
	}
	var ce *eh.CircuitOpenError
	if errors.As(err, &ce) {
		headers := map[string]string{}
		if ce.RetryAfter > 0 {
			headers["Retry-After"] = fmt.Sprint(int(ce.RetryAfter))
		}
		writeJSONHeaders(w, 503, map[string]any{"error": "upstream_throttled", "detail": ce.Reason}, headers)
		return
	}
	var ue *eh.UpstreamError
	if errors.As(err, &ue) {
		status := ue.Status
		if status == 403 && !strings.Contains(strings.ToLower(ue.Detail), "requires ipb cookies") && !strings.Contains(strings.ToLower(ue.Detail), "star membership") && !strings.Contains(strings.ToLower(ue.Detail), "insufficient gp") && !strings.Contains(strings.ToLower(ue.Detail), "cannot be archived") {
			status = 503
		}
		if status == 429 && ue.RetryAfter == "" {
			ue.RetryAfter = fmt.Sprint(int(300))
		}
		body := map[string]any{"error": "EHException", "detail": ue.Detail}
		if status == 400 {
			body = map[string]any{"detail": ue.Detail}
		}
		h := map[string]string{}
		if ue.RetryAfter != "" {
			h["Retry-After"] = ue.RetryAfter
		}
		writeJSONHeaders(w, status, body, h)
		return
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "star membership") || strings.Contains(lower, "insufficient gp") || strings.Contains(lower, "cannot be archived") {
		writeJSON(w, 403, map[string]any{"error": "ArchiverUnavailableError", "detail": err.Error()})
		return
	}
	writeJSON(w, 502, map[string]any{"error": "EHException", "detail": err.Error()})
}
func writeJSONHeaders(w http.ResponseWriter, status int, v any, headers map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	for k, x := range headers {
		w.Header().Set(k, x)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func pathID(r *http.Request) (int, string, error) {
	gid, err := strconv.Atoi(r.PathValue("gid"))
	if err != nil || gid <= 0 {
		return 0, "", fmt.Errorf("invalid gid")
	}
	token := r.PathValue("token")
	if token == "" || !regexp.MustCompile(`^[0-9a-fA-F]+$`).MatchString(token) {
		return 0, "", fmt.Errorf("invalid token")
	}
	return gid, token, nil
}
