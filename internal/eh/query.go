package eh

import (
	"net/url"
	"regexp"
	"strings"

	"pandaopds/internal/config"
)

var categoryNames = map[string]int{"doujinshi": 1021, "manga": 1019, "artist cg": 1015, "game cg": 1007, "western": 991, "non-h": 959, "image set": 895, "cosplay": 767, "asian porn": 511, "misc": 1022}

func ExtractAdvancedQuery(query string) (string, url.Values) {
	values := url.Values{}
	var out []string
	quoted := false
	for _, token := range strings.Fields(query) {
		if strings.Count(token, "\"")%2 == 1 {
			quoted = !quoted
			out = append(out, token)
			continue
		}
		if quoted {
			out = append(out, token)
			continue
		}
		t := strings.ToLower(token)
		switch {
		case regexp.MustCompile(`^rating:[1-5]$`).MatchString(t):
			values.Set("f_srdd", strings.TrimPrefix(t, "rating:"))
		case t == "expunged":
			values.Set("f_sh", "on")
		case t == "nohide:uploader":
			values.Set("f_sfu", "on")
		case t == "nohide:language":
			values.Set("f_sfl", "on")
		case t == "nohide:tags":
			values.Set("f_sft", "on")
		case t == "nohide:all":
			values.Set("f_sfu", "on")
			values.Set("f_sfl", "on")
			values.Set("f_sft", "on")
		case strings.HasPrefix(t, "rating:") || strings.HasPrefix(t, "nohide:"): // invalid controlled token is silently stripped
		default:
			out = append(out, token)
		}
	}
	if len(values) > 0 {
		values.Set("advsearch", "1")
	}
	return strings.Join(out, " "), values
}
func ExtractCategoryQuery(query string, cfg config.Settings, translate bool) (string, []string) {
	var selected []string
	var remaining []string
	for _, token := range strings.Fields(query) {
		low := strings.ToLower(token)
		matched := false
		prefix := "category:"
		if translate {
			prefix = "category:"
		}
		if strings.HasPrefix(low, prefix) {
			name := strings.TrimPrefix(token, prefix)
			if strings.HasPrefix(strings.ToLower(name), "category:") {
				name = name[len("category:"):]
			}
			norm := strings.ToLower(strings.ReplaceAll(name, "_", " "))
			if mask, ok := categoryNames[norm]; ok {
				selected = append(selected, strings.TrimSpace(strings.Trim(name, "\"")))
				_ = mask
				matched = true
			} else {
				for _, f := range cfg.Facets {
					if strings.EqualFold(f.Name, name) {
						selected = append(selected, f.Name)
						matched = true
						break
					}
				}
			}
		} else if translate && strings.HasPrefix(token, "分类:") {
			name := strings.TrimPrefix(token, "分类:")
			for _, f := range cfg.Facets {
				if strings.EqualFold(f.Name, name) {
					selected = append(selected, f.Name)
					matched = true
					break
				}
			}
		}
		if !matched {
			remaining = append(remaining, token)
		}
	}
	return strings.Join(remaining, " "), selected
}
func CategoryMask(names []string, cfg config.Settings) int {
	allowed := 0
	valid := false
	for _, n := range names {
		mask, ok := 0, false
		for _, f := range cfg.Facets {
			if strings.EqualFold(f.Name, n) {
				mask, ok = f.Mask, true
				break
			}
		}
		if !ok {
			mask, ok = categoryNames[strings.ToLower(n)]
		}
		if ok {
			allowed |= (1023 ^ mask)
			valid = true
		}
	}
	if !valid {
		return 0
	}
	return 1023 ^ allowed
}
func IntersectCategoryMasks(explicit, keyword int) int {
	return 1023 ^ ((1023 ^ explicit) & (1023 ^ keyword))
}
