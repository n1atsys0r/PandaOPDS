package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pandaopds/api"
	"pandaopds/app/webui"
	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
	"pandaopds/internal/favorites"
	"pandaopds/internal/progression"
	"pandaopds/internal/recorder"

	"golang.org/x/sync/singleflight"
)

type Server struct {
	Settings     config.Settings
	Service      *eh.Service
	Store        *archive.Store
	Archives     *archive.Manager
	Favorites    *favorites.Syncer
	Progression  *progression.Store
	Tracker      *recorder.Tracker
	Logger       *log.Logger
	mux          *http.ServeMux
	configOK     bool
	configError  any
	settingsPath string
	editMu       sync.Mutex
	editKey      []byte

	stopArchiveUpdates   context.CancelFunc
	archiveUpdatesDone   chan struct{}
	archiveScanMu        sync.RWMutex
	archiveScan          ArchiveScanJob
	archiveScanDone      chan struct{}
	archiveScanCancel    context.CancelFunc
	archiveScanNextID    uint64
	progressionArchiveMu sync.Mutex
	coverFlight          singleflight.Group
}

func New(settings config.Settings, logger *log.Logger) (*Server, error) {
	editKey := make([]byte, 32)
	if _, err := rand.Read(editKey); err != nil {
		return nil, fmt.Errorf("initialize editor ETag key: %w", err)
	}
	s := &Server{Settings: settings, Logger: logger, mux: http.NewServeMux(), settingsPath: config.EditablePath(), editKey: editKey}
	s.archiveScan.Status = "idle"
	if err := settings.Validate(); err != nil {
		s.configOK = false
		s.configError = err.Error()
	} else {
		s.configOK = true
	}
	service := eh.NewService(settings, nil)
	service.Logger = logger
	if service.Client != nil {
		service.Client.Logger = logger
	}
	s.Service = service
	service.StartTagTranslation()
	incidentsPath := filepath.Join(settings.CacheDir, "incidents.json")
	s.Tracker = recorder.NewTracker(incidentsPath, 1000)
	service.Throttle.Circuit.SetCallbacks(eh.CircuitCallbacks{
		OnTrip: func(reason string, cooldown time.Duration, detail, method, target string) {
			if detail == "" {
				detail = reason
			}
			if s.Tracker != nil {
				s.Tracker.OnCircuitTrip(reason, cooldown, recorder.RequestTrace{
					Method: method,
					Path:   target,
					Status: 503,
					Error:  detail,
				}, detail)
			}
		},
		OnReset: func() {
			if s.Tracker != nil {
				s.Tracker.OnCircuitReset()
			}
		},
		OnRecovered: func() {
			if s.Tracker != nil {
				s.Tracker.OnCircuitRecovered()
			}
		},
	})
	store, err := archive.NewStoreWithState(settings.ArchiveDir, settings.ArchiveStateDir)
	if err != nil {
		return nil, fmt.Errorf("open archive store: %w", err)
	}
	s.Store = store
	progressStore, err := progression.Open(settings.ArchiveStateDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("open progression store: %w", err)
	}
	s.Progression = progressStore
	service.AttachArchive(store)
	s.Archives = archive.NewManager(settings, service.Client, service, store)
	s.Archives.SetAvailabilityHook(s.refreshProgressionArchiveIndex)
	rolled := s.Archives.RecoverInterruptedUpgrades()
	if rolled > 0 && logger != nil {
		logger.Printf("archive startup: rolled back %d interrupted tier upgrade(s)", rolled)
	}
	s.refreshProgressionArchiveIndex()
	state := favorites.NewState(settings.FavoritesSyncState)
	s.Favorites = favorites.NewSyncer(settings, service, s.Archives, state)
	s.Favorites.Start()
	s.startArchiveVersionChecks()
	s.register()
	return s, nil
}
func (s *Server) Handler() http.Handler {
	return s.loggingMiddleware(s.recoverMiddleware(s.basicAuth(s.mux)))
}
func (s *Server) Close() error {
	if s.stopArchiveUpdates != nil {
		s.stopArchiveUpdates()
		<-s.archiveUpdatesDone
	}
	if s.Favorites != nil {
		s.Favorites.Stop()
	}
	s.archiveScanMu.RLock()
	scanDone := s.archiveScanDone
	scanCancel := s.archiveScanCancel
	s.archiveScanMu.RUnlock()
	if scanCancel != nil {
		scanCancel()
	}
	if scanDone != nil {
		<-scanDone
	}
	if s.Archives != nil {
		s.Archives.Close()
	}
	var closeErr error
	if s.Store != nil {
		closeErr = s.Store.Close()
	}
	if s.Progression != nil {
		closeErr = errors.Join(closeErr, s.Progression.Close())
	}
	if s.Service != nil {
		closeErr = errors.Join(closeErr, s.Service.Close())
	}
	return closeErr
}
func (s *Server) register() {
	m := s.mux
	// Public API contract and Swagger assets share the same auth gate.
	api.Register(m)
	m.HandleFunc("GET /health", s.health)
	m.HandleFunc("GET /opds/v1.2/{$}", s.redirectOPDS("/opds/v1.2"))
	m.HandleFunc("/opds/progression/{gid}/{token}", s.progressionHandler)
	m.HandleFunc("GET /opds/authentication.json", s.progressionAuthentication)
	m.HandleFunc("GET /opds/v1.2", s.rootV12)
	m.HandleFunc("GET /opds/v1.2/search.xml", s.searchXML12)
	m.HandleFunc("GET /opds/v1.2/gallery", s.gallery12)
	m.HandleFunc("GET /opds/v1.2/toplist", s.toplist12)
	m.HandleFunc("GET /opds/v1.2/archives", s.archives12)
	m.HandleFunc("GET /opds/v1.2/history", s.history12)
	m.HandleFunc("GET /opds/v1.2/gallery/{gid}/{token}/chapters", s.chapter12)
	m.HandleFunc("GET /opds/v2", s.redirectOPDS("/opds/v2.0"))
	m.HandleFunc("GET /opds/v2/{$}", s.redirectOPDS("/opds/v2.0"))
	m.HandleFunc("GET /opds/v2.0/{$}", s.redirectOPDS("/opds/v2.0"))
	m.HandleFunc("GET /opds/v2.0", s.rootV20)
	m.HandleFunc("GET /opds/v2.0/page/{id}", s.pageV20)
	m.HandleFunc("GET /opds/v2.0/search.xml", s.searchXML20)
	m.HandleFunc("GET /opds/v2.0/gallery", s.gallery20)
	m.HandleFunc("GET /opds/v2.0/toplist", s.toplist20)
	m.HandleFunc("GET /opds/v2.0/archives", s.archives20)
	m.HandleFunc("GET /opds/v2.0/history", s.history20)
	m.HandleFunc("GET /opds/v2.0/gallery/{gid}/{token}", s.detail20)
	m.HandleFunc("GET /opds/v2.0/gallery/{gid}/{token}/manifest", s.manifest20)
	m.HandleFunc("GET /opds/v2.0/gallery/{gid}/{token}/publication", s.publication20)
	m.HandleFunc("GET /stream/{gid}/{token}/page/{page}", s.streamPage)
	m.HandleFunc("GET /image/{gid}/{token}/thumb", s.thumb)
	m.HandleFunc("GET /image/{gid}/{token}/page/{page}/thumb", s.pageThumb)
	m.HandleFunc("GET /image/fetch", s.imageFetch)
	m.HandleFunc("GET /api/archive", s.archiveList)
	m.HandleFunc("GET /api/archive/import/scan", s.archiveScanStatus)
	m.HandleFunc("POST /api/archive/import/scan", s.startArchiveScan)
	m.HandleFunc("GET /api/archive/{gid}/{token}/quote", s.archiveQuote)
	m.HandleFunc("POST /api/archive/{gid}/{token}/start", s.archiveStart)
	m.HandleFunc("GET /api/archive/{gid}/{token}/metadata", s.archiveMetadata)
	m.HandleFunc("POST /api/archive/{gid}/{token}/metadata/refresh", s.archiveMetadataRefresh)
	m.HandleFunc("POST /api/archive/{gid}/{token}/refresh", s.archiveRefresh)
	m.HandleFunc("POST /api/archive/{gid}/{token}/update", s.archiveUpdate)
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
	m.HandleFunc("GET /api/tags/suggestions", s.tagSuggestions)
	m.HandleFunc("GET /api/config", s.apiConfig)
	m.HandleFunc("GET /api/config/edit", s.configEdit)
	m.HandleFunc("PATCH /api/config/edit", s.configEdit)
	m.HandleFunc("GET /api/home", s.apiHome)
	m.HandleFunc("GET /api/home/edit", s.homeEdit)
	m.HandleFunc("PUT /api/home/edit", s.homeEdit)
	m.HandleFunc("GET /api/home/file", s.homeFile)
	m.HandleFunc("PUT /api/home/file", s.homeFile)
	m.HandleFunc("POST /api/cache/clear", s.cacheClear)
	m.HandleFunc("GET /api/incidents", s.apiIncidents)
	m.HandleFunc("GET /api/incidents/{id}", s.apiIncidentDetail)
	m.HandleFunc("POST /api/circuit/reset", s.apiCircuitReset)
	m.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Handle("GET /webui-assets/assets/{name}", http.StripPrefix("/webui-assets/", http.FileServerFS(webui.Assets)))
	m.HandleFunc("GET /{$}", s.webPage)
}
func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.Settings
		editRoute := r.URL.Path == "/api/config/edit" || r.URL.Path == "/api/home/edit" || r.URL.Path == "/api/home/file" || r.URL.Path == "/api/circuit/reset"
		isWebUI := r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/webui-assets/") || r.URL.Path == "/favicon.ico"
		if !cfg.AuthEnabled() || (!editRoute && (r.URL.Path == "/health" || r.URL.Path == "/opds/authentication.json" || isWebUI || contains(cfg.AuthExemptPaths, r.URL.Path) || prefixMatch(cfg.AuthExemptPrefixes, r.URL.Path))) {
			next.ServeHTTP(w, r)
			return
		}
		u, p, ok := parseBasic(r.Header.Get("Authorization"))
		if ok && constantEqual(u, cfg.AuthUsername) && constantEqual(p, cfg.AuthPassword) {
			next.ServeHTTP(w, r)
			return
		}
		if s.Logger != nil {
			s.Logger.Printf("basic auth rejected: %s %s from %s", r.Method, r.URL.Path, clientIP(r))
		}
		if strings.HasPrefix(r.URL.Path, "/opds/progression/") || (r.Header.Get("X-Requested-With") != "XMLHttpRequest" && !strings.HasPrefix(r.URL.Path, "/api/")) {
			w.Header().Set("WWW-Authenticate", `Basic realm="PandaOPDS"`)
		}
		if strings.HasPrefix(r.URL.Path, "/opds/progression/") {
			writeBytes(w, http.StatusUnauthorized, "application/opds-authentication+json", nil, s.progressionAuthDocument())
			return
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
func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if s.Logger != nil {
					s.Logger.Printf("HTTP panic %s %s: %v\n%s", r.Method, r.URL.RequestURI(), v, debug.Stack())
				}
				writeJSON(w, 500, map[string]any{"detail": "Internal Server Error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Logger == nil && s.Tracker == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		duration := time.Since(start)
		ip := clientIP(r)
		uri := r.URL.RequestURI()
		if uri == "" {
			uri = r.URL.Path
		}
		proto := r.Proto
		if proto == "" {
			proto = "HTTP/1.1"
		}
		if s.Logger != nil {
			s.Logger.Printf("%s - %q %d %s", ip, r.Method+" "+uri+" "+proto, sw.status, duration.Round(time.Millisecond))
		}
		if s.Tracker != nil && isUpstreamRelated(uri) {
			s.Tracker.RecordTrace(recorder.RequestTrace{
				Timestamp:  start,
				Method:     r.Method,
				Path:       uri,
				Kind:       detectRequestKind(uri),
				Status:     sw.status,
				DurationMs: duration.Milliseconds(),
				ClientIP:   ip,
				UserAgent:  r.UserAgent(),
				Error:      sw.errDetail,
			})
		}
	})
}

func isUpstreamRelated(uri string) bool {
	path := uri
	if idx := strings.IndexByte(uri, '?'); idx != -1 {
		path = uri[:idx]
	}

	if strings.HasPrefix(path, "/opds/") {
		if path == "/opds/authentication.json" || strings.HasPrefix(path, "/opds/progression/") {
			return false
		}
		return true
	}

	if strings.HasPrefix(path, "/image/") || strings.HasPrefix(path, "/stream/") {
		return true
	}

	if strings.HasPrefix(path, "/api/archive/") {
		if strings.HasPrefix(path, "/api/archive/import/scan") {
			return false
		}
		return true
	}

	if strings.HasPrefix(path, "/api/favorites") {
		if path == "/api/favorites/sync" {
			return false
		}
		return true
	}

	if path == "/api/rating" {
		return true
	}

	return false
}

func detectRequestKind(uri string) string {
	path := uri
	if idx := strings.IndexByte(uri, '?'); idx != -1 {
		path = uri[:idx]
	}
	if strings.HasPrefix(path, "/image/") {
		if strings.Contains(path, "/thumb") {
			return "thumb"
		}
		return "image"
	}
	if strings.HasPrefix(path, "/stream/") {
		return "stream"
	}
	if strings.HasPrefix(path, "/opds/") {
		return "opds"
	}
	if strings.HasPrefix(path, "/api/archive/") {
		return "archive"
	}
	if strings.HasPrefix(path, "/api/favorites") {
		return "favorite"
	}
	if path == "/api/rating" {
		return "rating"
	}
	return "upstream"
}

type errorDetailSetter interface {
	SetErrorDetail(detail string)
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytes       int64
	errDetail   string
}

func (w *statusWriter) SetErrorDetail(detail string) {
	w.errDetail = detail
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	if status >= 400 && w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeBytes(w http.ResponseWriter, status int, contentType string, headers map[string]string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	if status >= 400 && w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || (r != nil && errors.Is(r.Context().Err(), context.Canceled)) {
		return
	}

	var detail string
	var status int

	var ae *archive.ArchiverUnavailableError
	if errors.As(err, &ae) {
		status = 403
		detail = ae.Detail
		if s.Logger != nil && r != nil {
			s.Logger.Printf("upstream error %s %s: status=403 archiver unavailable: %s", r.Method, r.URL.RequestURI(), ae.Detail)
		}
		if setter, ok := w.(errorDetailSetter); ok {
			setter.SetErrorDetail(detail)
		}
		if s.Tracker != nil && r != nil {
			s.Tracker.OnUpstreamError(status, detail, recorder.RequestTrace{
				Timestamp: time.Now(),
				Method:    r.Method,
				Path:      r.URL.RequestURI(),
				Kind:      detectRequestKind(r.URL.RequestURI()),
				Status:    status,
				ClientIP:  clientIP(r),
				UserAgent: r.UserAgent(),
				Error:     detail,
			})
		}
		writeJSON(w, 403, map[string]any{"error": "ArchiverUnavailableError", "detail": ae.Detail})
		return
	}

	var ce *eh.CircuitOpenError
	if errors.As(err, &ce) {
		detail = ce.Reason
		headers := map[string]string{}
		if ce.RetryAfter > 0 {
			headers["Retry-After"] = fmt.Sprint(int(ce.RetryAfter))
		}
		if s.Logger != nil && r != nil {
			s.Logger.Printf("upstream error %s %s: status=503 circuit open: %s", r.Method, r.URL.RequestURI(), ce.Reason)
		}
		if setter, ok := w.(errorDetailSetter); ok {
			setter.SetErrorDetail(detail)
		}
		if s.Tracker != nil && r != nil {
			s.Tracker.OnRequestBlocked(recorder.RequestTrace{
				Timestamp: time.Now(),
				Method:    r.Method,
				Path:      r.URL.RequestURI(),
				Kind:      detectRequestKind(r.URL.RequestURI()),
				Status:    503,
				ClientIP:  clientIP(r),
				UserAgent: r.UserAgent(),
				IsBlocked: true,
				Error:     ce.Reason,
			})
		}
		writeJSONHeaders(w, 503, map[string]any{"error": "upstream_throttled", "detail": ce.Reason}, headers)
		return
	}

	var ue *eh.UpstreamError
	if errors.As(err, &ue) {
		status = ue.Status
		detail = ue.Detail
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
		if s.Logger != nil && r != nil {
			s.Logger.Printf("upstream error %s %s: status=%d %s", r.Method, r.URL.RequestURI(), status, ue.Detail)
		}
		if setter, ok := w.(errorDetailSetter); ok {
			setter.SetErrorDetail(detail)
		}
		if s.Tracker != nil && r != nil && (status >= 400) {
			s.Tracker.OnUpstreamError(status, ue.Detail, recorder.RequestTrace{
				Timestamp: time.Now(),
				Method:    r.Method,
				Path:      r.URL.RequestURI(),
				Kind:      detectRequestKind(r.URL.RequestURI()),
				Status:    status,
				ClientIP:  clientIP(r),
				UserAgent: r.UserAgent(),
				Error:     ue.Detail,
			})
		}
		writeJSONHeaders(w, status, body, h)
		return
	}

	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "star membership") || strings.Contains(lower, "insufficient gp") || strings.Contains(lower, "cannot be archived") {
		status = 403
		detail = err.Error()
		if s.Logger != nil && r != nil {
			s.Logger.Printf("upstream error %s %s: status=403 %s", r.Method, r.URL.RequestURI(), err.Error())
		}
		if setter, ok := w.(errorDetailSetter); ok {
			setter.SetErrorDetail(detail)
		}
		if s.Tracker != nil && r != nil {
			s.Tracker.OnUpstreamError(status, detail, recorder.RequestTrace{
				Timestamp: time.Now(),
				Method:    r.Method,
				Path:      r.URL.RequestURI(),
				Kind:      detectRequestKind(r.URL.RequestURI()),
				Status:    status,
				ClientIP:  clientIP(r),
				UserAgent: r.UserAgent(),
				Error:     detail,
			})
		}
		writeJSON(w, 403, map[string]any{"error": "ArchiverUnavailableError", "detail": err.Error()})
		return
	}

	status = 502
	detail = err.Error()
	if s.Logger != nil && r != nil {
		s.Logger.Printf("upstream error %s %s: status=502 %s", r.Method, r.URL.RequestURI(), err.Error())
	}
	if setter, ok := w.(errorDetailSetter); ok {
		setter.SetErrorDetail(detail)
	}
	if s.Tracker != nil && r != nil {
		s.Tracker.OnUpstreamError(status, detail, recorder.RequestTrace{
			Timestamp: time.Now(),
			Method:    r.Method,
			Path:      r.URL.RequestURI(),
			Kind:      detectRequestKind(r.URL.RequestURI()),
			Status:    status,
			ClientIP:  clientIP(r),
			UserAgent: r.UserAgent(),
			Error:     detail,
		})
	}
	writeJSON(w, 502, map[string]any{"error": "EHException", "detail": err.Error()})
}
func writeJSONHeaders(w http.ResponseWriter, status int, v any, headers map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	for k, x := range headers {
		w.Header().Set(k, x)
	}
	if status >= 400 && w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
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
