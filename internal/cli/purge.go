package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var captureDir = regexp.MustCompile(`^[0-9]{4,6}-[0-9]{4,6}$`)
var previousReport = regexp.MustCompile(`^previous-[0-9]{4,6}-[0-9]{4,6}\.xml$`)

// purgeRuns recognises only the private grouping format created by local run.
func purgeRuns(root string, age time.Duration, now time.Time) ([]string, error) {
	out := []string{}
	if age < 24*time.Hour || !process.UserOnly(root, true) {
		return out, config.ErrInvalid
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		return out, e
	}
	for _, entry := range entries {
		if !entry.IsDir() || !protocol.UUID(entry.Name()) {
			continue
		}
		p, e := config.SafePath(root, entry.Name())
		if e != nil || !process.UserOnly(p, true) {
			continue
		}
		files, e := os.ReadDir(p)
		if e != nil {
			return out, e
		}
		known := len(files) > 0
		latest := time.Time{}
		captures := 0
		for _, f := range files {
			q := filepath.Join(p, f.Name())
			if f.IsDir() && captureDir.MatchString(f.Name()) {
				inner, e := os.ReadDir(q)
				if e != nil || len(inner) != 2 || !process.UserOnly(q, true) {
					known = false
					break
				}
				captures++
				for _, s := range inner {
					v := filepath.Join(q, s.Name())
					if s.Name() != "stdout" && s.Name() != "stderr" || !process.UserOnly(v, false) {
						known = false
						break
					}
					st, e := s.Info()
					if e != nil {
						known = false
						break
					}
					if st.ModTime().After(latest) {
						latest = st.ModTime()
					}
				}
			} else if previousReport.MatchString(f.Name()) && process.UserOnly(q, false) {
				st, e := f.Info()
				if e != nil {
					known = false
					break
				}
				if st.ModTime().After(latest) {
					latest = st.ModTime()
				}
			} else {
				known = false
				break
			}
		}
		if known && captures > 0 && !latest.IsZero() && now.Sub(latest) >= age {
			if e = os.RemoveAll(p); e != nil {
				return out, e
			}
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
