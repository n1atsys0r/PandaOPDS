package subscription

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"pandaopds/internal/eh"
)

type Config struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Source           string         `json:"source"`
	Query            map[string]any `json:"query"`
	IntervalSeconds  int            `json:"interval_seconds"`
	Enabled          bool           `json:"enabled"`
	AutoDownload     bool           `json:"auto_download"`
	DownloadRules    map[string]any `json:"download_rules"`
	Revision         int            `json:"revision"`
	BaselineComplete bool           `json:"baseline_complete"`
	LastRunAt        *time.Time     `json:"last_run_at"`
	LastSuccessAt    *time.Time     `json:"last_success_at"`
	LastError        string         `json:"last_error,omitempty"`
	Running          bool           `json:"running"`
}
type Store struct{ db *sql.DB }

var ErrStaleRevision = errors.New("subscription query revision changed during run")

func New(db *sql.DB) *Store  { return &Store{db: db} }
func (s *Store) DB() *sql.DB { return s.db }
func IsFavoritesPreset(preset string) bool {
	if preset == "favorites" {
		return true
	}
	if !strings.HasPrefix(preset, "favorites:") {
		return false
	}
	cat := strings.TrimPrefix(preset, "favorites:")
	return len(cat) == 1 && cat[0] >= '0' && cat[0] <= '9'
}
func IsFavoritesSubscription(c Config) bool {
	return c.Source == "preset" && IsFavoritesPreset(stringVal(c.Query["preset"]))
}
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func marshal(v any) string       { b, _ := json.Marshal(v); return string(b) }
func decode(raw string, out any) { _ = json.Unmarshal([]byte(raw), out) }
func (s *Store) Create(ctx context.Context, c Config) (Config, error) {
	if c.Source != "search" && c.Source != "preset" {
		return c, errors.New("source must be search or preset")
	}
	if c.Name == "" {
		return c, errors.New("name is required")
	}
	if c.IntervalSeconds < 0 {
		c.IntervalSeconds = 0
	}
	if c.Revision < 1 {
		c.Revision = 1
	}
	if c.Query == nil {
		c.Query = map[string]any{}
	}
	if c.DownloadRules == nil {
		c.DownloadRules = map[string]any{}
	}
	if c.Source == "preset" && c.Query["preset"] == nil {
		c.Query["preset"] = "latest"
	}
	id, e := randomID()
	if e != nil {
		return c, e
	}
	c.ID = id
	e = s.insertConfig(ctx, c)
	return c, e
}
func (s *Store) insertConfig(ctx context.Context, c Config) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, e := s.db.ExecContext(ctx, `INSERT INTO subscriptions(id,name,source,query_json,interval_seconds,enabled,auto_download,download_rules_json,revision,baseline_complete,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,0,?,?)`, c.ID, c.Name, c.Source, marshal(c.Query), c.IntervalSeconds, boolInt(c.Enabled), boolInt(c.AutoDownload), marshal(c.DownloadRules), c.Revision, now, now)
	return e
}
func (s *Store) Get(ctx context.Context, id string) (Config, error) {
	var c Config
	var q, r string
	var enabled, auto, base int
	var lastRun, lastOK sql.NullString
	var lastErr sql.NullString
	var running int
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.name,s.source,s.query_json,s.interval_seconds,s.enabled,s.auto_download,s.download_rules_json,s.revision,s.baseline_complete,(SELECT started_at FROM subscription_runs WHERE subscription_id=s.id ORDER BY started_at DESC LIMIT 1),(SELECT finished_at FROM subscription_runs WHERE subscription_id=s.id AND status='complete' ORDER BY finished_at DESC LIMIT 1),(SELECT error FROM subscription_runs WHERE subscription_id=s.id ORDER BY started_at DESC LIMIT 1),EXISTS(SELECT 1 FROM subscription_runs WHERE subscription_id=s.id AND status='running') FROM subscriptions s WHERE s.id=?`, id).Scan(&c.ID, &c.Name, &c.Source, &q, &c.IntervalSeconds, &enabled, &auto, &r, &c.Revision, &base, &lastRun, &lastOK, &lastErr, &running)
	if err != nil {
		return c, err
	}
	decode(q, &c.Query)
	decode(r, &c.DownloadRules)
	c.Enabled = enabled != 0
	c.AutoDownload = auto != 0
	c.BaselineComplete = base != 0
	c.Running = running != 0
	if lastRun.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastRun.String)
		c.LastRunAt = &t
	}
	if lastOK.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastOK.String)
		c.LastSuccessAt = &t
	}
	if lastErr.Valid {
		c.LastError = lastErr.String
	}
	return c, nil
}
func (s *Store) List(ctx context.Context) ([]Config, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM subscriptions ORDER BY created_at,id`)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := make([]Config, 0, len(ids))
	for _, id := range ids {
		c, e := s.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, nil
}

// MigrateLegacyFavoriteSubscriptions turns the old auto-created favorites
// subscription into an ordinary rolling preset, preserving its cached history.
// Multi-folder tracking scopes become the all-favorites preset because folder
// subscriptions are represented by the individual favorites:N presets.
func (s *Store) MigrateLegacyFavoriteSubscriptions(ctx context.Context) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	type legacy struct {
		id, queryJSON, rulesJSON string
		autoDownload             int
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,query_json,download_rules_json,auto_download FROM subscriptions WHERE source='favorites' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	items := []legacy{}
	for rows.Next() {
		var item legacy
		if err := rows.Scan(&item.id, &item.queryJSON, &item.rulesJSON, &item.autoDownload); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	disabledArchive := []string{}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items {
		query := map[string]any{}
		rules := map[string]any{}
		decode(item.queryJSON, &query)
		decode(item.rulesJSON, &rules)
		preset := "favorites"
		if ids, ok := intSlice(query["favorite_categories"]); ok && len(ids) == 1 && ids[0] >= 0 && ids[0] <= 9 {
			preset = fmt.Sprintf("favorites:%d", ids[0])
		}
		delete(query, "favorite_categories")
		query["preset"] = preset
		query["max_pages"] = 1
		hasFolderArchiveRules := hasAny(rules["categories"]) || hasAny(rules["excluded_categories"])
		delete(rules, "categories")
		delete(rules, "excluded_categories")
		autoDownload := item.autoDownload
		if hasFolderArchiveRules {
			autoDownload = 0
			disabledArchive = append(disabledArchive, item.id)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET source='preset',query_json=?,download_rules_json=?,auto_download=?,revision=revision+1,updated_at=? WHERE id=? AND source='favorites'`, marshal(query), marshal(rules), autoDownload, now, item.id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscription_items SET pending_auto=0 WHERE subscription_id=?`, item.id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return disabledArchive, nil
}

// FavoritePresetMemberships returns last-seen membership data from user-owned
// favorites presets. It is a display cache only; it is not reconciled against
// the full remote favorites collection.
func (s *Store) FavoritePresetMemberships(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.gid,p.token,si.favcat FROM subscription_items si JOIN publications p ON p.id=si.publication_id JOIN subscriptions s ON s.id=si.subscription_id WHERE s.source='preset' AND (json_extract(s.query_json,'$.preset')='favorites' OR json_extract(s.query_json,'$.preset') GLOB 'favorites:[0-9]') AND si.remote_member=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var gid int
		var token string
		var cat sql.NullInt64
		if err := rows.Scan(&gid, &token, &cat); err != nil {
			return nil, err
		}
		value := -1
		if cat.Valid {
			value = int(cat.Int64)
		}
		out[fmt.Sprintf("%d:%s", gid, token)] = value
	}
	return out, rows.Err()
}
func (s *Store) Update(ctx context.Context, id string, c Config) (Config, error) {
	old, e := s.Get(ctx, id)
	if e != nil {
		return c, e
	}
	if c.Name == "" {
		c.Name = old.Name
	}
	if c.Source == "" {
		c.Source = old.Source
	}
	if c.Query == nil {
		c.Query = old.Query
	}
	if c.DownloadRules == nil {
		c.DownloadRules = old.DownloadRules
	}
	changed := marshal(c.Query) != marshal(old.Query) || c.Source != old.Source
	if changed {
		c.Revision = old.Revision + 1
		c.BaselineComplete = false
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return c, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `UPDATE subscriptions SET name=?,source=?,query_json=?,interval_seconds=?,enabled=?,auto_download=?,download_rules_json=?,revision=?,baseline_complete=CASE WHEN ? THEN 0 ELSE baseline_complete END,updated_at=? WHERE id=?`, c.Name, c.Source, marshal(c.Query), c.IntervalSeconds, boolInt(c.Enabled), boolInt(c.AutoDownload), marshal(c.DownloadRules), func() int {
		if changed {
			return old.Revision + 1
		}
		return old.Revision
	}(), boolInt(changed), now, id)
	if e != nil {
		return c, e
	}
	if changed {
		_, e = tx.ExecContext(ctx, `UPDATE subscription_items SET pending_auto=0 WHERE subscription_id=?`, id)
		if e != nil {
			return c, e
		}
	}
	if e = tx.Commit(); e != nil {
		return c, e
	}
	return s.Get(ctx, id)
}
func (s *Store) Delete(ctx context.Context, id string) error {
	res, e := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=?`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type ItemQuery struct {
	SubscriptionIDs             []string
	CategoryNamesBySubscription map[string][]string
	View, Q, Category           string
	Page, PerPage               int
}

func placeholders(n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = "?"
	}
	return strings.Join(p, ",")
}
func (s *Store) ListItems(ctx context.Context, q ItemQuery) ([]map[string]any, int, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 {
		q.PerPage = 50
	}
	if q.PerPage > 200 {
		q.PerPage = 200
	}
	if len(q.SubscriptionIDs) == 0 {
		return []map[string]any{}, 0, nil
	}
	args := []any{}
	base := ` FROM subscription_items si JOIN publications p ON p.id=si.publication_id WHERE si.purged=0`
	if len(q.CategoryNamesBySubscription) == 0 {
		base += ` AND si.subscription_id IN (` + placeholders(len(q.SubscriptionIDs)) + `)`
		for _, id := range q.SubscriptionIDs {
			args = append(args, id)
		}
	} else {
		memberships := make([]string, 0, len(q.SubscriptionIDs))
		for _, id := range q.SubscriptionIDs {
			names, filtered := q.CategoryNamesBySubscription[id]
			if !filtered {
				memberships = append(memberships, `si.subscription_id=?`)
				args = append(args, id)
				continue
			}
			if len(names) == 0 {
				memberships = append(memberships, `(si.subscription_id=? AND 1=0)`)
				args = append(args, id)
				continue
			}
			memberships = append(memberships, ` (si.subscription_id=? AND lower(p.category) IN (`+placeholders(len(names))+`))`)
			args = append(args, id)
			for _, name := range names {
				args = append(args, strings.ToLower(name))
			}
		}
		base += ` AND (` + strings.Join(memberships, ` OR `) + `)`
	}
	// Legacy favorites known-key imports contain only identity and baseline
	// membership. Keep those rows for deduplication and remote reconciliation,
	// but don't expose them as blank publication cards until a fetch supplies
	// usable display metadata.
	base += ` AND (NULLIF(trim(COALESCE(p.title,'')),'') IS NOT NULL OR NULLIF(trim(COALESCE(p.title_jpn,'')),'') IS NOT NULL OR NULLIF(trim(COALESCE(json_extract(p.metadata_json,'$.title'),'')),'') IS NOT NULL OR NULLIF(trim(COALESCE(json_extract(p.metadata_json,'$.title_jpn'),'')),'') IS NOT NULL)`
	if q.Q != "" {
		base += ` AND (lower(COALESCE(NULLIF(p.title,''),json_extract(p.metadata_json,'$.title'),'')) LIKE ? OR lower(COALESCE(NULLIF(p.title_jpn,''),json_extract(p.metadata_json,'$.title_jpn'),'')) LIKE ? OR lower(COALESCE(p.uploader,'')) LIKE ?)`
		v := "%" + strings.ToLower(q.Q) + "%"
		args = append(args, v, v, v)
	}
	if q.Category != "" {
		base += ` AND lower(p.category)=lower(?)`
		args = append(args, q.Category)
	}
	having := ""
	switch q.View {
	case "unread":
		having = ` HAVING SUM(CASE WHEN si.hidden=0 AND si.inspected=1 THEN 1 ELSE 0 END)<SUM(CASE WHEN si.hidden=0 THEN 1 ELSE 0 END)`
	case "hidden":
		having = ` HAVING SUM(si.hidden)>0`
	case "all":
	}
	group := ` GROUP BY p.id` + having
	var total int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT p.id`+base+group+`)`, args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	query := `SELECT p.id,p.gid,p.token,p.metadata_json,p.title,p.title_jpn,p.cover_url,GROUP_CONCAT(DISTINCT si.subscription_id),SUM(CASE WHEN si.inspected=1 THEN 1 ELSE 0 END),SUM(CASE WHEN si.hidden=1 THEN 1 ELSE 0 END),COUNT(*),MIN(si.first_seen_at),MAX(si.last_seen_at),(SELECT GROUP_CONCAT(d.id) FROM documents d WHERE d.publication_id=p.id AND d.status='available'),EXISTS(SELECT 1 FROM documents d WHERE d.publication_id=p.id AND d.status='available'),(SELECT j.status FROM download_jobs j WHERE j.provider=p.provider AND j.gid=p.gid AND j.token=p.token ORDER BY j.created_at DESC LIMIT 1),EXISTS(SELECT 1 FROM covers c WHERE c.publication_id=p.id)` + base + group + ` ORDER BY MIN(si.first_seen_at) DESC,p.id DESC LIMIT ? OFFSET ?`
	params := append(append([]any{}, args...), q.PerPage, (q.Page-1)*q.PerPage)
	rows, e := s.db.QueryContext(ctx, query, params...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var pubID, gid int
		var token, raw, title, titleJPN, coverURL, members, first, last string
		var inspected, hidden, count int
		var docs sql.NullString
		var archived int
		var download sql.NullString
		var cachedCover int
		if e = rows.Scan(&pubID, &gid, &token, &raw, &title, &titleJPN, &coverURL, &members, &inspected, &hidden, &count, &first, &last, &docs, &archived, &download, &cachedCover); e != nil {
			return nil, 0, e
		}
		item := map[string]any{"publication_id": pubID, "gid": gid, "token": token, "inspected": inspected == count, "hidden": hidden == count, "first_seen_at": first, "last_seen_at": last, "archived": archived != 0, "document_ids": []string{}, "download_status": download.String}
		var meta map[string]any
		decode(raw, &meta)
		for _, k := range []string{"title", "title_jpn", "category", "cover_url", "page_count", "rating", "publish_time", "language", "tags"} {
			if v, ok := meta[k]; ok {
				item[k] = v
			}
		}
		if stringVal(item["title"]) == "" {
			item["title"] = title
		}
		if stringVal(item["title_jpn"]) == "" {
			item["title_jpn"] = titleJPN
		}
		// Always route cover requests through the local endpoint so it can fetch
		// and persist an upstream thumbnail on first use. Returning the raw URL
		// only worked in detail views that separately load the full metadata.
		if cachedCover != 0 || coverURL != "" || stringVal(meta["cover_url"]) != "" || stringVal(meta["thumb"]) != "" {
			item["cover_url"] = fmt.Sprintf("/api/publications/eh/%d/%s/cover", gid, url.PathEscape(token))
		}
		if docs.Valid && docs.String != "" {
			item["document_ids"] = strings.Split(docs.String, ",")
		}
		ids := strings.Split(members, ",")
		item["membership_ids"] = ids
		item["subscription_ids"] = ids
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (s *Store) AddPage(ctx context.Context, id, runID string, revision, pageNumber int, cursor string, baseline, auto bool, items []eh.GalleryListItem) ([]int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var currentRevision int
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, id).Scan(&currentRevision); e != nil {
		return nil, e
	}
	if currentRevision != revision {
		return nil, ErrStaleRevision
	}
	newIDs := []int64{}
	for _, it := range items {
		raw := marshal(it)
		var old string
		var metadataLevel int
		err := tx.QueryRowContext(ctx, `SELECT metadata_json,metadata_level FROM publications WHERE provider='eh' AND gid=? AND token=?`, it.GID, strings.ToLower(it.Token)).Scan(&old, &metadataLevel)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		var previous map[string]any
		if err == sql.ErrNoRows {
			previous = map[string]any{}
		} else {
			decode(old, &previous)
		}
		var next map[string]any
		decode(raw, &next)
		if metadataLevel < 2 {
			mergePublicationMetadata(previous, next)
		}
		raw = marshal(previous)
		cols := publicationColumns(previous)
		if err == sql.ErrNoRows {
			_, err = tx.ExecContext(ctx, `INSERT INTO publications(provider,gid,token,metadata_json,title,title_jpn,category,uploader,cover_url,page_count,rating,publish_time,language,tags_json,metadata_level,created_at,updated_at) VALUES('eh',?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)`, it.GID, strings.ToLower(it.Token), raw, cols.title, cols.titleJPN, cols.category, cols.uploader, cols.coverURL, cols.pageCount, cols.rating, cols.publishTime, cols.language, cols.tagsJSON, now, now)
		} else {
			if metadataLevel >= 2 {
				_, err = tx.ExecContext(ctx, `UPDATE publications SET updated_at=? WHERE provider='eh' AND gid=? AND token=?`, now, it.GID, strings.ToLower(it.Token))
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE publications SET metadata_json=?,title=?,title_jpn=?,category=?,uploader=?,cover_url=?,page_count=?,rating=?,publish_time=?,language=?,tags_json=?,metadata_level=MAX(metadata_level,1),updated_at=? WHERE provider='eh' AND gid=? AND token=?`, raw, cols.title, cols.titleJPN, cols.category, cols.uploader, cols.coverURL, cols.pageCount, cols.rating, cols.publishTime, cols.language, cols.tagsJSON, now, it.GID, strings.ToLower(it.Token))
			}
		}
		if err != nil {
			return nil, err
		}
		var pubID int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, it.GID, strings.ToLower(it.Token)).Scan(&pubID); err != nil {
			return nil, err
		}
		var favcat any
		if it.Favcat != nil {
			favcat = *it.Favcat
		}
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_items(subscription_id,publication_id,first_seen_at,last_seen_at,last_seen_run,inspected,hidden,remote_member,first_seen_revision,baseline_item,pending_auto,favcat) VALUES(?,?,?,?,?,0,0,1,?,?,?,?)`, id, pubID, now, now, runID, revision, boolInt(!baseline), 0, favcat)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			newIDs = append(newIDs, pubID)
			if baseline && auto {
				if _, err = tx.ExecContext(ctx, `UPDATE subscription_items SET pending_auto=1 WHERE subscription_id=? AND publication_id=?`, id, pubID); err != nil {
					return nil, err
				}
			}
		} else {
			if _, err = tx.ExecContext(ctx, `UPDATE subscription_items SET last_seen_at=?,last_seen_run=?,remote_member=1,favcat=COALESCE(?,favcat) WHERE subscription_id=? AND publication_id=?`, now, runID, favcat, id, pubID); err != nil {
				return nil, err
			}
		}
	}
	res, e := tx.ExecContext(ctx, `UPDATE subscription_runs SET pages=?,results=results+?,cursor=? WHERE id=? AND revision=? AND status='running'`, pageNumber, len(items), cursor, runID, revision)
	if e != nil {
		return nil, e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, ErrStaleRevision
	}
	return newIDs, tx.Commit()
}

type pubColumns struct {
	title, titleJPN, category, uploader, coverURL, publishTime, language, tagsJSON string
	pageCount                                                                      int
	rating                                                                         float64
}

func mergePublicationMetadata(dst, src map[string]any) {
	for k, v := range src {
		if k == "tags" {
			// List responses have a flat []GalleryTag; detail responses carry the
			// richer namespace map. Keep that complete form when it already exists.
			if _, oldIsMap := dst[k].(map[string]any); oldIsMap {
				if _, newIsList := v.([]any); newIsList {
					continue
				}
			}
		}
		empty := v == nil || v == "" || v == float64(0)
		if !empty || dst[k] == nil {
			dst[k] = v
		}
	}
}

func publicationColumns(m map[string]any) pubColumns {
	c := pubColumns{tagsJSON: "[]"}
	c.title = stringVal(m["title"])
	c.titleJPN = stringVal(m["title_jpn"])
	c.category = stringVal(m["category"])
	c.uploader = stringVal(m["uploader"])
	c.coverURL = stringVal(m["cover_url"])
	if c.coverURL == "" {
		c.coverURL = stringVal(m["thumb"])
	}
	c.pageCount = intNumber(m["page_count"])
	if c.pageCount == 0 {
		c.pageCount = intNumber(m["filecount"])
	}
	switch v := m["rating"].(type) {
	case float64:
		c.rating = v
	case int:
		c.rating = float64(v)
	}
	c.publishTime = stringVal(m["publish_time"])
	if c.publishTime == "" {
		if posted := intNumber(m["posted"]); posted > 0 {
			c.publishTime = time.Unix(int64(posted), 0).UTC().Format(time.RFC3339)
		}
	}
	c.language = stringVal(m["language"])
	if tags, ok := m["tags"]; ok && tags != nil {
		c.tagsJSON = marshal(tags)
	}
	return c
}

func (s *Store) MarkItems(ctx context.Context, ids []string, gid int, token string, inspected, hidden *bool) error {
	if len(ids) == 0 {
		return nil
	}
	var pubID int64
	if e := s.db.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider=? AND gid=? AND token=?`, "eh", gid, strings.ToLower(token)).Scan(&pubID); e != nil {
		return e
	}
	args := []any{}
	sets := []string{}
	if inspected != nil {
		sets = append(sets, "inspected=?")
		args = append(args, boolInt(*inspected))
	}
	if hidden != nil {
		sets = append(sets, "hidden=?")
		args = append(args, boolInt(*hidden))
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "last_seen_at=last_seen_at")
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, pubID)
	_, e := s.db.ExecContext(ctx, `UPDATE subscription_items SET `+strings.Join(sets, ",")+` WHERE subscription_id IN (`+placeholders(len(ids))+`) AND publication_id=?`, args...)
	return e
}

// CleanupItems removes selected entries from subscription views while keeping
// the membership tombstone and publication identity for future deduplication.
func (s *Store) CleanupItems(ctx context.Context, id, mode string) (int64, error) {
	var predicate string
	switch mode {
	case "inspected":
		predicate = "inspected=1"
	case "hidden":
		predicate = "hidden=1"
	default:
		return 0, errors.New("cleanup mode must be inspected or hidden")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE subscription_items SET purged=1,pending_auto=0 WHERE subscription_id=? AND purged=0 AND `+predicate, id)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func QueryFromConfig(c Config) url.Values {
	v := url.Values{}
	// Store the upstream category bitmask at subscription creation. Query
	// categories are otherwise presentation names and must not be guessed here.
	// Keywords are passed as Search's query argument, where advanced tokens are
	// extracted once; duplicating f_search here breaks operator/quoted searches.
	if mask := intNumber(c.Query["category_mask"]); mask > 0 {
		v.Set("f_cats", fmt.Sprint(mask))
	}
	return v
}
func stringVal(v any) string { s, _ := v.(string); return s }

func (s *Store) LatestRun(ctx context.Context, id string) map[string]any {
	var runID, status, started string
	var finished, errText sql.NullString
	var pages, results, complete int
	var coverage string
	err := s.db.QueryRowContext(ctx, `SELECT id,status,started_at,finished_at,pages,results,error,complete,coverage FROM subscription_runs WHERE subscription_id=? ORDER BY started_at DESC LIMIT 1`, id).Scan(&runID, &status, &started, &finished, &pages, &results, &errText, &complete, &coverage)
	if err != nil {
		return nil
	}
	out := map[string]any{"id": runID, "status": status, "started_at": started, "pages": pages, "results": results, "complete": complete != 0, "coverage": coverage}
	if finished.Valid {
		out["finished_at"] = finished.String
	}
	if errText.Valid {
		out["error"] = errText.String
	}
	return out
}
