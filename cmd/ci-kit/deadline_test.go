package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/planner"
)

func TestHostedUnitDeadlineCapsCombinedCommands(t *testing.T) {
	root := t.TempDir()
	unit := catalog.Unit{ID: "sample", TimeoutSeconds: 1, Checks: map[string]catalog.Check{"format": {}}}
	inv := planner.Invocation{Unit: "sample", Check: "format", Platform: "server-linux-x64", Commands: []catalog.Command{{Cwd: ".", Argv: []string{"first"}, TimeoutSeconds: 60}, {Cwd: ".", Argv: []string{"second"}, TimeoutSeconds: 60}, {Cwd: ".", Argv: []string{"unreachable"}, TimeoutSeconds: 60}}}
	started, completed := 0, 0
	run := func(ctx context.Context, cmd catalog.Command, source string) int {
		started++
		if cmd.TimeoutSeconds != 1 {
			t.Fatal("command extended the trusted unit cap")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("unit deadline missing")
		}
		duration := 900 * time.Millisecond
		if started == 1 {
			duration = 200 * time.Millisecond
		}
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-timer.C:
			completed++
			return 0
		case <-ctx.Done():
			return contextExit(ctx)
		}
	}
	code := runChecks(context.Background(), root, t.TempDir(), "server-linux-x64", []catalog.Unit{unit}, map[string]string{"sample": root}, []planner.Invocation{inv}, false, run, func(context.Context, string) error { return nil })
	if code != 4 || started != 2 || completed != 1 {
		t.Fatalf("unit deadline did not stop combined work: code=%d started=%d completed=%d", code, started, completed)
	}
}
func TestHostedUnitCancellationIsSeparateFromDeadline(t *testing.T) {
	root := t.TempDir()
	unit := catalog.Unit{ID: "sample", TimeoutSeconds: 30, Checks: map[string]catalog.Check{"format": {}}}
	inv := planner.Invocation{Unit: "sample", Check: "format", Platform: "server-linux-x64", Commands: []catalog.Command{{Cwd: ".", Argv: []string{"unreachable"}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	code := runChecks(ctx, root, t.TempDir(), "server-linux-x64", []catalog.Unit{unit}, map[string]string{"sample": root}, []planner.Invocation{inv}, false, func(context.Context, catalog.Command, string) int { called = true; return 0 }, func(context.Context, string) error { return nil })
	if code != 7 || called {
		t.Fatal("cancelled unit executed or became a deadline")
	}
}

func TestHostedUnitDeadlineChild(t *testing.T) {
	if len(os.Args) > 1 && os.Args[len(os.Args)-1] == "deadline-probe" {
		time.Sleep(1100 * time.Millisecond)
		os.Exit(0)
	}
}
func TestHostedPackStopsCombinedRealChildrenAtUnitDeadline(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	private, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(root, "ci"), 0700); e != nil {
		t.Fatal(e)
	}
	cfg := []byte(`{"schema_version":1,"concern":"sample","kit_api":1,"unit_roots":["."],"shared_units":"ci/units.json","local_policy":"developer-machine"}`)
	if e = os.WriteFile(filepath.Join(root, ".kd-ci.json"), cfg, 0600); e != nil {
		t.Fatal(e)
	}
	platform := "server-linux-x64"
	switch runtime.GOOS {
	case "darwin":
		platform = "macos-x64"
		if runtime.GOARCH == "arm64" {
			platform = "macos-arm64"
		}
	case "windows":
		platform = "windows-x64"
		if runtime.GOARCH == "arm64" {
			platform = "windows-arm64"
		}
	default:
		if runtime.GOARCH == "arm64" {
			platform = "server-linux-arm64"
		}
	}
	shared := catalog.Shared{Schema: 1, Units: []catalog.Unit{}, Packs: map[string]catalog.PackPolicy{platform: {Setup: []catalog.Command{}, Tools: map[string]string{}}}}
	raw, e := json.Marshal(shared)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "ci", "units.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	command := catalog.Command{Cwd: ".", Argv: []string{executable, "-test.run=^TestHostedUnitDeadlineChild$", "--", "deadline-probe"}, TimeoutSeconds: 60}
	unit := catalog.Unit{ID: "sample", TimeoutSeconds: 2, Checks: map[string]catalog.Check{"format": {}}}
	inv := planner.Invocation{Unit: "sample", Check: "format", Platform: platform, Commands: []catalog.Command{command, command}}
	if code := runPack(context.Background(), root, private, platform, []catalog.Unit{unit}, map[string]string{"sample": root}, []planner.Invocation{inv}, false); code != 4 {
		t.Fatalf("combined real children escaped unit deadline: got %d", code)
	}
}
