package state

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SessionEntry struct {
	ID      string
	Name    string
	Path    string
	RelPath string
	ModTime time.Time
	Size    int64
}

type SessionIndex struct {
	Root    string
	Entries []SessionEntry
}

func ScanSessions(root string) (SessionIndex, error) {
	index := SessionIndex{Root: root}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
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

	sort.SliceStable(index.Entries, func(i, j int) bool {
		left := index.Entries[i]
		right := index.Entries[j]
		if left.ModTime.Equal(right.ModTime) {
			return left.RelPath < right.RelPath
		}
		return left.ModTime.After(right.ModTime)
	})

	return index, nil
}

func LoadSessions(root string, limit int) ([]SessionEntry, error) {
	index, err := ScanSessions(root)
	if err != nil {
		return nil, err
	}
	return index.Limit(limit), nil
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
