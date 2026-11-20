package document

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
	"pandaopds/internal/storage"
)

const (
	maxArchiveBytes = int64(32 << 30)
	maxImageBytes   = uint64(64 << 20)
	maxTotalImages  = uint64(8 << 30)
	maxPages        = 50000
	maxZipEntries   = 100000
)

var ErrNotFound = errors.New("document not found")
var ErrInvalidArchive = errors.New("archive contains no supported readable image pages")
var ErrAssociationConflict = errors.New("document EH association conflicts with trusted download identity")
var ehGalleryURL = regexp.MustCompile(`(?i)https?://(?:www\.)?(?:e-hentai\.org|exhentai\.org)/g/([0-9]+)/([0-9a-f]+)`)

type Store struct {
	root           string
	db             *sql.DB
	mu             sync.Mutex
	beforeDelete   func(Document) error
	rollbackDelete func(Document) error
}

type Document struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Path              string     `json:"path"`
	Format            string     `json:"format"`
	Status            string     `json:"status"`
	Size              int64      `json:"size"`
	MTimeNS           int64      `json:"mtime_ns"`
	SHA256            string     `json:"-"`
	PageCount         int        `json:"page_count"`
	GID               *int       `json:"gid,omitempty"`
	Token             string     `json:"token,omitempty"`
	AssociationSource string     `json:"association_source,omitempty"`
	ProgressionTarget string     `json:"progression_target"`
	ProgressionGID    int        `json:"progression_gid,omitempty"`
	ProgressionToken  string     `json:"progression_token,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	MissingAt         *time.Time `json:"missing_at,omitempty"`
	Pages             []PageInfo `json:"-"`
	ClaimGID          int        `json:"-"`
	ClaimToken        string     `json:"-"`
	ClaimEvidence     string     `json:"-"`
}

type PageInfo struct {
	Index        int
	CentralIndex int
	EntryName    string
	MIME         string
	Size         uint64
}

func New(db *storage.DB, root string) (*Store, error) {
	if db == nil || db.DB == nil {
		return nil, errors.New("document store requires SQLite")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db.DB, root: filepath.Clean(abs)}
	if _, err = s.db.Exec(`UPDATE archive_scan_runs SET status='interrupted',finished_at=?,error='process restarted during scan' WHERE status='running'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) DB() *sql.DB { return s.db }

// Scan registers valid ZIP/CBZ files. A failed or partial walk never marks any
// document missing; absence is applied only after a complete successful walk.
func (s *Store) Scan(ctx context.Context) (map[string]any, error) {
	return s.scan(ctx, false)
}

// ScanDeep bypasses the quick file-signature path and re-reads every archive.
func (s *Store) ScanDeep(ctx context.Context) (map[string]any, error) { return s.scan(ctx, true) }

// RegisterPublished validates and registers one archive produced by a trusted
// download job. Unlike Scan, this never walks the archive root or changes the
// status of another document.
func (s *Store) RegisterPublished(ctx context.Context, relativePath string, gid int, token, source string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gid <= 0 || token == "" || source == "" {
		return Document{}, errors.New("complete EH identity and association source required")
	}
	if source != "download_job" {
		return Document{}, errors.New("unsupported trusted association source")
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))
	if rel == "." || filepath.IsAbs(filepath.FromSlash(rel)) || rel == ".." || strings.HasPrefix(rel, "../") {
		return Document{}, errors.New("invalid archive relative path")
	}
	ext := strings.ToLower(filepath.Ext(rel))
	if ext != ".zip" && ext != ".cbz" {
		return Document{}, ErrInvalidArchive
	}
	f, err := secureOpen(s.root, rel)
	if err != nil {
		return Document{}, err
	}
	before, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return Document{}, err
	}
	if !before.Mode().IsRegular() {
		_ = f.Close()
		return Document{}, ErrInvalidArchive
	}
	_ = f.Close()
	d, valid, err := s.inspect(ctx, filepath.Join(s.root, filepath.FromSlash(rel)), rel, before)
	if err != nil {
		return Document{}, err
	}
	if !valid {
		return Document{}, ErrInvalidArchive
	}
	token = strings.ToLower(token)
	if d.ClaimGID > 0 && (d.ClaimGID != gid || d.ClaimToken != token) {
		return Document{}, ErrAssociationConflict
	}
	d.ClaimGID, d.ClaimToken, d.ClaimEvidence = 0, "", ""

	// Match upsert's row preference so we can distinguish a genuinely new local
	// identity from an existing document whose local progression must stay put.
	var priorID, priorHash, priorStatus, priorSource string
	var priorPub sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT id,COALESCE(content_sha256,''),status,COALESCE(association_source,''),publication_id FROM documents WHERE archive_path=? ORDER BY CASE status WHEN 'available' THEN 0 WHEN 'invalid' THEN 1 ELSE 2 END,updated_at DESC LIMIT 1`, rel).Scan(&priorID, &priorHash, &priorStatus, &priorSource, &priorPub)
	if err != nil && err != sql.ErrNoRows {
		return Document{}, err
	}
	priorFound := err == nil
	existingIdentity := priorFound && (priorStatus == "available" || priorStatus == "missing") && priorHash != "" && priorHash == d.SHA256
	if existingIdentity && priorSource == "manual" && priorPub.Valid {
		var oldGID int
		var oldToken string
		if e := s.db.QueryRowContext(ctx, `SELECT gid,token FROM publications WHERE id=?`, priorPub.Int64).Scan(&oldGID, &oldToken); e != nil {
			return Document{}, e
		}
		if oldGID != gid || !strings.EqualFold(oldToken, token) {
			return Document{}, ErrAssociationConflict
		}
	}
	if existingIdentity && priorPub.Valid {
		var oldGID int
		var oldToken string
		if e := s.db.QueryRowContext(ctx, `SELECT gid,token FROM publications WHERE id=?`, priorPub.Int64).Scan(&oldGID, &oldToken); e != nil {
			return Document{}, e
		}
		if oldGID != gid || !strings.EqualFold(oldToken, token) {
			return Document{}, ErrAssociationConflict
		}
	}
	runID, err := randomID()
	if err != nil {
		return Document{}, err
	}
	if err = s.upsert(ctx, d, runID); err != nil {
		return Document{}, err
	}
	var id string
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM documents WHERE archive_path=? AND status='available' AND content_sha256=? ORDER BY updated_at DESC LIMIT 1`, rel, d.SHA256).Scan(&id); err != nil {
		return Document{}, err
	}
	if err = s.bindPublishedIdentity(ctx, id, gid, token, source, !existingIdentity); err != nil {
		return Document{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) bindPublishedIdentity(ctx context.Context, id string, gid int, token, source string, freezeEH bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',?,?,'{}',?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET updated_at=excluded.updated_at`, gid, token, now, now); err != nil {
		return err
	}
	var pubID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, gid, token).Scan(&pubID); err != nil {
		return err
	}
	var currentPub sql.NullInt64
	var target string
	var frozenGID int
	var frozenToken string
	if err = tx.QueryRowContext(ctx, `SELECT publication_id,progression_target,progression_gid,progression_token FROM documents WHERE id=? AND status='available'`, id).Scan(&currentPub, &target, &frozenGID, &frozenToken); err != nil {
		return err
	}
	if currentPub.Valid {
		var oldGID int
		var oldToken string
		if err = tx.QueryRowContext(ctx, `SELECT gid,token FROM publications WHERE id=?`, currentPub.Int64).Scan(&oldGID, &oldToken); err != nil {
			return err
		}
		if oldGID != gid || !strings.EqualFold(oldToken, token) {
			return ErrAssociationConflict
		}
	}
	if target == "eh" && (frozenGID != gid || !strings.EqualFold(frozenToken, token)) {
		return ErrAssociationConflict
	}
	if freezeEH {
		_, err = tx.ExecContext(ctx, `UPDATE documents SET publication_id=?,association_source=?,association_evidence='trusted_download_job',progression_target='eh',progression_gid=?,progression_token=?,updated_at=? WHERE id=? AND status='available'`, pubID, source, gid, token, now, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE documents SET publication_id=?,association_source=CASE WHEN association_source='manual' THEN association_source ELSE ? END,association_evidence=CASE WHEN association_source='manual' THEN association_evidence ELSE 'trusted_download_job' END,updated_at=? WHERE id=? AND status='available'`, pubID, source, now, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) scan(ctx context.Context, deep bool) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := time.Now().UTC()
	runID, err := randomID()
	if err != nil {
		return nil, err
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO archive_scan_runs(id,status,started_at,files_seen,documents_found) VALUES(?,'running',?,0,0)`, runID, start.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	seenPaths := map[string]bool{}
	finish := func(status, message string, seen, found int, completed bool) error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var missing *time.Time
		if completed {
			t := time.Now().UTC()
			missing = &t
		}
		tx, e := s.db.BeginTx(cleanupCtx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if completed {
			rows, e := tx.QueryContext(cleanupCtx, `SELECT id,archive_path FROM documents WHERE status='available'`)
			if e != nil {
				return e
			}
			type stale struct{ id, path string }
			toMark := []stale{}
			for rows.Next() {
				var x stale
				if e = rows.Scan(&x.id, &x.path); e != nil {
					rows.Close()
					return e
				}
				if !seenPaths[x.path] {
					toMark = append(toMark, x)
				}
			}
			if e = rows.Err(); e != nil {
				rows.Close()
				return e
			}
			rows.Close()
			for _, x := range toMark {
				if _, e = tx.ExecContext(cleanupCtx, `UPDATE documents SET status='missing',missing_at=?,updated_at=? WHERE id=?`, missing.Format(time.RFC3339Nano), missing.Format(time.RFC3339Nano), x.id); e != nil {
					return e
				}
			}
		}
		_, e = tx.ExecContext(cleanupCtx, `UPDATE archive_scan_runs SET status=?,finished_at=?,files_seen=?,documents_found=?,error=? WHERE id=?`, status, time.Now().UTC().Format(time.RFC3339Nano), seen, found, nullString(message), runID)
		if e != nil {
			return e
		}
		return tx.Commit()
	}
	seen, found := 0, 0
	err = s.walk(ctx, func(path, rel string, info os.FileInfo) error {
		seen++
		seenPaths[rel] = true
		if !deep {
			unchanged, available, e := s.markUnchanged(ctx, rel, info, runID)
			if e != nil {
				return e
			}
			if unchanged {
				if available {
					found++
				}
				return nil
			}
		}
		doc, valid, e := s.inspect(ctx, path, rel, info)
		if e != nil {
			return e
		}
		if !valid {
			if doc.Path != "" {
				if e = s.upsertInvalid(ctx, doc, runID); e != nil {
					return e
				}
			}
			return nil
		}
		if e = s.upsert(ctx, doc, runID); e != nil {
			return e
		}
		found++
		return nil
	})
	if err != nil {
		_ = finish("failed", err.Error(), seen, found, false)
		return map[string]any{"run_id": runID, "status": "failed", "files_seen": seen, "documents_found": found}, err
	}
	if err = finish("complete", "", seen, found, true); err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "status": "complete", "files_seen": seen, "documents_found": found}, nil
}

func (s *Store) walk(ctx context.Context, visit func(string, string, os.FileInfo) error) error {
	rootInfo, err := os.Lstat(s.root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("archive root is not a real directory")
	}
	return filepath.WalkDir(s.root, func(path string, ent os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if ent.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if ent.IsDir() {
			return nil
		}
		if !ent.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(ent.Name()))
		if ext != ".zip" && ext != ".cbz" {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("invalid archive relative path")
		}
		info, err := ent.Info()
		if err != nil {
			return err
		}
		return visit(path, filepath.ToSlash(rel), info)
	})
}

func (s *Store) inspect(ctx context.Context, path, rel string, before os.FileInfo) (Document, bool, error) {
	f, err := secureOpen(s.root, rel)
	if err != nil {
		return Document{}, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Document{}, false, err
	}
	if !os.SameFile(before, st) || before.Size() != st.Size() || !before.ModTime().Equal(st.ModTime()) {
		return Document{}, false, nil
	}
	invalid := Document{Path: rel, Title: titleFrom(rel), Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(rel)), "."), Status: "invalid", Size: st.Size(), MTimeNS: st.ModTime().UnixNano()}
	if st.Size() <= 0 || st.Size() > maxArchiveBytes {
		return invalid, false, nil
	}
	zw, err := zip.NewReader(f, st.Size())
	if err != nil {
		return invalid, false, nil
	}
	pages, err := readablePages(ctx, zw)
	if err != nil {
		if ctx.Err() != nil {
			return Document{}, false, ctx.Err()
		}
		return invalid, false, nil
	}
	if len(pages) == 0 {
		return invalid, false, nil
	}
	// Hashing the complete container gives moves and same-size replacements a
	// content identity. Stat data only avoids registering a changing file.
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return Document{}, false, err
	}
	h := sha256.New()
	buf := make([]byte, 256*1024)
	if _, err = io.CopyBuffer(h, contextReader{ctx: ctx, r: f}, buf); err != nil {
		return Document{}, false, err
	}
	var after os.FileInfo
	after, err = f.Stat()
	if err != nil {
		return Document{}, false, err
	}
	if after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		return Document{}, false, nil
	}
	now := time.Now().UTC()
	claimGID, claimToken := archiveIdentity(ctx, zw)
	return Document{Title: titleFrom(rel), Path: rel, Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(rel)), "."), Status: "available", Size: st.Size(), MTimeNS: st.ModTime().UnixNano(), SHA256: hex.EncodeToString(h.Sum(nil)), PageCount: len(pages), Pages: pages, ClaimGID: claimGID, ClaimToken: claimToken, ClaimEvidence: func() string {
		if claimGID > 0 {
			return "archive_metadata_url"
		}
		return ""
	}(), CreatedAt: now, UpdatedAt: now}, true, nil
}

func archiveIdentity(ctx context.Context, zr *zip.Reader) (int, string) {
	files := 0
	var total uint64
	var gid int
	var token string
	for _, f := range zr.File {
		if files >= 64 || f.UncompressedSize64 > 512<<10 || total+f.UncompressedSize64 > 2<<20 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".txt" && ext != ".xml" && ext != ".json" {
			continue
		}
		files++
		total += f.UncompressedSize64
		rc, e := f.Open()
		if e != nil {
			continue
		}
		b, e := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, r: rc}, (512<<10)+1))
		_ = rc.Close()
		if e != nil || len(b) > 512<<10 {
			continue
		}
		for _, match := range ehGalleryURL.FindAllSubmatch(b, -1) {
			n, e := strconv.Atoi(string(match[1]))
			if e != nil || n <= 0 {
				continue
			}
			t := strings.ToLower(string(match[2]))
			if gid != 0 && (gid != n || token != t) {
				return 0, ""
			}
			gid, token = n, t
		}
	}
	return gid, token
}

func (s *Store) upsert(ctx context.Context, d Document, runID string) error {
	var id, oldHash string
	var oldStatus string
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(content_sha256,''),status FROM documents WHERE archive_path=? ORDER BY CASE status WHEN 'available' THEN 0 WHEN 'invalid' THEN 1 ELSE 2 END,updated_at DESC LIMIT 1`, d.Path).Scan(&id, &oldHash, &oldStatus)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		id, err = randomID()
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,scan_run_id,created_at,updated_at) VALUES(?,?,?,?,?,'available',?,?,?,?,?,?)`, id, d.Path, d.Format, d.Size, d.MTimeNS, d.SHA256, d.Title, d.PageCount, runID, d.CreatedAt.Format(time.RFC3339Nano), d.UpdatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if err = s.replacePageIndex(ctx, id, d.Pages); err != nil {
			return err
		}
		return s.applyArchiveClaim(ctx, id, d)
	}
	if oldHash != "" && oldHash != d.SHA256 {
		// A replacement at the same name is a new local identity. Leave any
		// previous progress attached to the old document record.
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=?,updated_at=? WHERE id=?`, d.UpdatedAt.Format(time.RFC3339Nano), d.UpdatedAt.Format(time.RFC3339Nano), id); e != nil {
			return e
		}
		id, e = randomID()
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,scan_run_id,created_at,updated_at) VALUES(?,?,?,?,?,'available',?,?,?,?,?,?)`, id, d.Path, d.Format, d.Size, d.MTimeNS, d.SHA256, d.Title, d.PageCount, runID, d.CreatedAt.Format(time.RFC3339Nano), d.UpdatedAt.Format(time.RFC3339Nano))
		if e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		if e = s.replacePageIndex(ctx, id, d.Pages); e != nil {
			return e
		}
		return s.applyArchiveClaim(ctx, id, d)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE documents SET format=?,size=?,mtime_ns=?,status='available',content_sha256=?,title=?,page_count=?,scan_run_id=?,missing_at=NULL,updated_at=CASE WHEN status='available' AND COALESCE(content_sha256,'')=? AND size=? AND mtime_ns=? THEN updated_at ELSE ? END WHERE id=?`, d.Format, d.Size, d.MTimeNS, d.SHA256, d.Title, d.PageCount, runID, d.SHA256, d.Size, d.MTimeNS, d.UpdatedAt.Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	if err = s.replacePageIndex(ctx, id, d.Pages); err != nil {
		return err
	}
	return s.applyArchiveClaim(ctx, id, d)
}

func (s *Store) applyArchiveClaim(ctx context.Context, id string, d Document) error {
	if d.ClaimGID <= 0 || d.ClaimToken == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at)VALUES('eh',?,?,'{}',?,?)ON CONFLICT(provider,gid,token)DO UPDATE SET updated_at=excluded.updated_at`, d.ClaimGID, d.ClaimToken, now, now); e != nil {
		return e
	}
	var pubID int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, d.ClaimGID, d.ClaimToken).Scan(&pubID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE documents SET publication_id=?,association_source='archive_metadata',association_evidence=? WHERE id=? AND publication_id IS NULL`, pubID, d.ClaimEvidence, id); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) markUnchanged(ctx context.Context, rel string, info os.FileInfo, runID string) (bool, bool, error) {
	var id, status string
	var size, mtime int64
	var pages int
	var hash sql.NullString
	var publicationID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,status,size,mtime_ns,page_count,content_sha256,publication_id FROM documents WHERE archive_path=? AND status IN ('available','invalid') ORDER BY CASE status WHEN 'available' THEN 0 WHEN 'invalid' THEN 1 ELSE 2 END,updated_at DESC LIMIT 1`, rel).Scan(&id, &status, &size, &mtime, &pages, &hash, &publicationID)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if info.Size() != size || info.ModTime().UnixNano() != mtime {
		return false, false, nil
	}
	if status == "available" {
		if !hash.Valid || hash.String == "" {
			return false, false, nil
		}
		var indexed int
		if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM document_pages WHERE document_id=?`, id).Scan(&indexed); err != nil {
			return false, false, err
		}
		if indexed != pages {
			return false, false, nil
		}
	}
	f, err := secureOpen(s.root, rel)
	if err != nil {
		return false, false, nil
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return false, false, err
	}
	if !os.SameFile(info, st) || st.Size() != size || st.ModTime().UnixNano() != mtime {
		_ = f.Close()
		return false, false, nil
	}
	if status == "available" && !publicationID.Valid {
		if zr, e := zip.NewReader(f, st.Size()); e == nil {
			gid, token := archiveIdentity(ctx, zr)
			if gid > 0 && token != "" {
				if e = s.applyArchiveClaim(ctx, id, Document{ClaimGID: gid, ClaimToken: token, ClaimEvidence: "archive_metadata_url"}); e != nil {
					_ = f.Close()
					return false, false, e
				}
			}
		}
	}
	_ = f.Close()
	if _, err = s.db.ExecContext(ctx, `UPDATE documents SET scan_run_id=? WHERE id=?`, runID, id); err != nil {
		return false, false, err
	}
	return true, status == "available", nil
}

func (s *Store) upsertInvalid(ctx context.Context, d Document, runID string) error {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM documents WHERE archive_path=? AND status IN ('available','invalid') ORDER BY CASE status WHEN 'available' THEN 0 ELSE 1 END,updated_at DESC LIMIT 1`, d.Path).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE documents SET format=?,size=?,mtime_ns=?,status='invalid',title=?,page_count=0,scan_run_id=?,updated_at=? WHERE id=?`, d.Format, d.Size, d.MTimeNS, d.Title, runID, now, id)
		return err
	}
	id, err = randomID()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,scan_run_id,created_at,updated_at)VALUES(?,?,?,?,?,'invalid','',?,0,?,?,?)`, id, d.Path, d.Format, d.Size, d.MTimeNS, d.Title, runID, now, now)
	return err
}

func (s *Store) List(ctx context.Context, page, perPage int, status, linked string) ([]Document, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 200 {
		perPage = 200
	}
	if page > 1000000 {
		page = 1000000
	}
	where, args := []string{}, []any{}
	if status == "all" {
		// Diagnostic views intentionally include every persisted state.
	} else if status != "" {
		where = append(where, "status=?")
		args = append(args, status)
	} else {
		where = append(where, "status='available'")
	}
	if linked == "true" || linked == "linked" {
		where = append(where, "publication_id IS NOT NULL")
	} else if linked == "false" || linked == "unlinked" {
		where = append(where, "publication_id IS NULL")
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM documents"+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	start := (page - 1) * perPage
	if start >= total {
		return []Document{}, total, nil
	}
	selected := make([]Document, 0, perPage)
	offset := start
	var cursorCreated, cursorID string
	firstBatch := true
	for len(selected) < perPage {
		batchSize := perPage
		if firstBatch && batchSize > total-offset {
			batchSize = total - offset
		}
		queryWhere := clause
		queryArgs := append([]any{}, args...)
		if !firstBatch {
			if queryWhere == "" {
				queryWhere = " WHERE "
			} else {
				queryWhere += " AND "
			}
			queryWhere += `(d.created_at < ? OR (d.created_at = ? AND d.id > ?))`
			queryArgs = append(queryArgs, cursorCreated, cursorCreated, cursorID)
		}
		queryArgs = append(queryArgs, batchSize)
		query := `SELECT d.id,d.title,d.archive_path,d.format,d.status,d.size,d.mtime_ns,d.content_sha256,d.page_count,d.publication_id,d.association_source,d.created_at,d.updated_at,d.missing_at,p.gid,p.token,d.progression_target,d.progression_gid,d.progression_token FROM documents d LEFT JOIN publications p ON p.id=d.publication_id` + queryWhere + ` ORDER BY d.created_at DESC,d.id LIMIT ?`
		if firstBatch {
			query += ` OFFSET ?`
			queryArgs = append(queryArgs, offset)
		}
		rows, err := s.db.QueryContext(ctx, query, queryArgs...)
		if err != nil {
			return nil, 0, err
		}
		batch := []Document{}
		for rows.Next() {
			var d Document
			var created, updated string
			var src, missing sql.NullString
			var gid sql.NullInt64
			var token sql.NullString
			var pub sql.NullInt64
			if err = rows.Scan(&d.ID, &d.Title, &d.Path, &d.Format, &d.Status, &d.Size, &d.MTimeNS, &d.SHA256, &d.PageCount, &pub, &src, &created, &updated, &missing, &gid, &token, &d.ProgressionTarget, &d.ProgressionGID, &d.ProgressionToken); err != nil {
				rows.Close()
				return nil, 0, err
			}
			d.AssociationSource = src.String
			d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
			if missing.Valid {
				t, _ := time.Parse(time.RFC3339Nano, missing.String)
				d.MissingAt = &t
			}
			if gid.Valid {
				v := int(gid.Int64)
				d.GID = &v
				d.Token = token.String
			}
			batch = append(batch, d)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, 0, err
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}
		last := batch[len(batch)-1]
		cursorCreated, cursorID = last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID
		firstBatch = false
		for _, d := range batch {
			if status != "all" && d.Status == "available" {
				if err = s.verify(ctx, d); err != nil {
					continue
				}
			}
			selected = append(selected, d)
			if len(selected) == perPage {
				break
			}
		}
	}
	if status == "" || status == "all" {
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM documents"+clause, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return selected, total, nil
}

func (s *Store) Get(ctx context.Context, id string) (Document, error) {
	d, e := s.ListByID(ctx, id)
	if e != nil {
		return d, e
	}
	if d.Status == "available" {
		if e = s.verify(ctx, d); e != nil {
			return Document{}, ErrNotFound
		}
	}
	return d, nil
}

func (s *Store) verify(ctx context.Context, d Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, e := secureOpen(s.root, d.Path)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return e
	}
	if st.Size() != d.Size || st.ModTime().UnixNano() != d.MTimeNS {
		return errors.New("document changed; rescan required")
	}
	return nil
}

func (s *Store) SetDeleteHooks(before, rollback func(Document) error) {
	s.beforeDelete = before
	s.rollbackDelete = rollback
}

func (s *Store) Associate(ctx context.Context, id string, gid int, token string) error {
	if gid == 0 && token == "" {
		res, err := s.db.ExecContext(ctx, `UPDATE documents SET publication_id=NULL,association_source=NULL,association_evidence=NULL,updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
		return nil
	}
	if gid <= 0 || token == "" {
		return errors.New("complete EH identity required")
	}
	token = strings.ToLower(token)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',?,?,'{}',?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET updated_at=excluded.updated_at`, gid, token, now, now); err != nil {
		return err
	}
	var pubID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, gid, token).Scan(&pubID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE documents SET publication_id=?,association_source='manual',association_evidence='user',updated_at=? WHERE id=?`, pubID, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) ListByID(ctx context.Context, id string) (Document, error) {
	var d Document
	var created, updated string
	var src, missing sql.NullString
	var gid sql.NullInt64
	var token sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT d.id,d.title,d.archive_path,d.format,d.status,d.size,d.mtime_ns,d.content_sha256,d.page_count,d.publication_id,d.association_source,d.created_at,d.updated_at,d.missing_at,p.gid,p.token,d.progression_target,d.progression_gid,d.progression_token FROM documents d LEFT JOIN publications p ON p.id=d.publication_id WHERE d.id=?`, id).Scan(&d.ID, &d.Title, &d.Path, &d.Format, &d.Status, &d.Size, &d.MTimeNS, &d.SHA256, &d.PageCount, new(sql.NullInt64), &src, &created, &updated, &missing, &gid, &token, &d.ProgressionTarget, &d.ProgressionGID, &d.ProgressionToken)
	if err == sql.ErrNoRows {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.AssociationSource = src.String
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if missing.Valid {
		t, _ := time.Parse(time.RFC3339Nano, missing.String)
		d.MissingAt = &t
	}
	if gid.Valid {
		v := int(gid.Int64)
		d.GID = &v
		d.Token = token.String
	}
	return d, nil
}

func (s *Store) OpenPage(ctx context.Context, id string, page int) ([]byte, string, error) {
	d, e := s.Get(ctx, id)
	if e != nil {
		return nil, "", e
	}
	if d.Status != "available" {
		return nil, "", ErrNotFound
	}
	if page < 0 || page >= d.PageCount {
		return nil, "", ErrNotFound
	}
	f, e := secureOpen(s.root, d.Path)
	if e != nil {
		return nil, "", e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, "", e
	}
	if st.Size() != d.Size || st.ModTime().UnixNano() != d.MTimeNS {
		return nil, "", errors.New("document changed; rescan required")
	}
	pages, e := s.pageIndex(ctx, id)
	if e != nil {
		return nil, "", e
	}
	if len(pages) != d.PageCount {
		pages, e = s.rebuildPageIndex(ctx, d, f)
		if e != nil {
			return nil, "", e
		}
	}
	if page >= len(pages) {
		return nil, "", ErrNotFound
	}
	info := pages[page]
	zr, e := zip.NewReader(f, st.Size())
	if e != nil {
		return nil, "", e
	}
	var zf *zip.File
	if info.CentralIndex >= 0 && info.CentralIndex < len(zr.File) {
		zf = zr.File[info.CentralIndex]
	}
	if zf == nil || zf.UncompressedSize64 != info.Size {
		return nil, "", errors.New("document page index is stale")
	}
	rc, e := zf.Open()
	if e != nil {
		return nil, "", e
	}
	defer rc.Close()
	b, e := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, r: rc}, int64(maxImageBytes)+1))
	if e != nil {
		return nil, "", e
	}
	if len(b) > int(maxImageBytes) {
		return nil, "", errors.New("image page exceeds size limit")
	}
	return b, info.MIME, nil
}

func (s *Store) rebuildPageIndex(ctx context.Context, d Document, f *os.File) ([]PageInfo, error) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return nil, e
	}
	zr, e := zip.NewReader(f, d.Size)
	if e != nil {
		return nil, e
	}
	pages, e := readablePages(ctx, zr)
	if e != nil {
		return nil, e
	}
	if e = s.replacePageIndex(ctx, d.ID, pages); e != nil {
		return nil, e
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return nil, e
	}
	return pages, nil
}

func readablePages(ctx context.Context, zr *zip.Reader) ([]PageInfo, error) {
	if len(zr.File) > maxZipEntries {
		return nil, errors.New("archive has too many entries")
	}
	type indexedFile struct {
		index int
		file  *zip.File
	}
	pages := make([]indexedFile, 0, min(len(zr.File), maxPages))
	var total uint64
	for entryIndex, zf := range zr.File {
		if zf.FileInfo().IsDir() || zf.Mode()&os.ModeSymlink != 0 || !imageExtension(zf.Name) {
			continue
		}
		if zf.UncompressedSize64 > maxImageBytes || zf.UncompressedSize64 > maxTotalImages-total {
			return nil, errors.New("archive image data exceeds limit")
		}
		total += zf.UncompressedSize64
		pages = append(pages, indexedFile{index: entryIndex, file: zf})
		if len(pages) > maxPages {
			return nil, errors.New("archive has too many image pages")
		}
	}
	sort.SliceStable(pages, func(i, j int) bool { return naturalLess(pages[i].file.Name, pages[j].file.Name) })
	valid := make([]PageInfo, 0, len(pages))
	for _, indexed := range pages {
		zf := indexed.file
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		cfg, format, err := image.DecodeConfig(io.LimitReader(contextReader{ctx: ctx, r: rc}, int64(maxImageBytes)))
		_ = rc.Close()
		if err == nil && cfg.Width > 0 && cfg.Height > 0 {
			valid = append(valid, PageInfo{Index: len(valid), CentralIndex: indexed.index, EntryName: zf.Name, MIME: mimeForFormat(format), Size: zf.UncompressedSize64})
		}
	}
	return valid, nil
}

func mimeForFormat(format string) string {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func (s *Store) replacePageIndex(ctx context.Context, id string, pages []PageInfo) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, id); e != nil {
		return e
	}
	for _, p := range pages {
		if _, e = tx.ExecContext(ctx, `INSERT INTO document_pages(document_id,page_index,central_index,entry_name,mime,size)VALUES(?,?,?,?,?,?)`, id, p.Index, p.CentralIndex, p.EntryName, p.MIME, p.Size); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Store) pageIndex(ctx context.Context, id string) ([]PageInfo, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT page_index,central_index,entry_name,mime,size FROM document_pages WHERE document_id=? ORDER BY page_index`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PageInfo{}
	for rows.Next() {
		var p PageInfo
		if e = rows.Scan(&p.Index, &p.CentralIndex, &p.EntryName, &p.MIME, &p.Size); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PageMIME(ctx context.Context, id string, page int) (string, error) {
	d, e := s.Get(ctx, id)
	if e != nil {
		return "", e
	}
	pages, e := s.pageIndex(ctx, id)
	if e != nil {
		return "", e
	}
	if len(pages) != d.PageCount {
		f, e := secureOpen(s.root, d.Path)
		if e != nil {
			return "", e
		}
		defer f.Close()
		pages, e = s.rebuildPageIndex(ctx, d, f)
		if e != nil {
			return "", e
		}
	}
	if page < 0 || page >= len(pages) {
		return "", ErrNotFound
	}
	return pages[page].MIME, nil
}

func (s *Store) PageMIMEs(ctx context.Context, id string) ([]string, error) {
	d, e := s.Get(ctx, id)
	if e != nil {
		return nil, e
	}
	pages, e := s.pageIndex(ctx, id)
	if e != nil {
		return nil, e
	}
	if len(pages) != d.PageCount {
		f, err := secureOpen(s.root, d.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		pages, e = s.rebuildPageIndex(ctx, d, f)
		if e != nil {
			return nil, e
		}
	}
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.MIME
	}
	return out, nil
}

// PageMIMEProfile returns the common page MIME, or image/* when pages use more
// than one image format. It aggregates the persisted page index without
// reading archive members or materializing the full MIME list.
func (s *Store) PageMIMEProfile(ctx context.Context, id string) (string, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return "", err
	}
	var minMIME, maxMIME string
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(mime),MAX(mime) FROM document_pages WHERE document_id=?`, id).Scan(&minMIME, &maxMIME); err != nil {
		return "", err
	}
	if minMIME == "" {
		return "", ErrNotFound
	}
	if minMIME != maxMIME {
		return "image/*", nil
	}
	return minMIME, nil
}

// CoverMIME returns only the stored cover's content type; it never loads the
// cover BLOB into application memory.
func (s *Store) CoverMIME(ctx context.Context, id string) (string, bool, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return "", false, err
	}
	var mime string
	err := s.db.QueryRowContext(ctx, `SELECT c.mime FROM documents d JOIN covers c ON c.publication_id=d.publication_id WHERE d.id=?`, id).Scan(&mime)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return mime, true, nil
}

func (s *Store) Cover(ctx context.Context, id string) ([]byte, string, bool, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, "", false, err
	}
	if d.GID == nil {
		return nil, "", false, nil
	}
	var data []byte
	var mime string
	err = s.db.QueryRowContext(ctx, `SELECT c.data,c.mime FROM documents d JOIN covers c ON c.publication_id=d.publication_id WHERE d.id=?`, id).Scan(&data, &mime)
	if err == sql.ErrNoRows {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	return data, mime, true, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if d.Status != "available" {
		return ErrNotFound
	}
	root, e := openArchiveRoot(s.root)
	if e != nil {
		return e
	}
	defer root.Close()
	f, e := openRootFile(root, d.Path)
	if e != nil {
		return e
	}
	st, e := f.Stat()
	if e != nil {
		f.Close()
		return e
	}
	if st.Size() != d.Size || st.ModTime().UnixNano() != d.MTimeNS {
		f.Close()
		return errors.New("document changed; refusing deletion")
	}
	if e = f.Close(); e != nil {
		return e
	}
	if s.beforeDelete != nil {
		if e = s.beforeDelete(d); e != nil {
			return e
		}
	}
	if e = root.Remove(filepath.FromSlash(d.Path)); e != nil {
		if s.rollbackDelete != nil {
			e = errors.Join(e, s.rollbackDelete(d))
		}
		return e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, dbErr := s.db.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=?,updated_at=? WHERE id=?`, now, now, id)
	return errors.Join(e, dbErr)
}

// PurgeMissing removes only stale document metadata after verifying the payload
// is still absent. Progression history and shared publication data are retained.
func (s *Store) PurgeMissing(ctx context.Context, id string) error {
	d, err := s.ListByID(ctx, id)
	if err != nil {
		return err
	}
	if d.Status != "missing" {
		return ErrNotFound
	}
	root, openErr := openArchiveRoot(s.root)
	if openErr != nil {
		return openErr
	}
	defer root.Close()
	f, openErr := openRootFile(root, d.Path)
	if openErr == nil {
		_ = f.Close()
		return errors.New("document payload exists; scan before purging record")
	}
	if !errors.Is(openErr, os.ErrNotExist) {
		return openErr
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE download_jobs SET document_id=NULL WHERE document_id=?`, id); e != nil {
		return e
	}
	res, e := tx.ExecContext(ctx, `DELETE FROM documents WHERE id=? AND status='missing'`, id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func secureOpen(root, rel string) (*os.File, error) {
	r, e := openArchiveRoot(root)
	if e != nil {
		return nil, e
	}
	f, e := openRootFile(r, rel)
	_ = r.Close()
	return f, e
}

func openArchiveRoot(root string) (*os.Root, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("archive root is not a real directory")
	}
	return os.OpenRoot(root)
}

func openRootFile(r *os.Root, rel string) (*os.File, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, errors.New("invalid archive path")
	}
	cur := ""
	parts := strings.Split(clean, string(filepath.Separator))
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		fi, e := r.Lstat(cur)
		if e != nil {
			return nil, e
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("archive path contains a symlink")
		}
		if i < len(parts)-1 && !fi.IsDir() {
			return nil, errors.New("archive path component is not a directory")
		}
	}
	f, e := r.Open(cur)
	if e != nil {
		return nil, e
	}
	actual, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	if !actual.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("archive payload is not a regular file")
	}
	expected, e := r.Stat(cur)
	if e != nil || !os.SameFile(actual, expected) {
		f.Close()
		return nil, errors.New("archive path changed during access")
	}
	return f, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if e := c.ctx.Err(); e != nil {
		return 0, e
	}
	return c.r.Read(p)
}
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func titleFrom(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}
func imageExtension(n string) bool {
	switch strings.ToLower(filepath.Ext(n)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}
func mimeFor(n string, b []byte) string {
	if t := httpDetect(b); t != "application/octet-stream" {
		return t
	}
	switch strings.ToLower(filepath.Ext(n)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "application/octet-stream"
}
func httpDetect(b []byte) string {
	if len(b) > 512 {
		b = b[:512]
	}
	return http.DetectContentType(b)
}
func naturalLess(a, b string) bool {
	aa, bb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(aa) && j < len(bb) {
		if aa[i] >= '0' && aa[i] <= '9' && bb[j] >= '0' && bb[j] <= '9' {
			x, y := i, j
			for x < len(aa) && aa[x] >= '0' && aa[x] <= '9' {
				x++
			}
			for y < len(bb) && bb[y] >= '0' && bb[y] <= '9' {
				y++
			}
			ax, bx := i, y
			for ax < x-1 && aa[ax] == '0' {
				ax++
			}
			for bx < y-1 && bb[bx] == '0' {
				bx++
			}
			if x-ax != y-bx {
				return x-ax < y-bx
			}
			for k := 0; k < x-ax; k++ {
				if aa[ax+k] != bb[bx+k] {
					return aa[ax+k] < bb[bx+k]
				}
			}
			if x-i != y-j {
				return x-i < y-j
			}
			i, j = x, y
			continue
		}
		if aa[i] != bb[j] {
			return aa[i] < bb[j]
		}
		i++
		j++
	}
	return len(aa) < len(bb)
}
