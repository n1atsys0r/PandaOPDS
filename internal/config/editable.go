package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

var editableKeys = []string{"EH_SITE", "IPB_MEMBER_ID", "IPB_PASS_HASH", "IGNEOUS", "PUBLIC_BASE_URL", "PSE_PAGE_BASE", "OPDS_ACQ_DETAIL", "TAG_STATUS_FILTER", "TAG_TRANSLATION_ENABLED", "COMMENTS_ENABLED", "IMAGE_CACHE_ENABLED", "CACHE_MAX_GB", "FAVORITES_SYNC_INTERVAL_SECONDS"}

func EditablePath() string {
	if p := os.Getenv("SETTINGS_CONFIG"); p != "" {
		return p
	}
	if st, err := os.Stat("/config"); err == nil && st.IsDir() {
		return "/config/settings.toml"
	}
	return "./config/settings.toml"
}

func readEditableFile(path string) (map[string]any, error) {
	if path == "" {
		path = EditablePath()
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return DecodeEditableFile(b)
}
func DecodeEditableFile(b []byte) (map[string]any, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(b), &raw); err != nil {
		return nil, fmt.Errorf("invalid TOML")
	}
	for k := range raw {
		if !containsEditable(k) {
			return nil, fmt.Errorf("unknown settings key %q", k)
		}
		if err := ValidateEditableValue(k, raw[k]); err != nil {
			return nil, fmt.Errorf("invalid value for %s", k)
		}
	}
	_, member := raw["IPB_MEMBER_ID"]
	_, hash := raw["IPB_PASS_HASH"]
	if member != hash {
		return nil, fmt.Errorf("IPB_MEMBER_ID and IPB_PASS_HASH must be configured together")
	}
	if member {
		if strings.TrimSpace(fmt.Sprint(raw["IPB_MEMBER_ID"])) == "" || strings.TrimSpace(fmt.Sprint(raw["IPB_PASS_HASH"])) == "" {
			return nil, fmt.Errorf("IPB credential pair must be nonempty")
		}
	}
	return raw, nil
}

func ApplyEditableOverrides(s Settings, raw map[string]any) Settings {
	for _, key := range editableKeys {
		if _, ok := os.LookupEnv(key); ok {
			continue
		}
		if key == "OPDS_ACQ_DETAIL" {
			if _, ok := os.LookupEnv("OPDS_ACQ_MODE"); ok {
				continue
			}
		}
		if key == "IPB_MEMBER_ID" || key == "IPB_PASS_HASH" {
			if _, a := os.LookupEnv("IPB_MEMBER_ID"); a {
				continue
			}
			if _, b := os.LookupEnv("IPB_PASS_HASH"); b {
				continue
			}
		}
		if v, ok := raw[key]; ok {
			_ = setEditableValue(&s, key, v)
		}
	}
	return s
}

func containsEditable(k string) bool {
	for _, x := range editableKeys {
		if x == k {
			return true
		}
	}
	return false
}
func EditableKeys() []string      { return append([]string(nil), editableKeys...) }
func IsEditableKey(k string) bool { return containsEditable(k) }
func EditableValue(s Settings, k string) any {
	switch k {
	case "EH_SITE":
		return s.EHSite
	case "IPB_MEMBER_ID":
		return s.IPBMemberID
	case "IPB_PASS_HASH":
		return s.IPBPassHash
	case "IGNEOUS":
		return s.Igneous
	case "PUBLIC_BASE_URL":
		return s.PublicBaseURL
	case "PSE_PAGE_BASE":
		return s.PSEPageBase
	case "OPDS_ACQ_DETAIL":
		return s.OPDSAcqDetail
	case "TAG_STATUS_FILTER":
		return s.TagStatusFilter
	case "TAG_TRANSLATION_ENABLED":
		return s.TagTranslationEnabled
	case "COMMENTS_ENABLED":
		return s.CommentsEnabled
	case "IMAGE_CACHE_ENABLED":
		return s.ImageCacheEnabled
	case "CACHE_MAX_GB":
		return s.CacheMaxGB
	case "FAVORITES_SYNC_INTERVAL_SECONDS":
		return s.FavoritesSyncIntervalSeconds
	}
	return nil
}
func ValidateEditableValue(k string, v any) error {
	stringKey := k == "EH_SITE" || k == "IPB_MEMBER_ID" || k == "IPB_PASS_HASH" || k == "IGNEOUS" || k == "PUBLIC_BASE_URL" || k == "TAG_STATUS_FILTER"
	if stringKey {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s must be a string", k)
		}
	}
	if k == "OPDS_ACQ_DETAIL" || k == "TAG_TRANSLATION_ENABLED" || k == "COMMENTS_ENABLED" || k == "IMAGE_CACHE_ENABLED" {
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", k)
		}
	}
	if k == "PSE_PAGE_BASE" {
		switch v.(type) {
		case int64, float64:
		default:
			return fmt.Errorf("PSE_PAGE_BASE must be an integer")
		}
	}
	if k == "CACHE_MAX_GB" || k == "FAVORITES_SYNC_INTERVAL_SECONDS" {
		switch v.(type) {
		case int64, float64:
		default:
			return fmt.Errorf("%s must be numeric", k)
		}
	}
	var s Settings
	if err := setEditableValue(&s, k, v); err != nil {
		return fmt.Errorf("invalid value for %s", k)
	}
	switch k {
	case "EH_SITE":
		if s.EHSite != "e-hentai" && s.EHSite != "exhentai" {
			return fmt.Errorf("EH_SITE must be e-hentai or exhentai")
		}
	case "PSE_PAGE_BASE":
		if s.PSEPageBase != 0 && s.PSEPageBase != 1 {
			return fmt.Errorf("PSE_PAGE_BASE must be 0 or 1")
		}
	case "TAG_STATUS_FILTER":
		if s.TagStatusFilter != "strict" && s.TagStatusFilter != "balanced" && s.TagStatusFilter != "off" {
			return fmt.Errorf("TAG_STATUS_FILTER must be strict, balanced, or off")
		}
	case "CACHE_MAX_GB":
		if s.CacheMaxGB < 0 || math.IsNaN(s.CacheMaxGB) || math.IsInf(s.CacheMaxGB, 0) {
			return fmt.Errorf("CACHE_MAX_GB must be nonnegative")
		}
	case "FAVORITES_SYNC_INTERVAL_SECONDS":
		if s.FavoritesSyncIntervalSeconds < 0 || math.IsNaN(s.FavoritesSyncIntervalSeconds) || math.IsInf(s.FavoritesSyncIntervalSeconds, 0) {
			return fmt.Errorf("FAVORITES_SYNC_INTERVAL_SECONDS must be nonnegative")
		}
	}
	if k == "IPB_MEMBER_ID" || k == "IPB_PASS_HASH" || k == "IGNEOUS" {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s must be a string", k)
		}
	}
	if k == "PUBLIC_BASE_URL" {
		value := v.(string)
		if value != "" {
			u, err := url.Parse(value)
			if err != nil || u.Opaque != "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") {
				return fmt.Errorf("PUBLIC_BASE_URL must be an absolute http(s) URL without credentials, query, or fragment")
			}
		}
	}
	return nil
}

func setEditableValue(s *Settings, k string, v any) error {
	str := fmt.Sprint(v)
	boolean := func() (bool, error) {
		b, ok := v.(bool)
		if ok {
			return b, nil
		}
		return strconv.ParseBool(str)
	}
	integer := func() (int, error) {
		n, ok := v.(int64)
		if ok {
			return int(n), nil
		}
		return strconv.Atoi(str)
	}
	switch k {
	case "EH_SITE":
		s.EHSite = strings.ToLower(str)
	case "IPB_MEMBER_ID":
		s.IPBMemberID = str
	case "IPB_PASS_HASH":
		s.IPBPassHash = str
	case "IGNEOUS":
		s.Igneous = str
	case "PUBLIC_BASE_URL":
		s.PublicBaseURL = strings.TrimRight(str, "/")
	case "PSE_PAGE_BASE":
		n, e := integer()
		s.PSEPageBase = n
		return e
	case "OPDS_ACQ_DETAIL":
		b, e := boolean()
		s.OPDSAcqDetail = b
		return e
	case "TAG_STATUS_FILTER":
		s.TagStatusFilter = strings.ToLower(str)
	case "TAG_TRANSLATION_ENABLED":
		b, e := boolean()
		s.TagTranslationEnabled = b
		return e
	case "COMMENTS_ENABLED":
		b, e := boolean()
		s.CommentsEnabled = b
		return e
	case "IMAGE_CACHE_ENABLED":
		b, e := boolean()
		s.ImageCacheEnabled = b
		return e
	case "CACHE_MAX_GB":
		n, ok := v.(float64)
		if !ok {
			x, e := strconv.ParseFloat(str, 64)
			n = x
			if e != nil {
				return e
			}
		}
		s.CacheMaxGB = n
	case "FAVORITES_SYNC_INTERVAL_SECONDS":
		n, ok := v.(float64)
		if !ok {
			x, e := strconv.ParseFloat(str, 64)
			n = x
			if e != nil {
				return e
			}
		}
		s.FavoritesSyncIntervalSeconds = n
	default:
		return fmt.Errorf("unknown settings key %q", k)
	}
	return nil
}

func WriteEditableFile(path string, values map[string]any) error {
	if path == "" {
		path = EditablePath()
	}
	for k := range values {
		if !containsEditable(k) {
			return fmt.Errorf("unknown settings key %q", k)
		}
		if err := ValidateEditableValue(k, values[k]); err != nil {
			return err
		}
	}
	_, member := values["IPB_MEMBER_ID"]
	_, hash := values["IPB_PASS_HASH"]
	if member != hash {
		return fmt.Errorf("IPB_MEMBER_ID and IPB_PASS_HASH must be configured together")
	}
	if member && (strings.TrimSpace(fmt.Sprint(values["IPB_MEMBER_ID"])) == "" || strings.TrimSpace(fmt.Sprint(values["IPB_PASS_HASH"])) == "") {
		return fmt.Errorf("IPB credential pair must be nonempty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := toml.Marshal(values)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
