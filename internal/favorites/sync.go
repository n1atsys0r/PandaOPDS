package favorites

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/archive"
	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

type RunOptions struct {
	FullCatalog bool
}

type Syncer struct {
	Settings  config.Settings
	Service   *eh.Service
	Archive   *archive.Manager
	State     *State
	mu        sync.Mutex
	busy      bool
	lastError string
	cancel    context.CancelFunc
	done      chan struct{}
	trigger   chan struct{}
}

func NewSyncer(cfg config.Settings, service *eh.Service, arch *archive.Manager, state *State) *Syncer {
	return &Syncer{Settings: cfg, Service: service, Archive: arch, State: state, trigger: make(chan struct{}, 1)}
}
func (s *Syncer) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		var ticker *time.Ticker
		var tick <-chan time.Time
		if s.Settings.FavoritesSyncIntervalSeconds > 0 {
			ticker = time.NewTicker(time.Duration(s.Settings.FavoritesSyncIntervalSeconds * float64(time.Second)))
			tick = ticker.C
			defer ticker.Stop()
			_, _ = s.Run(ctx)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick:
				_, _ = s.Run(ctx)
			case <-s.trigger:
				select {
				case <-time.After(3 * time.Second):
				case <-ctx.Done():
					return
				}
				_, _ = s.Run(ctx)
			}
		}
	}()
}
func (s *Syncer) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}
func (s *Syncer) RequestRun() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}
func (s *Syncer) Status() map[string]any {
	s.mu.Lock()
	busy, err := s.busy, s.lastError
	s.mu.Unlock()
	known := s.State.Summary()
	var categories any = s.Settings.FavoritesSyncCategories
	if len(s.Settings.FavoritesSyncCategories) == 0 {
		categories = "all"
	}
	return map[string]any{"enabled": s.Settings.FavoritesSyncIntervalSeconds > 0, "interval_seconds": s.Settings.FavoritesSyncIntervalSeconds, "auto_archive": s.Settings.FavoritesSyncArchive, "categories": categories, "excluded_categories": s.Settings.FavoritesSyncExcludes, "running": busy, "last_error": nilOrString(err), "known_count": known["known_count"], "archived_count": known["archived_count"], "error_count": known["error_count"], "baseline": known["baseline"], "last_run": known["last_run"], "last_ok": known["last_ok"], "scanned_pages": known["scanned_pages"]}
}
func nilOrString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Syncer) Run(ctx context.Context) (map[string]any, error) {
	return s.RunWithOptions(ctx, RunOptions{})
}

func (s *Syncer) RunWithOptions(ctx context.Context, opts RunOptions) (map[string]any, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return nil, fmt.Errorf("favorites sync already in progress")
	}
	s.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	if !s.Settings.HasIPB() {
		return map[string]any{"ok": false, "skipped": true, "error": "favorites require IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)", "status": s.Status()}, nil
	}
	known := s.State.Known()
	archived := s.State.Archived()
	errs := s.State.Errors()
	favcats := s.State.Favcats()
	baseline := s.State.Baseline()
	catalogBackfilled := s.State.CatalogBackfilled()

	needBackfill := false
	if s.Archive != nil && baseline && !catalogBackfilled {
		if len(known) == 0 {
			catalogBackfilled = true
			_ = s.State.MarkCatalogBackfilled(true)
		} else {
			for k := range known {
				gid, token, ok := ParseKey(k)
				if ok && s.Archive.Store.Get(gid, token) == nil {
					needBackfill = true
					break
				}
			}
			if !needBackfill {
				catalogBackfilled = true
				_ = s.State.MarkCatalogBackfilled(true)
			}
		}
	}
	fullScan := opts.FullCatalog || !baseline || needBackfill

	newItems := []eh.GalleryListItem{}
	scannedItems := []eh.GalleryListItem{}
	seenInScan := map[string]bool{}
	discovered := map[string]bool{}
	consecutiveKnown := 0
	pages := 0
	cursor := ""
	favcatNames := map[int]string{}
	reachedEnd := false
scanPages:
	for pages < s.Settings.FavoritesSyncMaxPages {
		page, e := s.Service.FavoritesSyncPage(ctx, cursor)
		if e != nil {
			s.mu.Lock()
			s.lastError = e.Error()
			s.mu.Unlock()
			ok := false
			_ = s.State.Update(nil, nil, nil, nil, nil, nil, &ok, &pages)
			return nil, e
		}
		pages++
		for id, name := range page.FavcatMap {
			favcatNames[id] = name
		}
		if len(page.Galleries) == 0 {
			reachedEnd = true
			break
		}
		for _, item := range page.Galleries {
			k := fmt.Sprintf("%d:%s", item.GID, item.Token)
			if item.Favcat != nil {
				favcats[k] = *item.Favcat
			}
			if !seenInScan[k] {
				seenInScan[k] = true
				scannedItems = append(scannedItems, item)
			}
			if known[k] {
				consecutiveKnown++
				if !fullScan && consecutiveKnown >= s.Settings.FavoritesSyncMatchThreshold {
					break scanPages
				}
				continue
			}
			consecutiveKnown = 0
			if !discovered[k] {
				discovered[k] = true
				newItems = append(newItems, item)
			}
		}
		if page.NextGID == "" {
			reachedEnd = true
			break
		}
		cursor = page.NextGID
	}

	catalogedCount := 0
	if s.Archive != nil {
		baseCreated := float64(time.Now().Unix())
		for idx, item := range scannedItems {
			createdAt := baseCreated - float64(idx)*0.001
			payload := galleryPayloadFromListItem(item)
			created, err := s.Archive.Store.Catalog(item.GID, item.Token, map[string]any{"title": item.Title, "created_at": createdAt}, payload)
			if err == nil && created {
				catalogedCount++
			}
		}
	}

	if fullScan && reachedEnd && s.Archive != nil {
		for k := range known {
			if seenInScan[k] {
				continue
			}
			gid, token, ok := ParseKey(k)
			if !ok {
				continue
			}
			if meta := s.Archive.Store.Get(gid, token); meta != nil && fmt.Sprint(meta["status"]) == "unarchived" {
				if s.Archive.Remove(gid, token) {
					delete(known, k)
					delete(archived, k)
					delete(errs, k)
					delete(favcats, k)
				}
			}
		}
	}

	for key := range discovered {
		known[key] = true
	}
	isBaseline := !baseline
	newArchived := map[string]bool{}
	for k, v := range archived {
		newArchived[k] = v
	}
	if !isBaseline && s.Settings.FavoritesSyncArchive && s.Archive != nil {
		for _, item := range newItems {
			if !s.inScope(item.Favcat) {
				continue
			}
			k := fmt.Sprintf("%d:%s", item.GID, item.Token)
			if archived[k] {
				continue
			}
			if meta := s.Archive.Store.Get(item.GID, item.Token); meta != nil {
				st := fmt.Sprint(meta["status"])
				if st == "ready" {
					if strings.EqualFold(fmt.Sprint(meta["or"]), "res") && canonicalTier(s.Settings.ArchiveQuality) == "org" {
						_, e := s.Archive.Start(ctx, item.GID, item.Token, "org", true)
						if e != nil {
							errs[k] = e.Error()
						} else {
							newArchived[k] = true
						}
					}
					continue
				}
				if st != "unarchived" {
					continue
				}
			}
			if _, e := s.Archive.Start(ctx, item.GID, item.Token, s.Settings.ArchiveQuality, false); e != nil {
				errs[k] = e.Error()
			} else {
				newArchived[k] = true
			}
		}
	}
	ok := true
	var basePtr *bool
	if isBaseline {
		basePtr = &ok
	}
	_ = s.State.Update(known, newArchived, errs, favcats, favcatNames, basePtr, &ok, &pages)
	if s.Archive != nil && (isBaseline || needBackfill) {
		_ = s.State.MarkCatalogBackfilled(true)
	}
	s.mu.Lock()
	s.lastError = ""
	s.mu.Unlock()
	return map[string]any{"ok": true, "baseline": isBaseline, "new_count": len(newItems), "cataloged_count": catalogedCount, "archived_count": len(newArchived), "scanned_pages": pages, "status": s.Status()}, nil
}

func galleryPayloadFromListItem(item eh.GalleryListItem) map[string]any {
	tagsByNS := map[string][]eh.GalleryTag{}
	for _, t := range item.Tags {
		ns := t.Namespace
		if ns == "" {
			ns = "temp"
		}
		tagsByNS[ns] = append(tagsByNS[ns], t)
	}
	filecount := 0
	if item.PageCount != nil {
		filecount = *item.PageCount
	}
	var posted int64
	if item.PublishTime != "" {
		for _, layout := range []string{"2006-01-02 15:04", time.RFC3339} {
			if t, err := time.ParseInLocation(layout, item.PublishTime, time.UTC); err == nil {
				posted = t.Unix()
				break
			}
		}
	}
	return map[string]any{
		"gid":            item.GID,
		"token":          item.Token,
		"title":          item.Title,
		"title_jpn":      "",
		"category":       item.Category,
		"thumb":          item.CoverURL,
		"rating":         item.Rating,
		"tags":           tagsByNS,
		"filecount":      filecount,
		"filesize":       int64(0),
		"posted":         posted,
		"uploader":       item.Uploader,
		"torrentcount":   0,
		"expunged":       item.Expunged,
		"catalog_source": "favorites_list",
		"saved_at":       float64(time.Now().Unix()),
	}
}

func (s *Syncer) inScope(cat *int) bool {
	if cat == nil {
		return true
	}
	allow := len(s.Settings.FavoritesSyncCategories) == 0
	for _, id := range s.Settings.FavoritesSyncCategories {
		if *cat == id {
			allow = true
		}
	}
	for _, id := range s.Settings.FavoritesSyncExcludes {
		if *cat == id {
			return false
		}
	}
	return allow
}
func canonicalTier(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "original", "org":
		return "org"
	case "resample", "resized", "res":
		return "res"
	}
	return strings.ToLower(t)
}
func (s *Syncer) ParsedFavcat(key string) *int {
	v, ok := s.State.Favcats()[key]
	if !ok {
		return nil
	}
	return &v
}
func ParseKey(key string) (int, string, bool) {
	a, b, ok := strings.Cut(key, ":")
	if !ok {
		return 0, "", false
	}
	id, e := strconv.Atoi(a)
	return id, b, e == nil
}
