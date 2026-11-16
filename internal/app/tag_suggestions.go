package app

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

const tagSuggestionLimit = 10

func (s *Server) tagSuggestions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 256 {
		runes := []rune(query)
		query = string(runes[:256])
	}
	suggestions := s.Service.TagTranslator().Suggestions(query, tagSuggestionLimit)
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": suggestions})
}
