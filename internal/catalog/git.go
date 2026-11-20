package catalog

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/process"
)

var revision = regexp.MustCompile(`^[a-f0-9]{40}$`)

func objectCommand(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	gitPath, _ := ctx.Value(gitToolKey{}).(string)
	if gitPath == "" {
		gitPath = "/usr/bin/git"
		if runtime.GOOS == "windows" {
			gitPath = filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe")
		}
	}
	if !filepath.IsAbs(gitPath) {
		return nil, ErrInvalid
	}
	physical, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, ErrInvalid
	}
	resolved, e := filepath.EvalSymlinks(gitPath)
	if e != nil {
		return nil, ErrInvalid
	}
	st, e := os.Stat(resolved)
	if e != nil || !st.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	if !OutsideRoot(physical, resolved) {
		return nil, ErrInvalid
	}
	gitPath = resolved

	capture, e := os.MkdirTemp("", "ci-catalogue-")
	if e != nil {
		return nil, ErrInvalid
	}
	defer os.RemoveAll(capture)
	capture, e = filepath.EvalSymlinks(capture)
	if e != nil {
		return nil, ErrInvalid
	}
	private := filepath.Join(capture, "private")
	if process.MakePrivateDir(private) != nil {
		return nil, ErrInvalid
	}
	env := map[string]string{"PATH": filepath.Dir(gitPath), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_TERMINAL_PROMPT": "0"}
	result, e := process.Run(ctx, process.Spec{Root: physical, Cwd: ".", Argv: append([]string{gitPath, "--no-replace-objects", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-C", physical}, args...), ResultDir: filepath.Join(private, "run"), Timeout: 30 * time.Second, OutputLimit: int64(limit), Env: env})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e != nil || result.ExitCode != 0 || !result.CleanupComplete || result.Overflow || result.TimedOut {
		return nil, ErrInvalid
	}
	st, e = os.Stat(result.StdoutPath)
	if e != nil || st.Size() > int64(limit) {
		return nil, ErrInvalid
	}
	return os.ReadFile(result.StdoutPath)
}

// LoadAt reads descriptors and command directories from immutable Git blobs.
// It does not execute source code or inspect a dirty working-tree descriptor.
type gitToolKey struct{}

func LoadAt(root, sha string, gitPath ...string) ([]Unit, error) {
	return loadAtConfigured(context.Background(), root, sha, []string{"."}, "ci/units.json", true, true, gitPath...)
}
func loadAtConfigured(parent context.Context, root, sha string, unitRoots []string, sharedPath string, optionalShared, validate bool, gitPath ...string) ([]Unit, error) {
	if validateDiscovery(unitRoots, sharedPath) != nil {
		return nil, ErrInvalid
	}
	if parent == nil {
		parent = context.Background()
	}
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	if !revision.MatchString(sha) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if len(gitPath) > 1 {
		return nil, ErrInvalid
	}
	if len(gitPath) == 1 {
		ctx = context.WithValue(ctx, gitToolKey{}, gitPath[0])
	}
	raw, e := objectCommand(ctx, root, 8<<20, "ls-tree", "-r", "-z", sha)
	if e != nil {
		return nil, e
	}
	dirs := map[string]bool{".": true}
	links := map[string]bool{}
	type file struct{ name, oid string }
	files := []file{}
	for _, record := range bytes.Split(raw, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		head, name, ok := strings.Cut(string(record), "\t")
		fields := strings.Fields(head)
		if !ok || len(fields) != 3 {
			return nil, ErrInvalid
		}
		protected := path.Base(name) == "ci-unit.json" && descriptorIncluded(name, unitRoots) || name == sharedPath
		representable := safePattern(name) && !strings.ContainsAny(name, "*?[]{}")
		if !representable {
			if protected {
				return nil, ErrInvalid
			}
			// A safe command directory can still contain an ambiguous filename.
			for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
				if safePattern(directory) && !strings.ContainsAny(directory, "*?[]{}") {
					dirs[directory] = true
				}
			}
			continue
		}
		if fields[1] != "blob" {
			return nil, ErrInvalid
		}
		if fields[0] == "120000" {
			if path.Base(name) == "ci-unit.json" && descriptorIncluded(name, unitRoots) || name == sharedPath {
				return nil, ErrInvalid
			}
			links[name] = true
			continue
		}
		dir := path.Dir(name)
		for dir != "." {
			dirs[dir] = true
			dir = path.Dir(dir)
		}
		if path.Base(name) == "ci-unit.json" && descriptorIncluded(name, unitRoots) || name == sharedPath {
			files = append(files, file{name, fields[2]})
			if len(files) > 10000 {
				return nil, ErrInvalid
			}
		}
	}
	for _, unitRoot := range unitRoots {
		if !dirs[unitRoot] || linkedPath(links, unitRoot) {
			return nil, ErrInvalid
		}
	}
	foundShared := false
	for _, f := range files {
		foundShared = foundShared || f.name == sharedPath
	}
	if !optionalShared && !foundShared {
		return nil, ErrInvalid
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	units := make([]Unit, 0, len(files))
	for _, f := range files {
		raw, e := objectCommand(ctx, root, MaxDescriptorBytes, "cat-file", "blob", f.oid)
		if e != nil {
			return nil, e
		}
		if f.name == sharedPath {
			shared, e := DecodeShared(raw)
			if e != nil {
				return nil, e
			}
			units = append(units, shared.Units...)
			continue
		}
		u, e := Decode(raw)
		if e != nil {
			return nil, e
		}
		u.Root = path.Dir(f.name)
		for _, c := range u.Checks {
			for _, report := range c.Reports {
				if linkedPath(links, report) {
					return nil, ErrInvalid
				}
			}
			for _, commands := range c.Commands {
				for _, command := range commands {
					if !dirs[command.Cwd] || linkedPath(links, command.Cwd) {
						return nil, ErrInvalid
					}
				}
			}
		}
		units = append(units, u)
	}
	if len(units) == 0 {
		return nil, ErrInvalid
	}
	if validate && Validate(units) != nil {
		return nil, ErrInvalid
	}
	return units, nil
}

func linkedPath(links map[string]bool, p string) bool {
	for p != "." {
		if links[p] {
			return true
		}
		p = path.Dir(p)
	}
	return false
}

// LoadAtContext propagates caller cancellation through immutable Git metadata.
func LoadAtContext(ctx context.Context, root, sha string, gitPath ...string) ([]Unit, error) {
	return loadAtConfigured(ctx, root, sha, []string{"."}, "ci/units.json", true, true, gitPath...)
}
