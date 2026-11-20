package planner

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

//go:embed adapter.cjs
var adapter []byte

//go:embed package-lock.json
var packageLock []byte

type nxFeature struct {
	Name   string   `json:"name"`
	Root   string   `json:"root"`
	Inputs []string `json:"inputs"`
	Checks []string `json:"checks"`
}
type nxProjection struct {
	Features []nxFeature `json:"features"`
	Edges    [][2]string `json:"edges"`
}

func cleanEnv() []string {
	env := []string{}
	for _, k := range []string{"PATH", "HOME", "SystemRoot", "SYSTEMROOT", "WINDIR", "TMPDIR", "TEMP", "TMP"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "NX_DAEMON=false", "NX_CACHE_PROJECT_GRAPH=false", "NX_NO_CLOUD=true", "NX_SKIP_NX_CACHE=true", "NX_INTERACTIVE=false", "CI=true", "NX_NATIVE_COMMAND_RUNNER=false", "NX_LOAD_DOT_ENV_FILES=false", "RUSTUP_AUTO_INSTALL=0")
}
func privateCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	registry, _ := ctx.Value(toolRegistryKey{}).(map[string]string)
	sourceRoot, _ := ctx.Value(sourceRootKey{}).(string)
	if sourceRoot == "" {
		sourceRoot = dir
	}
	exe, e := approvedExecutable(sourceRoot, name, registry[name])
	if e != nil {
		return nil, ErrInvalid
	}
	physical, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return nil, ErrInvalid
	}
	capture, e := os.MkdirTemp("", "ci-metadata-")
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
	env := map[string]string{}
	for _, pair := range cleanEnv() {
		k, v, _ := strings.Cut(pair, "=")
		env[k] = v
	}
	dirs := []string{filepath.Dir(exe)}
	for tool, path := range registry {
		if bound, e := approvedExecutable(sourceRoot, tool, path); e == nil {
			dirs = append(dirs, filepath.Dir(bound))
		}
	}
	if runtime.GOOS != "windows" {
		dirs = append(dirs, "/usr/bin", "/bin")
	}
	env["PATH"] = strings.Join(dirs, string(os.PathListSeparator))
	result, e := process.Run(ctx, process.Spec{Root: physical, Cwd: ".", Argv: append([]string{exe}, args...), ResultDir: filepath.Join(private, "run"), Timeout: 90 * time.Second, OutputLimit: 8 << 20, Env: env})
	if e != nil || result.ExitCode != 0 || !result.CleanupComplete || result.Overflow || result.TimedOut {
		return nil, ErrInvalid
	}
	out, e := readBounded(result.StdoutPath, 8<<20)
	if e != nil {
		return nil, ErrInvalid
	}
	return out, nil
}
func nxAffected(root string, units []catalog.Unit, changed []string, opts Options) ([]string, map[string][]string, string) {
	ctx, cancel := context.WithTimeout(optionContext(opts), 90*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, toolRegistryKey{}, opts.Executables)
	ctx = context.WithValue(ctx, sourceRootKey{}, root)
	toolDir := opts.ToolDirectory
	if toolDir == "" || !filepath.IsAbs(toolDir) {
		return nil, nil, "missing trusted graph tool"
	}
	sourceAbs, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, nil, "invalid graph root"
	}
	toolAbs, e := filepath.EvalSymlinks(toolDir)
	if e != nil || toolAbs != filepath.Clean(toolDir) {
		return nil, nil, "untrusted graph tool"
	}
	if !catalog.OutsideRoot(sourceAbs, toolAbs) {
		return nil, nil, "source-contained graph tool"
	}
	st, e := os.Stat(toolAbs)
	if e != nil || !st.IsDir() || runtime.GOOS != "windows" && st.Mode().Perm()&0022 != 0 {
		return nil, nil, "unprotected graph tool"
	}
	installedAdapter, e := readBounded(filepath.Join(toolAbs, "adapter.cjs"), 1<<20)
	if e != nil || !bytes.Equal(installedAdapter, adapter) {
		return nil, nil, "graph adapter mismatch"
	}
	installedLock, e := readBounded(filepath.Join(toolAbs, "package-lock.json"), 2<<20)
	if e != nil || !bytes.Equal(installedLock, packageLock) {
		return nil, nil, "graph lock mismatch"
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if config.Read(filepath.Join(toolDir, "node_modules/nx/package.json"), &pkg, 1<<20) != nil {
		// Package metadata has upstream-owned fields: bounded decoding here is intentionally structural.
		raw, e := os.ReadFile(filepath.Join(toolDir, "node_modules/nx/package.json"))
		if e != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &pkg) != nil {
			return nil, nil, "missing graph tool"
		}
	}
	if pkg.Version != "23.3.2" {
		return nil, nil, "graph version mismatch"
	}
	projection, reason := projectMetadata(ctx, root, units)
	if reason != "" {
		return nil, nil, reason
	}
	analysis, cleanup, e := newAnalysisWorkspace()
	if e != nil {
		return nil, nil, "analysis workspace unavailable"
	}
	defer cleanup()
	write := func(rel string, data []byte) bool {
		p, e := config.SafePath(analysis, rel)
		if e != nil {
			return false
		}
		if os.MkdirAll(filepath.Dir(p), 0700) != nil {
			return false
		}
		return os.WriteFile(p, data, 0600) == nil
	}
	encoded, e := json.Marshal(projection)
	if e != nil || !write("catalogue.json", encoded) || !write("adapter.cjs", adapter) {
		return nil, nil, "graph projection failure"
	}
	// Resolve the reviewed devkit from the kit, never from the source checkout.
	adapterPath := filepath.Join(toolDir, "adapter.cjs")
	if !write("nx.json", mustJSON(map[string]any{"plugins": []string{adapterPath}, "neverConnectToCloud": true, "useInferencePlugins": false, "defaultBase": "analysis-base"})) || !write("package.json", []byte(`{"name":"private-analysis","private":true}`)) {
		return nil, nil, "graph projection failure"
	}
	if _, e = privateCommand(ctx, analysis, "node", filepath.Join(toolDir, "adapter.cjs"), "--prepare-workspace", analysis); e != nil {
		return nil, nil, "graph module projection failure"
	}
	// A clean repository with read-only object alternates provides exact source revisions
	// without evaluating source configuration, hooks, dotenv files or plugins.
	if _, e = privateCommand(ctx, analysis, "git", "init", "--quiet"); e != nil {
		return nil, nil, "analysis git failure"
	}
	common, e := git(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return nil, nil, "analysis git failure"
	}
	objectDir := filepath.Join(strings.TrimSpace(string(common)), "objects")
	if strings.ContainsAny(objectDir, "\r\n") {
		return nil, nil, "analysis git failure"
	}
	if !write(".git/objects/info/alternates", []byte(objectDir+"\n")) {
		return nil, nil, "analysis git failure"
	}
	sha := opts.SHA
	base := opts.Base
	if !shaPattern.MatchString(sha) {
		raw, e := git(ctx, root, "rev-parse", "HEAD")
		if e != nil {
			return nil, nil, "missing graph revision"
		}
		sha = strings.TrimSpace(string(raw))
	}
	if !shaPattern.MatchString(base) {
		base = sha
	}
	if _, e = privateCommand(ctx, analysis, "git", "update-ref", "refs/heads/analysis-head", sha); e != nil {
		return nil, nil, "analysis git failure"
	}
	if _, e = privateCommand(ctx, analysis, "git", "update-ref", "refs/heads/analysis-base", base); e != nil {
		return nil, nil, "analysis git failure"
	}
	if _, e = privateCommand(ctx, analysis, "git", "symbolic-ref", "HEAD", "refs/heads/analysis-head"); e != nil {
		return nil, nil, "analysis git failure"
	}
	if _, e = privateCommand(ctx, analysis, "git", "read-tree", sha); e != nil {
		return nil, nil, "analysis git failure"
	}
	paths, e := git(ctx, root, "ls-tree", "-r", "--name-only", "-z", sha)
	if e != nil || len(paths) > 8<<20 {
		return nil, nil, "graph file census failure"
	}
	names := strings.Split(string(paths), "\x00")
	if len(names) > 100001 {
		return nil, nil, "graph file census bound"
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if !safeTransport(name) {
			return nil, nil, "unrepresentable graph path"
		}
		if name == "nx.json" || name == "package.json" || name == "catalogue.json" || name == "adapter.cjs" || strings.HasPrefix(name, ".git/") {
			continue
		}
		if !write(name, nil) {
			return nil, nil, "graph file projection failure"
		}
	}
	for _, f := range projection.Features {
		if f.Root != "." {
			if os.MkdirAll(filepath.Join(analysis, filepath.FromSlash(f.Root)), 0700) != nil {
				return nil, nil, "graph root projection failure"
			}
		}
	}
	// Exact object diff is authoritative. Synthetic direct-path tests may supply an
	// explicit file list only when all names are losslessly representable.
	args := []string{filepath.Join(toolDir, "node_modules/nx/dist/bin/nx.js"), "show", "projects", "--affected", "--base=" + base, "--head=" + sha, "--json"}
	if opts.Base == "" && opts.SHA == "" {
		args = []string{args[0], "show", "projects", "--affected", "--files=" + strings.Join(changed, ","), "--json"}
	}
	out, e := privateCommand(ctx, analysis, "node", args...)
	if e != nil {
		return nil, nil, "affected graph unavailable"
	}
	var projects []string
	if config.Decode(out, &projects, 8<<20) != nil || len(projects) > len(units) {
		return nil, nil, "invalid affected graph output"
	}
	known := map[string]bool{}
	for _, u := range units {
		known[u.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range projects {
		if !known[id] || seen[id] {
			return nil, nil, "unknown affected graph project"
		}
		seen[id] = true
	}
	sort.Strings(projects)
	dependencies := map[string][]string{}
	for _, edge := range projection.Edges {
		dependencies[edge[0]] = append(dependencies[edge[0]], edge[1])
	}
	return projects, dependencies, ""
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func projectMetadata(ctx context.Context, root string, units []catalog.Unit) (nxProjection, string) {
	projection := nxProjection{Features: []nxFeature{}, Edges: [][2]string{}}
	roots := map[string]string{}
	byID := map[string]catalog.Unit{}
	edges := map[[2]string]bool{}
	for _, u := range units {
		r := u.Root
		if r == "" {
			return projection, "missing feature root"
		}
		if r == "." {
			return projection, "shared root requires full selection"
		}
		if _, e := config.SafePath(root, r); e != nil {
			return projection, "invalid feature root"
		}
		for other := range roots {
			if other == r || strings.HasPrefix(r, other+"/") || strings.HasPrefix(other, r+"/") {
				return projection, "overlapping feature roots"
			}
		}
		roots[r] = u.ID
		byID[u.ID] = u
		inputs := append(append(append([]string{}, u.Paths...), u.TestPaths...), u.Inputs...)
		checks := []string{}
		for check, c := range u.Checks {
			checks = append(checks, check)
			inputs = append(inputs, c.Paths...)
			inputs = append(inputs, c.TestPaths...)
			inputs = append(inputs, c.Inputs...)
			for _, d := range c.DependsOn {
				edges[[2]string{u.ID, d}] = true
			}
		}
		for _, d := range append(append([]string{}, u.DependsOn...), u.OptionalContracts...) {
			edges[[2]string{u.ID, d}] = true
		}
		sort.Strings(inputs)
		sort.Strings(checks)
		projection.Features = append(projection.Features, nxFeature{Name: u.ID, Root: r, Inputs: unique(inputs), Checks: checks})
	}
	owner := func(rel string) string {
		for r, id := range roots {
			if rel == r || strings.HasPrefix(rel, r+"/") {
				return id
			}
		}
		return ""
	}
	// Read actual package references under every owned feature; lock inputs remain
	// descriptor policy and changing any lock selects full before this adapter.
	packages := map[string]string{}
	deps := map[string][]string{}
	cargoManifests := []string{}
	unclassifiedRust := ""
	tsReferences := map[string][]string{}
	sourceFiles := map[string]string{}
	dynamicImport := regexp.MustCompile(`\b(import|require)\s*\(`)
	uncertainJS := regexp.MustCompile(`\brequire\b|createRequire|\beval\b|\bFunction\b`)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "target" || d.Name() == ".nx") {
			return filepath.SkipDir
		}
		if strings.EqualFold(filepath.Base(filepath.Dir(p)), ".cargo") && (strings.EqualFold(d.Name(), "config") || strings.EqualFold(d.Name(), "config.toml")) {
			unclassifiedRust = "source Cargo configuration is unclassified"
			return filepath.SkipAll
		}
		if strings.EqualFold(d.Name(), "rust-toolchain") || strings.EqualFold(d.Name(), "rust-toolchain.toml") {
			unclassifiedRust = "source Rust toolchain configuration is unclassified"
			return filepath.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.Name() == "Cargo.toml" {
			cargoManifests = append(cargoManifests, p)
		}
		if strings.HasPrefix(d.Name(), "tsconfig") && strings.HasSuffix(d.Name(), ".json") {
			raw, e := readBounded(p, 1<<20)
			if e != nil {
				return e
			}
			var ts struct {
				References []struct {
					Path string `json:"path"`
				} `json:"references"`
			}
			if json.Unmarshal(raw, &ts) != nil {
				return ErrInvalid
			}
			from := owner(rel)
			for _, ref := range ts.References {
				target := filepath.Clean(filepath.Join(filepath.Dir(rel), ref.Path))
				if _, e := config.SafePath(root, filepath.ToSlash(target)); e != nil {
					return ErrInvalid
				}
				if from == "" {
					return ErrInvalid
				}
				tsReferences[from] = append(tsReferences[from], filepath.ToSlash(target))
			}
		}
		ext := filepath.Ext(p)
		if ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".cjs" {
			raw, e := readBounded(p, 1<<20)
			if e != nil {
				return e
			}
			// Syntax outside the small declared static-import contract is uncertainty,
			// never evidence that a dependency is absent.
			obscured := (bytes.Contains(raw, []byte("import")) || bytes.Contains(raw, []byte("from"))) && (bytes.Contains(raw, []byte("/*")) || bytes.Contains(raw, []byte("//")))
			if dynamicImport.Match(raw) || uncertainJS.Match(raw) || obscured {
				return ErrInvalid
			}
			if owner(rel) == "" {
				return ErrInvalid
			}
			sourceFiles[rel] = string(raw)
		}
		if d.Name() != "package.json" {
			return nil
		}
		id := owner(rel)
		if id == "" {
			return nil
		}
		raw, e := readBounded(p, 1<<20)
		if e != nil {
			return e
		}
		var pkg struct {
			Name                                                                  string `json:"name"`
			Dependencies, DevDependencies, PeerDependencies, OptionalDependencies map[string]string
		}
		if json.Unmarshal(raw, &pkg) != nil {
			return ErrInvalid
		}
		if pkg.Name != "" {
			if packages[pkg.Name] != "" && packages[pkg.Name] != id {
				return ErrInvalid
			}
			packages[pkg.Name] = id
		}
		for _, m := range []map[string]string{pkg.Dependencies, pkg.DevDependencies, pkg.PeerDependencies, pkg.OptionalDependencies} {
			for name := range m {
				deps[id] = append(deps[id], name)
			}
		}
		return nil
	})
	if unclassifiedRust != "" {
		return projection, unclassifiedRust
	}
	if err != nil {
		return projection, "package metadata unavailable"
	}
	for from, refs := range tsReferences {
		for _, ref := range refs {
			target := owner(ref)
			if target == "" {
				return projection, "unresolved TypeScript project reference"
			}
			if target != from {
				edges[[2]string{from, target}] = true
			}
		}
	}
	importPattern := regexp.MustCompile(`(?:from\s*|import\s*)['"]([^'"]+)['"]`)
	for rel, source := range sourceFiles {
		from := owner(rel)
		for _, match := range importPattern.FindAllStringSubmatch(source, -1) {
			spec := match[1]
			if strings.HasPrefix(spec, "node:") {
				continue
			}
			if strings.HasPrefix(spec, ".") {
				targetRel := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(rel), spec)))
				if _, e := config.SafePath(root, targetRel); e != nil {
					return projection, "unresolved TypeScript input"
				}
				target := owner(targetRel)
				if target == "" {
					return projection, "unowned TypeScript input"
				}
				found := false
				for _, candidate := range []string{targetRel, targetRel + ".ts", targetRel + ".tsx", targetRel + ".js", targetRel + ".jsx", targetRel + "/index.ts", targetRel + "/index.js"} {
					if st, e := os.Lstat(filepath.Join(root, filepath.FromSlash(candidate))); e == nil && st.Mode().IsRegular() {
						found = true
						break
					}
				}
				if !found {
					return projection, "unresolved TypeScript input"
				}
				if target != from {
					edges[[2]string{from, target}] = true
				}
			} else {
				pkg := spec
				if strings.HasPrefix(pkg, "@") {
					parts := strings.Split(pkg, "/")
					if len(parts) < 2 {
						return projection, "unresolved package import"
					}
					pkg = strings.Join(parts[:2], "/")
				} else {
					pkg = strings.Split(pkg, "/")[0]
				}
				if !contains(deps[from], pkg) {
					return projection, "undeclared package import"
				}
				if target := packages[pkg]; target != "" && target != from {
					edges[[2]string{from, target}] = true
				}
			}
		}
	}
	for id, names := range deps {
		for _, name := range names {
			if target := packages[name]; target != "" && target != id {
				edges[[2]string{id, target}] = true
			}
		}
	}
	// cargo metadata never executes builds; all workspace and target kinds remain.
	cargoRoots := map[string]bool{}
	for _, manifest := range cargoManifests {
		out, e := privateCommand(ctx, root, "cargo", "metadata", "--format-version", "1", "--locked", "--offline", "--all-features", "--manifest-path", manifest)
		if e != nil {
			return projection, "locked cargo metadata unavailable"
		}
		// Cargo's snake-case manifest field is intentionally explicit below.
		var rawMeta struct {
			WorkspaceRoot string `json:"workspace_root"`
			Packages      []struct {
				ID           string `json:"id"`
				ManifestPath string `json:"manifest_path"`
			} `json:"packages"`
			Resolve *struct {
				Nodes []struct {
					ID           string   `json:"id"`
					Dependencies []string `json:"dependencies"`
				}
			} `json:"resolve"`
		}
		if json.Unmarshal(out, &rawMeta) != nil || rawMeta.Resolve == nil {
			return projection, "invalid cargo metadata"
		}
		if cargoRoots[rawMeta.WorkspaceRoot] {
			continue
		}
		cargoRoots[rawMeta.WorkspaceRoot] = true
		packageIDs := map[string]string{}
		for _, p := range rawMeta.Packages {
			rel, e := filepath.Rel(root, p.ManifestPath)
			if e == nil && !strings.HasPrefix(rel, "..") {
				packageIDs[p.ID] = owner(filepath.ToSlash(rel))
				if packageIDs[p.ID] == "" {
					return projection, "unowned local cargo package"
				}
			}
		}
		for _, n := range rawMeta.Resolve.Nodes {
			from := packageIDs[n.ID]
			if from == "" {
				continue
			}
			for _, d := range n.Dependencies {
				to := packageIDs[d]
				if to != "" && from != to {
					edges[[2]string{from, to}] = true
				}
			}
		}
	}
	for edge := range edges {
		if byID[edge[0]].ID == "" || byID[edge[1]].ID == "" {
			return projection, "unrepresented metadata dependency"
		}
		projection.Edges = append(projection.Edges, edge)
	}
	sort.Slice(projection.Features, func(i, j int) bool { return projection.Features[i].Name < projection.Features[j].Name })
	sort.Slice(projection.Edges, func(i, j int) bool {
		a, b := projection.Edges[i], projection.Edges[j]
		return a[0]+"\x00"+a[1] < b[0]+"\x00"+b[1]
	})
	return projection, ""
}
func readBounded(path string, max int) ([]byte, error) {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Size() > int64(max) {
		return nil, ErrInvalid
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(max)+1))
}

type toolRegistryKey struct{}
type sourceRootKey struct{}

func approvedExecutable(root, name, registered string) (string, error) {
	if registered == "" {
		if name != "git" {
			return "", ErrInvalid
		}
		registered = "/usr/bin/git"
		if runtime.GOOS == "windows" {
			registered = filepath.Join(os.Getenv("ProgramFiles"), "Git", "cmd", "git.exe")
		}
	}
	if !filepath.IsAbs(registered) {
		return "", ErrInvalid
	}
	resolved, e := filepath.EvalSymlinks(registered)
	if e != nil {
		return "", ErrInvalid
	}
	st, e := os.Stat(resolved)
	if e != nil || !st.Mode().IsRegular() {
		return "", ErrInvalid
	}
	source, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", ErrInvalid
	}
	if !catalog.OutsideRoot(source, resolved) {
		return "", ErrInvalid
	}
	return resolved, nil
}

func newAnalysisWorkspace() (string, func(), error) {
	parent, e := os.MkdirTemp("", "ci-analysis-")
	if e != nil {
		return "", nil, ErrInvalid
	}
	physical, e := filepath.EvalSymlinks(parent)
	if e != nil {
		os.RemoveAll(parent)
		return "", nil, ErrInvalid
	}
	workspace := filepath.Join(physical, "workspace")
	if process.MakePrivateDir(workspace) != nil {
		os.RemoveAll(physical)
		return "", nil, ErrInvalid
	}
	cleanup := func() { _ = os.Remove(filepath.Join(workspace, "node_modules")); _ = os.RemoveAll(physical) }
	return workspace, cleanup, nil
}
