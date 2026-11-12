package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type ImportReport struct{ State, Gallery, Cover, Payloads, Skipped, Conflicts int }

type importFile struct {
	path string
	rel  string
	kind string
}

// ImportSidecars copies legacy state and presentation sidecars into a separate
// state volume. ZIPs and staging files are never copied or changed. zipRoots
// lists payload roots that may hold the source ZIPs (for cross-instance import).
func ImportSidecars(sourceRoot, stateRoot string, zipRoots ...string) (ImportReport, error) {
	return ImportSidecarsContext(context.Background(), sourceRoot, stateRoot, zipRoots...)
}

func ImportSidecarsContext(ctx context.Context, sourceRoot, stateRoot string, zipRoots ...string) (ImportReport, error) {
	return importSidecars(ctx, sourceRoot, stateRoot, nil, zipRoots...)
}

// ImportArchive is the offline CLI workflow: recover any interrupted payload
// moves, import missing sidecars, then move owned legacy ZIPs into flat storage.
func ImportArchive(sourceRoot, stateRoot string, zipRoot string) (ImportReport, error) {
	recovered, err := recoverPayloadMigrationJournals(zipRoot, stateRoot)
	if err != nil {
		return ImportReport{}, err
	}
	report, err := ImportSidecars(sourceRoot, stateRoot, zipRoot)
	if err != nil {
		return report, err
	}
	moved, conflicts, err := MigrateLegacyPayloads(zipRoot, stateRoot)
	report.Payloads += recovered + moved
	report.Conflicts += conflicts
	return report, err
}

func importSidecars(ctx context.Context, sourceRoot, stateRoot string, allowRoot func(int, string) bool, zipRoots ...string) (ImportReport, error) {
	var report ImportReport
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return report, err
	}
	base, err := filepath.Abs(sourceRoot)
	if err != nil {
		return report, err
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	groups := map[string][]importFile{}
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
			return nil
		}
		kind := ""
		switch d.Name() {
		case stateName, metaName:
			kind = "state"
		case galleryName, metadataName:
			kind = "gallery"
		case coverName:
			kind = "cover"
		default:
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == ".." || filepath.IsAbs(rel) {
			return errors.New("invalid legacy sidecar path")
		}
		groups[filepath.Dir(rel)] = append(groups[filepath.Dir(rel)], importFile{path: path, rel: rel, kind: kind})
		return nil
	})
	if err != nil {
		return report, err
	}

	if len(zipRoots) == 0 {
		zipRoots = []string{base}
	}
	for _, files := range groups {
		stateFiles := []importFile{}
		identityGID, identityToken := 0, ""
		valid := true
		for _, file := range files {
			if file.kind != "state" {
				continue
			}
			stateFiles = append(stateFiles, file)
			b, err := os.ReadFile(file.path)
			if err != nil {
				return report, err
			}
			var m map[string]any
			if json.Unmarshal(b, &m) != nil || !validStateIdentity(m) {
				valid = false
				continue
			}
			gid, token := toInt(m["gid"]), toString(m["token"])
			if identityGID == 0 {
				identityGID, identityToken = gid, token
			} else if gid != identityGID || token != identityToken {
				valid = false
			}
		}
		if len(stateFiles) == 0 || !valid {
			report.Skipped += len(files)
			continue
		}
		rootGID, rootToken := identityGID, identityToken
		for _, file := range stateFiles {
			b, _ := os.ReadFile(file.path)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			if toInt(m["root_gid"]) > 0 && toString(m["root_token"]) != "" {
				rootGID, rootToken = toInt(m["root_gid"]), toString(m["root_token"])
				break
			}
		}
		if allowRoot != nil && !allowRoot(rootGID, rootToken) {
			report.Skipped += len(files)
			continue
		}

		conflict := false
		stateTarget, targetErr := validateImportTarget(stateRoot, filepath.Join(filepath.Dir(files[0].rel), stateName))
		if targetErr != nil {
			report.Conflicts += len(files)
			continue
		}
		targetDir := filepath.Dir(stateTarget)
		for _, name := range []string{stateName, metaName} {
			path := filepath.Join(targetDir, name)
			if st, err := os.Lstat(path); err == nil {
				if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
					conflict = true
					continue
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return report, err
				}
				var existing map[string]any
				if json.Unmarshal(b, &existing) != nil || !validStateIdentity(existing) || toInt(existing["gid"]) != identityGID || toString(existing["token"]) != identityToken {
					conflict = true
				}
			} else if !os.IsNotExist(err) {
				return report, err
			}
		}
		for _, file := range files {
			target, err := validateImportTarget(stateRoot, file.rel)
			if err != nil {
				conflict = true
				continue
			}
			if st, err := os.Lstat(target); err == nil {
				if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
					conflict = true
					continue
				}
				if file.kind == "state" {
					b, err := os.ReadFile(target)
					if err != nil {
						return report, err
					}
					var m map[string]any
					if json.Unmarshal(b, &m) != nil || !validStateIdentity(m) || toInt(m["gid"]) != identityGID || toString(m["token"]) != identityToken {
						conflict = true
						continue
					}
				}
				same, err := sameFile(file.path, target)
				if err != nil {
					return report, err
				}
				if !same {
					conflict = true
				}
			} else if !os.IsNotExist(err) {
				return report, err
			}
		}
		if conflict {
			report.Conflicts += len(files)
			continue
		}

		for _, file := range files {
			target, err := validateImportTarget(stateRoot, file.rel)
			if err != nil {
				return report, err
			}
			if _, err := os.Lstat(target); err == nil {
				report.Skipped++
				continue
			} else if !os.IsNotExist(err) {
				return report, err
			}
			if err := writeDirtyMarkerAt(stateRoot); err != nil {
				return report, err
			}
			if err := copyAtomicNoClobber(file.path, target); err != nil {
				if errors.Is(err, os.ErrExist) {
					report.Conflicts++
					continue
				}
				return report, err
			}
			switch file.kind {
			case "state":
				report.State++
			case "gallery":
				report.Gallery++
			case "cover":
				report.Cover++
			}
			if file.kind == "state" {
				if err := indexImportedZIP(ctx, zipRoots, base, stateRoot, file.rel, target); err != nil {
					return report, err
				}
			}
		}
	}
	return report, nil
}

func validStateIdentity(m map[string]any) bool {
	if m == nil || toInt(m["gid"]) <= 0 || toString(m["token"]) == "" {
		return false
	}
	rootGID, rootToken := toInt(m["root_gid"]), toString(m["root_token"])
	return (rootGID == 0 && rootToken == "") || (rootGID > 0 && rootToken != "")
}

func indexImportedZIP(ctx context.Context, roots []string, sourceRoot, stateRoot, stateRel, statePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || !validStateIdentity(m) {
		return nil
	}
	gid, token := toInt(m["gid"]), toString(m["token"])
	rootGID, rootToken := toInt(m["root_gid"]), toString(m["root_token"])
	if rootGID <= 0 || rootToken == "" {
		rootGID, rootToken = gid, token
	}
	rootMeta := findRootState(sourceRoot, stateRoot, rootGID, rootToken)
	var zipRel string
	switch {
	case toString(rootMeta["payload_layout"]) == "flat-gid":
		zipRel = filepath.Join(strconv.Itoa(gid) + ".zip")
	case gid == rootGID && token == rootToken && toString(rootMeta["layout"]) == "flat":
		zipRel = filepath.Join(filepath.Dir(stateRel), strconv.Itoa(gid)+".zip")
	case toString(m["storage_dir"]) == "." && toString(rootMeta["layout"]) == "flat":
		zipRel = filepath.Join(filepath.Dir(stateRel), strconv.Itoa(gid)+".zip")
	default:
		zipRel = filepath.Join(filepath.Dir(stateRel), zipName)
	}
	var zipPath string
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidate := filepath.Join(root, zipRel)
		if _, err := os.Stat(candidate); err == nil {
			zipPath = candidate
			break
		} else if !os.IsNotExist(err) {
			return nil
		}
	}
	if zipPath == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return nil
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil
	}
	pages := len(zr.File)
	_ = zr.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if pages == 0 {
		return nil
	}
	m["archive_size"], m["page_count"] = st.Size(), pages
	if versions, ok := m[versionsName].(map[string]any); ok {
		if v, ok := versions[key(gid, token)].(map[string]any); ok {
			v["archive_size"], v["page_count"] = st.Size(), pages
		}
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicImport(statePath, encoded)
}

func findRootState(sourceRoot, stateRoot string, gid int, token string) map[string]any {
	for _, root := range []string{sourceRoot, stateRoot} {
		for _, name := range []string{stateName, metaName} {
			b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(gid), name))
			if err != nil {
				continue
			}
			var m map[string]any
			if json.Unmarshal(b, &m) == nil && toInt(m["gid"]) == gid && toString(m["token"]) == token {
				return m
			}
		}
	}
	return nil
}

func sameFile(a, b string) (bool, error) {
	aBytes, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bBytes, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	if filepath.Base(a) == stateName || filepath.Base(a) == metaName {
		var left, right map[string]any
		if json.Unmarshal(aBytes, &left) == nil && json.Unmarshal(bBytes, &right) == nil {
			stripDerivedFields(left)
			stripDerivedFields(right)
			return reflect.DeepEqual(left, right), nil
		}
	}
	return bytes.Equal(aBytes, bBytes), nil
}

func stripDerivedFields(m map[string]any) {
	delete(m, "archive_size")
	delete(m, "page_count")
	delete(m, "payload_layout")
	for _, v := range m {
		if child, ok := v.(map[string]any); ok {
			stripDerivedFields(child)
		}
	}
}

func copyAtomicNoClobber(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".archive-import-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmpPath, dst); err != nil {
		if os.IsExist(err) {
			return os.ErrExist
		}
		return err
	}
	if err := syncDirectory(filepath.Dir(dst)); err != nil {
		return err
	}
	return nil
}

func validateImportTarget(root, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid import target path")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}
	target := filepath.Join(rootAbs, clean)
	if relCheck, err := filepath.Rel(rootAbs, target); err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", errors.New("import target escapes state root")
	}
	current := rootAbs
	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", errors.New("import target traverses a non-directory path")
		}
	}
	return target, nil
}

func writeAtomicImport(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".archive-state-import-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
