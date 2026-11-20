package packs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

// VerifyAndroidPackages reads the official sdkmanager installed-package table.
// Required committed package revisions must match after the maintained action's
// setup, so a new mutable stable-channel download cannot silently change inputs.
func VerifyAndroidPackages(data []byte, want map[string]string) error {
	if len(data) == 0 || len(data) > 1<<20 || len(want) == 0 {
		return ErrSupport
	}
	got := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		columns := strings.Split(line, "|")
		if len(columns) < 3 {
			continue
		}
		path, version := strings.TrimSpace(columns[0]), strings.TrimSpace(columns[1])
		if path == "Path" || strings.HasPrefix(path, "---") {
			continue
		}
		if _, ok := want[path]; !ok {
			continue
		}
		if got[path] != "" {
			return ErrSupport
		}
		got[path] = version
	}
	for path, version := range want {
		if got[path] != version {
			return ErrSupport
		}
	}
	return nil
}

// VerifySDKFiles binds executable bytes to privately reviewed digest records.
func VerifySDKFiles(ctx context.Context, root string, digests map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if len(digests) == 0 {
		return ErrSupport
	}
	for rel, want := range digests {
		path, e := config.SafePath(root, rel)
		if e != nil {
			return ErrSupport
		}
		st, e := os.Lstat(path)
		if e != nil || !st.Mode().IsRegular() || st.Size() > 8<<30 {
			return ErrSupport
		}
		f, e := os.Open(path)
		if e != nil {
			return ErrSupport
		}
		h := sha256.New()
		_, e = io.Copy(h, io.LimitReader(contextReader{ctx, f}, 8<<30))
		closeErr := f.Close()
		if e != nil || closeErr != nil || hex.EncodeToString(h.Sum(nil)) != want {
			return ErrSupport
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}

// VerifyAndroidCommandLine binds the installed command-line directory metadata;
// sdkmanager may omit this preinstalled package from its installed-package table.
func VerifyAndroidCommandLine(data []byte, want string) error {
	if len(data) == 0 || len(data) > 65536 || !numericVersion.MatchString(want) {
		return ErrSupport
	}
	found := ""
	seen := false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "Pkg.Revision" {
			if seen {
				return ErrSupport
			}
			seen = true
			found = strings.TrimSpace(value)
		}
	}
	if found != want {
		return ErrSupport
	}
	return nil
}
