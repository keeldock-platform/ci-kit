package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Schema is the local-only Draft 2020-12 descriptor contract.
//
//go:embed unit-v1.json
var Schema []byte

const MaxDescriptorBytes = 256 << 10

var ErrInvalid = errors.New("invalid execution catalogue")
var once sync.Once
var compiled *jsonschema.Schema
var compileErr error

// Decode validates bounded, unambiguous JSON using the bundled schema only.
func Decode(data []byte) (Unit, error) {
	var u Unit
	if err := config.Decode(data, &u, MaxDescriptorBytes); err != nil {
		return u, ErrInvalid
	}
	once.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.UseLoader(jsonschema.SchemeURLLoader{})
		var schema any
		if compileErr = json.Unmarshal(Schema, &schema); compileErr != nil {
			return
		}
		if compileErr = compiler.AddResource("https://example.invalid/ci/unit-v1.json", schema); compileErr != nil {
			return
		}
		compiled, compileErr = compiler.Compile("https://example.invalid/ci/unit-v1.json")
	})
	if compileErr != nil {
		return u, ErrInvalid
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&doc) != nil || compiled.Validate(doc) != nil {
		return u, ErrInvalid
	}
	if validateUnit(u) != nil {
		return u, ErrInvalid
	}
	digest := sha256.Sum256(data)
	u.Digest = hex.EncodeToString(digest[:])
	return u, nil
}

// Load discovers one descriptor per feature without following directory symlinks.
func Load(root string) ([]Unit, error) { return load(root, true) }
func load(root string, validate bool) ([]Unit, error) {
	return loadConfigured(root, []string{"."}, "ci/units.json", validate, true)
}
func loadConfigured(root string, unitRoots []string, sharedPath string, validate, optionalShared bool) ([]Unit, error) {
	if validateDiscovery(unitRoots, sharedPath) != nil {
		return nil, ErrInvalid
	}
	if _, err := config.SafePath(root, "."); err != nil {
		return nil, ErrInvalid
	}
	for _, unitRoot := range unitRoots {
		resolved, e := config.SafePath(root, unitRoot)
		if e != nil {
			return nil, ErrInvalid
		}
		st, e := os.Stat(resolved)
		if e != nil || !st.IsDir() {
			return nil, ErrInvalid
		}
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return ErrInvalid
		}
		relative, e := filepath.Rel(root, p)
		if e != nil {
			return ErrInvalid
		}
		relative = filepath.ToSlash(relative)
		if d.IsDir() && !directoryRelevant(relative, unitRoots) {
			return filepath.SkipDir
		}
		if !d.IsDir() && !descriptorIncluded(relative, unitRoots) {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".nx") {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.Name() == "ci-unit.json" {
				return ErrInvalid
			}
			return nil
		}
		if !d.IsDir() && d.Name() == "ci-unit.json" {
			files = append(files, p)
			if len(files) > 10000 {
				return ErrInvalid
			}
		}
		return nil
	})
	if err != nil {
		return nil, ErrInvalid
	}
	sort.Strings(files)
	units := make([]Unit, 0, len(files))
	seen := map[string]bool{}
	for _, p := range files {
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() > MaxDescriptorBytes {
			return nil, ErrInvalid
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, ErrInvalid
		}
		b, err := io.ReadAll(io.LimitReader(f, MaxDescriptorBytes+1))
		f.Close()
		if err != nil {
			return nil, ErrInvalid
		}
		u, err := Decode(b)
		if err != nil || seen[u.ID] {
			return nil, ErrInvalid
		}
		seen[u.ID] = true
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return nil, ErrInvalid
		}
		u.Root = filepath.ToSlash(rel)
		for _, c := range u.Checks {
			for _, report := range c.Reports {
				if _, e := config.SafePath(root, report); e != nil {
					return nil, ErrInvalid
				}
			}
			for _, cmds := range c.Commands {
				for _, cmd := range cmds {
					resolved, resolveErr := config.SafePath(root, cmd.Cwd)
					st, statErr := os.Stat(resolved)
					if resolveErr != nil || statErr != nil || !st.IsDir() {
						return nil, ErrInvalid
					}
				}
			}
		}
		units = append(units, u)
	}
	shared, e := loadSharedPath(root, sharedPath, optionalShared)
	if e != nil {
		return nil, e
	}
	units = append(units, shared.Units...)
	if len(units) == 0 {
		return nil, ErrInvalid
	}
	if validate {
		if err := Validate(units); err != nil {
			return nil, err
		}
	}
	return units, nil
}

// Validate checks cross-descriptor references and exact execution coverage.
func Validate(units []Unit) error {
	if len(units) == 0 || len(units) > 10000 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	canonical := map[string]bool{}
	for _, u := range units {
		if seen[u.ID] || validateUnit(u) != nil {
			return ErrInvalid
		}
		seen[u.ID] = true
		for check, c := range u.Checks {
			for _, p := range c.Platforms {
				id := u.ID + "-" + check + "@" + p
				if canonical[id] {
					return ErrInvalid
				}
				canonical[id] = true
				if len(canonical) > 100000 {
					return ErrInvalid
				}
			}
		}
	}
	for _, u := range units {
		for _, d := range append(append([]string{}, u.DependsOn...), u.OptionalContracts...) {
			if !seen[d] || d == u.ID {
				return ErrInvalid
			}
		}
		for _, d := range u.Needs {
			if !seen[d] && !canonical[d] {
				return ErrInvalid
			}
		}
		for _, c := range u.Checks {
			for _, d := range c.DependsOn {
				if !seen[d] || d == u.ID {
					return ErrInvalid
				}
			}
			for _, d := range c.Needs {
				if !seen[d] && !canonical[d] {
					return ErrInvalid
				}
			}
		}
	}
	// Product edges and prerequisites are independent DAGs.
	product := simple.NewDirectedGraph()
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	nodes := map[string]int64{}
	for i, id := range ids {
		nodes[id] = int64(i)
		product.AddNode(simple.Node(i))
	}
	for _, u := range units {
		deps := append(append([]string{}, u.DependsOn...), u.OptionalContracts...)
		for _, c := range u.Checks {
			deps = append(deps, c.DependsOn...)
		}
		for _, d := range deps {
			product.SetEdge(product.NewEdge(simple.Node(nodes[d]), simple.Node(nodes[u.ID])))
		}
	}
	if _, e := topo.Sort(product); e != nil {
		return ErrInvalid
	}
	prereq := simple.NewDirectedGraph()
	canonIDs := make([]string, 0, len(canonical))
	for id := range canonical {
		canonIDs = append(canonIDs, id)
	}
	sort.Strings(canonIDs)
	canonNodes := map[string]int64{}
	for i, id := range canonIDs {
		canonNodes[id] = int64(i)
		prereq.AddNode(simple.Node(i))
	}
	for _, u := range units {
		for check, c := range u.Checks {
			for _, p := range c.Platforms {
				id := u.ID + "-" + check + "@" + p
				for _, need := range append(append([]string{}, u.Needs...), c.Needs...) {
					targets := []string{}
					if canonical[need] {
						targets = append(targets, need)
					} else {
						for _, candidateUnit := range units {
							if candidateUnit.ID != need {
								continue
							}
							for candidateCheck, candidatePolicy := range candidateUnit.Checks {
								if slices.Contains(candidatePolicy.Platforms, p) {
									targets = append(targets, candidateUnit.ID+"-"+candidateCheck+"@"+p)
								}
							}
						}
					}
					if len(targets) == 0 {
						return ErrInvalid
					}
					for _, target := range targets {
						if target == id {
							return ErrInvalid
						}
						prereq.SetEdge(prereq.NewEdge(simple.Node(canonNodes[target]), simple.Node(canonNodes[id])))
					}
				}
			}
		}
	}
	if _, e := topo.Sort(prereq); e != nil {
		return ErrInvalid
	}
	return nil
}
func validateUnit(u Unit) error {
	if u.Schema != 1 || !aliasID.MatchString(u.ID) || u.Owner == "" || len(u.Checks) == 0 || len(u.Commands) != 0 || len(u.Platforms) == 0 || u.TimeoutSeconds < 1 || u.TimeoutSeconds > 7200 || u.Resources.Weight < 1 || u.Resources.Weight > 16 || u.Outputs.Visibility != "private-runner-only" || len(u.Paths) == 0 {
		return ErrInvalid
	}
	for _, assertion := range u.Assertions {
		owners := 0
		for _, c := range u.Checks {
			if slices.Contains(c.Assertions, assertion) {
				owners++
			}
		}
		if owners != 1 {
			return ErrInvalid
		}
	}
	for _, p := range append(append(append(append([]string{}, u.Paths...), u.TestPaths...), u.Inputs...), u.Cache.Locks...) {
		if !safePattern(p) {
			return ErrInvalid
		}
	}
	for check, c := range u.Checks {
		for _, p := range append(append(append(append([]string{}, c.Paths...), c.TestPaths...), c.Inputs...), c.Reports...) {
			if !safePattern(p) {
				return ErrInvalid
			}
		}
		// Optional selection metadata must still cover every mandatory invariant.
		for _, assertion := range c.Assertions {
			if len(c.Census) > 0 && !slices.Contains(c.Census, assertion) {
				return ErrInvalid
			}
		}
		if (check == "unit" || check == "integration" || check == "e2e" || check == "visual") && len(c.Reports) == 0 {
			return ErrInvalid
		}
		if !slices.Contains(CheckTypes, check) || len(c.Platforms) == 0 {
			return ErrInvalid
		}
		for _, p := range c.Platforms {
			if !slices.Contains(Platforms, p) || !slices.Contains(u.Platforms, p) {
				return ErrInvalid
			}
			cmds := c.Commands[p]
			if len(cmds) == 0 {
				cmds = c.Commands["default"]
			}
			if len(cmds) == 0 {
				return ErrInvalid
			}
			for _, cmd := range cmds {
				if !safePattern(cmd.Cwd) || strings.ContainsAny(cmd.Cwd, "*?[]{}") || len(cmd.Argv) == 0 {
					return ErrInvalid
				}
				binary := filepath.Base(cmd.Argv[0])
				if binary == "sh" || binary == "bash" || binary == "zsh" || binary == "cmd" || binary == "powershell" || binary == "pwsh" {
					for _, arg := range cmd.Argv[1:] {
						if arg == "-c" || strings.EqualFold(arg, "/c") || strings.EqualFold(arg, "-command") || strings.EqualFold(arg, "-encodedcommand") {
							return ErrInvalid
						}
					}
				}
				for _, a := range cmd.Argv {
					if strings.ContainsAny(a, "\x00\r\n") || strings.Contains(a, "<") || strings.Contains(a, ">") {
						return ErrInvalid
					}
				}
			}
		}
		for p := range c.PlatformInputs {
			if !slices.Contains(c.Platforms, p) {
				return ErrInvalid
			}
		}
		ids := map[string]bool{}
		axes := map[string]bool{}
		for _, v := range c.Variants {
			if v.ID == "" || ids[v.ID] || len(v.Axes) == 0 {
				return ErrInvalid
			}
			ids[v.ID] = true
			b, _ := json.Marshal(v.Axes)
			if axes[string(b)] {
				return ErrInvalid
			}
			axes[string(b)] = true
		}
		if s := c.Shards; s != nil {
			if !sameSet(c.Census, s.Census) {
				return ErrInvalid
			}
			if s.Count < 2 || s.Count > 32 || len(s.Assignments) != s.Count || len(s.DurationSeconds) != s.Count || s.Measurement == "" || len(s.Census) == 0 {
				return ErrInvalid
			}
			expected := map[string]bool{}
			for _, t := range s.Census {
				if expected[t] {
					return ErrInvalid
				}
				expected[t] = true
			}
			got := map[string]bool{}
			for i, tests := range s.Assignments {
				if len(tests) == 0 || s.DurationSeconds[i] <= 0 {
					return ErrInvalid
				}
				for _, t := range tests {
					if !expected[t] || got[t] {
						return ErrInvalid
					}
					got[t] = true
				}
			}
			if len(got) != len(expected) {
				return ErrInvalid
			}
		}
		if r := c.Related; r != nil {
			if !sameSet(c.Census, r.Census) {
				return ErrInvalid
			}
			expected := map[string]bool{}
			for _, t := range r.Census {
				expected[t] = true
			}
			if len(expected) == 0 {
				return ErrInvalid
			}
			for _, tests := range r.Tests {
				for _, t := range tests {
					if !expected[t] {
						return ErrInvalid
					}
				}
			}
		}
	}
	return nil
}

// Entries expands declared combinations and measured shards in stable order.
func Entries(units []Unit) ([]Entry, error) {
	if err := Validate(units); err != nil {
		return nil, err
	}
	var entries []Entry
	for _, u := range units {
		if u.Digest == "" {
			raw, _ := json.Marshal(u)
			sum := sha256.Sum256(raw)
			u.Digest = hex.EncodeToString(sum[:])
		}
		keys := make([]string, 0, len(u.Checks))
		for k := range u.Checks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, check := range keys {
			c := u.Checks[check]
			platforms := append([]string{}, c.Platforms...)
			sort.Strings(platforms)
			variants := append([]Variant{}, c.Variants...)
			if len(variants) == 0 {
				variants = []Variant{{}}
			}
			sort.Slice(variants, func(i, j int) bool { return variants[i].ID < variants[j].ID })
			for _, p := range platforms {
				for _, v := range variants {
					count := 1
					if c.Shards != nil {
						count = c.Shards.Count
					}
					for i := 1; i <= count; i++ {
						cmds := c.Commands[p]
						if len(cmds) == 0 {
							cmds = c.Commands["default"]
						}
						cloned := make([]Command, len(cmds))
						for j, cmd := range cmds {
							cloned[j] = cmd
							cloned[j].Argv = append(append([]string{}, cmd.Argv...), v.Args...)
						}
						id := u.ID + "-" + check + "@" + p
						shard := ""
						if count > 1 {
							shard = fmt.Sprintf("%d/%d", i, count)
							for j := range cloned {
								switch c.Shards.Runner {
								case "nextest":
									cloned[j].Argv = append(cloned[j].Argv, "--partition", fmt.Sprintf("count:%d/%d", i, count))
								case "playwright":
									cloned[j].Argv = append(cloned[j].Argv, "--shard", shard)
								default:
									return nil, ErrInvalid
								}
							}
						}
						if len(entries) >= 100000 {
							return nil, ErrInvalid
						}
						entries = append(entries, Entry{ID: id, Unit: u.ID, Check: check, Platform: p, Variant: v.ID, Shard: shard, Commands: cloned, Digest: u.Digest})
					}
				}
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		return a.ID+"\x00"+a.Variant+"\x00"+a.Shard < b.ID+"\x00"+b.Variant+"\x00"+b.Shard
	})
	return entries, nil
}

func safePattern(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:\x00\r\n\t") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == ".." {
			return false
		}
	}
	_, e := doublestar.Match(p, "")
	return e == nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		if seen[x] {
			return false
		}
		seen[x] = true
	}
	for _, x := range b {
		if !seen[x] {
			return false
		}
		delete(seen, x)
	}
	return len(seen) == 0
}

// LoadComposition combines locked source roots without rewriting declared identities.
// Origins stays private and locates each invocation's own checkout.
func LoadComposition(roots map[string]string) ([]Unit, map[string]string, error) {
	return LoadCompositionConfigured(context.Background(), roots)
}
