package planner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

func feature(id, root string) catalog.Unit {
	c := catalog.Check{Census: []string{"suite/one", "suite/two"}, Paths: []string{root + "/src/**"}, TestPaths: []string{root + "/tests/**"}, Inputs: []string{}, Platforms: []string{"linux-debian", "windows-arm64"}, Commands: map[string][]catalog.Command{"default": {{Cwd: root, Argv: []string{"vitest", "run"}}}}, Needs: []string{}, DependsOn: []string{}, Variants: []catalog.Variant{}, Assertions: []string{}, Reports: []string{"reports/" + id + ".xml"}}
	lint := c
	lint.Census = []string{}
	lint.Paths = []string{root + "/lint/**"}
	lint.Reports = []string{}
	lint.TestPaths = []string{}
	return catalog.Unit{Schema: 1, ID: id, Owner: "test-maintainers", Root: root, Paths: []string{root + "/**"}, TestPaths: []string{}, Inputs: []string{}, Platforms: c.Platforms, RequiredIn: []string{"affected", "full", "candidate"}, TimeoutSeconds: 300, Resources: catalog.Resources{Weight: 1}, Needs: []string{}, DependsOn: []string{}, OptionalContracts: []string{}, Cache: catalog.Cache{Policy: "none"}, Assertions: []string{}, Outputs: catalog.Outputs{Visibility: "private-runner-only"}, Pack: "workspace", CostClass: "normal", Checks: map[string]catalog.Check{"unit": c, "lint": lint}}
}
func ids(r Result) []string {
	out := []string{}
	for _, i := range r.Selected {
		out = append(out, i.ID)
	}
	sort.Strings(out)
	return out
}
func TestSingleCheckPlatformAndPack(t *testing.T) {
	a, b := feature("plugin-alpha", "alpha"), feature("plugin-beta", "beta")
	r, e := Select("", []catalog.Unit{a, b}, nil, Options{Units: []string{"plugin-alpha-unit@windows-arm64"}})
	if e != nil || !reflect.DeepEqual(ids(r), []string{"plugin-alpha-unit@windows-arm64"}) || len(r.Packs) != 1 {
		t.Fatalf("exact unit: %#v %v", r, e)
	}
	r, e = Select("", []catalog.Unit{a, b}, nil, Options{Mode: "full"})
	if e != nil || len(r.Packs) != 2 || len(r.Selected) != 8 {
		t.Fatal("one pack per platform")
	}
	for _, opts := range []Options{{Units: []string{"missing"}}, {Units: []string{"plugin-alpha-unit@windows-arm64"}, Platforms: []string{"linux-debian"}}, {Admission: true, Units: []string{a.ID}}, {Admission: true, Quick: true}, {Admission: true, Variants: []string{"theme=light"}}, {Quick: true, Mode: "full"}} {
		if _, e := Select("", []catalog.Unit{a, b}, nil, opts); e == nil {
			t.Fatal("invalid selection accepted")
		}
	}
}
func TestQuickRelatedAndAdmissionComplete(t *testing.T) {
	a := feature("plugin-alpha", "alpha")
	c := a.Checks["unit"]
	c.Related = &catalog.RelatedPolicy{Runner: "vitest", Census: c.Census, Tests: map[string][]string{"alpha/src/one.ts": {"suite/one"}}}
	a.Checks["unit"] = c
	r, e := Select("", []catalog.Unit{a}, []string{"alpha/src/one.ts"}, Options{Quick: true, Units: []string{"plugin-alpha-unit@linux-debian"}})
	if e != nil || len(r.Selected) != 1 || !reflect.DeepEqual(r.Selected[0].Tests, []string{"suite/one"}) || !strings.Contains(strings.Join(r.Selected[0].Commands[0].Argv, " "), "related alpha/src/one.ts --run") {
		t.Fatalf("quick related: %#v %v", r, e)
	}
	r, e = Select("", []catalog.Unit{a}, []string{"alpha/src/one.ts"}, Options{Admission: true})
	if e != nil || !r.FullFallback || len(r.Selected) != 4 {
		t.Fatal("prequalification admission must be full")
	}
	for _, i := range r.Selected {
		if strings.Contains(strings.Join(i.Commands[0].Argv, " "), "related") {
			t.Fatal("admission used related filters")
		}
	}
	r, e = Select("", []catalog.Unit{a}, []string{"alpha/src/unmapped.ts"}, Options{Quick: true, Units: []string{"plugin-alpha-unit@linux-debian"}})
	if e != nil || len(r.Selected[0].Tests) != 2 || strings.Contains(strings.Join(r.Selected[0].Commands[0].Argv, " "), "related") {
		t.Fatal("unknown map must run complete unit")
	}
}
func TestIndependentVariantShard(t *testing.T) {
	a := feature("plugin-alpha", "alpha")
	c := a.Checks["unit"]
	c.Variants = []catalog.Variant{{ID: "light", Axes: map[string]string{"theme": "light"}, Args: []string{}}, {ID: "dark", Axes: map[string]string{"theme": "dark"}, Args: []string{}}}
	c.Shards = &catalog.ShardPolicy{Count: 2, Census: c.Census, Assignments: [][]string{{"suite/one"}, {"suite/two"}}, Measurement: "duration-example", DurationSeconds: []float64{1, 2}, Runner: "playwright"}
	c.Commands = map[string][]catalog.Command{"default": {{Cwd: "alpha", Argv: []string{"playwright", "test"}}}}
	a.Checks["unit"] = c
	r, e := Select("", []catalog.Unit{a}, nil, Options{Units: []string{"plugin-alpha-unit@linux-debian#1/2"}, Variants: []string{"theme=dark"}})
	if e != nil || len(r.Selected) != 1 || r.Selected[0].Shard != "1/2" || r.Selected[0].Variant != "dark" || !reflect.DeepEqual(r.Selected[0].Tests, []string{"suite/one"}) {
		t.Fatalf("isolated shard variant: %#v %v", r, e)
	}
	for _, opts := range []Options{{Units: []string{"plugin-alpha-unit@linux-debian#3/2"}}, {Units: []string{a.ID}, Variants: []string{"theme=unknown"}}, {Units: []string{a.ID}, Variants: []string{"theme=dark", "theme=dark"}}} {
		if _, e := Select("", []catalog.Unit{a}, nil, opts); e == nil {
			t.Fatal("bad shard/variant")
		}
	}
}
func TestDiffPathsAndConservativeFallback(t *testing.T) {
	got, e := DiffPaths([]byte("R100\x00alpha/old.ts\x00alpha/new.ts\x00D\x00beta/deleted.ts\x00"))
	if e != nil || !reflect.DeepEqual(got, []string{"alpha/new.ts", "alpha/old.ts", "beta/deleted.ts"}) {
		t.Fatal("rename/delete preservation")
	}
	for _, raw := range []string{"M\x00bad\nname\x00", "M\x00missing-terminator", "R100\x00only-one\x00"} {
		if _, e := DiffPaths([]byte(raw)); e == nil {
			t.Fatal("unsafe diff accepted")
		}
	}
	units := []catalog.Unit{feature("plugin-alpha", "alpha"), feature("plugin-beta", "beta")}
	for _, paths := range [][]string{{"unknown/source.ts"}, {"alpha/runtime/Cargo.lock"}, {"package-lock.json"}, {"bad,filename.ts"}} {
		r, e := Select("", units, paths, Options{})
		if e != nil || !r.FullFallback || len(r.Selected) != 8 {
			t.Fatal("conservative fallback")
		}
	}
}
func writeFixture(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(root, rel)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-C", root}, args...)...)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git fixture: %v", e)
	}
	return strings.TrimSpace(string(out))
}
func TestActualNxAffectedAndPrerequisites(t *testing.T) {
	toolDir, e := filepath.Abs("../../planner")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(toolDir, "node_modules/nx/package.json")); os.IsNotExist(e) {
		t.Skip("reviewed Nx dependencies are not provisioned; affected qualification remains disabled")
	} else if e != nil {
		t.Fatal("Nx package metadata unavailable")
	}
	if _, e = os.Stat(filepath.Join(toolDir, "node_modules/nx/dist/bin/nx.js")); e != nil {
		t.Fatal("provisioned Nx is missing its pinned public CLI entrypoint")
	}
	root := t.TempDir()
	a, b := feature("plugin-alpha", "alpha"), feature("plugin-beta", "beta")
	for _, u := range []catalog.Unit{a, b} {
		data, _ := json.Marshal(u)
		writeFixture(t, root, u.Root+"/ci-unit.json", data)
		writeFixture(t, root, u.Root+"/src/one.ts", []byte("export const one = 1;"))
		writeFixture(t, root, u.Root+"/lint/config.txt", []byte("one"))
	}
	gitFixture(t, root, "init", "--quiet")
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Create synthetic fixtures")
	base := gitFixture(t, root, "rev-parse", "HEAD")
	writeFixture(t, root, "alpha/src/one.ts", []byte("export const one = 2;"))
	gitFixture(t, root, "add", ".")
	gitFixture(t, root, "-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Change one synthetic input")
	sha := gitFixture(t, root, "rev-parse", "HEAD")
	nodePath, _ := exec.LookPath("node")
	gitPath, _ := exec.LookPath("git")
	cargoPath, _ := exec.LookPath("cargo")
	opts := Options{Base: base, SHA: sha, ToolDirectory: toolDir, Mode: "affected", Executables: map[string]string{"node": nodePath, "git": gitPath, "cargo": cargoPath}}
	r, e := Plan(root, []catalog.Unit{a, b}, opts)
	if e != nil || r.FullFallback || !reflect.DeepEqual(ids(r), []string{"plugin-alpha-unit@linux-debian", "plugin-alpha-unit@windows-arm64"}) {
		t.Fatalf("actual Nx exact feature: %#v %v", r, e)
	}
	b.OptionalContracts = []string{a.ID}
	r, e = Plan(root, []catalog.Unit{a, b}, opts)
	if e != nil || r.FullFallback || len(r.Selected) != 6 {
		t.Fatalf("optional dependent selection: %#v %v", r, e)
	}
	b.OptionalContracts = nil
	policy := b.Checks["unit"]
	policy.DependsOn = []string{a.ID}
	b.Checks["unit"] = policy
	mixed, e := Select(root, []catalog.Unit{a, b}, []string{"alpha/src/one.ts", "beta/lint/config.txt"}, Options{ToolDirectory: toolDir, Executables: opts.Executables})
	if e != nil || mixed.FullFallback || len(mixed.Selected) != 6 {
		t.Fatalf("mixed direct and dependency checks: %#v %v", mixed, e)
	}
	policy.DependsOn = nil
	b.Checks["unit"] = policy
	c := a.Checks["unit"]
	c.Needs = []string{"plugin-beta-lint@linux-debian"}
	a.Checks["unit"] = c
	r, e = Plan(root, []catalog.Unit{a, b}, opts)
	if e != nil || r.FullFallback || len(r.Selected) != 3 || r.Selected[0].ID != "plugin-beta-lint@linux-debian" {
		t.Fatalf("prerequisite isolation/order: %#v %v", r, e)
	}
}

func TestEmbeddedAdapterBundleMatches(t *testing.T) {
	for _, fixture := range []struct {
		Path string
		Data []byte
	}{{"../../planner/adapter.cjs", adapter}, {"../../planner/package-lock.json", packageLock}} {
		data, e := os.ReadFile(fixture.Path)
		if e != nil || string(data) != string(fixture.Data) {
			t.Fatal("embedded analysis bundle drift")
		}
	}
}

func TestIndependentCheapChecksBeforeTests(t *testing.T) {
	a := feature("plugin-alpha", "alpha")
	a.Checks["integration"] = a.Checks["unit"]
	r, e := Select("", []catalog.Unit{a}, nil, Options{Mode: "full"})
	if e != nil {
		t.Fatal(e)
	}
	seenTest := false
	for _, inv := range r.Packs[0].Units {
		if checkRank(inv.Check) >= 3 {
			seenTest = true
		} else if seenTest {
			t.Fatal("independent cheap check scheduled after tests")
		}
	}
}

func FuzzDiffPaths(f *testing.F) {
	f.Add([]byte("M\x00alpha/source.ts\x00"))
	f.Add([]byte("R100\x00alpha/old.ts\x00beta/new.ts\x00"))
	f.Add([]byte("M\x00bad\nname\x00"))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DiffPaths(data) })
}

func TestUnclassifiedJavaScriptForcesFull(t *testing.T) {
	for _, source := range []string{`const load = require /* comment */ ("../beta/one");`, `const load = import /* comment */ ("../beta/one");`, `const load = createRequire(import.meta.url);`, `const load = new Function("return 1");`} {
		root := t.TempDir()
		writeFixture(t, root, "alpha/src/one.ts", []byte(source))
		_, reason := projectMetadata(context.Background(), root, []catalog.Unit{feature("plugin-alpha", "alpha")})
		if reason == "" {
			t.Fatal("unclassified JavaScript was treated as a complete dependency map")
		}
	}
}

func TestRegisteredToolsCannotHideInsidePhysicalSource(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "node", []byte("synthetic"))
	alias := filepath.Join(t.TempDir(), "checkout")
	if e := os.Symlink(root, alias); e != nil {
		t.Skip("symlink creation unavailable on this venue")
	}
	if _, e := approvedExecutable(alias, "node", filepath.Join(root, "node")); e == nil {
		t.Fatal("source-contained registered executable accepted through a source alias")
	}
}

func TestCancelledContextCannotBecomeFullFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	units := []catalog.Unit{feature("plugin-alpha", "alpha")}
	for _, mode := range []string{"full", "affected"} {
		opts := Options{Context: ctx, Mode: mode}
		result, e := Plan("", units, opts)
		if !errors.Is(e, context.Canceled) || result.FullFallback {
			t.Fatal("cancelled plan became work")
		}
		result, e = Select("", units, []string{"unknown.ts"}, opts)
		if !errors.Is(e, context.Canceled) || result.FullFallback {
			t.Fatal("cancelled selection became fallback work")
		}
	}
}

func TestAnalysisWorkspaceIsUserOnly(t *testing.T) {
	workspace, cleanup, e := newAnalysisWorkspace()
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if !process.UserOnly(workspace, true) {
		t.Fatal("analysis root is not user-only before projection")
	}
}

func TestSourceRustConfigurationPreventsMetadataExecution(t *testing.T) {
	cases := map[string]string{".cargo/config": "source Cargo configuration is unclassified", ".cargo/config.toml": "source Cargo configuration is unclassified", "rust-toolchain": "source Rust toolchain configuration is unclassified", "rust-toolchain.toml": "source Rust toolchain configuration is unclassified"}
	for path, want := range cases {
		root := t.TempDir()
		writeFixture(t, root, "alpha/Cargo.toml", []byte("[package]\nname = 'synthetic'\nversion = '0.1.0'\n"))
		writeFixture(t, root, path, []byte("unclassified source configuration"))
		_, reason := projectMetadata(context.Background(), root, []catalog.Unit{feature("plugin-alpha", "alpha")})
		if reason != want {
			t.Fatalf("source configuration reached metadata: %s", reason)
		}
	}
	if !contains(cleanEnv(), "RUSTUP_AUTO_INSTALL=0") {
		t.Fatal("metadata can implicitly install a toolchain")
	}
}
