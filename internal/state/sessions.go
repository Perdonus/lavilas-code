package state

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SessionEntry struct {
	Path    string
	RelPath string
	ModTime time.Time
	Size    int64
}

func LoadSessions(root string, limit int) ([]SessionEntry, error) {
	var sessions []SessionEntry
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = d.Name()
		}
		sessions = append(sessions, SessionEntry{
			Path:    path,
			RelPath: rel,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}
