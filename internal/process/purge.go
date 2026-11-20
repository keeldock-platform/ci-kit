package process

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var resultID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$`)

// Purge removes only inactive recognised capture directories; unknown content is retained.
func Purge(root string, olderThan time.Duration, now time.Time) ([]string, error) {
	if olderThan < 24*time.Hour {
		return nil, ErrInvalid
	}
	if _, e := SafePath(root, "."); e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		return nil, ErrInvalid
	}
	deleted := []string{}
	for _, entry := range entries {
		if !entry.IsDir() || !resultID.MatchString(entry.Name()) {
			continue
		}
		p := filepath.Join(root, entry.Name())
		if _, e := SafePath(root, entry.Name()); e != nil {
			continue
		}
		files, e := os.ReadDir(p)
		if e != nil {
			return nil, ErrCapture
		}
		recognised := len(files) == 2
		var newest time.Time
		for _, f := range files {
			if (f.Name() != "stdout" && f.Name() != "stderr") || !f.Type().IsRegular() {
				recognised = false
				break
			}
			s, e := f.Info()
			if e != nil {
				return nil, ErrCapture
			}
			if !s.Mode().IsRegular() {
				recognised = false
				break
			}
			if s.ModTime().After(newest) {
				newest = s.ModTime()
			}
		}
		if !recognised || newest.IsZero() || now.Sub(newest) < olderThan {
			continue
		}
		if e = os.RemoveAll(p); e != nil {
			return deleted, ErrCapture
		}
		deleted = append(deleted, entry.Name())
	}
	sort.Strings(deleted)
	return deleted, nil
}
