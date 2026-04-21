package state

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const sessionIndexFileName = ".index.json"

type SessionEntry struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	RelPath string    `json:"rel_path"`
	CWD     string    `json:"cwd,omitempty"`
	Branch  string    `json:"branch,omitempty"`
	Preview string    `json:"preview,omitempty"`
	Created time.Time `json:"created_at,omitempty"`
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
}

type SessionIndex struct {
	Root    string
	Entries []SessionEntry
}

type sessionIndexFile struct {
	GeneratedAt string         `json:"generated_at,omitempty"`
	Entries     []SessionEntry `json:"entries"`
}

func ScanSessions(root string) (SessionIndex, error) {
	index := SessionIndex{Root: root}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == sessionIndexFileName || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry, err := loadSessionEntry(root, path, info)
		if err != nil {
			return err
		}
		index.Entries = append(index.Entries, entry)
		return nil
	})
	if err != nil {
		return SessionIndex{}, err
	}

	sortSessionEntries(index.Entries)
	_ = SaveSessionIndex(index)
	return index, nil
}

func LoadSessions(root string, limit int) ([]SessionEntry, error) {
	index, err := LoadSessionIndex(root)
	if err == nil {
		return index.Limit(limit), nil
	}
	if !os.IsNotExist(err) {
		scanned, scanErr := ScanSessions(root)
		if scanErr != nil {
			return nil, scanErr
		}
		return scanned.Limit(limit), nil
	}

	index, err = ScanSessions(root)
	if err != nil {
		return nil, err
	}
	return index.Limit(limit), nil
}

func ResolveSessionEntry(root string, selector string) (SessionEntry, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return SessionEntry{}, os.ErrNotExist
	}

	candidate := selector
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return loadSessionEntry(root, candidate, info)
	}

	entries, err := LoadSessions(root, 0)
	if err != nil {
		return SessionEntry{}, err
	}
	for _, entry := range entries {
		if sessionEntryMatchesSelector(entry, selector) {
			return entry, nil
		}
	}
	for _, entry := range entries {
		meta, err := LoadSessionMeta(entry.Path)
		if err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(meta.SessionID), selector) {
			return entry, nil
		}
	}
	return SessionEntry{}, os.ErrNotExist
}

func LoadSessionIndex(root string) (SessionIndex, error) {
	data, err := os.ReadFile(sessionIndexPath(root))
	if err != nil {
		return SessionIndex{}, err
	}

	var payload sessionIndexFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return SessionIndex{}, err
	}

	index := SessionIndex{
		Root:    root,
		Entries: payload.Entries,
	}
	changed, err := hydrateSessionIndexEntries(&index)
	if err != nil {
		return SessionIndex{}, err
	}
	sortSessionEntries(index.Entries)
	if changed {
		_ = SaveSessionIndex(index)
	}
	return index, nil
}

func SaveSessionIndex(index SessionIndex) error {
	payload := sessionIndexFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Entries:     index.Clone().Entries,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(index.Root, 0o755); err != nil {
		return err
	}
	return os.WriteFile(sessionIndexPath(index.Root), data, 0o644)
}

func UpsertSessionIndexEntry(root string, entry SessionEntry) error {
	index, err := LoadSessionIndex(root)
	if err != nil {
		if os.IsNotExist(err) {
			index = SessionIndex{Root: root}
		} else {
			index, err = ScanSessions(root)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if os.IsNotExist(err) {
				index = SessionIndex{Root: root}
			}
		}
	}

	index.Root = root
	for i := range index.Entries {
		if index.Entries[i].Path == entry.Path {
			index.Entries[i] = entry
			sortSessionEntries(index.Entries)
			return SaveSessionIndex(index)
		}
	}
	index.Entries = append(index.Entries, entry)
	sortSessionEntries(index.Entries)
	return SaveSessionIndex(index)
}

func (s SessionIndex) Clone() SessionIndex {
	entries := make([]SessionEntry, len(s.Entries))
	copy(entries, s.Entries)
	return SessionIndex{
		Root:    s.Root,
		Entries: entries,
	}
}

func (s SessionIndex) Latest() (SessionEntry, bool) {
	if len(s.Entries) == 0 {
		return SessionEntry{}, false
	}
	return s.Entries[0], true
}

func (s SessionIndex) Limit(limit int) []SessionEntry {
	entries := s.Entries
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	result := make([]SessionEntry, len(entries))
	copy(result, entries)
	return result
}

func loadSessionEntry(root string, path string, info fs.FileInfo) (SessionEntry, error) {
	meta, err := LoadSessionMeta(path)
	if err != nil {
		return SessionEntry{}, err
	}
	return buildSessionEntry(root, path, info, meta), nil
}

func buildSessionEntry(root string, path string, info fs.FileInfo, meta SessionMeta) SessionEntry {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	name := filepath.Base(path)
	id := strings.TrimSpace(meta.SessionID)
	if id == "" {
		id = strings.TrimSuffix(name, filepath.Ext(name))
	}
	createdAt := info.ModTime()
	if !meta.CreatedAt.IsZero() {
		createdAt = meta.CreatedAt.UTC()
	}
	modTime := info.ModTime()
	if !meta.UpdatedAt.IsZero() {
		modTime = meta.UpdatedAt.UTC()
	}
	return SessionEntry{
		ID:      id,
		Name:    name,
		Path:    path,
		RelPath: rel,
		CWD:     meta.CWD,
		Branch:  meta.Branch,
		Preview: meta.Preview,
		Created: createdAt,
		ModTime: modTime,
		Size:    info.Size(),
	}
}

func hydrateSessionIndexEntries(index *SessionIndex) (bool, error) {
	changed := false
	for i := range index.Entries {
		entry := &index.Entries[i]
		if strings.TrimSpace(entry.CWD) != "" &&
			strings.TrimSpace(entry.Preview) != "" &&
			!entry.Created.IsZero() &&
			!entry.ModTime.IsZero() &&
			entry.Size > 0 {
			continue
		}
		info, err := os.Stat(entry.Path)
		if err != nil {
			return false, err
		}
		hydrated, err := loadSessionEntry(index.Root, entry.Path, info)
		if err != nil {
			return false, err
		}
		if hydrated.CWD != "" {
			entry.CWD = hydrated.CWD
			changed = true
		}
		if entry.Branch != hydrated.Branch {
			entry.Branch = hydrated.Branch
			changed = true
		}
		if entry.Preview != hydrated.Preview {
			entry.Preview = hydrated.Preview
			changed = true
		}
		if !hydrated.Created.IsZero() && !entry.Created.Equal(hydrated.Created) {
			entry.Created = hydrated.Created
			changed = true
		}
		if !hydrated.ModTime.IsZero() && !entry.ModTime.Equal(hydrated.ModTime) {
			entry.ModTime = hydrated.ModTime
			changed = true
		}
		if entry.Size != hydrated.Size {
			entry.Size = hydrated.Size
			changed = true
		}
	}
	return changed, nil
}

func sortSessionEntries(entries []SessionEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.ModTime.Equal(right.ModTime) {
			return left.RelPath < right.RelPath
		}
		return left.ModTime.After(right.ModTime)
	})
}

func sessionIndexPath(root string) string {
	return filepath.Join(root, sessionIndexFileName)
}

func sessionEntryMatchesSelector(entry SessionEntry, selector string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return false
	}
	normalizedSelector := normalizeSessionSelector(selector)
	for _, candidate := range []string{
		entry.Path,
		entry.RelPath,
		entry.Name,
		entry.ID,
	} {
		if normalizedSelector == normalizeSessionSelector(candidate) {
			return true
		}
	}
	return false
}

func normalizeSessionSelector(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(value))
}
