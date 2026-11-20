package cli

import (
	"context"
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rewriteFixtureJSON(t *testing.T, path string, v any) {
	t.Helper()
	if os.Remove(path) != nil {
		t.Fatal("remove private fixture file")
	}
	writePrivateJSON(t, path, v)
}
func TestTwoSourceCatalogueAndOpaqueVariantShardParity(t *testing.T) {
	root, platform, base := localFixture(t)
	machine, e := config.LoadMachine()
	if e != nil {
		t.Fatal(e)
	}
	git := machine.Tools["git"]
	dependency := filepath.Join(filepath.Dir(root), "dependency")
	if os.Mkdir(dependency, 0700) != nil {
		t.Fatal("dependency mkdir")
	}
	os.Mkdir(filepath.Join(dependency, "ci"), 0700)
	raw, e := os.ReadFile(filepath.Join(root, "ci-unit.json"))
	if e != nil {
		t.Fatal(e)
	}
	var unit catalog.Unit
	if json.Unmarshal(raw, &unit) != nil {
		t.Fatal("unit")
	}
	unit.ID = "dependency"
	depPlatform := "windows-arm64"
	if depPlatform == platform {
		depPlatform = "web-linux"
	}
	unit.Platforms = []string{depPlatform}
	check := unit.Checks["format"]
	check.Platforms = unit.Platforms
	check.Census = []string{"suite/one", "suite/two"}
	check.Assertions = []string{"suite/one"}
	check.Reports = []string{"reports/dependency.xml"}
	check.Variants = []catalog.Variant{{ID: "dark", Axes: map[string]string{"theme": "dark"}, Args: []string{}}, {ID: "light", Axes: map[string]string{"theme": "light"}, Args: []string{}}}
	check.Shards = &catalog.ShardPolicy{Count: 2, Census: check.Census, Assignments: [][]string{{"suite/one"}, {"suite/two"}}, Measurement: "synthetic-measured", DurationSeconds: []float64{1, 1}, Runner: "playwright"}
	unit.Checks = map[string]catalog.Check{"unit": check}
	writePrivateJSON(t, filepath.Join(dependency, "ci-unit.json"), unit)
	writePrivateJSON(t, filepath.Join(dependency, "ci", "units.json"), map[string]any{"schema": 1, "units": []any{}, "packs": map[string]any{}})
	writePrivateJSON(t, filepath.Join(dependency, ".kd-ci.json"), map[string]any{"schema_version": 1, "concern": "dependency", "kit_api": 1, "unit_roots": []string{"."}, "shared_units": "ci/units.json", "local_policy": "developer-machine"})
	os.WriteFile(filepath.Join(dependency, "sample.txt"), []byte("dependency\n"), 0600)
	gitFixture(t, git, dependency, "init", "-b", "main")
	gitFixture(t, git, dependency, "add", ".")
	gitFixture(t, git, dependency, "-c", "user.name=Test Maintainer", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--no-gpg-sign", "-m", "Add dependency fixture")
	depSHA := strings.TrimSpace(gitFixture(t, git, dependency, "rev-parse", "HEAD"))
	cfg, e := config.Load(root, "")
	if e != nil {
		t.Fatal(e)
	}
	cfg.CompositionLock = "ci/composition.json"
	rewriteFixtureJSON(t, filepath.Join(root, ".kd-ci.json"), cfg)
	writePrivateJSON(t, filepath.Join(root, "ci", "composition.json"), config.Composition{SchemaVersion: 1, Sources: []config.LockedSource{{Alias: "dependency", SHA: depSHA, BaseSHA: depSHA}}})
	gitFixture(t, git, root, "add", ".")
	gitFixture(t, git, root, "-c", "user.name=Test Maintainer", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "commit", "--no-gpg-sign", "-m", "Lock synthetic dependency")
	sha := strings.TrimSpace(gitFixture(t, git, root, "rev-parse", "HEAD"))
	machine.CompositionRoots = map[string]string{"dependency": dependency}
	userDir, _ := os.UserConfigDir()
	rewriteFixtureJSON(t, filepath.Join(userDir, "KeelDock", "kd-ci-machine.json"), machine)
	a := &app{ctx: context.Background()}
	units, e := a.committedCatalogue(root, sha, cfg)
	if e != nil {
		t.Fatal(e)
	}
	sources := map[string]catalog.CompositionSource{"synthetic": {Root: root, SHA: sha}, "dependency": {Root: dependency, SHA: depSHA}}
	hosted, origins, e := catalog.LoadCompositionAt(context.Background(), sources, git)
	if e != nil || origins["dependency"] != dependency {
		t.Fatal("hosted assembly")
	}
	first, _ := catalog.CatalogueDigest(units)
	second, _ := catalog.CatalogueDigest(hosted)
	if first != second {
		t.Fatal("local/hosted catalogue mismatch")
	}
	entries, _ := catalog.Entries(units)
	selected := []catalog.Entry{}
	for _, entry := range entries {
		if entry.Unit == "dependency" && entry.Variant == "dark" && entry.Shard == "2/2" {
			selected = append(selected, entry)
		}
	}
	if len(selected) != 1 {
		t.Fatal("variant/shard discovery")
	}
	selector, e := catalog.Selector(units, selected)
	if e != nil || strings.Contains(selector, "dependency") || strings.Contains(selector, "dark") {
		t.Fatal("opaque selector")
	}
	resolved, e := catalog.ResolveSelector(hosted, selector)
	if e != nil || len(resolved) != 1 || resolved[0].Variant != "dark" || resolved[0].Shard != "2/2" {
		t.Fatal("two-source exact selector mismatch")
	}
	plan, e := a.feedbackSelection(root, sha, cfg, units, planner.Options{Context: a.ctx, Base: base, SHA: sha, Mode: "affected", Executables: machine.Tools})
	if e != nil || len(plan.Packs) != 2 {
		t.Fatal("feedback expected platforms do not bind full fallback")
	}
	unit.Owner = "dirty-local-owner"
	rewriteFixtureJSON(t, filepath.Join(dependency, "ci-unit.json"), unit)
	immutable, e := a.committedCatalogue(root, sha, cfg)
	if e != nil {
		t.Fatal(e)
	}
	digest, _ := catalog.CatalogueDigest(immutable)
	if digest != first {
		t.Fatal("dirty dependency changed immutable selector")
	}
	machine.CompositionRoots = nil
	rewriteFixtureJSON(t, filepath.Join(userDir, "KeelDock", "kd-ci-machine.json"), machine)
	if _, e = a.committedCatalogue(root, sha, cfg); e == nil {
		t.Fatal("missing approved local dependency mapping accepted")
	}
}
