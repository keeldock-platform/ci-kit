// Package planner applies conservative policy around the maintained affected graph.
package planner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// Options bounds feedback selection. Qualified is controlled by reviewed policy, never a descriptor.
type Options struct {
	Context                            context.Context `json:"-"`
	Base, SHA, Mode                    string
	ToolDirectory                      string
	Executables                        map[string]string
	Units, Platforms, Checks, Variants []string
	Shard                              string
	Admission, Quick, Qualified        bool
}

// Invocation is one complete check/platform/variant/shard command contract.
type Invocation struct {
	ID, Unit, Check, Platform, Variant, Shard string
	Commands                                  []catalog.Command
	Tests                                     []string
}

// Pack shares setup and checkout for all selected invocations on one platform.
type Pack struct {
	Platform string
	Units    []Invocation
}

// Result is private planning metadata, never a hosted output payload.
type Result struct {
	Selected       []Invocation
	Packs          []Pack
	Seeds, Reasons []string
	FullFallback   bool
}

var ErrInvalid = errors.New("invalid selection")
var shaPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// Plan validates exact Git objects and selects work from a complete NUL-delimited diff.
func Plan(root string, units []catalog.Unit, opts Options) (Result, error) {
	ctx, cancel := context.WithTimeout(optionContext(opts), 90*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, toolRegistryKey{}, opts.Executables)
	ctx = context.WithValue(ctx, sourceRootKey{}, root)
	opts.Context = ctx
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if opts.Mode == "quick" {
		opts.Quick = true
	}
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}
	if opts.Quick {
		if !shaPattern.MatchString(opts.Base) || !shaPattern.MatchString(opts.SHA) {
			return Result{}, ErrInvalid
		}
		head, e := git(ctx, root, "rev-parse", "HEAD")
		if e != nil || strings.TrimSpace(string(head)) != opts.SHA {
			return Result{}, ErrInvalid
		}
		dirty, e := git(ctx, root, "status", "--porcelain", "-z", "--untracked-files=all")
		if e != nil || len(dirty) != 0 {
			return Result{}, ErrInvalid
		}
	}
	if opts.Mode == "full" || opts.Mode == "candidate" || len(opts.Units) > 0 && !opts.Quick {
		return Select(root, units, nil, opts)
	}
	if !shaPattern.MatchString(opts.Base) || !shaPattern.MatchString(opts.SHA) {
		return full(units, opts, "missing exact revision")
	}
	for _, sha := range []string{opts.Base, opts.SHA} {
		if _, e := git(ctx, root, "cat-file", "-e", sha+"^{commit}"); e != nil {
			return full(units, opts, "missing revision object")
		}
	}
	if _, e := git(ctx, root, "merge-base", "--is-ancestor", opts.Base, opts.SHA); e != nil {
		return full(units, opts, "base is not ancestor")
	}
	shallow, e := git(ctx, root, "rev-parse", "--is-shallow-repository")
	if e != nil || strings.TrimSpace(string(shallow)) != "false" {
		return full(units, opts, "incomplete repository")
	}
	raw, e := git(ctx, root, "diff", "--name-status", "-z", "--find-renames", opts.Base, opts.SHA, "--")
	if e != nil {
		return full(units, opts, "incomplete diff")
	}
	paths, e := DiffPaths(raw)
	if e != nil {
		return full(units, opts, "unrepresentable diff")
	}
	return Select(root, units, paths, opts)
}
func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	return privateCommand(ctx, root, "git", append([]string{"--no-replace-objects", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-C", root}, args...)...)
}

// DiffPaths accepts Git name-status -z records and preserves both rename sides.
func DiffPaths(raw []byte) ([]string, error) {
	if len(raw) > 8<<20 {
		return nil, ErrInvalid
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return []string{}, nil
	}
	if parts[len(parts)-1] != "" {
		return nil, ErrInvalid
	}
	parts = parts[:len(parts)-1]
	var paths []string
	for i := 0; i < len(parts); {
		status := parts[i]
		i++
		if status == "" {
			return nil, ErrInvalid
		}
		count := 1
		if status[0] == 'R' || status[0] == 'C' {
			count = 2
		}
		if !strings.ContainsRune("ACDMRTUXB", rune(status[0])) || i+count > len(parts) {
			return nil, ErrInvalid
		}
		for j := 0; j < count; j++ {
			p := parts[i]
			i++
			if !safeTransport(p) {
				return nil, ErrInvalid
			}
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return unique(paths), nil
}
func safeTransport(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\,:\r\n\t\x00") {
		return false
	}
	for _, c := range p {
		if c < 32 || c == 127 {
			return false
		}
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	return true
}
func validateOptions(o Options) error {
	if o.Mode != "" && o.Mode != "unit" && o.Mode != "affected" && o.Mode != "full" && o.Mode != "candidate" && o.Mode != "quick" {
		return ErrInvalid
	}
	if o.Mode == "quick" {
		o.Quick = true
	}
	if o.Admission && (o.Quick || len(o.Units) > 0 || len(o.Variants) > 0 || o.Shard != "" || len(o.Checks) > 0 || len(o.Platforms) > 0) {
		return ErrInvalid
	}
	if o.Quick && (o.Mode == "full" || o.Mode == "candidate") {
		return ErrInvalid
	}
	return nil
}

// Select applies the same policy to supplied complete paths, allowing offline fixture verification.
func Select(root string, units []catalog.Unit, changed []string, opts Options) (Result, error) {
	if err := optionContext(opts).Err(); err != nil {
		return Result{}, err
	}
	if opts.Mode == "quick" {
		opts.Quick = true
	}
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}
	if err := catalog.Validate(units); err != nil {
		return Result{}, err
	}
	if opts.Admission && !opts.Qualified {
		return full(units, opts, "affected admission is not qualified")
	}
	if opts.Mode == "full" || opts.Mode == "candidate" {
		return expand(units, nil, nil, opts, false, nil)
	}
	if len(opts.Units) > 0 {
		return expand(units, nil, changed, opts, false, nil)
	}
	if len(changed) == 0 {
		return full(units, opts, "empty or unavailable change classification")
	}
	direct := map[string]map[string]bool{}
	var seeds []string
	for _, p := range changed {
		if !safeTransport(p) {
			return full(units, opts, "unrepresentable filename")
		}
		if highRisk(p) {
			return full(units, opts, "shared lock or policy input")
		}
		owned := false
		for _, u := range units {
			matchedCheck := false
			for check, c := range u.Checks {
				matches := matchAny(p, append(append(append([]string{}, c.Paths...), c.TestPaths...), c.Inputs...))
				if matches {
					matchedCheck = true
					owned = true
					if direct[u.ID] == nil {
						direct[u.ID] = map[string]bool{}
						seeds = append(seeds, u.ID)
					}
					direct[u.ID][check] = true
				}
			}
			if matchAny(p, append(append(append([]string{}, u.Paths...), u.TestPaths...), u.Inputs...)) {
				owned = true
				if direct[u.ID] == nil {
					direct[u.ID] = map[string]bool{}
					seeds = append(seeds, u.ID)
				}
				if !matchedCheck {
					for check := range u.Checks {
						direct[u.ID][check] = true
					}
				}
			}
		}
		if !owned {
			return full(units, opts, "unowned changed path")
		}
	}
	affected, graphDependencies, reason := nxAffected(root, units, changed, opts)
	if err := optionContext(opts).Err(); err != nil {
		return Result{}, err
	}
	if reason != "" {
		return full(units, opts, reason)
	}
	selected := map[string]map[string]bool{}
	for _, u := range units {
		if !contains(affected, u.ID) {
			continue
		}
		selected[u.ID] = map[string]bool{}
		for check := range direct[u.ID] {
			selected[u.ID][check] = true
		}
		global := false
		for _, dependency := range append(append([]string{}, u.DependsOn...), u.OptionalContracts...) {
			if contains(affected, dependency) {
				global = true
			}
		}
		declaredCheckDependency := false
		for check, policy := range u.Checks {
			for _, dependency := range policy.DependsOn {
				if contains(affected, dependency) {
					selected[u.ID][check] = true
					declaredCheckDependency = true
				}
			}
		}
		// Undeclared metadata-derived edges conservatively retain the full consumer.
		inferred := false
		for _, dependency := range graphDependencies[u.ID] {
			if !contains(affected, dependency) {
				continue
			}
			classified := contains(u.DependsOn, dependency) || contains(u.OptionalContracts, dependency)
			for _, policy := range u.Checks {
				classified = classified || contains(policy.DependsOn, dependency)
			}
			if !classified {
				inferred = true
			}
		}
		if global || inferred || direct[u.ID] == nil && !declaredCheckDependency {
			for check := range u.Checks {
				selected[u.ID][check] = true
			}
		}
	}
	for id := range direct {
		if selected[id] == nil {
			return full(units, opts, "graph omitted direct owner")
		}
	}
	sort.Strings(seeds)
	r, e := expand(units, selected, changed, opts, false, nil)
	r.Seeds = unique(seeds)
	return r, e
}
func highRisk(p string) bool {
	base := p[strings.LastIndex(p, "/")+1:]
	switch base {
	case "Cargo.lock", "Cargo.toml", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "npm-shrinkwrap.json", "go.sum", "go.mod", "rust-toolchain.toml", "rust-toolchain", "nx.json", "tsconfig.json", "composition.lock", "ci-unit.json":
		return true
	}
	return strings.HasPrefix(p, ".github/") || strings.HasPrefix(p, "ci/")
}
func matchAny(p string, patterns []string) bool {
	for _, pattern := range patterns {
		ok, e := doublestar.Match(pattern, p)
		if e == nil && ok {
			return true
		}
	}
	return false
}
func full(units []catalog.Unit, opts Options, reason string) (Result, error) {
	if err := optionContext(opts).Err(); err != nil {
		return Result{}, err
	}
	opts.Units = nil
	return expand(units, nil, nil, opts, true, []string{reason})
}
func expand(units []catalog.Unit, selected map[string]map[string]bool, changed []string, opts Options, fallback bool, reasons []string) (Result, error) {
	if err := optionContext(opts).Err(); err != nil {
		return Result{}, err
	}
	entries, e := catalog.Entries(units)
	if e != nil {
		return Result{}, e
	}
	wanted := map[string]bool{}
	selectorFound := map[string]bool{}
	for _, entry := range entries {
		baseMatch := selected == nil || selected[entry.Unit] != nil && selected[entry.Unit][entry.Check]
		if len(opts.Units) > 0 {
			baseMatch = false
			for _, s := range opts.Units {
				base, shard, _ := strings.Cut(s, "#")
				if base == entry.Unit || base == entry.ID {
					if shard == "" || shard == entry.Shard {
						baseMatch = true
						selectorFound[s] = true
					}
				}
			}
		}
		if !baseMatch || len(opts.Platforms) > 0 && !contains(opts.Platforms, entry.Platform) || len(opts.Checks) > 0 && !contains(opts.Checks, entry.Check) {
			continue
		}
		if opts.Shard != "" && entry.Shard != opts.Shard {
			continue
		}
		if !variantMatches(units, entry, opts.Variants) {
			continue
		}
		// Platform-specific input ownership can narrow direct changes only.
		c := findCheck(units, entry.Unit, entry.Check)
		if selected != nil && containsPathOwner(units, enUnit(entry), changed) && len(c.PlatformInputs) > 0 && len(changed) > 0 {
			if pats, ok := c.PlatformInputs[entry.Platform]; ok && !matchAnyList(changed, pats) && !commonChanged(c, changed) {
				continue
			}
		}
		wanted[key(entry)] = true
	}
	for _, s := range opts.Units {
		if !selectorFound[s] {
			return Result{}, ErrInvalid
		}
	}
	if len(wanted) == 0 {
		return Result{}, ErrInvalid
	}
	// Gonum orders prerequisites. Only prerequisite targets are added, not reverse product dependants.
	byID := map[string][]catalog.Entry{}
	for _, en := range entries {
		byID[en.ID] = append(byID[en.ID], en)
	}
	g := simple.NewDirectedGraph()
	nodes := map[string]int64{}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i, id := range ids {
		nodes[id] = int64(i)
		g.AddNode(simple.Node(i))
	}
	for _, u := range units {
		for check, c := range u.Checks {
			for _, p := range c.Platforms {
				id := u.ID + "-" + check + "@" + p
				for _, need := range append(append([]string{}, u.Needs...), c.Needs...) {
					targets := []string{}
					if _, ok := byID[need]; ok {
						targets = append(targets, need)
					} else {
						for _, other := range entries {
							if other.Unit == need && other.Platform == p && !contains(targets, other.ID) {
								targets = append(targets, other.ID)
							}
						}
					}
					if len(targets) == 0 {
						return Result{}, ErrInvalid
					}
					for _, target := range targets {
						if target == id {
							return Result{}, ErrInvalid
						}
						g.SetEdge(g.NewEdge(simple.Node(nodes[target]), simple.Node(nodes[id])))
					}
				}
			}
		}
	}
	ordered, err := topo.SortStabilized(g, func(nodes []graph.Node) {
		sort.Slice(nodes, func(i, j int) bool {
			a, b := byID[ids[nodes[i].ID()]][0], byID[ids[nodes[j].ID()]][0]
			ar, br := checkRank(a.Check), checkRank(b.Check)
			if ar != br {
				return ar < br
			}
			return a.ID < b.ID
		})
	})
	if err != nil {
		return Result{}, ErrInvalid
	}
	// Propagate prerequisite requirements through the ordered DAG using maintained ancestry queries.
	for _, node := range ordered {
		_ = node
	}
	for id, ens := range byID {
		active := false
		for _, en := range ens {
			active = active || wanted[key(en)]
		}
		if !active {
			continue
		}
		for _, ancestor := range ids {
			if ancestor != id && topo.PathExistsIn(g, simple.Node(nodes[ancestor]), simple.Node(nodes[id])) {
				for _, en := range byID[ancestor] {
					wanted[key(en)] = true
				}
			}
		}
	}
	result := Result{FullFallback: fallback, Reasons: reasons}
	packs := map[string][]Invocation{}
	for _, node := range ordered {
		for _, en := range byID[ids[node.ID()]] {
			if !wanted[key(en)] {
				continue
			}
			cmds := en.Commands
			quickOpts := opts
			if opts.Quick && en.Shard != "" {
				fullOpts := opts
				fullOpts.Quick = false
				if reflectSame(expectedTests(findCheck(units, en.Unit, en.Check), en, changed, opts), expectedTests(findCheck(units, en.Unit, en.Check), en, nil, fullOpts)) {
					quickOpts.Quick = false
				}
			}
			if quickOpts.Quick && !opts.Admission {
				cmds = relatedCommands(findCheck(units, en.Unit, en.Check), cmds, changed, opts.Base)
			}
			inv := Invocation{ID: en.ID, Unit: en.Unit, Check: en.Check, Platform: en.Platform, Variant: en.Variant, Shard: en.Shard, Commands: cmds, Tests: expectedTests(findCheck(units, en.Unit, en.Check), en, changed, quickOpts)}
			result.Selected = append(result.Selected, inv)
			packs[en.Platform] = append(packs[en.Platform], inv)
		}
	}
	platforms := make([]string, 0, len(packs))
	for p := range packs {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	for _, p := range platforms {
		result.Packs = append(result.Packs, Pack{Platform: p, Units: packs[p]})
	}
	if err := optionContext(opts).Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
func key(e catalog.Entry) string { return e.ID + "\x00" + e.Variant + "\x00" + e.Shard }
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func unique(xs []string) []string {
	out := xs[:0]
	for _, x := range xs {
		if len(out) == 0 || out[len(out)-1] != x {
			out = append(out, x)
		}
	}
	return out
}
func findCheck(units []catalog.Unit, id, check string) catalog.Check {
	for _, u := range units {
		if u.ID == id {
			return u.Checks[check]
		}
	}
	return catalog.Check{}
}
func matchAnyList(paths, patterns []string) bool {
	for _, p := range paths {
		if matchAny(p, patterns) {
			return true
		}
	}
	return false
}
func commonChanged(c catalog.Check, paths []string) bool { return matchAnyList(paths, c.Inputs) }
func variantMatches(units []catalog.Unit, e catalog.Entry, selectors []string) bool {
	if len(selectors) == 0 {
		return true
	}
	c := findCheck(units, e.Unit, e.Check)
	for _, v := range c.Variants {
		if v.ID != e.Variant {
			continue
		}
		seen := map[string]bool{}
		for _, s := range selectors {
			axis, value, ok := strings.Cut(s, "=")
			if !ok || seen[axis] || v.Axes[axis] != value {
				return false
			}
			seen[axis] = true
		}
		return true
	}
	return false
}
func relatedCommands(c catalog.Check, commands []catalog.Command, changed []string, base string) []catalog.Command {
	if c.Related == nil || len(changed) == 0 {
		return commands
	}
	tests := []string{}
	for _, p := range changed {
		mapped := false
		for pattern, ts := range c.Related.Tests {
			if matchAny(p, []string{pattern}) {
				tests = append(tests, ts...)
				mapped = true
			}
		}
		if !mapped {
			return commands
		}
	}
	sort.Strings(tests)
	tests = unique(tests)
	if len(tests) == 0 {
		return commands
	}
	out := make([]catalog.Command, len(commands))
	for i, cmd := range commands {
		out[i] = cmd
		args := append([]string{}, cmd.Argv...)
		switch c.Related.Runner {
		case "vitest":
			found := false
			for j, a := range args {
				if a == "run" {
					args[j] = "related"
					found = true
					break
				}
			}
			if !found {
				return commands
			}
			args = append(args, changed...)
			args = append(args, "--run")
		case "nextest":
			terms := make([]string, len(tests))
			for j, t := range tests {
				terms[j] = "test(=" + fmt.Sprintf("%q", t) + ")"
			}
			args = append(args, "-E", strings.Join(terms, " | "))
		case "playwright":
			terms := make([]string, len(tests))
			for j, t := range tests {
				terms[j] = regexp.QuoteMeta(t)
			}
			args = append(args, "--grep", strings.Join(terms, "|"))
		default:
			return commands
		}
		out[i].Argv = args
	}
	return out
}

func enUnit(e catalog.Entry) string { return e.Unit }
func containsPathOwner(units []catalog.Unit, id string, changed []string) bool {
	for _, u := range units {
		if u.ID == id {
			return matchAnyList(changed, append(append(append([]string{}, u.Paths...), u.TestPaths...), u.Inputs...))
		}
	}
	return false
}

func expectedTests(c catalog.Check, e catalog.Entry, changed []string, opts Options) []string {
	tests := append([]string{}, c.Census...)
	if c.Shards != nil && e.Shard != "" {
		var index, count int
		if _, err := fmt.Sscanf(e.Shard, "%d/%d", &index, &count); err == nil && index > 0 && index <= len(c.Shards.Assignments) {
			tests = append([]string{}, c.Shards.Assignments[index-1]...)
		}
	}
	if opts.Quick && c.Related != nil && len(changed) > 0 {
		related := []string{}
		for _, p := range changed {
			matched := false
			for pattern, mapped := range c.Related.Tests {
				if matchAny(p, []string{pattern}) {
					related = append(related, mapped...)
					matched = true
				}
			}
			if !matched {
				return tests
			}
		}
		narrowed := []string{}
		for _, test := range tests {
			if contains(related, test) {
				narrowed = append(narrowed, test)
			}
		}
		if len(narrowed) > 0 {
			return narrowed
		}
	}
	return tests
}

func reflectSame(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func checkRank(check string) int {
	switch check {
	case "format":
		return 0
	case "lint":
		return 1
	case "types":
		return 2
	default:
		return 3
	}
}

func optionContext(opts Options) context.Context {
	if opts.Context != nil {
		return opts.Context
	}
	return context.Background()
}
