package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Facet struct {
	Name string
	Mask int
}

type Settings struct {
	AuthUsername, AuthPassword                                                        string
	AuthExemptPaths, AuthExemptPrefixes                                               []string
	IPBMemberID, IPBPassHash, Igneous                                                 string
	EHSite, NW, DataTags                                                              string
	TimeoutSeconds, HTMLIntervalSeconds, BannedCooldownSeconds, ExceedCooldownSeconds float64
	Retries, MaxConcurrency, ImageMaxConcurrency, ThumbMaxConcurrency                 int
	PublicBaseURL                                                                     string
	PSEPageBase                                                                       int
	OPDSAcqDetail                                                                     bool
	CacheDir                                                                          string
	CacheMaxGB                                                                        float64
	ImageCacheEnabled                                                                 bool
	MetadataTTLSeconds, PageURLTTLSeconds, ListCacheTTLSeconds                        int
	ImageSourceRetries                                                                int
	TagStatusFilter                                                                   string
	CommentsEnabled                                                                   bool
	ImageProxyHosts                                                                   []string
	EHProfile                                                                         string
	HomeConfigPath                                                                    string
	Facets                                                                            []Facet
	ArchiveDir, ArchiveQuality                                                        string
	ArchiveDownloadConcurrency                                                        int
	ArchiveUpdateIntervalSeconds                                                      int
	ArchiveAutoUpdate                                                                 bool
	FavoritesSyncIntervalSeconds                                                      float64
	FavoritesSyncArchive                                                              bool
	FavoritesSyncCategories, FavoritesSyncExcludes                                    []int
	FavoritesSyncState                                                                string
	FavoritesSyncMatchThreshold, FavoritesSyncMaxPages                                int
	MyTagsTTLSeconds                                                                  float64
	MyTagsState                                                                       string
	TagTranslationEnabled                                                             bool
	TagTranslationURL                                                                 string
	TagTranslationIntervalSeconds                                                     float64
	TagTranslationState                                                               string
}

var defaultFacets = []Facet{{"Doujinshi", 1021}, {"Manga", 1019}, {"Artist CG", 1015}, {"Game CG", 1007}, {"Western", 991}, {"Non-H", 959}, {"Image Set", 895}, {"Cosplay", 767}, {"Asian Porn", 511}, {"Misc", 1022}}

func Load() Settings {
	return LoadWithConfigPath(os.Getenv("SETTINGS_CONFIG"))
}

// LoadWithConfigPath applies the persisted editable settings first; environment
// variables remain authoritative. Empty path selects /config/settings.toml in
// containers and ./config/settings.toml for local runs.
func LoadWithConfigPath(path string) Settings {
	if path == "" {
		if st, err := os.Stat("/config"); err == nil && st.IsDir() {
			path = "/config/settings.toml"
		} else {
			path = "./config/settings.toml"
		}
	}
	fileValues, fileErr := readEditableFile(path)
	if _, member := os.LookupEnv("IPB_MEMBER_ID"); member {
		delete(fileValues, "IPB_MEMBER_ID")
		delete(fileValues, "IPB_PASS_HASH")
	}
	if _, hash := os.LookupEnv("IPB_PASS_HASH"); hash {
		delete(fileValues, "IPB_MEMBER_ID")
		delete(fileValues, "IPB_PASS_HASH")
	}
	_ = fileErr // Startup retains Go defaults on malformed settings; admin editor reports the error.
	return loadWithEditableValues(fileValues)
}

// LoadWithEditableValues evaluates an editor snapshot against Go defaults and environment values.
func LoadWithEditableValues(fileValues map[string]any) Settings {
	copyValues := make(map[string]any, len(fileValues))
	for k, v := range fileValues {
		copyValues[k] = v
	}
	fileValues = copyValues
	if _, member := os.LookupEnv("IPB_MEMBER_ID"); member {
		delete(fileValues, "IPB_MEMBER_ID")
		delete(fileValues, "IPB_PASS_HASH")
	}
	if _, hash := os.LookupEnv("IPB_PASS_HASH"); hash {
		delete(fileValues, "IPB_MEMBER_ID")
		delete(fileValues, "IPB_PASS_HASH")
	}
	return loadWithEditableValues(fileValues)
}

func loadWithEditableValues(fileValues map[string]any) Settings {
	base := func(name, fallback string) string {
		if v, ok := os.LookupEnv(name); ok {
			return strings.TrimSpace(v)
		}
		if v, ok := fileValues[name]; ok {
			return fmt.Sprint(v)
		}
		return fallback
	}
	s := Settings{
		AuthUsername: base("AUTH_USERNAME", ""), AuthPassword: base("AUTH_PASSWORD", ""),
		AuthExemptPaths: csv(base("AUTH_EXEMPT_PATHS", "")), AuthExemptPrefixes: csv(base("AUTH_EXEMPT_PREFIXES", "")),
		IPBMemberID: base("IPB_MEMBER_ID", ""), IPBPassHash: base("IPB_PASS_HASH", ""), Igneous: base("IGNEOUS", ""),
		EHSite: strings.ToLower(base("EH_SITE", "e-hentai")), NW: defaultString(base("NW", "1"), "1"), DataTags: defaultString(base("DATATAGS", "1"), "1"),
		TimeoutSeconds: floatEnv("TIMEOUT_SECONDS", 6), Retries: intEnv("RETRIES", 3), HTMLIntervalSeconds: floatEnv("HTML_INTERVAL_SECONDS", .3),
		MaxConcurrency: intEnv("MAX_CONCURRENCY", 5), ImageMaxConcurrency: intEnv("IMAGE_MAX_CONCURRENCY", 5), ThumbMaxConcurrency: intEnv("THUMB_MAX_CONCURRENCY", 25),
		PublicBaseURL: strings.TrimRight(base("PUBLIC_BASE_URL", ""), "/"), PSEPageBase: intEnv("PSE_PAGE_BASE", 1),
		OPDSAcqDetail: boolEnv("OPDS_ACQ_DETAIL", strings.EqualFold(base("OPDS_ACQ_MODE", ""), "detail")),
		CacheDir:      base("CACHE_DIR", "./cache"), CacheMaxGB: floatEnv("CACHE_MAX_GB", 4), ImageCacheEnabled: boolEnv("IMAGE_CACHE_ENABLED", true),
		MetadataTTLSeconds: intEnv("METADATA_TTL_SECONDS", 600), PageURLTTLSeconds: intEnv("PAGE_URL_TTL_SECONDS", 3600), ListCacheTTLSeconds: intEnv("LIST_CACHE_TTL_SECONDS", 600),
		ImageSourceRetries: intEnv("IMAGE_SOURCE_RETRIES", 2), BannedCooldownSeconds: floatEnv("BANNED_COOLDOWN_SECONDS", 1800), ExceedCooldownSeconds: floatEnv("EXCEED_COOLDOWN_SECONDS", 300),
		TagStatusFilter: defaultString(strings.ToLower(base("TAG_STATUS_FILTER", "balanced")), "balanced"), CommentsEnabled: boolEnv("COMMENTS_ENABLED", true),
		ImageProxyHosts: defaultCSV(base("IMAGE_PROXY_HOSTS", ""), []string{"ehgt.org", "s.exhentai.org"}), EHProfile: base("EH_PROFILE", "PandaOPDS"),
		HomeConfigPath: base("HOME_CONFIG", "./config/home.toml"), Facets: parseFacets(os.Getenv("FACETS")),
		ArchiveDir: base("ARCHIVE_DIR", "./archives"), ArchiveQuality: defaultString(strings.ToLower(base("ARCHIVE_QUALITY", "original")), "original"), ArchiveDownloadConcurrency: intEnv("ARCHIVE_DOWNLOAD_CONCURRENCY", 5),
		ArchiveUpdateIntervalSeconds: 7 * 86400,
		FavoritesSyncIntervalSeconds: floatEnv("FAVORITES_SYNC_INTERVAL_SECONDS", 3600), FavoritesSyncArchive: boolEnv("FAVORITES_SYNC_ARCHIVE", false),
		FavoritesSyncCategories: parseIDs(os.Getenv("FAVORITES_SYNC_CATEGORIES"), false), FavoritesSyncExcludes: parseIDs(os.Getenv("FAVORITES_SYNC_CATEGORIES"), true),
		FavoritesSyncState: base("FAVORITES_SYNC_STATE", defaultStatePath("favorites_sync.json")), FavoritesSyncMatchThreshold: intEnv("FAVORITES_SYNC_MATCH_THRESHOLD", 5), FavoritesSyncMaxPages: intEnv("FAVORITES_SYNC_MAX_PAGES", 50),
		MyTagsTTLSeconds: floatEnv("MYTAGS_TTL_SECONDS", 21600), MyTagsState: base("MYTAGS_STATE", defaultStatePath("mytags.json")),
		TagTranslationEnabled: boolEnv("TAG_TRANSLATION_ENABLED", false), TagTranslationURL: base("TAG_TRANSLATION_URL", "https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.json"),
		TagTranslationIntervalSeconds: floatEnv("TAG_TRANSLATION_INTERVAL_SECONDS", 86400), TagTranslationState: base("TAG_TRANSLATION_STATE", defaultStatePath("tag_translation.json")),
	}
	s = ApplyEditableOverrides(s, fileValues)
	return s
}

func (s Settings) SiteHost() string {
	if s.EHSite == "exhentai" {
		return "exhentai.org"
	}
	return "e-hentai.org"
}
func (s Settings) HasIPB() bool      { return s.IPBMemberID != "" && s.IPBPassHash != "" }
func (s Settings) AuthEnabled() bool { return s.AuthUsername != "" && s.AuthPassword != "" }
func (s Settings) Validate() error {
	if s.EHSite != "e-hentai" && s.EHSite != "exhentai" {
		return strconv.ErrSyntax
	}
	return nil
}
func defaultStatePath(name string) string {
	if st, err := os.Stat("/config"); err == nil && st.IsDir() {
		return filepath.Join("/config", name)
	}
	return "./" + name
}
func defaultString(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
func csv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func defaultCSV(v string, d []string) []string {
	x := csv(strings.ToLower(v))
	if len(x) == 0 {
		return d
	}
	return x
}
func intEnv(k string, d int) int {
	v, e := strconv.Atoi(strings.TrimSpace(os.Getenv(k)))
	if e != nil {
		return d
	}
	return v
}
func floatEnv(k string, d float64) float64 {
	v, e := strconv.ParseFloat(strings.TrimSpace(os.Getenv(k)), 64)
	if e != nil {
		return d
	}
	return v
}
func boolEnv(k string, d bool) bool {
	v, ok := os.LookupEnv(k)
	if !ok {
		return d
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
func parseFacets(v string) []Facet {
	if strings.TrimSpace(v) == "" {
		return append([]Facet(nil), defaultFacets...)
	}
	out := []Facet{}
	for _, p := range strings.Split(v, ",") {
		n, m, ok := strings.Cut(p, ":")
		if !ok {
			continue
		}
		x, e := strconv.Atoi(strings.TrimSpace(m))
		if e == nil {
			out = append(out, Facet{strings.TrimSpace(n), x})
		}
	}
	if len(out) == 0 {
		return append([]Facet(nil), defaultFacets...)
	}
	return out
}
func parseIDs(v string, excludes bool) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		neg := strings.HasPrefix(p, "-")
		if neg {
			p = strings.TrimPrefix(p, "-")
		}
		if neg != excludes {
			continue
		}
		n, e := strconv.Atoi(p)
		if e == nil && !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	return out
}
