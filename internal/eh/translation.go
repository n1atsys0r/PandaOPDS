package eh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"pandaopds/internal/config"
)

type TagTranslator struct {
	settings config.Settings
	client   *http.Client
	mu       sync.RWMutex
	refresh  sync.Mutex
	names    map[string]string
	abbrs    map[string]string
	tags     map[string]string
	forward  map[string]string
	reverse  map[string][]tagTranslation
	bare     map[string][]tagTranslation
	nsLookup map[string]string
	savedAt  time.Time
	cancel   context.CancelFunc
	done     chan struct{}
}

type tagTranslation struct{ namespace, key string }

var (
	translationTokenRE = regexp.MustCompile(`[^[:space:]]+:"[^"]*"|"[^"]*"|[^[:space:]]+`)
	translationColonRE = regexp.MustCompile(`:\s+"`)
	translationAliases = map[string]string{"char": "character", "circle": "group", "lang": "language", "series": "parody"}
)

func NewTagTranslator(settings config.Settings, transport http.RoundTripper) *TagTranslator {
	if transport == nil {
		transport = http.DefaultTransport
	}
	t := &TagTranslator{settings: settings, client: &http.Client{Transport: transport, Timeout: time.Duration(settings.TimeoutSeconds * 10 * float64(time.Second))}, names: map[string]string{}, abbrs: map[string]string{}, tags: map[string]string{}, forward: map[string]string{}, reverse: map[string][]tagTranslation{}, bare: map[string][]tagTranslation{}, nsLookup: map[string]string{}}
	t.load()
	return t
}

func cleanTranslation(value string) string {
	var cleaned strings.Builder
	for _, r := range value {
		if r >= 0x1F000 && r <= 0x1FFFF || r >= 0x2600 && r <= 0x27BF || r >= 0x2B00 && r <= 0x2BFF || r == 0xFE0F || r == 0x200D || r == 0x20E3 {
			continue
		}
		cleaned.WriteRune(r)
	}
	return strings.Join(strings.Fields(cleaned.String()), " ")
}

func translationKey(ns, key string) string { return strings.ToLower(ns) + ":" + strings.ToLower(key) }

func (t *TagTranslator) rebuildLocked() {
	t.forward = map[string]string{}
	t.reverse = map[string][]tagTranslation{}
	t.bare = map[string][]tagTranslation{}
	t.nsLookup = map[string]string{}
	for ns, cn := range t.names {
		t.nsLookup[strings.ToLower(ns)] = ns
		if cn != "" {
			t.nsLookup[strings.ToLower(cn)] = ns
		}
	}
	for abbr, ns := range t.abbrs {
		if _, exists := t.nsLookup[strings.ToLower(abbr)]; !exists {
			t.nsLookup[strings.ToLower(abbr)] = ns
		}
	}
	for alias, ns := range translationAliases {
		if _, exists := t.nsLookup[alias]; !exists {
			t.nsLookup[alias] = ns
		}
	}
	tagKeys := make([]string, 0, len(t.tags))
	for raw := range t.tags {
		tagKeys = append(tagKeys, raw)
	}
	sort.Strings(tagKeys)
	for _, raw := range tagKeys {
		name := t.tags[raw]
		ns, key, ok := strings.Cut(raw, ":")
		if !ok || ns == "" || key == "" || name == "" {
			continue
		}
		t.forward[translationKey(ns, key)] = name
		t.reverse[translationKey(ns, name)] = append(t.reverse[translationKey(ns, name)], tagTranslation{namespace: ns, key: key})
		t.bare[strings.ToLower(name)] = append(t.bare[strings.ToLower(name)], tagTranslation{namespace: ns, key: key})
	}
}

func (t *TagTranslator) load() {
	if t.settings.TagTranslationState == "" {
		return
	}
	body, err := os.ReadFile(t.settings.TagTranslationState)
	if err != nil {
		return
	}
	var state struct {
		Namespaces map[string]string `json:"namespaces"`
		Abbrs      map[string]string `json:"abbrs"`
		Tags       map[string]string `json:"tags"`
		SavedAt    float64           `json:"saved_at"`
	}
	if json.Unmarshal(body, &state) != nil || state.Tags == nil {
		return
	}
	t.names, t.abbrs, t.tags = state.Namespaces, state.Abbrs, state.Tags
	if t.names == nil {
		t.names = map[string]string{}
	}
	if t.abbrs == nil {
		t.abbrs = map[string]string{}
	}
	t.savedAt = time.UnixMilli(int64(state.SavedAt * 1000))
	t.rebuildLocked()
}

func parseTranslationDB(body []byte) (map[string]string, map[string]string, map[string]string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, nil, nil, err
	}
	names, abbrs, tags := map[string]string{}, map[string]string{}, map[string]string{}
	var data json.RawMessage
	if err := json.Unmarshal(root["data"], &data); err != nil {
		return nil, nil, nil, err
	}
	var buckets []map[string]json.RawMessage
	if json.Unmarshal(data, &buckets) == nil {
		for _, bucket := range buckets {
			var ns string
			_ = json.Unmarshal(bucket["namespace"], &ns)
			if ns == "" || ns == "ns" || ns == "rows" {
				continue
			}
			var fm struct {
				Name string `json:"name"`
				Abbr string `json:"abbr"`
			}
			_ = json.Unmarshal(bucket["frontMatters"], &fm)
			if name := cleanTranslation(fm.Name); name != "" {
				names[ns] = name
			}
			if abbr := strings.ToLower(strings.TrimSpace(fm.Abbr)); abbr != "" {
				abbrs[abbr] = ns
			}
			var entries map[string]map[string]any
			_ = json.Unmarshal(bucket["data"], &entries)
			for key, entry := range entries {
				if name, ok := entry["name"].(string); ok {
					if cleaned := cleanTranslation(name); cleaned != "" {
						tags[ns+":"+key] = cleaned
					}
				}
			}
		}
	} else {
		var legacy map[string]map[string]json.RawMessage
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, nil, nil, err
		}
		for ns, bucket := range legacy {
			if ns == "ns" || ns == "rows" {
				continue
			}
			var entries map[string]map[string]string
			_ = json.Unmarshal(bucket["raw"], &entries)
			for key, entry := range entries {
				if cleaned := cleanTranslation(entry["name"]); cleaned != "" {
					tags[ns+":"+key] = cleaned
				}
			}
		}
		var nsBucket map[string]json.RawMessage
		_ = json.Unmarshal(legacy["ns"]["raw"], &nsBucket)
		for ns, raw := range nsBucket {
			var entry map[string]string
			_ = json.Unmarshal(raw, &entry)
			if cleaned := cleanTranslation(entry["name"]); cleaned != "" {
				names[ns] = cleaned
			}
		}
	}
	if len(tags) == 0 {
		return nil, nil, nil, io.ErrUnexpectedEOF
	}
	return names, tags, abbrs, nil
}

func (t *TagTranslator) install(names, tags, abbrs map[string]string) {
	t.mu.Lock()
	t.names, t.tags, t.abbrs = names, tags, abbrs
	t.savedAt = time.Now()
	t.rebuildLocked()
	t.mu.Unlock()
	path := t.settings.TagTranslationState
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	payload, err := json.Marshal(map[string]any{"namespaces": names, "abbrs": abbrs, "tags": tags, "saved_at": float64(time.Now().UnixMilli()) / 1000})
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, payload, 0o600) == nil {
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
		}
	}
}

func (t *TagTranslator) Refresh(ctx context.Context) error {
	t.refresh.Lock()
	defer t.refresh.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.settings.TagTranslationURL, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &UpstreamError{Status: resp.StatusCode, Detail: "tag translation database fetch failed"}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	names, tags, abbrs, err := parseTranslationDB(body)
	if err != nil {
		return err
	}
	t.install(names, tags, abbrs)
	return nil
}

func (t *TagTranslator) Start() {
	if !t.settings.TagTranslationEnabled || t.done != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel, t.done = cancel, make(chan struct{})
	go func() {
		defer close(t.done)
		for {
			_ = t.Refresh(ctx)
			if t.settings.TagTranslationIntervalSeconds <= 0 {
				return
			}
			timer := time.NewTimer(time.Duration(t.settings.TagTranslationIntervalSeconds * float64(time.Second)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (t *TagTranslator) Stop() {
	if t.cancel != nil {
		t.cancel()
		<-t.done
		t.cancel, t.done = nil, nil
	}
}

func (t *TagTranslator) DisplayTag(namespace, key string) (string, bool) {
	if !t.settings.TagTranslationEnabled {
		return namespace + ":" + key, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	canonical := t.resolveNamespaceLocked(namespace)
	displayNS, knownNS := t.names[canonical]
	if knownNS && displayNS != "" {
		if translated := t.forward[translationKey(canonical, key)]; translated != "" {
			return displayNS + ":" + translated, true
		}
		return displayNS + ":" + key, true
	}
	return namespace + ":" + key, false
}

func (t *TagTranslator) DisplayCategory(category string) string {
	if !t.settings.TagTranslationEnabled {
		return "category:" + category
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if translated := t.forward[translationKey("reclass", category)]; translated != "" {
		return "分类:" + translated
	}
	return "分类:" + category
}

func (t *TagTranslator) resolveNamespaceLocked(prefix string) string {
	if ns := t.nsLookup[strings.ToLower(prefix)]; ns != "" {
		return ns
	}
	return prefix
}

func (t *TagTranslator) TranslateQuery(query string) string {
	if query == "" {
		return query
	}
	query = strings.NewReplacer("“", `"`, "”", `"`, "„", `"`, "«", `"`, "»", `"`, "＂", `"`).Replace(query)
	query = translationColonRE.ReplaceAllString(query, `:"`)
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.tags) == 0 {
		return query
	}
	tokens := translationTokenRE.FindAllString(query, -1)
	var out []string
	changed := false
	for i := 0; i < len(tokens); {
		tok := tokens[i]
		if strings.HasPrefix(tok, `"`) && strings.HasSuffix(tok, `"`) && len(tok) > 2 {
			inner := tok[1 : len(tok)-1]
			if prefix, rest, ok := strings.Cut(inner, ":"); ok {
				if native, yes := t.translatePrefixedLocked(prefix, rest); yes {
					out, changed = append(out, native), true
					i++
					continue
				}
			}
			out, i = append(out, tok), i+1
			continue
		}
		if prefix, rest, ok := strings.Cut(tok, ":"); ok && rest != "" {
			if native, yes := t.translatePrefixedLocked(prefix, strings.Trim(rest, `"`)); yes {
				out, changed = append(out, native), true
				i++
				continue
			}
			ns := t.resolveNamespaceLocked(prefix)
			if ns != prefix {
				candidate := strings.Trim(rest, `"`)
				longest, end := "", i
				if _, found := t.reverse[translationKey(ns, candidate)]; !found {
					for j := i + 1; j < len(tokens) && j <= i+5; j++ {
						if strings.Contains(tokens[j], ":") || strings.HasPrefix(tokens[j], `"`) {
							break
						}
						candidate += " " + tokens[j]
						if _, found := t.reverse[translationKey(ns, candidate)]; found {
							longest, end = candidate, j
						}
					}
				}
				if longest != "" {
					if native, yes := t.translatePrefixedLocked(prefix, longest); yes {
						out, changed = append(out, native), true
						i = end + 1
						continue
					}
				}
			}
			if ns != prefix {
				out, changed = append(out, formatSearchTag(ns, strings.Trim(rest, `"`))), true
				i++
				continue
			}
		}
		candidates := t.bare[strings.ToLower(tok)]
		if len(candidates) == 1 {
			out, changed = append(out, formatSearchTag(candidates[0].namespace, candidates[0].key)), true
		} else {
			out = append(out, tok)
		}
		i++
	}
	if changed {
		return strings.Join(out, " ")
	}
	return query
}

func (t *TagTranslator) translatePrefixedLocked(prefix, name string) (string, bool) {
	ns := t.resolveNamespaceLocked(prefix)
	for _, candidate := range t.reverse[translationKey(ns, name)] {
		return formatSearchTag(candidate.namespace, candidate.key), true
	}
	return "", false
}

func formatSearchTag(namespace, key string) string {
	if strings.Contains(key, " ") {
		return namespace + `:"` + key + `"`
	}
	return namespace + ":" + key
}

func (t *TagTranslator) NormalizeCategoryQuery(query string, facets []config.Facet) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.tags) == 0 {
		return query
	}
	for _, token := range strings.Fields(query) {
		prefix, value, ok := strings.Cut(token, ":")
		if !ok || (prefix != "category" && prefix != "分类") {
			continue
		}
		for _, facet := range facets {
			if translated := t.forward[translationKey("reclass", facet.Name)]; strings.EqualFold(translated, strings.Trim(value, `"`)) {
				query = strings.Replace(query, token, "category:"+facet.Name, 1)
				break
			}
		}
	}
	return query
}
