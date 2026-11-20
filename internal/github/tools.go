package github

import (
	"context"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var toolVersionRE = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+([-+][a-zA-Z0-9.-]+)?\b`)

// ToolVersion captures a fixed source-free version query and returns only its version token.
func ToolVersion(ctx context.Context, root, name string) (string, error) {
	p, e := TrustedExecutable(name, root)
	if e != nil {
		return "", e
	}
	dir, e := os.MkdirTemp("", "kd-ci-version-")
	if e != nil {
		return "", Fail(2, "configuration")
	}
	defer os.RemoveAll(dir)
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		return "", Fail(2, "configuration")
	}
	res, e := process.Run(ctx, process.Spec{Root: root, Cwd: ".", Argv: []string{p, "--version"}, ResultDir: filepath.Join(dir, "capture"), Timeout: 10 * time.Second, OutputLimit: 64 << 10, Env: map[string]string{"PATH": filepath.Dir(p)}})
	if e != nil || res.ExitCode != 0 {
		return "", Fail(2, "configuration")
	}
	f, e := os.Open(res.StdoutPath)
	if e != nil {
		return "", Fail(2, "configuration")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if e != nil || len(b) > 64<<10 {
		return "", Fail(2, "configuration")
	}
	v := toolVersionRE.Find(b)
	if len(v) == 0 {
		return "", Fail(2, "configuration")
	}
	return string(v), nil
}
