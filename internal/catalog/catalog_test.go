package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

const validJSON = `{"schema":1,"id":"plugin-alpha","owner":"test-maintainers","paths":["alpha/**"],"test_paths":[],"inputs":[],"platforms":["linux-debian","windows-arm64"],"required_in":["affected","full","candidate"],"timeout_seconds":300,"resources":{"weight":1},"needs":[],"depends_on":[],"optional_contracts":[],"cache":{"policy":"none"},"assertions":[],"outputs":{"visibility":"private-runner-only"},"pack":"workspace","cost_class":"normal","checks":{"unit":{"census":["suite/one","suite/two"],"paths":["alpha/src/**"],"test_paths":["alpha/tests/**"],"inputs":[],"platforms":["linux-debian","windows-arm64"],"commands":{"default":[{"cwd":"alpha","argv":["vitest","run"]}]},"needs":[],"depends_on":[],"variants":[],"assertions":["suite/one"],"reports":["reports/alpha.xml"]}}}`

func fixture(t *testing.T) Unit {
	t.Helper()
	u, e := Decode([]byte(validJSON))
	if e != nil {
		t.Fatalf("valid schema bootstrap: %v", e)
	}
	u.Root = "alpha"
	return u
}
func TestSchemaAndStrictDecode(t *testing.T) {
	u := fixture(t)
	if u.ID != "plugin-alpha" || u.Digest == "" {
		t.Fatal("missing identity")
	}
	public, e := os.ReadFile("../../schemas/unit-v1.json")
	if e != nil || !bytes.Equal(public, Schema) {
		t.Fatal("bundled schema differs")
	}
	for _, input := range []string{strings.Replace(validJSON, `"schema":1`, `"schema":1,"schema":1`, 1), strings.Replace(validJSON, `"schema":1`, `"schema":1,"extra":true`, 1), validJSON + `{}`, strings.Replace(validJSON, `"alpha/**"`, `"../alpha/**"`, 1), strings.Replace(validJSON, `"linux-debian"`, `"imaginary"`, 1), strings.Replace(validJSON, `"cwd":"alpha"`, `"cwd":"../alpha"`, 1), strings.Replace(validJSON, `"owner":"test-maintainers"`, `"owner":"\ud800"`, 1)} {
		if _, e := Decode([]byte(input)); e == nil {
			t.Fatal("accepted malformed descriptor")
		}
	}
	if _, e := Decode(bytes.Repeat([]byte(" "), MaxDescriptorBytes+1)); e == nil {
		t.Fatal("accepted oversized descriptor")
	}
}
func TestCatalogueCoverageAndSelectors(t *testing.T) {
	u := fixture(t)
	c := u.Checks["unit"]
	c.Variants = []Variant{{ID: "light", Axes: map[string]string{"theme": "light"}, Args: []string{"--theme=light"}}, {ID: "dark", Axes: map[string]string{"theme": "dark"}, Args: []string{"--theme=dark"}}}
	c.Shards = &ShardPolicy{Count: 2, Census: c.Census, Assignments: [][]string{{"suite/one"}, {"suite/two"}}, Measurement: "measured-example", DurationSeconds: []float64{1, 2}, Runner: "playwright"}
	u.Checks["unit"] = c
	entries, e := Entries([]Unit{u})
	if e != nil || len(entries) != 8 {
		t.Fatalf("matrix: %d %v", len(entries), e)
	}
	selected := []Entry{entries[3]}
	wire, e := Selector([]Unit{u}, selected)
	if e != nil || strings.Contains(wire, "alpha") {
		t.Fatal("opaque selector")
	}
	resolved, e := ResolveSelector([]Unit{u}, wire)
	if e != nil || len(resolved) != 1 || entryKey(resolved[0]) != entryKey(selected[0]) {
		t.Fatal("selector roundtrip")
	}
	for _, bad := range []string{wire + ",3", wire + ",0", strings.Replace(wire, "v1:", "v2:", 1), wire + ",999999"} {
		if _, e := ResolveSelector([]Unit{u}, bad); e == nil {
			t.Fatal("invalid selector accepted")
		}
	}
	u.Owner = "changed-owner"
	u.Digest = "changed"
	if _, e := ResolveSelector([]Unit{u}, wire); e == nil {
		t.Fatal("stale selector accepted")
	}
	c.Shards.Assignments = [][]string{{"suite/one"}, {"suite/one"}}
	u.Checks["unit"] = c
	if _, e := Entries([]Unit{u}); e == nil {
		t.Fatal("overlapping shards accepted")
	}
}
func TestCyclesAndMissingCommands(t *testing.T) {
	a := fixture(t)
	b := fixture(t)
	b.ID = "plugin-beta"
	b.Root = "beta"
	a.DependsOn = []string{b.ID}
	b.OptionalContracts = []string{a.ID}
	if Validate([]Unit{a, b}) == nil {
		t.Fatal("product cycle accepted")
	}
	a.DependsOn = nil
	b.OptionalContracts = nil
	a.Needs = []string{"plugin-beta-unit@linux-debian"}
	c := b.Checks["unit"]
	c.Needs = []string{"plugin-alpha-unit@linux-debian"}
	b.Checks["unit"] = c
	if Validate([]Unit{a, b}) == nil {
		t.Fatal("prerequisite cycle accepted")
	}
	a = fixture(t)
	c = a.Checks["unit"]
	c.Commands = nil
	a.Checks["unit"] = c
	if Validate([]Unit{a}) == nil {
		t.Fatal("missing commands accepted")
	}
	if Validate([]Unit{fixture(t), fixture(t)}) == nil {
		t.Fatal("duplicate identity accepted")
	}
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestLoadAndImmutableGitCatalogue(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "alpha", "ci-unit.json"), []byte(validJSON))
	write(t, filepath.Join(root, "alpha", "src", "one.ts"), []byte("export const one = 1;"))
	write(t, filepath.Join(root, "alpha", "src", "unrelated\nname.ts"), []byte("export const two = 2;"))
	units, e := Load(root)
	if e != nil || len(units) != 1 || units[0].Root != "alpha" {
		t.Fatal("descriptor discovery")
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-C", root}, args...)...)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git fixture: %v", e)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("add", ".")
	git("-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Create synthetic tree")
	sha := git("rev-parse", "HEAD")
	write(t, filepath.Join(root, "alpha", "ci-unit.json"), []byte(`{}`))
	bound, e := LoadAt(root, sha)
	if e != nil || len(bound) != 1 || bound[0].ID != "plugin-alpha" {
		t.Fatalf("immutable descriptor read: %v", e)
	}
	write(t, filepath.Join(root, "alpha", "ci-unit.json"), []byte(validJSON))
	write(t, filepath.Join(root, "unsafe\nroot", "ci-unit.json"), []byte(validJSON))
	git("add", ".")
	git("-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Add unsafe protected descriptor")
	unsafeSHA := git("rev-parse", "HEAD")
	if _, e := LoadAt(root, unsafeSHA); e == nil {
		t.Fatal("unsafe protected descriptor path accepted")
	}
}
func TestSharedUnitsAndReportContracts(t *testing.T) {
	u := fixture(t)
	raw, _ := json.Marshal(Shared{Schema: 1, Units: []Unit{u}, Packs: map[string]PackPolicy{"linux-debian": {Setup: []Command{{Cwd: ".", Argv: []string{"node", "--version"}, TimeoutSeconds: 30}}, Tools: map[string]string{"node": "26.8.1"}}}})
	s, e := DecodeShared(raw)
	if e != nil || len(s.Units) != 1 {
		t.Fatalf("shared config: %v", e)
	}
	c := u.Checks["unit"]
	c.Census = nil
	u.Checks["unit"] = c
	if Validate([]Unit{u}) == nil {
		t.Fatal("zero required census accepted")
	}
}

func TestUnrelatedSymlinkAndProtectedPaths(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "alpha", "ci-unit.json"), []byte(validJSON))
	write(t, filepath.Join(root, "alpha", "src", "one.ts"), []byte("export const one = 1;"))
	if e := os.Symlink("unavailable-target", filepath.Join(root, "unrelated-link")); e != nil {
		t.Skip("symlink creation unavailable on this test venue")
	}
	if _, e := Load(root); e != nil {
		t.Fatal("unrelated non-followed link blocked catalogue")
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(root, "reports")); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(root); e == nil {
		t.Fatal("linked report path accepted")
	}
}
func FuzzDecodeDescriptor(f *testing.F) {
	f.Add([]byte(validJSON))
	f.Add([]byte(`{"schema":1,"schema":1}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = Decode(data) })
}

func TestSharedSupportIsBoundedAndPreserved(t *testing.T) {
	support := json.RawMessage(`{"kind":"generic","version":1}`)
	raw, _ := json.Marshal(Shared{Schema: 1, Units: []Unit{}, Packs: map[string]PackPolicy{"web-linux": {Setup: []Command{}, Tools: map[string]string{}, Support: support}}})
	decoded, e := DecodeShared(raw)
	if e != nil || string(decoded.Packs["web-linux"].Support) != string(support) {
		t.Fatalf("support projection changed: %v", e)
	}
	raw, _ = json.Marshal(Shared{Schema: 1, Units: []Unit{}, Packs: map[string]PackPolicy{"web-linux": {Setup: []Command{}, Tools: map[string]string{}, Support: json.RawMessage(`{"data":"` + strings.Repeat("x", 16<<10) + `"}`)}}})
	if _, e := DecodeShared(raw); e == nil {
		t.Fatal("oversized venue support accepted")
	}
}

func TestAggregateAliasesAndConfiguredDiscovery(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "alpha", "ci-unit.json"), []byte(validJSON))
	write(t, filepath.Join(root, "alpha", "src", "one.ts"), []byte("export const one = 1;"))
	write(t, filepath.Join(root, "excluded", "ci-unit.json"), []byte(`{}`))
	shared := Shared{Schema: 1, Units: []Unit{}, Packs: map[string]PackPolicy{}, Aliases: map[string][]string{"all-checks": {"plugin-alpha"}, "focused": {"plugin-alpha-unit@linux-debian"}, "nested": {"focused"}}}
	raw, _ := json.Marshal(shared)
	write(t, filepath.Join(root, "config", "shared.json"), raw)
	units, e := LoadWithConfig(root, []string{"alpha"}, "config/shared.json")
	if e != nil || len(units) != 1 {
		t.Fatalf("configured discovery: %v", e)
	}
	bound, e := LoadSharedPath(root, "config/shared.json")
	if e != nil {
		t.Fatal(e)
	}
	selectors, e := ExpandAliases(units, bound, []string{"nested"})
	if e != nil || len(selectors) != 1 || selectors[0] != "plugin-alpha-unit@linux-debian" {
		t.Fatal("aggregate alias expansion")
	}
	metadata, e := AggregateAliases(units, bound)
	if e != nil || len(metadata) != 3 {
		t.Fatal("aggregate alias metadata")
	}
	bound.Aliases["focused"] = []string{"nested"}
	if _, e := ExpandAliases(units, bound, []string{"focused"}); e == nil {
		t.Fatal("alias cycle accepted")
	}
	if _, e := LoadWithConfig(root, []string{"alpha/**"}, "config/shared.json"); e == nil {
		t.Fatal("glob unit root accepted")
	}
	if _, e := LoadWithConfig(root, []string{"alpha"}, "missing.json"); e == nil {
		t.Fatal("missing configured shared file accepted")
	}
}

func TestDescriptorIDVocabulary(t *testing.T) {
	for _, id := range []string{"leaf.v1", "leaf_main", "leaf.v1_main-part"} {
		data := strings.Replace(validJSON, `"id":"plugin-alpha"`, `"id":"`+id+`"`, 1)
		if _, e := Decode([]byte(data)); e != nil {
			t.Fatalf("valid ID refused: %v", e)
		}
	}
	for _, id := range []string{".leaf", "_leaf", "Leaf", "leaf/name", "leaf name", "leaf:unit"} {
		data := strings.Replace(validJSON, `"id":"plugin-alpha"`, `"id":"`+id+`"`, 1)
		if _, e := Decode([]byte(data)); e == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	u := fixture(t)
	shared := Shared{Aliases: map[string][]string{"bundle.all_checks": {u.ID}}}
	if _, e := ExpandAliases([]Unit{u}, shared, []string{"bundle.all_checks"}); e != nil {
		t.Fatal("valid aggregate alias refused")
	}
}

func TestCancelledImmutableCatalogueDoesNotSpawn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := LoadAtContext(ctx, "unavailable-root", strings.Repeat("a", 40)); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled immutable catalogue did not preserve cancellation")
	}
}

func TestConfiguredImmutableCompositionSelectorParity(t *testing.T) {
	ctx := context.Background()
	local := map[string]string{}
	sources := map[string]CompositionSource{}
	heads := map[string]string{}
	for _, name := range []string{"alpha", "beta"} {
		root := t.TempDir()
		local[name] = root
		u := fixture(t)
		u.ID = "plugin-" + name
		u.Paths = []string{name + "/**"}
		u.Platforms = []string{"linux-debian", "windows-arm64"}
		c := u.Checks["unit"]
		c.Paths = []string{name + "/src/**"}
		c.TestPaths = []string{name + "/tests/**"}
		c.Commands = map[string][]Command{"default": {{Cwd: name, Argv: []string{"playwright", "test"}}}}
		if name == "alpha" {
			u.DependsOn = []string{"plugin-beta"}
			c.Variants = []Variant{{ID: "dark", Axes: map[string]string{"theme": "dark"}, Args: []string{}}, {ID: "light", Axes: map[string]string{"theme": "light"}, Args: []string{}}}
			c.Shards = &ShardPolicy{Count: 2, Census: c.Census, Assignments: [][]string{{"suite/one"}, {"suite/two"}}, Measurement: "measured-example", DurationSeconds: []float64{1, 1}, Runner: "playwright"}
		}
		u.Checks["unit"] = c
		data, _ := json.Marshal(u)
		write(t, filepath.Join(root, name, "ci-unit.json"), data)
		write(t, filepath.Join(root, name, "src", "one.ts"), []byte("export const one = 1;"))
		write(t, filepath.Join(root, "excluded", "ci-unit.json"), []byte(`{}`))
		shared, _ := json.Marshal(Shared{Schema: 1, Units: []Unit{}, Packs: map[string]PackPolicy{}})
		write(t, filepath.Join(root, "configuration", "pack.json"), shared)
		cfg := config.Repository{SchemaVersion: 1, Concern: "synthetic", KitAPI: 1, UnitRoots: []string{name}, SharedUnits: "configuration/pack.json", LocalPolicy: "developer-machine", ResultsDir: ".ci-local/results", RetentionDays: 7}
		raw, _ := json.Marshal(cfg)
		write(t, filepath.Join(root, ".kd-ci.json"), raw)
		git := func(args ...string) string {
			t.Helper()
			cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=" + os.DevNull, "-C", root}, args...)...)
			out, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatalf("git composition fixture: %v", e)
			}
			return strings.TrimSpace(string(out))
		}
		git("init", "--quiet")
		git("add", ".")
		git("-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Create configured composition fixture")
		heads[name] = git("rev-parse", "HEAD")
		host := filepath.Join(t.TempDir(), "checkout")
		// Clone committed objects through Git; copying .git races its transient maintenance locks.
		git("clone", "--quiet", "--no-hardlinks", root, host)
		sources[name] = CompositionSource{Root: host, SHA: heads[name]}
	}
	working, _, e := LoadCompositionConfigured(ctx, local)
	if e != nil {
		t.Fatalf("configured composition: %v", e)
	}
	gitPath, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	immutable, origins, e := LoadCompositionAt(ctx, sources, gitPath)
	if e != nil {
		t.Fatalf("immutable composition: %v", e)
	}
	left, e := CatalogueDigest(working)
	if e != nil {
		t.Fatal(e)
	}
	right, e := CatalogueDigest(immutable)
	if e != nil || left != right {
		t.Fatal("local and hosted catalogue digests differ")
	}
	entries, e := Entries(working)
	if e != nil {
		t.Fatal(e)
	}
	selected := []Entry{}
	for _, entry := range entries {
		if entry.ID == "plugin-alpha-unit@windows-arm64" && entry.Variant == "dark" && entry.Shard == "1/2" {
			selected = append(selected, entry)
		}
	}
	if len(selected) != 1 {
		t.Fatal("single variant/shard was not isolated")
	}
	wire, e := Selector(working, selected)
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := ResolveSelector(immutable, wire)
	if e != nil || len(resolved) != 1 || resolved[0].Variant != "dark" || resolved[0].Shard != "1/2" {
		t.Fatal("two-source selector parity failed")
	}
	expected, e := filepath.EvalSymlinks(sources["alpha"].Root)
	if e != nil || origins["plugin-alpha"] != expected {
		t.Fatal("execution origin changed")
	}
	missing := sources["beta"]
	missing.SHA = strings.Repeat("f", 40)
	sources["beta"] = missing
	if _, _, e := LoadCompositionAt(ctx, sources, gitPath); e == nil {
		t.Fatal("missing locked object was not refused")
	}
}
