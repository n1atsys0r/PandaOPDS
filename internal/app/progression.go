package app

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pandaopds/internal/progression"
)

func (s *Server) refreshProgressionArchiveIndex() {
	if s.Progression == nil || s.Store == nil {
		return
	}
	s.progressionArchiveMu.Lock()
	defer s.progressionArchiveMu.Unlock()
	archiveGroups := []progression.Group{}
	for _, group := range s.Store.ProgressionArchiveSnapshot() {
		activeGID, _ := strconv.Atoi(group.Active[0])
		activeToken := group.Active[1]
		if activeGID <= 0 || activeToken == "" {
			activeGID, _ = strconv.Atoi(group.Root[0])
			activeToken = group.Root[1]
		}
		pg := progression.Group{Active: progression.Identity{GID: activeGID, Token: activeToken}}
		for _, id := range group.Identities {
			gid, _ := strconv.Atoi(id[0])
			if gid > 0 && id[1] != "" {
				pg.Identities = append(pg.Identities, progression.Identity{GID: gid, Token: id[1]})
			}
		}
		for _, id := range group.Available {
			gid, _ := strconv.Atoi(id[0])
			if gid > 0 && id[1] != "" {
				pg.Available = append(pg.Available, progression.Identity{GID: gid, Token: id[1]})
			}
		}
		archiveGroups = append(archiveGroups, pg)
	}
	_ = s.Progression.ReconcileArchives(archiveGroups)
}

func (s *Server) progressionAuthentication(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/opds-authentication+json")
	writeBytes(w, 200, "application/opds-authentication+json", nil, s.progressionAuthDocument())
}

func (s *Server) progressionAuthDocument() []byte {
	b, _ := json.Marshal(map[string]any{"id": s.builder().Href("/opds/authentication.json"), "title": "PandaOPDS", "authentication": []map[string]any{{"type": "http://opds-spec.org/auth/basic", "labels": map[string]string{"login": "Username", "password": "Password"}}}})
	return b
}

func (s *Server) progressionHandler(w http.ResponseWriter, r *http.Request) {
	bad := func(status int, typ, title string) {
		w.Header().Set("Content-Type", "application/problem+json")
		writeBytes(w, status, "application/problem+json", nil, []byte(`{"type":"`+typ+`","title":"`+title+`"}`))
	}
	gid, token, err := pathID(r)
	if err != nil {
		bad(400, "https://registry.opds.io/error#progression-invalid-payload", "Progression could not be updated for this publication.")
		return
	}
	token = strings.ToLower(token)
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		docs, err := s.Progression.Get(gid, token)
		if err != nil {
			bad(500, "about:blank", "Internal Server Error")
			return
		}
		w.Header().Set("Content-Type", "application/opds-progression+json")
		if r.Method == http.MethodHead {
			if len(docs) == 0 {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(200)
			}
			return
		}
		if len(docs) == 0 {
			w.WriteHeader(200)
			return
		}
		writeBytes(w, 200, "application/opds-progression+json", nil, documentBytes(docs[0]))
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT, HEAD")
		bad(405, "about:blank", "Method not allowed.")
		return
	}
	const maxBody = 1 << 20
	bodyBytes, readErr := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	dec := json.NewDecoder(strings.NewReader(string(bodyBytes)))
	var raw struct {
		Modified    string             `json:"modified"`
		Device      progression.Device `json:"device"`
		Progression *float64           `json:"progression"`
		Title       json.RawMessage    `json:"title"`
		References  []string           `json:"references"`
	}
	if readErr != nil || len(bodyBytes) > maxBody || dec.Decode(&raw) != nil || len(bodyBytes) == 0 {
		bad(400, "https://registry.opds.io/error#progression-invalid-payload", "Progression could not be updated due to an invalid payload.")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		bad(400, "https://registry.opds.io/error#progression-invalid-payload", "Progression could not be updated due to an invalid payload.")
		return
	}
	modified, e := time.Parse(time.RFC3339Nano, raw.Modified)
	deviceURL, eURL := url.Parse(raw.Device.ID)
	valid := e == nil && raw.Modified != "" && raw.Device.Name != "" && strings.TrimSpace(raw.Device.Name) == raw.Device.Name && eURL == nil && deviceURL.IsAbs() && deviceURL.Scheme != "" && raw.Progression != nil && *raw.Progression >= 0 && *raw.Progression <= 1 && !math.IsNaN(*raw.Progression) && !math.IsInf(*raw.Progression, 0)
	var top map[string]json.RawMessage
	if json.Unmarshal(bodyBytes, &top) != nil {
		valid = false
	}
	if rawRefs, ok := top["references"]; ok && string(rawRefs) == "null" {
		valid = false
	}
	if rawRefs, ok := top["references"]; ok {
		var elems []json.RawMessage
		if json.Unmarshal(rawRefs, &elems) != nil {
			valid = false
		} else {
			for _, elem := range elems {
				var value string
				if string(elem) == "null" || json.Unmarshal(elem, &value) != nil {
					valid = false
					break
				}
			}
		}
	}
	var title *string
	if rawTitle, ok := top["title"]; ok {
		if string(rawTitle) == "null" || json.Unmarshal(rawTitle, &title) != nil {
			valid = false
		}
	}
	for _, ref := range raw.References {
		if strings.TrimSpace(ref) != ref {
			valid = false
			break
		}
		if _, e := url.Parse(ref); e != nil {
			valid = false
			break
		}
	}
	if !valid {
		bad(400, "https://registry.opds.io/error#progression-invalid-payload", "Progression could not be updated due to an invalid payload.")
		return
	}
	doc := progression.Document{Modified: modified.UTC(), Device: raw.Device, Progression: *raw.Progression, Title: title, References: raw.References}
	status, responseDoc, err := s.Progression.Put(gid, token, doc)
	if err != nil {
		if err == progression.ErrConflict {
			bad(409, "https://registry.opds.io/error#progression-date", "A more recent progression point is already available.")
		} else {
			bad(507, "https://registry.opds.io/error#progression-invalid-payload", "Progression storage capacity reached.")
		}
		return
	}
	w.Header().Set("Content-Type", "application/opds-progression+json")
	writeBytes(w, status, "application/opds-progression+json", nil, documentBytes(responseDoc))
}

func documentBytes(d progression.Document) []byte { b, _ := json.Marshal(d); return b }
