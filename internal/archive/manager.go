package archive

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bodgit/sevenzip"
	xhtml "golang.org/x/net/html"

	"pandaopds/internal/config"
	"pandaopds/internal/eh"
)

type Manager struct {
	Settings config.Settings
	Client   *eh.Client
	Service  *eh.Service
	Store    *Store
	slots    chan struct{}
	mu       sync.Mutex
	startMu  sync.Mutex
	wg       sync.WaitGroup
	jobs     map[string]context.CancelFunc
	jobDone  map[string]chan struct{}
	jobRoots map[string]string
	progress map[string]int64
	ctx      context.Context
	cancel   context.CancelFunc
}

type ArchiverUnavailableError struct{ Detail string }

func (e *ArchiverUnavailableError) Error() string { return e.Detail }

type VersionUpdateConflictError struct{ Detail string }

func (e *VersionUpdateConflictError) Error() string { return e.Detail }

func NewManager(cfg config.Settings, c *eh.Client, s *eh.Service, store *Store) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	n := cfg.ArchiveDownloadConcurrency
	if n < 1 {
		n = 5
	}
	return &Manager{Settings: cfg, Client: c, Service: s, Store: store, slots: make(chan struct{}, n), jobs: map[string]context.CancelFunc{}, jobDone: map[string]chan struct{}{}, jobRoots: map[string]string{}, progress: map[string]int64{}, ctx: ctx, cancel: cancel}
}
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	for _, c := range m.jobs {
		c()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Manager) HasIPB() bool { return m.Settings.HasIPB() }

func (m *Manager) stagingRoot() string {
	dir := strings.TrimSpace(m.Settings.CacheDir)
	if dir == "" {
		dir = "./cache"
	}
	return filepath.Join(dir, eh.ArchiveStagingDirName)
}

func (m *Manager) stagingPart(gid int, token string) string {
	return filepath.Join(m.stagingRoot(), strconv.Itoa(gid), token, partName)
}

// adoptLegacyPart preserves resumable downloads created before archive staging
// was moved out of ARCHIVE_DIR.
func (m *Manager) adoptLegacyPart(ctx context.Context, gid int, token string) (string, error) {
	part := m.stagingPart(gid, token)
	if _, err := os.Stat(part); err == nil {
		return part, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	rootGID, rootToken, managed := m.Store.RootIdentity(gid, token)
	if managed && (rootGID != gid || rootToken != token) {
		return part, nil
	}
	legacy := filepath.Join(m.Store.dir(gid, token), partName)
	if _, err := os.Stat(legacy); err == nil {
		if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
			return "", err
		}
		tmp := part + ".migrate.tmp"
		_ = os.Remove(tmp)
		if err := copyFileContext(ctx, legacy, tmp); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
		if err := os.Rename(tmp, part); err != nil {
			_ = os.Remove(tmp)
			return "", err
		}
		if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return part, nil
}

type option struct {
	Or         string `json:"or"`
	Label      string `json:"label"`
	DLCheck    string `json:"-"`
	GPPrice    int    `json:"gp_price"`
	Size       string `json:"size"`
	Available  bool   `json:"available"`
	Unlocked   bool   `json:"unlocked"`
	priceKnown bool
}
type archiverPage struct {
	Title             string
	Options           []option
	State, URL, Error string
	GP                int
}

func parseArchiver(body, pageURL string) archiverPage {
	doc, _ := xhtml.Parse(strings.NewReader(body))
	txt := nodeText(doc)
	p := archiverPage{}
	for _, tag := range []string{"h1", "h3"} {
		if n := firstNode(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == tag }); n != nil {
			p.Title = nodeText(n)
			if p.Title != "" {
				break
			}
		}
	}
	if x := regexp.MustCompile(`(?i)(?:GP|Funds):\s*([\d,]+)`).FindStringSubmatch(txt); len(x) > 1 {
		p.GP, _ = strconv.Atoi(strings.ReplaceAll(x[1], ",", ""))
	}
	for _, marker := range []struct{ s, msg string }{{"Star membership", "archiver requires Star membership"}, {"Insufficient GP", "Insufficient GP for archive"}, {"cannot be archived", "gallery cannot be archived"}} {
		if strings.Contains(strings.ToLower(txt), strings.ToLower(marker.s)) {
			p.Error = marker.msg
			break
		}
	}
	lower := strings.ToLower(txt)
	if strings.Contains(lower, "successfully prepared") {
		p.State = "ready"
	} else if strings.Contains(lower, "being prepared") || strings.Contains(lower, "preparing") || strings.Contains(lower, "please wait") {
		p.State = "preparing"
	}
	for _, a := range allNodes(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "a" }) {
		href := attr(a, "href")
		if strings.Contains(href, "?start=") {
			p.URL = resolve(href, pageURL)
			break
		}
		if strings.Contains(href, "hath.network") {
			p.URL = href
			break
		}
	}
	unlocked := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?i)You unlocked\s+(.+?)\s+download`).FindAllStringSubmatch(txt, -1) {
		label := strings.ToLower(match[1])
		if strings.Contains(label, "original") {
			unlocked["org"] = true
		} else if strings.Contains(label, "resample") {
			unlocked["res"] = true
		}
	}
	seen := map[string]bool{}
	for _, form := range allNodes(doc, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "form" }) {
		if !strings.Contains(strings.ToLower(attr(form, "action")), "archiver.php") {
			continue
		}
		var dt, check string
		var submit *xhtml.Node
		for _, in := range allNodes(form, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "input" }) {
			switch attr(in, "name") {
			case "dltype":
				dt = attr(in, "value")
			case "dlcheck":
				check = attr(in, "value")
				submit = in
			}
		}
		if dt == "" || seen[dt] {
			continue
		}
		seen[dt] = true
		container := tierContainer(form)
		costText := labeledRow(container, "download cost")
		price, priceKnown := parseTierPrice(costText)
		sizeText := labeledRow(container, "estimated size")
		size := ""
		if x := regexp.MustCompile(`(?i)Estimated\s+Size:\s*([\d.]+\s*(?:KiB|MiB|GiB|TiB|KB|MB|GB))`).FindStringSubmatch(sizeText); len(x) > 1 {
			size = strings.TrimSpace(x[1])
		}
		label := strings.TrimSpace(strings.TrimPrefix(check, "Download "))
		p.Options = append(p.Options, option{Or: dt, Label: label, DLCheck: check, GPPrice: price, Size: size, Available: !hasAttr(submit, "disabled"), Unlocked: unlocked[dt], priceKnown: priceKnown})
	}
	return p
}

var (
	gpCostRE = regexp.MustCompile(`(?i)^([\d,]+)\s*GP\b`)
)

// tierContainer walks only the nearest ancestor which contains this tier's
// cost row and no other archiver form. This keeps a neighboring tier's price
// (or the page-wide GP balance) out of the quote.
func tierContainer(form *xhtml.Node) *xhtml.Node {
	for n := form.Parent; n != nil; n = n.Parent {
		if countArchiverForms(n) > 1 {
			break
		}
		if labeledRow(n, "download cost") != "" {
			return n
		}
	}
	return form.Parent
}

func countArchiverForms(n *xhtml.Node) int {
	count := 0
	for _, f := range allNodes(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode && x.Data == "form" && strings.Contains(strings.ToLower(attr(x, "action")), "archiver.php")
	}) {
		if firstNode(f, func(x *xhtml.Node) bool {
			return x.Type == xhtml.ElementNode && x.Data == "input" && attr(x, "name") == "dltype"
		}) != nil {
			count++
		}
	}
	return count
}

func labeledRow(container *xhtml.Node, label string) string {
	if container == nil {
		return ""
	}
	var best string
	for _, n := range allNodes(container, func(x *xhtml.Node) bool { return x.Type == xhtml.ElementNode }) {
		text := strings.Join(strings.Fields(nodeText(n)), " ")
		if !strings.HasPrefix(strings.ToLower(text), label+":") {
			continue
		}
		// Prefer the tightest node containing the label, avoiding a wrapper
		// that also contains unrelated GP text or another row.
		if best == "" || len(text) < len(best) {
			best = text
		}
	}
	return best
}

func parseTierPrice(costText string) (int, bool) {
	i := strings.Index(strings.ToLower(costText), "download cost:")
	if i < 0 {
		return 0, false
	}
	value := strings.TrimSpace(costText[i+len("download cost:"):])
	if regexp.MustCompile(`(?i)^free!?$`).MatchString(value) {
		return 0, true
	}
	if m := gpCostRE.FindStringSubmatch(value); len(m) > 1 {
		price, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
		return price, err == nil
	}
	return 0, false
}

func hasAttr(n *xhtml.Node, key string) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
func attr(n *xhtml.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}
func nodeText(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x == nil {
			return
		}
		if x.Type == xhtml.TextNode {
			b.WriteString(x.Data)
			b.WriteByte(' ')
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
func firstNode(n *xhtml.Node, pred func(*xhtml.Node) bool) *xhtml.Node {
	if n == nil {
		return nil
	}
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if x := firstNode(c, pred); x != nil {
			return x
		}
	}
	return nil
}
func allNodes(n *xhtml.Node, pred func(*xhtml.Node) bool) []*xhtml.Node {
	out := []*xhtml.Node{}
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x == nil {
			return
		}
		if pred(x) {
			out = append(out, x)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}
func resolve(href, page string) string {
	u, e := url.Parse(href)
	if e != nil {
		return href
	}
	b, e := url.Parse(page)
	if e != nil {
		return href
	}
	return b.ResolveReference(u).String()
}
func (m *Manager) archiverURL(gid int, token string) string {
	u, _ := url.Parse(m.Client.BaseURL() + "/archiver.php")
	q := u.Query()
	q.Set("gid", strconv.Itoa(gid))
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}
func (m *Manager) fetchPage(ctx context.Context, gid int, token string) (archiverPage, error) {
	u := m.archiverURL(gid, token)
	b, _, e := m.Client.Get(ctx, u, eh.KindHTML)
	if e != nil {
		return archiverPage{}, e
	}
	p := parseArchiver(string(b), u)
	if p.Error != "" {
		return p, fmt.Errorf("%s", p.Error)
	}
	return p, nil
}
func (m *Manager) Quote(ctx context.Context, gid int, token string) (map[string]any, error) {
	if !m.HasIPB() {
		return nil, &ArchiverUnavailableError{Detail: "archiver requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"}
	}
	p, e := m.fetchPage(ctx, gid, token)
	if e != nil {
		return nil, e
	}
	opts := []map[string]any{}
	for _, o := range p.Options {
		opts = append(opts, map[string]any{"or": o.Or, "label": o.Label, "gp_price": o.GPPrice, "size": o.Size, "available": o.Available, "unlocked": o.Unlocked})
	}
	return map[string]any{"gid": gid, "token": token, "title": p.Title, "gp_balance": p.GP, "options": opts, "download_state": p.State, "download_url": p.URL}, nil
}
func (m *Manager) match(opts []option, quality string) option {
	if len(opts) == 0 {
		return option{}
	}
	q := strings.ToLower(strings.TrimSpace(quality))
	if q == "" {
		q = m.Settings.ArchiveQuality
	}
	alias := map[string]string{"original": "org", "resample": "res", "resized": "res"}
	if x := alias[q]; x != "" {
		q = x
	}
	for _, o := range opts {
		if strings.ToLower(o.Or) == q {
			return o
		}
	}
	for _, o := range opts {
		if strings.Contains(strings.ToLower(o.Label), strings.ToLower(quality)) {
			return o
		}
	}
	for _, o := range opts {
		if o.Available {
			return o
		}
	}
	return opts[0]
}
func (m *Manager) Start(ctx context.Context, gid int, token, quality string, force bool) (map[string]any, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if rootGID, rootToken, ok := m.Store.RootIdentity(gid, token); ok && rootGID == gid && rootToken == token {
		gid, token = m.Store.ResolveActive(gid, token)
	}
	if !m.HasIPB() {
		return nil, &ArchiverUnavailableError{Detail: "archiver requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"}
	}
	k := key(gid, token)
	m.mu.Lock()
	active := m.jobs[k] != nil
	m.mu.Unlock()
	if active {
		return m.Status(gid, token), nil
	}
	old := m.Store.Get(gid, token)
	if !force && old != nil {
		st := fmt.Sprint(old["status"])
		if st == "pending" || st == "downloading" || st == "zipping" {
			return m.Status(gid, token), nil
		}
		if st == "ready" {
			v := m.Status(gid, token)
			v["skipped"] = true
			v["reason"] = "already_archived"
			return v, nil
		}
	}
	if err := m.Store.CanStart(gid, token); err != nil {
		return nil, err
	}
	page, e := m.fetchPage(ctx, gid, token)
	if e != nil {
		return nil, e
	}
	o := m.match(page.Options, quality)
	if o.Or == "" {
		return nil, errors.New("archiver offered no tiers for this gallery")
	}
	if !o.Available {
		return nil, fmt.Errorf("tier %q is not available for this gallery", o.Label)
	}
	if !o.priceKnown {
		return nil, fmt.Errorf("unable to determine the price for archive tier %q; refusing to submit", o.Label)
	}
	if err := os.MkdirAll(filepath.Dir(m.stagingPart(gid, token)), 0o755); err != nil {
		return nil, err
	}
	form := url.Values{"dltype": {o.Or}, "dlcheck": {o.DLCheck}}
	submitted, _, e := m.Client.PostFormOnce(ctx, m.archiverURL(gid, token), form, eh.KindHTML)
	if e != nil {
		var upstream *eh.UpstreamError
		if !errors.As(e, &upstream) {
			// The server may have accepted the POST before the connection failed.
			// Record the uncertain outcome and never replay the paid operation.
			_, _ = m.Store.Upsert(gid, token, map[string]any{"title": firstNonEmpty(page.Title, "Archive submission"), "quality": o.Label, "or": o.Or, "gp_price": o.GPPrice, "status": "failed", "error": "archive submit outcome uncertain; inspect upstream before retrying: " + e.Error()})
		}
		return nil, e
	}
	p := parseArchiver(string(submitted), m.archiverURL(gid, token))
	if p.Error != "" {
		return nil, errors.New(p.Error)
	}
	if p.URL == "" {
		return nil, errors.New("archive submission returned no status URL")
	}
	backup := map[string]any{}
	if old != nil && old["status"] == "ready" {
		for _, k := range []string{"or", "quality", "gp_price", "download_url", "page_count", "bytes", "total_bytes"} {
			if v, ok := old[k]; ok {
				backup[k] = v
			}
		}
		if !strings.EqualFold(fmt.Sprint(old["or"]), o.Or) {
			_ = os.Remove(m.stagingPart(gid, token))
			_ = os.Remove(filepath.Join(m.Store.dir(gid, token), partName))
		}
	}
	_, e = m.Store.Upsert(gid, token, map[string]any{"title": firstNonEmpty(p.Title, page.Title), "quality": o.Label, "or": o.Or, "gp_price": o.GPPrice, "download_url": p.URL, "status": "pending", "error": nil, "upgrade_backup": backup})
	if e != nil {
		return nil, e
	}
	taskCtx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.mu.Lock()
	m.jobs[k] = cancel
	m.jobDone[k] = done
	rootGID, rootToken, _ := m.Store.RootIdentity(gid, token)
	m.jobRoots[k] = key(rootGID, rootToken)
	m.mu.Unlock()
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.run(taskCtx, gid, token, done) }()
	_ = ctx
	return m.Status(gid, token), nil
}

// StartVersionUpdate submits an archive for the gdata current version while
// keeping the original shelf identity stable. maxGP is an optional UI quote
// cap; nil is reserved for configured background updates.
func (m *Manager) StartVersionUpdate(ctx context.Context, rootGID int, rootToken string, targetGID int, targetToken string, maxGP *int) (map[string]any, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	conflict := func(message string) (map[string]any, error) {
		return nil, &VersionUpdateConflictError{Detail: message}
	}
	if !m.HasIPB() {
		return nil, &ArchiverUnavailableError{Detail: "archiver requires IPB cookies (IPB_MEMBER_ID + IPB_PASS_HASH)"}
	}
	if currentGID, currentToken := m.Store.CurrentUpdate(rootGID, rootToken); currentGID != targetGID || currentToken != targetToken {
		return conflict("target is stale; refresh the gallery version links")
	}
	activeGID, activeToken := m.Store.ResolveActive(rootGID, rootToken)
	if existing := m.Store.Get(targetGID, targetToken); existing != nil && existing["is_version_update"] == true && existing["submit_attempted"] == true {
		if toInt(existing["root_gid"]) != rootGID || toString(existing["root_token"]) != rootToken || toInt(existing["source_gid"]) != activeGID || toString(existing["source_token"]) != activeToken {
			return conflict("a previous paid attempt exists for this target from a different active version")
		}
		if toString(existing["download_url"]) == "" {
			return conflict("a previous submission attempt has no reusable status URL; inspect the upstream archiver before taking any further action")
		}
		rootKey := key(rootGID, rootToken)
		jobKey := key(targetGID, targetToken)
		m.mu.Lock()
		if m.jobs[jobKey] == nil {
			busy := false
			for _, jobRoot := range m.jobRoots {
				if jobRoot == rootKey {
					busy = true
					break
				}
			}
			if !busy {
				taskCtx, cancel := context.WithCancel(m.ctx)
				done := make(chan struct{})
				m.jobs[jobKey], m.jobDone[jobKey], m.jobRoots[jobKey] = cancel, done, rootKey
				m.wg.Add(1)
				go func() { defer m.wg.Done(); m.run(taskCtx, targetGID, targetToken, done) }()
			}
		}
		m.mu.Unlock()
		return m.Status(rootGID, rootToken), nil
	}
	if err := m.Store.CanAddVersion(rootGID, rootToken, targetGID, targetToken); err != nil {
		return conflict(err.Error())
	}
	old := m.Store.Get(activeGID, activeToken)
	if old == nil || old["status"] != "ready" {
		return conflict("active archive is not ready")
	}
	tier := toString(old["or"])
	if tier == "" {
		return conflict("active archive tier is unknown")
	}
	rootKey := key(rootGID, rootToken)
	m.mu.Lock()
	for _, jobRoot := range m.jobRoots {
		if jobRoot == rootKey {
			m.mu.Unlock()
			return conflict("an archive task is already running for this shelf")
		}
	}
	m.mu.Unlock()
	page, err := m.fetchPage(ctx, targetGID, targetToken)
	if err != nil {
		return nil, err
	}
	var selected option
	for _, candidate := range page.Options {
		if strings.EqualFold(candidate.Or, tier) {
			selected = candidate
			break
		}
	}
	if selected.Or == "" {
		return conflict("active archive tier is unavailable for the target version")
	}
	if !selected.Available {
		return conflict("active archive tier is not available for the target version")
	}
	if !selected.priceKnown {
		return conflict("unable to determine the target archive price; refusing to submit")
	}
	chargedGP := selected.GPPrice
	if selected.Unlocked {
		chargedGP = 0
	}
	if maxGP != nil && chargedGP > *maxGP {
		return conflict("target archive price exceeds the confirmed GP limit")
	}
	if err := m.Store.CanAddVersion(rootGID, rootToken, targetGID, targetToken); err != nil {
		return conflict(err.Error())
	}
	if currentGID, currentToken := m.Store.CurrentUpdate(rootGID, rootToken); currentGID != targetGID || currentToken != targetToken {
		return conflict("target became stale before submission")
	}
	if err := os.MkdirAll(filepath.Dir(m.stagingPart(targetGID, targetToken)), 0o755); err != nil {
		return nil, err
	}
	// Persist the paid-operation attempt before POST. A restart or uncertain
	// response must never resubmit this target automatically.
	_, err = m.Store.Upsert(targetGID, targetToken, map[string]any{
		"is_version_update": true,
		"root_gid":          rootGID, "root_token": rootToken,
		"source_gid": activeGID, "source_token": activeToken,
		"submit_attempted": true, "submitted_at": float64(time.Now().Unix()),
		"title":   firstNonEmpty(page.Title, toString(old["title"])),
		"quality": selected.Label, "or": selected.Or, "gp_price": chargedGP,
		"status": "submitting", "error": nil,
	})
	if err != nil {
		return nil, err
	}
	form := url.Values{"dltype": {selected.Or}, "dlcheck": {selected.DLCheck}}
	submitted, _, err := m.Client.PostFormOnce(ctx, m.archiverURL(targetGID, targetToken), form, eh.KindHTML)
	if err != nil {
		_, _ = m.Store.Upsert(targetGID, targetToken, map[string]any{"status": "failed", "error": "archive update submission outcome uncertain; automatic retry suppressed: " + err.Error()})
		return nil, err
	}
	result := parseArchiver(string(submitted), m.archiverURL(targetGID, targetToken))
	if result.Error != "" {
		_, _ = m.Store.Upsert(targetGID, targetToken, map[string]any{"status": "failed", "error": result.Error})
		return nil, errors.New(result.Error)
	}
	if result.URL == "" {
		_, _ = m.Store.Upsert(targetGID, targetToken, map[string]any{"status": "failed", "error": "archive update submission returned no status URL; automatic retry suppressed"})
		return nil, errors.New("archive submission returned no status URL")
	}
	_, err = m.Store.Upsert(targetGID, targetToken, map[string]any{"title": firstNonEmpty(result.Title, page.Title), "download_url": result.URL, "status": "pending", "error": nil})
	if err != nil {
		return nil, err
	}
	jobKey := key(targetGID, targetToken)
	taskCtx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.mu.Lock()
	m.jobs[jobKey] = cancel
	m.jobDone[jobKey] = done
	m.jobRoots[jobKey] = rootKey
	m.mu.Unlock()
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.run(taskCtx, targetGID, targetToken, done) }()
	return m.Status(rootGID, rootToken), nil
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func (m *Manager) Status(gid int, token string) map[string]any {
	v := m.Store.Get(gid, token)
	if v == nil {
		return map[string]any{"gid": gid, "token": token, "status": "absent"}
	}
	k := key(gid, token)
	m.mu.Lock()
	rootGID, rootToken := toInt(v["root_gid"]), toString(v["root_token"])
	if rootGID <= 0 || rootToken == "" {
		rootGID, rootToken = gid, token
	}
	rootKey := key(rootGID, rootToken)
	active := m.jobs[k] != nil
	n := m.progress[k]
	if !active {
		for jobKey, jobRoot := range m.jobRoots {
			if jobRoot == rootKey {
				active = true
				if n == 0 {
					n = m.progress[jobKey]
				}
				break
			}
		}
	}
	m.mu.Unlock()
	for _, field := range []string{versionsName, "activation_pending", "upgrade_backup", "submit_attempted", "submitted_at", "source_gid", "source_token"} {
		delete(v, field)
	}
	v["active"] = active
	if n == 0 {
		n = toInt64(v["bytes"])
	}
	v["bytes_downloaded"] = n
	stageGID, stageToken := gid, token
	if update, ok := v["version_update"].(map[string]any); ok && active {
		if targetGID, targetToken := toInt(update["gid"]), toString(update["token"]); targetGID > 0 && targetToken != "" {
			stageGID, stageToken = targetGID, targetToken
		}
	}
	if st, err := os.Stat(m.stagingPart(stageGID, stageToken)); err == nil {
		v["part_size"] = st.Size()
	}
	return v
}
func (m *Manager) run(ctx context.Context, gid int, token string, done chan struct{}) {
	defer func() {
		m.mu.Lock()
		delete(m.jobs, key(gid, token))
		delete(m.progress, key(gid, token))
		delete(m.jobDone, key(gid, token))
		delete(m.jobRoots, key(gid, token))
		close(done)
		m.mu.Unlock()
	}()
	state := m.Store.Get(gid, token)
	if state == nil {
		return
	}
	rootGID, rootToken := toInt(state["root_gid"]), toString(state["root_token"])
	if rootGID <= 0 || rootToken == "" {
		rootGID, rootToken = gid, token
	}
	versionUpdate := state["is_version_update"] == true
	if versionUpdate && state["status"] == "ready" {
		m.activateVersionUpdate(ctx, rootGID, rootToken, gid, token)
		return
	}
	var snapshotErr error
	if !versionUpdate {
		_, snapshotErr = m.snapshot(ctx, gid, token, false)
	}
	if state["status"] != "zipping" {
		if e := m.awaitReady(ctx, gid, token); e != nil {
			if ctx.Err() != nil {
				return
			}
			m.fail(gid, token, e.Error())
			return
		}
	}
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-m.slots }()
	if state["status"] != "zipping" {
		if e := m.download(ctx, gid, token); e != nil {
			if ctx.Err() != nil {
				return
			}
			m.fail(gid, token, e.Error())
			return
		}
	}
	if e := m.finalize(ctx, gid, token); e != nil {
		_ = os.Remove(m.stagingPart(gid, token))
		if ctx.Err() != nil {
			return
		}
		m.fail(gid, token, e.Error())
		return
	}
	if versionUpdate {
		m.activateVersionUpdate(ctx, rootGID, rootToken, gid, token)
		return
	}
	if snapshotErr != nil {
		_, _ = m.snapshot(ctx, gid, token, true)
	}
}

func (m *Manager) activateVersionUpdate(ctx context.Context, rootGID int, rootToken string, gid int, token string) {
	snapshot, cover, hasCover, coverMime, err := m.captureSnapshot(ctx, gid, token, true)
	if err != nil {
		_, _ = m.Store.Upsert(gid, token, map[string]any{"activation_error": err.Error(), "error": err.Error()})
		return
	}
	snapshot["cover_mime"] = coverMime
	saved := toInt64(snapshot["saved_at"])
	if _, err = m.Store.Upsert(gid, token, map[string]any{"metadata_at": saved, "title": toString(snapshot["title"]), "activation_error": nil}); err != nil {
		_, _ = m.Store.Upsert(gid, token, map[string]any{"activation_error": err.Error()})
		return
	}
	if err = m.Store.ActivateVersion(rootGID, rootToken, gid, token, snapshot, cover, hasCover); err != nil {
		_, _ = m.Store.Upsert(gid, token, map[string]any{"activation_error": err.Error(), "error": err.Error()})
	}
}
func (m *Manager) awaitReady(ctx context.Context, gid int, token string) error {
	deadline := time.Now().Add(2 * time.Hour)
	for time.Now().Before(deadline) {
		meta := m.Store.Get(gid, token)
		if meta == nil {
			return errors.New("archive removed")
		}
		raw := fmt.Sprint(meta["download_url"])
		b, _, e := m.Client.Get(ctx, raw, eh.KindHTML)
		if e == nil {
			p := parseArchiver(string(b), raw)
			if p.Error != "" {
				return errors.New(p.Error)
			}
			if p.State == "ready" && p.URL != "" {
				_, e = m.Store.Upsert(gid, token, map[string]any{"download_url": p.URL})
				return e
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Second):
		}
	}
	return errors.New("archive preparation timed out upstream")
}
func (m *Manager) download(ctx context.Context, gid int, token string) error {
	meta := m.Store.Get(gid, token)
	if meta == nil {
		return errors.New("archive entry disappeared")
	}
	u := fmt.Sprint(meta["download_url"])
	if !strings.Contains(u, "?start=") {
		return errors.New("no final download URL available")
	}
	_, e := m.Store.Upsert(gid, token, map[string]any{"status": "downloading", "error": nil})
	if e != nil {
		return e
	}
	part, e := m.adoptLegacyPart(ctx, gid, token)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(part), 0o755); e != nil {
		return e
	}
	offset := int64(0)
	if st, x := os.Stat(part); x == nil {
		offset = st.Size()
	}
	total, e := m.Client.Download(ctx, u, part, offset, func(n int64) { m.mu.Lock(); m.progress[key(gid, token)] = n; m.mu.Unlock() })
	if e != nil {
		return e
	}
	_, e = m.Store.Upsert(gid, token, map[string]any{"bytes": total, "total_bytes": total})
	return e
}
func (m *Manager) finalize(ctx context.Context, gid int, token string) error {
	part, err := m.adoptLegacyPart(ctx, gid, token)
	if err != nil {
		return err
	}
	if _, x := m.Store.Upsert(gid, token, map[string]any{"status": "zipping"}); x != nil {
		return x
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	validatedPath := part
	convertedPath := part + ".zip-convert"
	defer os.Remove(convertedPath)
	if zr, e := zip.OpenReader(part); e != nil {
		_ = os.Remove(convertedPath)
		if x := convert7zToZIP(ctx, part, convertedPath); x != nil {
			return fmt.Errorf("archive was neither a valid zip nor a readable 7z: %w", x)
		}
		validatedPath = convertedPath
	} else {
		_ = zr.Close()
	}
	r, e := zip.OpenReader(validatedPath)
	if e != nil {
		return fmt.Errorf("archive zip failed validation: %w", e)
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(r.File) == 0 {
		r.Close()
		return errors.New("archive zip has no pages")
	}
	f, e := r.File[0].Open()
	if e == nil {
		_, e = io.Copy(io.Discard, f)
		_ = f.Close()
	}
	count := len(r.File)
	_ = r.Close()
	if e != nil {
		return fmt.Errorf("archive zip failed validation: %w", e)
	}
	target, publish, err := m.Store.ArchivePublishPath(gid, token)
	if err != nil {
		return err
	}
	if e = os.MkdirAll(filepath.Dir(target), 0o755); e != nil {
		return e
	}
	_ = os.Remove(publish)
	defer os.Remove(publish)
	validatedStat, e := os.Stat(validatedPath)
	if e != nil {
		return e
	}
	if e = copyFileContext(ctx, validatedPath, publish); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = m.Store.PublishArchive(gid, token, publish); e != nil {
		return e
	}
	_, e = m.Store.Upsert(gid, token, map[string]any{"status": "ready", "page_count": count, "total_bytes": validatedStat.Size(), "error": nil, "upgrade_backup": map[string]any{}})
	if e == nil {
		_ = os.Remove(part)
	}
	return e
}

func convert7zToZIP(ctx context.Context, source, target string) error {
	r, err := sevenzip.OpenReader(source)
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := target + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(f)
	for _, entry := range r.File {
		if err := ctx.Err(); err != nil {
			_ = zw.Close()
			return err
		}
		name := entry.Name
		if name == "" {
			continue
		}
		if entry.FileInfo().IsDir() && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: entry.Modified}
		h.SetMode(entry.Mode())
		out, err := zw.CreateHeader(h)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		in, err := entry.Open()
		if err != nil {
			_ = zw.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := in.Close()
		if copyErr != nil {
			_ = zw.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = zw.Close()
			return closeErr
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	ok = true
	return nil
}

func copyFileContext(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, &contextReader{ctx: ctx, r: in})
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func (m *Manager) fail(gid int, token, message string) {
	meta := m.Store.Get(gid, token)
	if meta == nil {
		return
	}
	backup, _ := meta["upgrade_backup"].(map[string]any)
	if len(backup) > 0 {
		if _, e := os.Stat(m.Store.archivePath(gid, token)); e == nil {
			patch := map[string]any{"status": "ready", "error": message, "upgrade_backup": map[string]any{}}
			for _, k := range []string{"or", "quality", "gp_price", "download_url", "page_count", "bytes", "total_bytes"} {
				if v, ok := backup[k]; ok {
					patch[k] = v
				}
			}
			_, _ = m.Store.Upsert(gid, token, patch)
			return
		}
	}
	_, _ = m.Store.Upsert(gid, token, map[string]any{"status": "failed", "error": message, "upgrade_backup": map[string]any{}})
}
func (m *Manager) RecoverInterruptedUpgrades() int {
	rolled := 0
	for _, entry := range m.Store.RecoveryEntries() {
		st := fmt.Sprint(entry["status"])
		backup, _ := entry["upgrade_backup"].(map[string]any)
		versionUpdate := entry["is_version_update"] == true
		gid := toInt(entry["gid"])
		token := fmt.Sprint(entry["token"])
		archivePath := m.Store.archivePath(gid, token)
		if archivePath == "" {
			continue
		}
		_ = os.Remove(archivePath + ".publish.tmp")
		if len(backup) > 0 && !versionUpdate {
			if _, e := os.Stat(archivePath); e != nil {
				continue
			}
			patch := map[string]any{"status": "ready", "error": "upgrade interrupted by restart; rolled back", "upgrade_backup": map[string]any{}}
			for _, field := range []string{"or", "quality", "gp_price", "download_url", "page_count", "bytes", "total_bytes"} {
				if value, ok := backup[field]; ok {
					patch[field] = value
				}
			}
			_, _ = m.Store.Upsert(gid, token, patch)
			_ = os.RemoveAll(filepath.Dir(m.stagingPart(gid, token)))
			_ = os.Remove(filepath.Join(m.Store.dir(gid, token), partName))
			rolled++
			continue
		}
		if !versionUpdate {
			// Preserve the established non-version recovery path: a resumable
			// partial remains available, but the user can retry through Start.
			stageDir := filepath.Dir(m.stagingPart(gid, token))
			_ = os.Remove(m.stagingPart(gid, token) + ".migrate.tmp")
			if st == "zipping" {
				_ = os.RemoveAll(stageDir)
				_ = os.Remove(filepath.Join(m.Store.dir(gid, token), partName))
			}
			_, _ = m.Store.Upsert(gid, token, map[string]any{"status": "failed", "error": "archive task interrupted by restart"})
			continue
		}
		if st == "submitting" || toString(entry["download_url"]) == "" {
			message := "archive task interrupted before a reusable status URL was saved"
			if entry["submit_attempted"] == true {
				message = "archive submission outcome is unknown; inspect the upstream archiver before retrying"
			}
			_, _ = m.Store.Upsert(gid, token, map[string]any{"status": "failed", "error": message})
			continue
		}
		rootGID, rootToken := toInt(entry["root_gid"]), toString(entry["root_token"])
		if rootGID <= 0 || rootToken == "" {
			rootGID, rootToken, _ = m.Store.RootIdentity(gid, token)
		}
		rootKey := key(rootGID, rootToken)
		jobKey := key(gid, token)
		m.mu.Lock()
		if m.jobs[jobKey] == nil {
			taskCtx, cancel := context.WithCancel(m.ctx)
			done := make(chan struct{})
			m.jobs[jobKey], m.jobDone[jobKey], m.jobRoots[jobKey] = cancel, done, rootKey
			m.wg.Add(1)
			go func() { defer m.wg.Done(); m.run(taskCtx, gid, token, done) }()
		}
		m.mu.Unlock()
	}
	return rolled
}
func (m *Manager) Remove(gid int, token string) bool {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	rootGID, rootToken, _ := m.Store.RootIdentity(gid, token)
	rootKey := key(rootGID, rootToken)
	if rootGID <= 0 || rootToken == "" {
		rootKey = key(gid, token)
	}
	m.mu.Lock()
	dones := []chan struct{}{}
	for jobKey, jobRoot := range m.jobRoots {
		if jobKey == rootKey || jobRoot == rootKey {
			if cancel := m.jobs[jobKey]; cancel != nil {
				cancel()
			}
			if done := m.jobDone[jobKey]; done != nil {
				dones = append(dones, done)
			}
		}
	}
	m.mu.Unlock()
	for _, done := range dones {
		<-done
	}
	m.mu.Lock()
	for jobKey, jobRoot := range m.jobRoots {
		if jobRoot == rootKey {
			delete(m.progress, jobKey)
		}
	}
	m.mu.Unlock()
	for _, identity := range m.Store.Identities(rootGID, rootToken) {
		stageGID, _ := strconv.Atoi(identity[0])
		_ = os.RemoveAll(filepath.Dir(m.stagingPart(stageGID, identity[1])))
	}
	return m.Store.Remove(gid, token)
}
func (m *Manager) Refresh(ctx context.Context, gid int, token string) (map[string]any, error) {
	if rootGID, rootToken, ok := m.Store.RootIdentity(gid, token); ok && rootGID == gid && rootToken == token {
		gid, token = m.Store.ResolveActive(gid, token)
	}
	meta := m.Store.Get(gid, token)
	if meta == nil {
		return nil, errors.New("no archive entry to refresh (start first)")
	}
	return m.Start(ctx, gid, token, fmt.Sprint(meta["or"]), true)
}
func (m *Manager) Snapshot(ctx context.Context, gid int, token string, force bool) (map[string]any, error) {
	if m.Store.Get(gid, token) == nil {
		return nil, errors.New("no archive entry to refresh metadata (start an archive first)")
	}
	if rootGID, rootToken, ok := m.Store.RootIdentity(gid, token); ok && rootGID == gid && rootToken == token {
		gid, token = m.Store.ResolveActive(gid, token)
	}
	return m.snapshot(ctx, gid, token, force)
}
func (m *Manager) snapshot(ctx context.Context, gid int, token string, force bool) (map[string]any, error) {
	payload, cover, hasCover, coverMime, err := m.captureSnapshot(ctx, gid, token, force)
	if err != nil {
		return nil, err
	}
	if hasCover {
		_ = m.Store.WriteCover(gid, token, cover)
	}
	payload["cover_mime"] = coverMime
	saved := toInt64(payload["saved_at"])
	if err = m.Store.WriteMetadata(gid, token, payload); err != nil {
		return nil, err
	}
	meta, _ := m.Service.GetMetadata(ctx, gid, token, false)
	_, err = m.Store.Upsert(gid, token, map[string]any{"metadata_at": saved, "title": meta.Title})
	if err != nil {
		return nil, err
	}
	return map[string]any{"gid": gid, "token": token, "title": meta.Title, "title_jpn": meta.TitleJPN, "category": meta.Category, "rating": meta.Rating, "filecount": meta.Filecount, "filesize": meta.Filesize, "posted": meta.Posted, "uploader": meta.Uploader, "expunged": meta.Expunged, "cover_mime": coverMime, "saved_at": saved}, nil
}

func (m *Manager) captureSnapshot(ctx context.Context, gid int, token string, force bool) (map[string]any, []byte, bool, string, error) {
	meta, err := m.Service.GetMetadata(ctx, gid, token, force)
	if err != nil {
		return nil, nil, false, "", err
	}
	cover, coverMime, hasCover := []byte(nil), "", false
	if meta.Thumb != "" {
		if b, mime, fetchErr := m.Service.FetchCoverBytes(ctx, meta.Thumb); fetchErr == nil {
			cover, coverMime, hasCover = b, mime, true
		}
	}
	payload := map[string]any{}
	b, _ := json.Marshal(meta)
	_ = json.Unmarshal(b, &payload)
	payload["gid"], payload["token"] = gid, token
	payload["saved_at"] = float64(time.Now().Unix())
	payload["cover_mime"] = coverMime
	return payload, cover, hasCover, coverMime, nil
}

func (m *Manager) Metadata(gid int, token string) (map[string]any, error) {
	if rootGID, rootToken, ok := m.Store.RootIdentity(gid, token); ok && rootGID == gid && rootToken == token {
		gid, token = m.Store.ResolveActive(gid, token)
	}
	return m.Store.ReadMetadata(gid, token)
}
func (m *Manager) Cover(gid int, token string) ([]byte, bool) {
	if rootGID, rootToken, ok := m.Store.RootIdentity(gid, token); ok && rootGID == gid && rootToken == token {
		gid, token = m.Store.ResolveActive(gid, token)
	}
	return m.Store.Cover(gid, token)
}
func (m *Manager) ListEntries() []map[string]any {
	out := m.Store.Entries()
	for _, entry := range out {
		for _, field := range []string{versionsName, "activation_pending", "upgrade_backup", "submit_attempted", "submitted_at", "source_gid", "source_token"} {
			delete(entry, field)
		}
	}
	return out
}
func (m *Manager) Stats() map[string]any {
	stats := m.Store.Stats()
	var stagingBytes int64
	_ = filepath.WalkDir(m.stagingRoot(), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == partName {
			if st, e := entry.Info(); e == nil {
				stagingBytes += st.Size()
			}
		}
		return nil
	})
	stats["bytes"] = toInt64(stats["bytes"]) + stagingBytes
	return stats
}
