package eh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/config"
)

type Service struct {
	Settings   config.Settings
	Client     *Client
	Throttle   *Throttle
	mu         sync.Mutex
	lists      map[string]cached[GalleryPageInfo]
	cover      map[string]cached[string]
	pages      map[string]cached[[]Thumbnail]
	details    map[string]cached[DetailPageInfo]
	imgPages   map[string]cached[ImagePageInfo]
	metadata   map[string]cached[GalleryMetadata]
	categories map[string]cached[map[int]string]
	locks      map[string]*sync.Mutex
	disk       *DiskCache
	started    time.Time
	archive    ArchiveReader
	myTags     map[string]map[string]string
	myTagsAt   time.Time
	myTagsSeen bool
	translator *TagTranslator
}

// ArchiveReader is implemented by the persistent archive store. Keeping this
// narrow interface prevents the EH package from depending on archive handlers.
type ArchiveReader interface {
	GetPage(gid int, token string, page int) ([]byte, bool, error)
	ReadyCount() int
	ListReady(page, perPage int) []ArchiveInfo
	GetCover(gid int, token string) ([]byte, bool)
	GetMetadataSnapshot(gid int, token string) (map[string]any, bool)
	PageCount(gid int, token string) int
}
type ArchiveInfo struct {
	GID        int    `json:"gid"`
	Token      string `json:"token"`
	Title      string `json:"title,omitempty"`
	PageCount  int    `json:"page_count"`
	CreatedAt  int64  `json:"created_at"`
	Quality    string `json:"quality,omitempty"`
	Status     string `json:"status"`
	MetadataAt int64  `json:"metadata_at,omitempty"`
}

func NewService(cfg config.Settings, transport http.RoundTripper) *Service {
	th := NewThrottle(cfg)
	s := &Service{Settings: cfg, Throttle: th, Client: NewClient(cfg, transport, th), lists: map[string]cached[GalleryPageInfo]{}, cover: map[string]cached[string]{}, pages: map[string]cached[[]Thumbnail]{}, details: map[string]cached[DetailPageInfo]{}, imgPages: map[string]cached[ImagePageInfo]{}, metadata: map[string]cached[GalleryMetadata]{}, categories: map[string]cached[map[int]string]{}, locks: map[string]*sync.Mutex{}, disk: NewDiskCache(cfg.CacheDir, cfg.CacheMaxGB, cfg.ImageCacheEnabled), started: time.Now(), myTags: map[string]map[string]string{}, translator: NewTagTranslator(cfg, transport)}
	s.loadMyTags()
	return s
}
func (s *Service) AttachArchive(a ArchiveReader) { s.archive = a }
func (s *Service) Close() error {
	if s.translator != nil {
		s.translator.Stop()
	}
	return s.Client.Close()
}
func (s *Service) StartTagTranslation() {
	if s.translator != nil {
		s.translator.Start()
	}
}
func (s *Service) TagTranslator() *TagTranslator { return s.translator }
func (s *Service) lock(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[key] = l
	}
	return l
}
func (s *Service) GetList(ctx context.Context, query, next string, extra url.Values, bypass bool) (GalleryPageInfo, error) {
	params := url.Values{}
	for k, v := range extra {
		params[k] = append([]string(nil), v...)
	}
	clean, adv := ExtractAdvancedQuery(query)
	for k, v := range adv {
		params[k] = append(params[k], v...)
	}
	cacheKey := query + "?" + next + "?" + params.Encode()
	s.mu.Lock()
	if !bypass {
		if v, ok := lookup(s.lists, cacheKey); ok {
			s.mu.Unlock()
			return v, nil
		}
	}
	s.mu.Unlock()
	lock := s.lock("list:" + cacheKey)
	lock.Lock()
	defer lock.Unlock()
	s.mu.Lock()
	if !bypass {
		if v, ok := lookup(s.lists, cacheKey); ok {
			s.mu.Unlock()
			return v, nil
		}
	}
	s.mu.Unlock()
	raw, _, err := s.Client.Get(ctx, s.Client.ListURL(clean, next, params), KindHTML)
	if err != nil {
		return GalleryPageInfo{}, err
	}
	info, err := ParseListPage(string(raw))
	if err != nil {
		return GalleryPageInfo{}, err
	}
	ttl := time.Duration(s.Settings.ListCacheTTLSeconds) * time.Second
	s.mu.Lock()
	put(s.lists, cacheKey, info, ttl)
	for _, item := range info.Galleries {
		if item.CoverURL != "" {
			put(s.cover, fmt.Sprintf("%d:%s", item.GID, item.Token), item.CoverURL, time.Duration(s.Settings.PageURLTTLSeconds)*time.Second)
		}
	}
	s.mu.Unlock()
	return info, nil
}
func (s *Service) Search(ctx context.Context, query, next string, extra url.Values) (GalleryPageInfo, error) {
	if s.Settings.TagTranslationEnabled && s.translator != nil {
		query = s.translator.NormalizeCategoryQuery(query, s.Settings.Facets)
	}
	query, cats := ExtractCategoryQuery(query, s.Settings, s.Settings.TagTranslationEnabled)
	if s.Settings.TagTranslationEnabled && s.translator != nil {
		query = s.translator.TranslateQuery(query)
	}
	if len(cats) > 0 {
		mask := CategoryMask(cats, s.Settings)
		if existing := extra.Get("f_cats"); existing != "" {
			if n, e := strconv.Atoi(existing); e == nil {
				mask = IntersectCategoryMasks(n, mask)
			}
		}
		if mask > 0 {
			if extra == nil {
				extra = url.Values{}
			}
			extra.Set("f_cats", fmt.Sprint(mask))
		}
	}
	return s.GetList(ctx, query, next, extra, false)
}
func (s *Service) Popular(ctx context.Context, next string) (GalleryPageInfo, error) {
	return s.GetList(ctx, "popular", next, nil, false)
}
func (s *Service) Watched(ctx context.Context, next string) (GalleryPageInfo, error) {
	return s.GetList(ctx, "watched", next, nil, false)
}
func (s *Service) Favorites(ctx context.Context, next string, favcat string) (GalleryPageInfo, error) {
	query := "favorites"
	extra := url.Values{}
	if favcat != "" {
		extra.Set("favcat", favcat)
	}
	return s.GetList(ctx, query, next, extra, false)
}
func (s *Service) Toplist(ctx context.Context, period string, page int) (GalleryPageInfo, error) {
	key := fmt.Sprintf("toplist:%s:%d", period, page)
	s.mu.Lock()
	if v, ok := lookup(s.lists, key); ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	raw, _, err := s.Client.Get(ctx, s.Client.ToplistURL(period, page), KindHTML)
	if err != nil {
		return GalleryPageInfo{}, err
	}
	v, err := ParseListPage(string(raw))
	if err != nil {
		return v, err
	}
	s.mu.Lock()
	put(s.lists, key, v, time.Duration(s.Settings.ListCacheTTLSeconds)*time.Second)
	s.mu.Unlock()
	return v, nil
}
func (s *Service) GetDetail(ctx context.Context, gid int, token string) (DetailPageInfo, error) {
	key := fmt.Sprintf("detail:%d:%s", gid, token)
	s.mu.Lock()
	if v, ok := lookup(s.details, key); ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	lock := s.lock(key)
	lock.Lock()
	defer lock.Unlock()
	s.mu.Lock()
	if v, ok := lookup(s.details, key); ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	d, err := s.getDetailPage(ctx, gid, token, 0)
	if err != nil && s.archive != nil && s.archiveReady(gid, token) {
		if snap, ok := s.archive.GetMetadataSnapshot(gid, token); ok {
			d = detailFromSnapshot(snap, s.archive.PageCount(gid, token))
			err = nil
		}
	}
	if err != nil {
		return DetailPageInfo{}, err
	}
	s.mu.Lock()
	put(s.details, key, d, time.Duration(s.Settings.PageURLTTLSeconds)*time.Second)
	s.mu.Unlock()
	return d, nil
}

// GetMyTags lazily refreshes the account's tag-style map. A stale snapshot is
// returned when the upstream page is unavailable so highlighting never
// breaks publication rendering.
func (s *Service) GetMyTags(ctx context.Context) map[string]map[string]string {
	if !s.Settings.HasIPB() {
		return map[string]map[string]string{}
	}
	lock := s.lock("mytags")
	lock.Lock()
	defer lock.Unlock()
	s.mu.Lock()
	if s.myTagsSeen && s.Settings.MyTagsTTLSeconds > 0 && time.Since(s.myTagsAt) < time.Duration(s.Settings.MyTagsTTLSeconds*float64(time.Second)) {
		out := cloneStyles(s.myTags)
		s.mu.Unlock()
		return out
	}
	s.mu.Unlock()
	body, _, err := s.Client.Get(ctx, s.Client.BaseURL()+"/mytags", KindHTML)
	if err != nil {
		return s.CachedMyTags()
	}
	styles := ParseMyTags(string(body))
	s.mu.Lock()
	s.myTags, s.myTagsAt, s.myTagsSeen = styles, time.Now(), true
	out := cloneStyles(styles)
	s.mu.Unlock()
	s.persistMyTags(styles)
	return out
}

// CachedMyTags returns the in-memory or persisted map without upstream I/O.
// Archive-backed detail reads use this path to remain fully offline.
func (s *Service) CachedMyTags() map[string]map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStyles(s.myTags)
}

func (s *Service) ApplyMyTagStyles(tags []GalleryTag) []GalleryTag {
	styles := s.CachedMyTags()
	return applyStyles(tags, styles)
}

func applyStyles(tags []GalleryTag, styles map[string]map[string]string) []GalleryTag {
	if len(styles) == 0 {
		return tags
	}
	out := append([]GalleryTag(nil), tags...)
	for i := range out {
		if len(out[i].Style) > 0 {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(out[i].String()), " "))
		style := styles[key]
		if style == nil {
			_, name, _ := strings.Cut(key, ":")
			style = styles["*:"+name]
		}
		if style != nil {
			out[i].Style = map[string]string{}
			for k, v := range style {
				out[i].Style[k] = v
			}
		}
	}
	return out
}

// ApplyTagsWithStyles attaches persisted My Tags styles to tags that do not
// already carry an inline style.
func ApplyTagsWithStyles(tags []GalleryTag, styles map[string]map[string]string) []GalleryTag {
	return applyStyles(tags, styles)
}

func cloneStyles(styles map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(styles))
	for k, v := range styles {
		out[k] = map[string]string{}
		for name, value := range v {
			out[k][name] = value
		}
	}
	return out
}

func (s *Service) loadMyTags() {
	if s.Settings.MyTagsState == "" {
		return
	}
	body, err := os.ReadFile(s.Settings.MyTagsState)
	if err != nil {
		return
	}
	var state struct {
		Styles  map[string]map[string]string `json:"styles"`
		SavedAt float64                      `json:"saved_at"`
	}
	if json.Unmarshal(body, &state) != nil || state.Styles == nil {
		return
	}
	s.myTags = state.Styles
	s.myTagsAt = time.UnixMilli(int64(state.SavedAt * 1000))
	s.myTagsSeen = true
}

func (s *Service) persistMyTags(styles map[string]map[string]string) {
	path := s.Settings.MyTagsState
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	payload, err := json.MarshalIndent(map[string]any{"styles": styles, "saved_at": float64(time.Now().UnixMilli()) / 1000}, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}
func (s *Service) archiveReady(gid int, token string) bool {
	for _, a := range s.archive.ListReady(1, 10000) {
		if a.GID == gid && a.Token == token {
			return true
		}
	}
	return false
}
func detailFromSnapshot(snap map[string]any, count int) DetailPageInfo {
	d := DetailPageInfo{ImageCount: count, Title: toString(snap["title"]), TitleJPN: toString(snap["title_jpn"]), Category: toString(snap["category"]), CoverURL: toString(snap["thumb"]), Rating: toFloat(snap["rating"]), Uploader: toString(snap["uploader"]), Expunged: toBool(snap["expunged"]), Tags: []GalleryTag{}}
	if d.Title == "" {
		d.Title = d.TitleJPN
	}
	if raw, ok := snap["tags"].(map[string]any); ok {
		for ns, v := range raw {
			if list, ok := v.([]any); ok {
				for _, entry := range list {
					if m, ok := entry.(map[string]any); ok {
						tag := GalleryTag{Namespace: toString(m["namespace"]), Key: toString(m["key"]), Status: toString(m["status"])}
						if tag.Namespace == "" {
							tag.Namespace = ns
						}
						if tag.Status == "" {
							tag.Status = "confidence"
						}
						d.Tags = append(d.Tags, tag)
					}
				}
			}
		}
	}
	if posted := toInt64(snap["posted"]); posted > 0 {
		d.PublishTime = time.Unix(posted, 0).UTC().Format("2006-01-02 15:04")
	}
	if size := toInt64(snap["filesize"]); size > 0 {
		d.FilesizeBytes = &size
	}
	d.Language = languageFromTags(d.Tags)
	if d.ImageCount > 0 {
		d.PageCount = (d.ImageCount + 19) / 20
	}
	return d
}
func languageFromTags(tags []GalleryTag) string {
	for _, t := range tags {
		if t.Namespace == "language" {
			if v := language(t.Key); v != "" {
				return v
			}
		}
	}
	return ""
}
func (s *Service) getDetailPage(ctx context.Context, gid int, token string, index int) (DetailPageInfo, error) {
	key := detailPageCacheKey(gid, token, index)
	s.mu.Lock()
	if v, ok := lookup(s.details, key); ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	u := s.Client.GalleryURL(gid, token)
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	q.Set("p", fmt.Sprint(index))
	q.Set("inline_set", "dm_e")
	parsed.RawQuery = q.Encode()
	raw, _, err := s.Client.Get(ctx, parsed.String(), KindHTML)
	if err != nil {
		return DetailPageInfo{}, err
	}
	d, err := ParseDetailPage(string(raw), s.Settings.SiteHost(), index)
	if err != nil {
		return d, err
	}
	s.mu.Lock()
	put(s.details, key, d, time.Duration(s.Settings.PageURLTTLSeconds)*time.Second)
	put(s.pages, fmt.Sprintf("%d:%s", gid, token), d.Thumbnails, time.Duration(s.Settings.PageURLTTLSeconds)*time.Second)
	if d.CoverURL != "" {
		put(s.cover, fmt.Sprintf("%d:%s", gid, token), d.CoverURL, time.Duration(s.Settings.PageURLTTLSeconds)*time.Second)
	}
	s.mu.Unlock()
	return d, nil
}

func detailPageCacheKey(gid int, token string, pageIndex int) string {
	return fmt.Sprintf("detailpage:%d:%s:%d", gid, token, pageIndex)
}

func imagePageCacheKey(gid int, token string, pageNo int) string {
	return fmt.Sprintf("%d:%s:%d", gid, token, pageNo)
}

func (s *Service) resolveImagePage(ctx context.Context, gid int, token string, pageNo int) (ImagePageInfo, error) {
	key := imagePageCacheKey(gid, token, pageNo)
	s.mu.Lock()
	if info, ok := lookup(s.imgPages, key); ok {
		s.mu.Unlock()
		return info, nil
	}
	s.mu.Unlock()
	lock := s.lock("imgpage:" + key)
	lock.Lock()
	defer lock.Unlock()
	s.mu.Lock()
	if info, ok := lookup(s.imgPages, key); ok {
		s.mu.Unlock()
		return info, nil
	}
	s.mu.Unlock()
	pageIndex := (pageNo - 1) / 20
	detail, err := s.getDetailPage(ctx, gid, token, pageIndex)
	if err != nil {
		return ImagePageInfo{}, err
	}
	thumbIndex := (pageNo - 1) % 20
	if thumbIndex >= len(detail.Thumbnails) {
		return ImagePageInfo{}, &UpstreamError{Status: 404, Detail: "image page not found"}
	}
	href := detail.Thumbnails[thumbIndex].Href
	if !strings.HasPrefix(strings.ToLower(href), "http") {
		href = s.Client.BaseURL() + href
	}
	pageHTML, _, err := s.Client.Get(ctx, href, KindImage)
	if err != nil {
		return ImagePageInfo{}, err
	}
	info, err := ParseImagePage(string(pageHTML))
	if err != nil {
		return info, err
	}
	if info.ImageURL != "" && !strings.HasPrefix(strings.ToLower(info.ImageURL), "http") {
		info.ImageURL = s.Client.BaseURL() + info.ImageURL
	}
	s.mu.Lock()
	put(s.imgPages, key, info, time.Duration(s.Settings.PageURLTTLSeconds)*time.Second)
	s.mu.Unlock()
	return info, nil
}

func (s *Service) invalidateImageSource(gid int, token string, pageNo int) {
	s.mu.Lock()
	delete(s.imgPages, imagePageCacheKey(gid, token, pageNo))
	delete(s.details, detailPageCacheKey(gid, token, (pageNo-1)/20))
	delete(s.pages, fmt.Sprintf("%d:%s", gid, token))
	s.mu.Unlock()
}

func retryableImageError(err error) bool {
	var circuit *CircuitOpenError
	if errors.As(err, &circuit) {
		return false
	}
	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		if upstream.Status == 404 || upstream.Status == 429 || strings.Contains(strings.ToLower(upstream.Detail), "banned") || strings.Contains(strings.ToLower(upstream.Detail), "image limit") {
			return false
		}
	}
	return true
}

func (s *Service) GetImage(ctx context.Context, gid int, token string, page int) ([]byte, string, error) {
	if page < s.Settings.PSEPageBase {
		return nil, "", fmt.Errorf("page must be >= %d (PSE page base)", s.Settings.PSEPageBase)
	}
	pageNo := page
	if s.Settings.PSEPageBase == 0 {
		pageNo++
	}
	index := pageNo - 1
	if s.archive != nil {
		if b, ok, err := s.archive.GetPage(gid, token, index); err == nil && ok {
			return b, detectImageType(b), nil
		}
	}
	key := fmt.Sprintf("%d:%s:%d", gid, token, pageNo)
	if data, ok := s.disk.Get(key); ok {
		return data, detectImageType(data), nil
	}
	retries := s.Settings.ImageSourceRetries
	if retries < 0 {
		retries = 0
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		info, err := s.resolveImagePage(ctx, gid, token, pageNo)
		if err != nil {
			lastErr = err
			if !retryableImageError(err) || attempt >= retries {
				return nil, "", err
			}
			s.invalidateImageSource(gid, token, pageNo)
			continue
		}
		if info.Is509 {
			s.mu.Lock()
			delete(s.imgPages, imagePageCacheKey(gid, token, pageNo))
			s.mu.Unlock()
			s.Throttle.Circuit.Trip("exceed", time.Duration(s.Settings.ExceedCooldownSeconds*float64(time.Second)))
			return nil, "", &UpstreamError{Status: 429, Detail: "image limit exceeded (509 placeholder)"}
		}
		data, _, fetchErr := s.Client.Get(ctx, info.ImageURL, KindImage)
		if fetchErr != nil && info.ReloadKey != "" && attempt == 0 && retries >= 1 && retryableImageError(fetchErr) {
			parsed, parseErr := url.Parse(info.ImageURL)
			if parseErr == nil {
				q := parsed.Query()
				q.Set("nl", info.ReloadKey)
				parsed.RawQuery = q.Encode()
				data, _, fetchErr = s.Client.Get(ctx, parsed.String(), KindImage)
			}
		}
		if fetchErr != nil {
			lastErr = fetchErr
			if !retryableImageError(fetchErr) || attempt >= retries {
				return nil, "", fetchErr
			}
			s.invalidateImageSource(gid, token, pageNo)
			continue
		}
		if isHTML(data) {
			lastErr = &UpstreamError{Status: 502, Detail: "upstream returned HTML instead of image bytes"}
			if attempt >= retries {
				return nil, "", lastErr
			}
			s.invalidateImageSource(gid, token, pageNo)
			continue
		}
		mime := detectImageType(data)
		if mime != "application/octet-stream" || !isHTML(data) {
			s.disk.Put(key, data)
		}
		return data, mime, nil
	}
	return nil, "", lastErr
}
func (s *Service) GetThumb(ctx context.Context, gid int, token string) ([]byte, string, error) {
	if s.archive != nil {
		if b, ok := s.archive.GetCover(gid, token); ok {
			return b, "image/jpeg", nil
		}
	}
	diskKey := fmt.Sprintf("%d:%s:-1", gid, token)
	if data, ok := s.disk.Get(diskKey); ok {
		return data, detectImageType(data), nil
	}
	key := fmt.Sprintf("%d:%s", gid, token)
	s.mu.Lock()
	u, ok := lookup(s.cover, key)
	s.mu.Unlock()
	if !ok {
		d, err := s.getDetailPage(ctx, gid, token, 0)
		if err != nil {
			return nil, "", err
		}
		if len(d.Thumbnails) == 0 {
			return nil, "", errors.New("gallery has no thumbnail")
		}
		u = d.Thumbnails[0].ThumbURL
	}
	data, _, e := s.Client.Get(ctx, u, KindThumb)
	if e != nil {
		return nil, "", e
	}
	mime := detectImageType(data)
	s.disk.Put(diskKey, data)
	return data, mime, nil
}
func (s *Service) FetchCoverBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	data, _, e := s.Client.Get(ctx, rawURL, KindThumb)
	if e != nil {
		return nil, "", e
	}
	mime := detectImageType(data)
	return data, mime, nil
}
func (s *Service) GetPageThumbnail(ctx context.Context, gid int, token string, page int) ([]byte, string, error) {
	if page < s.Settings.PSEPageBase {
		return nil, "", fmt.Errorf("page must be >= %d", s.Settings.PSEPageBase)
	}
	pageNo := page
	if s.Settings.PSEPageBase == 0 {
		pageNo++
	}
	index := pageNo - 1
	previewKey := fmt.Sprintf("%d:%s:%d", gid, token, -(pageNo + 1))
	if data, ok := s.disk.Get(previewKey); ok {
		return data, "image/jpeg", nil
	}
	if s.archive != nil {
		if pageBytes, ok, err := s.archive.GetPage(gid, token, index); err == nil && ok {
			data, e := renderJPEGPreview(pageBytes, nil, 400, 400)
			if e == nil {
				s.disk.Put(previewKey, data)
				return data, "image/jpeg", nil
			}
		}
	}
	d, e := s.getDetailPage(ctx, gid, token, index/20)
	if e != nil {
		return nil, "", e
	}
	i := index % 20
	if i >= len(d.Thumbnails) {
		return nil, "", &UpstreamError{Status: 404, Detail: "thumbnail page not found"}
	}
	thumb := d.Thumbnails[i]
	width, height := int(math.Round(thumb.Width)), int(math.Round(thumb.Height))
	geometry := thumb.BackgroundX != nil && thumb.BackgroundY != nil && math.Trunc(*thumb.BackgroundX) == *thumb.BackgroundX && math.Trunc(*thumb.BackgroundY) == *thumb.BackgroundY && width > 0 && height > 0
	if thumb.IsSprite != nil && *thumb.IsSprite && !geometry {
		return nil, "", errors.New("sprite thumbnail dimensions or offsets are missing")
	}
	if thumb.IsSprite == nil && !geometry {
		return nil, "", errors.New("thumbnail layout is ambiguous; preview omitted")
	}
	if !geometry {
		width, height = 400, 400
	}
	if width > 2000 || height > 2000 {
		return nil, "", errors.New("thumbnail dimensions are missing or unsafe")
	}
	b, _, e := s.Client.Get(ctx, thumb.ThumbURL, KindThumb)
	if e != nil {
		return nil, "", e
	}
	var crop *image.Rectangle
	if geometry {
		left, top := int(math.Round(-*thumb.BackgroundX)), int(math.Round(-*thumb.BackgroundY))
		r := image.Rect(left, top, left+width, top+height)
		crop = &r
	}
	preview, e := renderJPEGPreview(b, crop, width, height)
	if e != nil {
		return nil, "", fmt.Errorf("cannot decode page thumbnail: %w", e)
	}
	s.disk.Put(previewKey, preview)
	return preview, "image/jpeg", nil
}

func detectImageType(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg"
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	case len(data) >= 2 && string(data[:2]) == "BM":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}

func isHTML(data []byte) bool {
	prefix := strings.ToLower(strings.TrimSpace(string(data[:min(len(data), 1024)])))
	return strings.HasPrefix(prefix, "<!doctype html") || strings.HasPrefix(prefix, "<html") || strings.HasPrefix(prefix, "<body")
}

func renderJPEGPreview(source []byte, crop *image.Rectangle, maxWidth, maxHeight int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	if crop != nil {
		bounds := img.Bounds()
		requested := crop.Add(bounds.Min)
		if requested.Empty() || !requested.In(bounds) {
			return nil, errors.New("sprite crop is outside source image bounds")
		}
		cropped := image.NewRGBA(image.Rect(0, 0, requested.Dx(), requested.Dy()))
		draw.Draw(cropped, cropped.Bounds(), img, requested.Min, draw.Src)
		img = cropped
	} else {
		bounds := img.Bounds()
		if bounds.Dx() > maxWidth || bounds.Dy() > maxHeight {
			scale := math.Min(float64(maxWidth)/float64(bounds.Dx()), float64(maxHeight)/float64(bounds.Dy()))
			w := max(1, int(math.Round(float64(bounds.Dx())*scale)))
			h := max(1, int(math.Round(float64(bounds.Dy())*scale)))
			resized := image.NewRGBA(image.Rect(0, 0, w, h))
			for y := 0; y < h; y++ {
				sy := bounds.Min.Y + y*bounds.Dy()/h
				for x := 0; x < w; x++ {
					sx := bounds.Min.X + x*bounds.Dx()/w
					resized.Set(x, y, img.At(sx, sy))
				}
			}
			img = resized
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func (s *Service) GetMetadata(ctx context.Context, gid int, token string, force bool) (GalleryMetadata, error) {
	key := fmt.Sprintf("%d:%s", gid, token)
	s.mu.Lock()
	if !force {
		if v, ok := lookup(s.metadata, key); ok {
			s.mu.Unlock()
			return v, nil
		}
	}
	s.mu.Unlock()
	m, e := s.Client.GData(ctx, gid, token)
	if e != nil {
		return m, e
	}
	s.mu.Lock()
	put(s.metadata, key, m, time.Duration(s.Settings.MetadataTTLSeconds)*time.Second)
	s.mu.Unlock()
	return m, nil
}
func (s *Service) FavoriteCategories(ctx context.Context) (map[int]string, error) {
	s.mu.Lock()
	if v, ok := lookup(s.categories, "favorites"); ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	b, _, e := s.Client.Get(ctx, s.Client.BaseURL()+"/favorites.php", KindHTML)
	if e != nil {
		return nil, e
	}
	result := ParseFavoritesCategories(string(b))
	s.mu.Lock()
	put(s.categories, "favorites", result, 10*time.Minute)
	s.mu.Unlock()
	return result, nil
}
func (s *Service) FavoriteAction(ctx context.Context, action string, items []FavoriteItem, favcat *int, note string) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result := map[string]any{"gid": item.GID, "token": item.Token, "ok": false}
		apply := "Apply Changes"
		if action == "add" {
			apply = "Add to Favorites"
		}
		form := url.Values{"favcat": {}, "favnote": {note}, "apply": {apply}, "update": {"1"}}
		if action == "remove" {
			form.Set("favcat", "favdel")
		} else if favcat != nil {
			form.Set("favcat", strconv.Itoa(*favcat))
		}
		target := fmt.Sprintf("%s/gallerypopups.php?gid=%d&t=%s&act=addfav", s.Client.BaseURL(), item.GID, url.QueryEscape(item.Token))
		_, _, e := s.Client.PostForm(ctx, target, form, KindHTML)
		if e == nil {
			result["ok"] = true
			s.InvalidateFavorites()
		} else {
			result["error"] = e.Error()
		}
		out = append(out, result)
	}
	return out
}

type FavoriteItem struct {
	GID   int    `json:"gid"`
	Token string `json:"token"`
}

func (s *Service) InvalidateFavorites() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.lists {
		if strings.HasPrefix(k, "favorites?") {
			delete(s.lists, k)
		}
	}
}
func (s *Service) RateGallery(ctx context.Context, gid int, token string, score float64) (map[string]any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		page, _, err := s.Client.Get(ctx, s.Client.GalleryURL(gid, token), KindHTML)
		if err != nil {
			return nil, err
		}
		key := regexp.MustCompile(`\bvar\s+apikey\s*=\s*["']([\w-]+)["']`).FindSubmatch(page)
		if len(key) < 2 {
			return nil, fmt.Errorf("gallery detail page has no rating apikey")
		}
		uid, _ := strconv.Atoi(s.Settings.IPBMemberID)
		payload := map[string]any{"method": "rategallery", "apikey": string(key[1]), "apiuid": uid, "gid": gid, "token": token, "rating": int(score * 2)}
		body, _, err := s.Client.PostJSON(ctx, s.Client.APIURL(), payload, KindAPI)
		if err != nil {
			return nil, err
		}
		var result map[string]any
		if json.Unmarshal(body, &result) != nil {
			result = map[string]any{}
		}
		if result["rating_usr"] != nil && result["rating_avg"] != nil && result["rating_cnt"] != nil {
			usr := toFloat(result["rating_usr"])
			avg := toFloat(result["rating_avg"])
			cnt := toInt(result["rating_cnt"])
			return map[string]any{"rating_usr": usr, "rating_avg": avg, "rating_cnt": cnt}, nil
		}
		if attempt == 0 {
			continue
		}
		if e, ok := result["error"].(string); ok && e != "" {
			return nil, fmt.Errorf("%s", e)
		}
		return nil, fmt.Errorf("rating response is missing expected fields")
	}
	return nil, fmt.Errorf("rating response is missing expected fields")
}
func (s *Service) ArchivedGalleries(page int) []GalleryListItem {
	if s.archive == nil {
		return nil
	}
	items := []GalleryListItem{}
	for _, a := range s.archive.ListReady(page, 100) {
		snap, ok := s.archive.GetMetadataSnapshot(a.GID, a.Token)
		title := a.Title
		category := ""
		rating := float64(0)
		tags := []GalleryTag{}
		if ok {
			jpn := toString(snap["title_jpn"])
			if jpn != "" {
				title = jpn
			} else if t := toString(snap["title"]); t != "" {
				title = t
			}
			category = toString(snap["category"])
			rating = toFloat(snap["rating"])
			if raw, ok := snap["tags"].(map[string]any); ok {
				for ns, v := range raw {
					if list, ok := v.([]any); ok {
						for _, entry := range list {
							if m, ok := entry.(map[string]any); ok {
								t := GalleryTag{Namespace: toString(m["namespace"]), Key: toString(m["key"]), Status: toString(m["status"])}
								if t.Namespace == "" {
									t.Namespace = ns
								}
								if t.Status == "" {
									t.Status = "confidence"
								}
								tags = append(tags, t)
							}
						}
					}
				}
			}
		}
		count := a.PageCount
		publishTime := ""
		if posted := toInt64(snap["posted"]); ok && posted > 0 {
			publishTime = time.Unix(posted, 0).UTC().Format("2006-01-02 15:04")
		}
		items = append(items, GalleryListItem{GID: a.GID, Token: a.Token, Title: title, Category: category, PageCount: &count, Rating: rating, PublishTime: publishTime, Language: languageFromTags(tags), Tags: tags})
	}
	return items
}
func (s *Service) Status() map[string]any {
	s.mu.Lock()
	lists := len(s.lists)
	covers := len(s.cover)
	pages := len(s.pages)
	s.mu.Unlock()
	return map[string]any{"uptime_seconds": int64(time.Since(s.started).Seconds()), "requests": s.Throttle.Counts(), "circuit_breaker": s.Throttle.Circuit.Snapshot(), "cache": map[string]any{"list_entries": lists, "cover_entries": covers, "page_entries": pages, "image": s.disk.Stats()}}
}
func (s *Service) ClearCache() error {
	s.mu.Lock()
	s.lists = map[string]cached[GalleryPageInfo]{}
	s.cover = map[string]cached[string]{}
	s.pages = map[string]cached[[]Thumbnail]{}
	s.details = map[string]cached[DetailPageInfo]{}
	s.metadata = map[string]cached[GalleryMetadata]{}
	s.categories = map[string]cached[map[int]string]{}
	s.mu.Unlock()
	return s.disk.Clear()
}
func (s *Service) ArchiveSnapshot(ctx context.Context, gid int, token string) ([]byte, error) {
	m, e := s.GetMetadata(ctx, gid, token, true)
	if e != nil {
		return nil, e
	}
	return json.Marshal(m)
}
func (s *Service) ReadAllUpstream(ctx context.Context, target string, kind RequestKind) ([]byte, http.Header, error) {
	return s.Client.Get(ctx, target, kind)
}
func readAndClose(r io.ReadCloser) ([]byte, error) { defer r.Close(); return io.ReadAll(r) }
func toString(v any) string                        { s, _ := v.(string); return s }
func toInt(v any) int {
	return int(toInt64(v))
}
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}
func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}
func toBool(v any) bool { x, _ := v.(bool); return x }
