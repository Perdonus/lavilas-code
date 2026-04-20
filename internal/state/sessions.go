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
		index.Entries = append(index.Entries, buildSessionEntry(root, path, info))
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
	sortSessionEntries(index.Entries)
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

func buildSessionEntry(root string, path string, info fs.FileInfo) SessionEntry {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	name := filepath.Base(path)
	return SessionEntry{
		ID:      strings.TrimSuffix(name, filepath.Ext(name)),
		Name:    name,
		Path:    path,
		RelPath: rel,
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
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
