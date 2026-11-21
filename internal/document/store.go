package document

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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
	"syscall"
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
var ErrTransientRead = errors.New("archive could not be read completely; retry reading")
var ErrAssociationConflict = errors.New("document EH association conflicts with trusted download identity")
var ehGalleryURL = regexp.MustCompile(`(?i)https?://(?:www\.)?(?:e-hentai\.org|exhentai\.org)/g/([0-9]+)/([0-9a-f]+)`)

type Store struct {
	root            string
	db              *sql.DB
	mu              sync.Mutex
	payloadOpenHook func(string)
	zipReaderHook   func(*os.File, int64) (*zip.Reader, error)
	beforeDelete    func(Document) error
	rollbackDelete  func(Document) error
}

func (s *Store) newZipReader(file *os.File, size int64) (*zip.Reader, error) {
	if s.zipReaderHook != nil {
		return s.zipReaderHook(file, size)
	}
	return zip.NewReader(file, size)
}

// ValidationError keeps the stage and underlying cause of a failed document
// access while exposing only a relative archive path and stat signatures to
// callers. Cause is intentionally not serialized by the HTTP layer.
type ValidationError struct {
	Reason       string
	Phase        string
	Path         string
	StoredSize   int64
	StoredMTime  int64
	CurrentSize  *int64
	CurrentMTime *int64
	Cause        error
}

func (e *ValidationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("document %s during %s: %v", e.Reason, e.Phase, e.Cause)
	}
	return fmt.Sprintf("document %s during %s", e.Reason, e.Phase)
}

func (e *ValidationError) Unwrap() error { return e.Cause }

func ValidationReason(err error) string {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Reason
	}
	if errors.Is(err, ErrInvalidArchive) {
		return "invalid_archive"
	}
	if errors.Is(err, ErrNotFound) {
		return "missing_record"
	}
	return "io_error"
}

func validationCauseCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, syscall.EIO):
		return "io_error"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "permission_denied"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "timeout"
	case errors.Is(err, syscall.ESTALE):
		return "stale_mount"
	default:
		return fmt.Sprintf("%T", err)
	}
}

func ValidationCauseCode(err error) string { return validationCauseCode(err) }

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

func (s *Store) openPayload(rel string) (*os.File, error) {
	if s.payloadOpenHook != nil {
		s.payloadOpenHook(rel)
	}
	return secureOpen(s.root, rel)
}

// Scan registers valid ZIP/CBZ files. A failed or partial walk never marks any
// document missing; absence is applied only after a complete successful walk.
func (s *Store) Scan(ctx context.Context) (map[string]any, error) {
	return s.scan(ctx, false)
}

// ScanDeep explicitly reads each archive to build page indexes and SHA-256
// content fingerprints. It does not perform ZIP CRC verification.
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
	before, err := secureStat(s.root, rel)
	if err != nil {
		return Document{}, err
	}
	if !before.Mode().IsRegular() {
		return Document{}, ErrInvalidArchive
	}
	d := candidateDocument(rel, before)
	token = strings.ToLower(token)
	runID, err := randomID()
	if err != nil {
		return Document{}, err
	}
	if err = s.upsertCandidate(ctx, d, runID, true); err != nil {
		return Document{}, err
	}
	rows, err := pathRows(ctx, s.db, rel)
	if err != nil {
		return Document{}, err
	}
	selected, found, _ := choosePathRow(rows)
	if !found || selected.status != "available" {
		return Document{}, ErrNotFound
	}
	id := selected.id
	if err = s.bindPublishedIdentity(ctx, id, gid, token, source); err != nil {
		return Document{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) bindPublishedIdentity(ctx context.Context, id string, gid int, token, source string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentPub sql.NullInt64
	var currentSource sql.NullString
	var target string
	var hasProgress bool
	if err = tx.QueryRowContext(ctx, `SELECT publication_id,association_source,progression_target,EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=documents.id) FROM documents WHERE id=? AND status='available'`, id).Scan(&currentPub, &currentSource, &target, &hasProgress); err != nil {
		return err
	}
	// A trusted completed download is authoritative for its own document and
	// may override a prior manual unlink. If another record already owns this
	// gallery, keep the completed file but leave it unlinked for user handling.
	var occupied int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM documents d JOIN publications p ON p.id=d.publication_id WHERE d.id<>? AND p.provider='eh' AND p.gid=? AND lower(p.token)=lower(?))`, id, gid, token).Scan(&occupied); err != nil {
		return err
	}
	if occupied != 0 {
		_, err = tx.ExecContext(ctx, `UPDATE documents SET publication_id=NULL,association_source='manual',association_evidence='trusted_download_conflict',updated_at=? WHERE id=?`, now, id)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',?,?,'{}',?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET updated_at=excluded.updated_at`, gid, token, now, now); err != nil {
		return err
	}
	var pubID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, gid, token).Scan(&pubID); err != nil {
		return err
	}
	freezeEH := !hasProgress && target != "eh"
	_, err = tx.ExecContext(ctx, `UPDATE documents SET publication_id=?,association_source=?,association_evidence='trusted_download_job',progression_target=CASE WHEN ? THEN 'eh' ELSE progression_target END,progression_gid=CASE WHEN ? THEN ? ELSE progression_gid END,progression_token=CASE WHEN ? THEN ? ELSE progression_token END,updated_at=? WHERE id=? AND status='available'`, pubID, source, freezeEH, freezeEH, gid, freezeEH, token, now, id)
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
			rows, e := tx.QueryContext(cleanupCtx, `SELECT id,archive_path FROM documents WHERE status IN ('available','invalid')`)
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
				if _, e = tx.ExecContext(cleanupCtx, `UPDATE documents SET status='missing',missing_at=?,updated_at=? WHERE id=? AND status IN ('available','invalid')`, missing.Format(time.RFC3339Nano), missing.Format(time.RFC3339Nano), x.id); e != nil {
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
			doc := candidateDocument(rel, info)
			if e := s.upsertCandidate(ctx, doc, runID, false); e != nil {
				return e
			}
			var available int
			if e := s.db.QueryRowContext(ctx, `SELECT CASE WHEN status='available' THEN 1 ELSE 0 END FROM documents WHERE archive_path=? AND size=? AND mtime_ns=? AND status IN ('available','invalid') ORDER BY CASE status WHEN 'available' THEN 0 ELSE 1 END,updated_at DESC LIMIT 1`, rel, info.Size(), info.ModTime().UnixNano()).Scan(&available); e != nil && e != sql.ErrNoRows {
				return e
			}
			found += available
			return nil
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

func candidateDocument(rel string, info os.FileInfo) Document {
	return Document{
		Title: titleFrom(rel), Path: rel,
		Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(rel)), "."),
		Status: "available", Size: info.Size(), MTimeNS: info.ModTime().UnixNano(),
		PageCount: 0,
	}
}

type pathRow struct {
	id, status, source, target, created string
	size, mtime                         int64
	publicationID                       sql.NullInt64
	publicationGID                      sql.NullInt64
	publicationToken                    sql.NullString
	localProgress                       bool
}

type pathQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func pathRows(ctx context.Context, q pathQuerier, rel string) ([]pathRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT d.id,d.status,COALESCE(d.association_source,''),d.progression_target,d.created_at,d.size,d.mtime_ns,d.publication_id,p.gid,p.token,EXISTS(SELECT 1 FROM progressions WHERE target_type='document' AND target_id=d.id) FROM documents d LEFT JOIN publications p ON p.id=d.publication_id WHERE d.archive_path=? ORDER BY d.created_at,d.id`, rel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pathRow{}
	for rows.Next() {
		var row pathRow
		if err = rows.Scan(&row.id, &row.status, &row.source, &row.target, &row.created, &row.size, &row.mtime, &row.publicationID, &row.publicationGID, &row.publicationToken, &row.localProgress); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func choosePathRow(rows []pathRow) (pathRow, bool, bool) {
	if len(rows) == 0 {
		return pathRow{}, false, false
	}
	findOne := func(candidates []pathRow) (pathRow, bool) {
		if len(candidates) == 1 {
			return candidates[0], true
		}
		return pathRow{}, false
	}
	active := []pathRow{}
	for _, row := range rows {
		if row.status == "available" {
			active = append(active, row)
			if row.publicationID.Valid || row.source == "manual" || row.localProgress {
				return row, true, false
			}
		}
	}
	manual := []pathRow{}
	manualIdentities := map[string]bool{}
	for _, row := range rows {
		if row.source == "manual" {
			manual = append(manual, row)
			key := "unlinked"
			if row.publicationID.Valid && row.publicationGID.Valid {
				key = fmt.Sprintf("%d:%s", row.publicationGID.Int64, strings.ToLower(row.publicationToken.String))
			}
			manualIdentities[key] = true
		}
	}
	if len(manualIdentities) > 1 {
		if len(active) > 0 {
			return active[0], true, true
		}
		return manual[0], true, true
	}
	if len(manual) > 0 {
		for _, row := range manual {
			if row.status == "available" {
				return row, true, false
			}
		}
		return manual[0], true, false
	}
	pubRows := []pathRow{}
	pubIdentities := map[string]bool{}
	for _, row := range rows {
		if row.publicationID.Valid && row.publicationGID.Valid {
			pubRows = append(pubRows, row)
			pubIdentities[fmt.Sprintf("%d:%s", row.publicationGID.Int64, strings.ToLower(row.publicationToken.String))] = true
		}
	}
	if len(pubIdentities) == 1 && len(pubRows) > 0 {
		for _, row := range pubRows {
			if row.localProgress {
				return row, true, false
			}
		}
		for _, row := range pubRows {
			if row.status == "available" {
				return row, true, false
			}
		}
		return pubRows[0], true, false
	}
	conflict := len(pubIdentities) > 1 || len(manual) > 1
	if conflict {
		if len(active) > 0 {
			return active[0], true, true
		}
		return rows[0], true, true
	}
	frozen := []pathRow{}
	frozenIDs := map[string]bool{}
	for _, row := range rows {
		if row.target == "eh" && row.publicationGID.Valid {
			frozen = append(frozen, row)
			frozenIDs[fmt.Sprintf("%d:%s", row.publicationGID.Int64, strings.ToLower(row.publicationToken.String))] = true
		}
	}
	if len(frozenIDs) == 1 && len(frozen) == 1 {
		return frozen[0], true, false
	}
	progress := []pathRow{}
	for _, row := range rows {
		if row.localProgress {
			progress = append(progress, row)
		}
	}
	if selected, ok := findOne(progress); ok {
		return selected, true, false
	}
	if len(active) > 0 {
		return active[0], true, conflict || len(frozenIDs) > 1 || len(progress) > 1
	}
	return rows[0], true, conflict || len(frozenIDs) > 1 || len(progress) > 1
}

// upsertCandidate records only path and stat metadata. It never opens the ZIP.
// The archive-relative path is the document identity; stat changes only
// invalidate the optional hash and page index.
func (s *Store) upsertCandidate(ctx context.Context, d Document, runID string, _ bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := pathRows(ctx, tx, d.Path)
	if err != nil {
		return err
	}
	chosen, found, _ := choosePathRow(rows)
	if !found {
		id, e := randomID()
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,scan_run_id,created_at,updated_at) VALUES(?,?,?,?,?,'available','',?,0,?,?,?)`, id, d.Path, d.Format, d.Size, d.MTimeNS, d.Title, runID, now, now); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=COALESCE(missing_at,?),updated_at=? WHERE archive_path=? AND status='available' AND id<>?`, now, now, d.Path, chosen.id); err != nil {
			return err
		}
		changed := chosen.size != d.Size || chosen.mtime != d.MTimeNS
		resetIndex := changed || chosen.status == "invalid"
		nextStatus := "available"
		_, err = tx.ExecContext(ctx, `UPDATE documents SET format=?,size=?,mtime_ns=?,status=?,content_sha256=CASE WHEN size<>? OR mtime_ns<>? OR status='invalid' THEN '' ELSE content_sha256 END,title=?,page_count=CASE WHEN size<>? OR mtime_ns<>? OR status='invalid' THEN 0 ELSE page_count END,scan_run_id=?,missing_at=NULL,updated_at=CASE WHEN size<>? OR mtime_ns<>? OR status<>? THEN ? ELSE updated_at END WHERE id=?`, d.Format, d.Size, d.MTimeNS, nextStatus, d.Size, d.MTimeNS, d.Title, d.Size, d.MTimeNS, runID, d.Size, d.MTimeNS, nextStatus, now, chosen.id)
		if err != nil {
			return err
		}
		if resetIndex {
			if _, err = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, chosen.id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
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
	f, err := s.openPayload(rel)
	if err != nil {
		return Document{}, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Document{}, false, err
	}
	// The file handle is authoritative. WalkDir's timestamp can have a
	// different precision on network filesystems; it is not identity proof.
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
	if after.Size() != st.Size() || after.ModTime().UnixNano() != st.ModTime().UnixNano() {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := pathRows(ctx, tx, d.Path)
	if err != nil {
		return err
	}
	chosen, found, _ := choosePathRow(rows)
	id := chosen.id
	createdAt := d.CreatedAt.Format(time.RFC3339Nano)
	updatedAt := d.UpdatedAt.Format(time.RFC3339Nano)
	if !found {
		id, err = randomID()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,scan_run_id,created_at,updated_at) VALUES(?,?,?,?,?,'available',?,?,?,?,?,?)`, id, d.Path, d.Format, d.Size, d.MTimeNS, d.SHA256, d.Title, d.PageCount, runID, createdAt, updatedAt); err != nil {
			return err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=COALESCE(missing_at,?),updated_at=? WHERE archive_path=? AND status='available' AND id<>?`, updatedAt, updatedAt, d.Path, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE documents SET format=?,size=?,mtime_ns=?,status='available',content_sha256=?,title=?,page_count=?,scan_run_id=?,missing_at=NULL,updated_at=CASE WHEN size<>? OR mtime_ns<>? OR COALESCE(content_sha256,'')<>? OR status<>'available' THEN ? ELSE updated_at END WHERE id=?`, d.Format, d.Size, d.MTimeNS, d.SHA256, d.Title, d.PageCount, runID, d.Size, d.MTimeNS, d.SHA256, updatedAt, id)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, id); err != nil {
		return err
	}
	if err = insertPageIndex(ctx, tx, id, d.Pages); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.applyArchiveClaim(ctx, id, d)
}

func insertPageIndex(ctx context.Context, tx *sql.Tx, id string, pages []PageInfo) error {
	for _, page := range pages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO document_pages(document_id,page_index,central_index,entry_name,mime,size)VALUES(?,?,?,?,?,?)`, id, page.Index, page.CentralIndex, page.EntryName, page.MIME, page.Size); err != nil {
			return err
		}
	}
	return nil
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
	var occupied int
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE publication_id=? AND id<>?)`, pubID, id).Scan(&occupied); e != nil {
		return e
	}
	if occupied != 0 {
		_, e = tx.ExecContext(ctx, `UPDATE documents SET association_source='manual',association_evidence='archive_metadata_conflict' WHERE id=? AND publication_id IS NULL AND association_source IS NULL`, id)
		if e != nil {
			return e
		}
		return tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE documents SET publication_id=?,association_source='archive_metadata',association_evidence=? WHERE id=? AND publication_id IS NULL AND association_source IS NULL`, pubID, d.ClaimEvidence, id); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) upsertInvalid(ctx context.Context, d Document, runID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := pathRows(ctx, tx, d.Path)
	if err != nil {
		return err
	}
	chosen, found, _ := choosePathRow(rows)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if found {
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=COALESCE(missing_at,?),updated_at=? WHERE archive_path=? AND status='available' AND id<>?`, now, now, d.Path, chosen.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET format=?,size=?,mtime_ns=?,status='invalid',content_sha256='',title=?,page_count=0,scan_run_id=?,missing_at=NULL,updated_at=? WHERE id=?`, d.Format, d.Size, d.MTimeNS, d.Title, runID, now, chosen.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, chosen.id); err != nil {
			return err
		}
		return tx.Commit()
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO documents(id,archive_path,format,size,mtime_ns,status,content_sha256,title,page_count,scan_run_id,created_at,updated_at)VALUES(?,?,?,?,?,'invalid','',?,0,?,?,?)`, id, d.Path, d.Format, d.Size, d.MTimeNS, d.Title, runID, now, now); err != nil {
		return err
	}
	return tx.Commit()
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
		query := `SELECT d.id,COALESCE(NULLIF(p.title,''),d.title),d.archive_path,d.format,d.status,d.size,d.mtime_ns,d.content_sha256,d.page_count,d.publication_id,d.association_source,d.created_at,d.updated_at,d.missing_at,p.gid,p.token,d.progression_target,d.progression_gid,d.progression_token FROM documents d LEFT JOIN publications p ON p.id=d.publication_id` + queryWhere + ` ORDER BY d.created_at DESC,d.id LIMIT ?`
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
		reason := "io_error"
		if errors.Is(e, ErrNotFound) {
			reason = "missing_record"
		}
		return d, &ValidationError{Reason: reason, Phase: "load", Cause: e}
	}
	if d.Status == "missing" {
		rows, err := pathRows(ctx, s.db, d.Path)
		if err != nil {
			return d, &ValidationError{Reason: "io_error", Phase: "canonical_lookup", Path: d.Path, Cause: err}
		}
		if canonical, ok, _ := choosePathRow(rows); ok && canonical.status == "available" && canonical.id != d.ID {
			d, e = s.ListByID(ctx, canonical.id)
			if e != nil {
				return d, &ValidationError{Reason: "io_error", Phase: "canonical_load", Path: d.Path, Cause: e}
			}
		}
	}
	if d.Status == "available" {
		if e = s.verify(ctx, d); e != nil {
			return d, e
		}
	}
	return d, nil
}

func (s *Store) verify(ctx context.Context, d Document) error {
	if err := ctx.Err(); err != nil {
		return &ValidationError{Reason: "io_error", Phase: "stat", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: err}
	}
	st, e := secureStat(s.root, d.Path)
	if e != nil {
		reason := "io_error"
		switch {
		case errors.Is(e, os.ErrNotExist):
			reason = "file_missing"
		case strings.Contains(e.Error(), "symlink") || strings.Contains(e.Error(), "invalid archive path") || strings.Contains(e.Error(), "not a real directory"):
			reason = "unsafe_path"
		}
		return &ValidationError{Reason: reason, Phase: "stat", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: e}
	}
	if !st.Mode().IsRegular() {
		return &ValidationError{Reason: "file_missing", Phase: "stat", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: ErrInvalidArchive}
	}
	return nil
}

// EnsurePageIndex lazily opens and inspects a candidate only when a client
// explicitly requests its manifest or a page. It never computes a container
// hash; the stat signature is checked before and after inspection.
func (s *Store) EnsurePageIndex(ctx context.Context, id string) (Document, []PageInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.ListByID(ctx, id)
	if err != nil {
		reason := "io_error"
		if errors.Is(err, ErrNotFound) {
			reason = "missing_record"
		}
		return Document{}, nil, &ValidationError{Reason: reason, Phase: "load", Cause: err}
	}
	rows, err := pathRows(ctx, s.db, d.Path)
	if err != nil {
		return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "canonical_lookup", Path: d.Path, Cause: err}
	}
	if canonical, ok, _ := choosePathRow(rows); ok && canonical.id != id {
		id = canonical.id
		d, err = s.ListByID(ctx, id)
		if err != nil {
			return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "canonical_load", Path: d.Path, Cause: err}
		}
	}
	if d.Status != "available" && d.Status != "invalid" && d.Status != "missing" {
		return Document{}, nil, &ValidationError{Reason: "record_unavailable", Phase: "record", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS}
	}
	pages, err := s.pageIndex(ctx, id)
	if err != nil {
		return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "page_index_cache", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: err}
	}
	f, err := s.openPayload(d.Path)
	if err != nil {
		return Document{}, nil, &ValidationError{Reason: validationReasonForPathError(err), Phase: "open", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: err}
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "stat_open_file", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: err}
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxArchiveBytes {
		return Document{}, nil, &ValidationError{Reason: "invalid_archive", Phase: "open", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: ErrInvalidArchive}
	}
	currentMTime := before.ModTime().UnixNano()
	indexMatchesCurrent := d.Size == before.Size() && d.MTimeNS == currentMTime && d.PageCount > 0 && len(pages) == d.PageCount
	if indexMatchesCurrent {
		tx, txErr := s.db.BeginTx(ctx, nil)
		if txErr != nil {
			return Document{}, nil, txErr
		}
		defer tx.Rollback()
		if _, txErr = tx.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=COALESCE(missing_at,?) WHERE archive_path=? AND status='available' AND id<>?`, time.Now().UTC().Format(time.RFC3339Nano), d.Path, id); txErr != nil {
			return Document{}, nil, txErr
		}
		if _, txErr = tx.ExecContext(ctx, `UPDATE documents SET status='available',missing_at=NULL WHERE id=?`, id); txErr != nil {
			return Document{}, nil, txErr
		}
		if txErr = tx.Commit(); txErr != nil {
			return Document{}, nil, txErr
		}
		d.Status, d.MissingAt = "available", nil
		return d, pages, nil
	}
	zr, err := s.newZipReader(f, before.Size())
	if err != nil {
		if errors.Is(err, zip.ErrFormat) {
			return s.invalidateUnreadable(ctx, d, "invalid_archive", err)
		}
		return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "zip_directory", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: fmt.Errorf("%w: %w", ErrTransientRead, err)}
	}
	pages, err = readablePages(ctx, zr)
	if err != nil {
		if ctx.Err() != nil {
			return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "page_index", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: ctx.Err()}
		}
		return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "page_index", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: fmt.Errorf("%w: %w", ErrTransientRead, err)}
	}
	if len(pages) == 0 {
		return s.invalidateUnreadable(ctx, d, "invalid_archive", ErrInvalidArchive)
	}
	after, err := f.Stat()
	if err != nil {
		return Document{}, nil, &ValidationError{Reason: "io_error", Phase: "stat_after_index", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: err}
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		size, mtime := after.Size(), after.ModTime().UnixNano()
		return Document{}, nil, &ValidationError{Reason: "stale_signature", Phase: "page_index", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, CurrentSize: &size, CurrentMTime: &mtime, Cause: errors.New("archive changed while page index was read; retry the read")}
	}
	claimGID, claimToken := archiveIdentity(ctx, zr)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, id); err != nil {
		return Document{}, nil, err
	}
	if err = insertPageIndex(ctx, tx, id, pages); err != nil {
		return Document{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE documents SET status='missing',missing_at=COALESCE(missing_at,?) WHERE archive_path=? AND status='available' AND id<>?`, time.Now().UTC().Format(time.RFC3339Nano), d.Path, id); err != nil {
		return Document{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE documents SET size=?,mtime_ns=?,content_sha256=CASE WHEN size<>? OR mtime_ns<>? THEN '' ELSE content_sha256 END,page_count=?,status='available',missing_at=NULL,updated_at=CASE WHEN size<>? OR mtime_ns<>? OR page_count=0 OR status<>'available' THEN ? ELSE updated_at END WHERE id=?`, before.Size(), currentMTime, before.Size(), currentMTime, len(pages), before.Size(), currentMTime, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return Document{}, nil, err
	}
	if err = tx.Commit(); err != nil {
		return Document{}, nil, err
	}
	if claimGID > 0 && claimToken != "" {
		if err = s.applyArchiveClaim(ctx, id, Document{ClaimGID: claimGID, ClaimToken: claimToken, ClaimEvidence: "archive_metadata_url"}); err != nil {
			return Document{}, nil, err
		}
	}
	d, err = s.ListByID(ctx, id)
	if err != nil {
		return Document{}, nil, err
	}
	return d, pages, nil
}

func (s *Store) invalidateUnreadable(ctx context.Context, d Document, reason string, cause error) (Document, []PageInfo, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE documents SET status='invalid',page_count=0,updated_at=? WHERE id=? AND size=? AND mtime_ns=?`, time.Now().UTC().Format(time.RFC3339Nano), d.ID, d.Size, d.MTimeNS); err != nil {
		return Document{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, d.ID); err != nil {
		return Document{}, nil, err
	}
	if err = tx.Commit(); err != nil {
		return Document{}, nil, err
	}
	return Document{}, nil, &ValidationError{Reason: reason, Phase: "zip_directory", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: cause}
}

func validationReasonForPathError(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "file_missing"
	}
	if strings.Contains(err.Error(), "symlink") || strings.Contains(err.Error(), "invalid archive path") || strings.Contains(err.Error(), "not a real directory") {
		return "unsafe_path"
	}
	return "io_error"
}

func (s *Store) SetDeleteHooks(before, rollback func(Document) error) {
	s.beforeDelete = before
	s.rollbackDelete = rollback
}

func (s *Store) Associate(ctx context.Context, id string, gid int, token string) error {
	if gid == 0 && token == "" {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var pub sql.NullInt64
		var gidv sql.NullInt64
		var tok sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT d.publication_id,p.gid,p.token FROM documents d LEFT JOIN publications p ON p.id=d.publication_id WHERE d.id=?`, id).Scan(&pub, &gidv, &tok); err == sql.ErrNoRows {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		evidence := "user_unlinked"
		if gidv.Valid {
			evidence = fmt.Sprintf("user_unlinked:%d:%s", gidv.Int64, strings.ToLower(tok.String))
		}
		query := `UPDATE documents SET publication_id=NULL,association_source='manual',association_evidence=?,updated_at=? WHERE id=? AND publication_id IS NULL`
		args := []any{evidence, time.Now().UTC().Format(time.RFC3339Nano), id}
		if pub.Valid {
			query = `UPDATE documents SET publication_id=NULL,association_source='manual',association_evidence=?,updated_at=? WHERE id=? AND publication_id=?`
			args = append(args, pub.Int64)
		}
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrAssociationConflict
		}
		return tx.Commit()
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
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE id=?)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO publications(provider,gid,token,metadata_json,created_at,updated_at) VALUES('eh',?,?,'{}',?,?) ON CONFLICT(provider,gid,token) DO UPDATE SET updated_at=excluded.updated_at`, gid, token, now, now); err != nil {
		return err
	}
	var pubID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM publications WHERE provider='eh' AND gid=? AND token=?`, gid, token).Scan(&pubID); err != nil {
		return err
	}
	var occupied int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE publication_id=? AND id<>?)`, pubID, id).Scan(&occupied); err != nil {
		return err
	}
	if occupied != 0 {
		return ErrAssociationConflict
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

func (s *Store) UnlinkGallery(ctx context.Context, gid int, token string) (string, error) {
	token = strings.ToLower(token)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var pub int64
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT p.id,d.id FROM publications p JOIN documents d ON d.publication_id=p.id WHERE p.provider='eh' AND p.gid=? AND lower(p.token)=?`, gid, token).Scan(&pub, &id); err == sql.ErrNoRows {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	evidence := fmt.Sprintf("user_unlinked:%d:%s", gid, token)
	res, err := tx.ExecContext(ctx, `UPDATE documents SET publication_id=NULL,association_source='manual',association_evidence=?,updated_at=? WHERE id=? AND publication_id=?`, evidence, time.Now().UTC().Format(time.RFC3339Nano), id, pub)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return "", ErrAssociationConflict
	}
	return id, tx.Commit()
}

func (s *Store) ListByID(ctx context.Context, id string) (Document, error) {
	var d Document
	var created, updated string
	var src, missing sql.NullString
	var gid sql.NullInt64
	var token sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT d.id,COALESCE(NULLIF(p.title,''),d.title),d.archive_path,d.format,d.status,d.size,d.mtime_ns,d.content_sha256,d.page_count,d.publication_id,d.association_source,d.created_at,d.updated_at,d.missing_at,p.gid,p.token,d.progression_target,d.progression_gid,d.progression_token FROM documents d LEFT JOIN publications p ON p.id=d.publication_id WHERE d.id=?`, id).Scan(&d.ID, &d.Title, &d.Path, &d.Format, &d.Status, &d.Size, &d.MTimeNS, &d.SHA256, &d.PageCount, new(sql.NullInt64), &src, &created, &updated, &missing, &gid, &token, &d.ProgressionTarget, &d.ProgressionGID, &d.ProgressionToken)
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

// AssociatedDocument returns the unique document associated with this exact EH identity.
func (s *Store) AssociatedDocument(ctx context.Context, gid int, token string) (Document, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT d.id FROM documents d JOIN publications p ON p.id=d.publication_id WHERE p.provider='eh' AND p.gid=? AND lower(p.token)=lower(?)`, gid, token).Scan(&id)
	if err == sql.ErrNoRows {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) ManagedArchiveAllowed(ctx context.Context, rel string, gid int, token string) bool {
	var linked int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM documents d LEFT JOIN publications p ON p.id=d.publication_id WHERE d.archive_path=? AND (d.status<>'available' OR p.id IS NULL OR p.provider<>'eh' OR p.gid<>? OR lower(p.token)<>lower(?)))`, filepath.ToSlash(rel), gid, token).Scan(&linked)
	if err != nil {
		return false
	}
	return linked == 0
}

func (s *Store) OpenPage(ctx context.Context, id string, page int) ([]byte, string, error) {
	for attempt := 0; attempt < 2; attempt++ {
		d, pages, e := s.EnsurePageIndex(ctx, id)
		if e != nil {
			return nil, "", e
		}
		id = d.ID
		if page < 0 || page >= d.PageCount || page >= len(pages) {
			return nil, "", &ValidationError{Reason: "page_out_of_range", Phase: "page_lookup", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS}
		}
		f, e := s.openPayload(d.Path)
		if e != nil {
			return nil, "", &ValidationError{Reason: validationReasonForPathError(e), Phase: "open_page", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: e}
		}
		st, e := f.Stat()
		if e != nil {
			_ = f.Close()
			return nil, "", &ValidationError{Reason: "io_error", Phase: "stat_page", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: e}
		}
		if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > maxArchiveBytes {
			_ = f.Close()
			return nil, "", &ValidationError{Reason: "invalid_archive", Phase: "open_page", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: ErrInvalidArchive}
		}
		if st.Size() != d.Size || st.ModTime().UnixNano() != d.MTimeNS {
			_ = f.Close()
			if attempt == 0 {
				if e = s.clearPageIndex(ctx, id); e != nil {
					return nil, "", e
				}
				continue
			}
			size, mtime := st.Size(), st.ModTime().UnixNano()
			return nil, "", &ValidationError{Reason: "stale_signature", Phase: "open_page", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, CurrentSize: &size, CurrentMTime: &mtime, Cause: errors.New("archive changed while opening page; retry")}
		}
		info := pages[page]
		zr, e := s.newZipReader(f, st.Size())
		if e != nil {
			_ = f.Close()
			if errors.Is(e, zip.ErrFormat) {
				return nil, "", &ValidationError{Reason: "invalid_archive", Phase: "zip_directory", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: e}
			}
			return nil, "", &ValidationError{Reason: "io_error", Phase: "zip_directory", Path: d.Path, StoredSize: d.Size, StoredMTime: d.MTimeNS, Cause: fmt.Errorf("%w: %w", ErrTransientRead, e)}
		}
		var zf *zip.File
		if info.CentralIndex >= 0 && info.CentralIndex < len(zr.File) {
			zf = zr.File[info.CentralIndex]
		}
		if zf == nil || zf.Name != info.EntryName || zf.UncompressedSize64 != info.Size {
			_ = f.Close()
			if attempt == 0 {
				if e = s.clearPageIndex(ctx, id); e != nil {
					return nil, "", e
				}
				continue
			}
			return nil, "", &ValidationError{Reason: "io_error", Phase: "page_index_changed", Path: d.Path, Cause: errors.New("archive changed while opening page; retry")}
		}
		rc, e := zf.Open()
		if e != nil {
			_ = f.Close()
			return nil, "", &ValidationError{Reason: "io_error", Phase: "open_page_member", Path: d.Path, Cause: fmt.Errorf("%w: %w", ErrTransientRead, e)}
		}
		b, e := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, r: rc}, int64(maxImageBytes)+1))
		_ = rc.Close()
		if e != nil {
			_ = f.Close()
			return nil, "", &ValidationError{Reason: "io_error", Phase: "read_page_member", Path: d.Path, Cause: fmt.Errorf("%w: %w", ErrTransientRead, e)}
		}
		if len(b) > int(maxImageBytes) {
			_ = f.Close()
			return nil, "", errors.New("image page exceeds size limit")
		}
		after, statErr := f.Stat()
		_ = f.Close()
		if statErr != nil {
			return nil, "", statErr
		}
		if after.Size() != st.Size() || after.ModTime().UnixNano() != st.ModTime().UnixNano() {
			if attempt == 0 {
				_ = s.clearPageIndex(ctx, id)
				continue
			}
			return nil, "", &ValidationError{Reason: "stale_signature", Phase: "read_page_member", Path: d.Path, Cause: errors.New("archive changed while reading page; retry")}
		}
		return b, info.MIME, nil
	}
	return nil, "", &ValidationError{Reason: "io_error", Phase: "open_page", Cause: ErrTransientRead}
}

func (s *Store) clearPageIndex(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE documents SET page_count=0 WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
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
			return nil, fmt.Errorf("open image member %q: %w", zf.Name, err)
		}
		cfg, format, err := image.DecodeConfig(io.LimitReader(contextReader{ctx: ctx, r: rc}, int64(maxImageBytes)))
		_ = rc.Close()
		if err != nil {
			// Decode failures other than a format mismatch can be caused by a
			// transient read from a mounted archive. Do not publish a partial
			// index or persist invalid status in that case.
			if errors.Is(err, image.ErrFormat) {
				continue
			}
			return nil, fmt.Errorf("decode image member %q: %w", zf.Name, err)
		}
		if cfg.Width > 0 && cfg.Height > 0 {
			valid = append(valid, PageInfo{Index: len(valid), CentralIndex: indexed.index, EntryName: zf.Name, MIME: mimeForFormat(format), Size: zf.UncompressedSize64})
		} else {
			return nil, fmt.Errorf("decode image member %q: invalid dimensions", zf.Name)
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
	_, pages, e := s.EnsurePageIndex(ctx, id)
	if e != nil {
		return "", e
	}
	if page < 0 || page >= len(pages) {
		return "", ErrNotFound
	}
	return pages[page].MIME, nil
}

// CachedPageMIME reports a MIME from an already persisted page index without
// opening the archive. Catalog builders use it to preserve the no-payload-I/O
// contract for summary feeds.
func (s *Store) CachedPageMIME(ctx context.Context, id string, page int) (string, bool, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return "", false, err
	}
	id = d.ID
	var mime string
	err = s.db.QueryRowContext(ctx, `SELECT p.mime FROM document_pages p JOIN documents d ON d.id=p.document_id WHERE p.document_id=? AND p.page_index=? AND d.page_count>0 AND (SELECT COUNT(*) FROM document_pages WHERE document_id=d.id)=d.page_count`, id, page).Scan(&mime)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return mime, true, nil
}

func (s *Store) PageMIMEs(ctx context.Context, id string) ([]string, error) {
	_, pages, e := s.EnsurePageIndex(ctx, id)
	if e != nil {
		return nil, e
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
	d, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	id = d.ID
	var minMIME, maxMIME string
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(mime),MAX(mime),COUNT(*) FROM document_pages WHERE document_id=?`, id).Scan(&minMIME, &maxMIME, &count); err != nil {
		return "", err
	}
	if minMIME == "" || d.PageCount <= 0 || count != d.PageCount {
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
	d, err := s.Get(ctx, id)
	if err != nil {
		return "", false, err
	}
	id = d.ID
	var mime string
	err = s.db.QueryRowContext(ctx, `SELECT c.mime FROM documents d JOIN covers c ON c.publication_id=d.publication_id WHERE d.id=?`, id).Scan(&mime)
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
	id = d.ID
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
	if d.Status == "missing" {
		return s.PurgeMissing(ctx, id)
	}
	if d.Status != "available" && d.Status != "invalid" {
		return ErrNotFound
	}
	id = d.ID
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
	if d.SHA256 != "" {
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			_ = f.Close()
			return e
		}
		h := sha256.New()
		if _, e = io.Copy(h, contextReader{ctx: ctx, r: f}); e != nil {
			_ = f.Close()
			return e
		}
		if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), d.SHA256) {
			_ = f.Close()
			return errors.New("document content changed; refusing deletion")
		}
		after, statErr := f.Stat()
		if statErr != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
			_ = f.Close()
			return errors.New("document changed during deletion verification")
		}
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

func secureStat(root, rel string) (os.FileInfo, error) {
	r, err := openArchiveRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, errors.New("invalid archive path")
	}
	cur := ""
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		cur = filepath.Join(cur, part)
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
		if i == len(parts)-1 {
			return fi, nil
		}
	}
	return nil, errors.New("invalid archive path")
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
