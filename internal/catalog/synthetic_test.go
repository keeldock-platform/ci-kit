package catalog

import (
	"github.com/keeldock-platform/ci-kit/internal/config"
	"os/exec"
	"path/filepath"
	"testing"
)

// Validate the generated native variants before any fixture commands can run.
func TestMaterializedSyntheticCatalogue(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	root := filepath.Join(t.TempDir(), "source")
	if out, err := exec.Command(node, "../../fixtures/synthetic/materialize.mjs", root).CombinedOutput(); err != nil {
		t.Fatalf("materializer failed: %v (%d bytes)", err, len(out))
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	units, err := LoadWithConfig(root, cfg.UnitRoots, cfg.SharedUnits)
	if err != nil {
		t.Fatal("generated fixture catalogue refused", err)
	}
	if len(units) != 7 {
		t.Fatal("fixture unit coverage incomplete")
	}
	entries, err := Entries(units)
	if err != nil || len(entries) != 39 {
		t.Fatal("fixture execution coverage incomplete", len(entries), err)
	}
}
