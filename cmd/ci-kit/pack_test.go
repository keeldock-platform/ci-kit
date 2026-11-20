package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/planner"
)

func TestMissingSecondVariantCannotReuseFirstReport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixture; native Windows process evidence is separate")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	private, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if e := os.Mkdir(filepath.Join(root, "ci"), 0700); e != nil {
		t.Fatal(e)
	}
	cfg := []byte(`{"schema_version":1,"concern":"sample","kit_api":1,"unit_roots":["."],"shared_units":"ci/units.json","local_policy":"developer-machine"}`)
	if e := os.WriteFile(filepath.Join(root, ".kd-ci.json"), cfg, 0600); e != nil {
		t.Fatal(e)
	}
	platform := "server-linux-x64"
	if runtime.GOOS == "darwin" {
		platform = "macos-x64"
		if runtime.GOARCH == "arm64" {
			platform = "macos-arm64"
		}
	} else if runtime.GOARCH == "arm64" {
		platform = "server-linux-arm64"
	}
	shared := catalog.Shared{Schema: 1, Units: []catalog.Unit{}, Packs: map[string]catalog.PackPolicy{platform: {Setup: []catalog.Command{}, Tools: map[string]string{}}}}
	data, e := json.Marshal(shared)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "ci", "units.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	script := "#!/bin/sh\nif test -f marker; then exit 0; fi\nprintf '%s' '<testsuite name=\"suite\"><testcase classname=\"suite\" name=\"case\"/></testsuite>' > result.xml\ntouch marker\n"
	if e = os.WriteFile(filepath.Join(root, "fixture.sh"), []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	command := catalog.Command{Cwd: ".", Argv: []string{"/bin/sh", "fixture.sh"}, TimeoutSeconds: 10}
	check := catalog.Check{Reports: []string{"result.xml"}, Assertions: []string{"suite/case"}, Census: []string{"suite/case"}}
	unit := catalog.Unit{ID: "sample", TimeoutSeconds: 10, Checks: map[string]catalog.Check{"unit": check}}
	invocation := planner.Invocation{ID: "sample-unit@" + platform, Unit: "sample", Check: "unit", Platform: platform, Variant: "first", Commands: []catalog.Command{command}, Tests: []string{"suite/case"}}
	second := invocation
	second.Variant = "second"
	if got := runPack(context.Background(), root, private, platform, []catalog.Unit{unit}, map[string]string{"sample": root}, []planner.Invocation{invocation}, false); got != 0 {
		t.Fatalf("positive capture: got exit %d", got)
	}
	for _, name := range []string{"marker", "result.xml"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	private, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got := runPack(context.Background(), root, private, platform, []catalog.Unit{unit}, map[string]string{"sample": root}, []planner.Invocation{invocation, second}, false)
	if got != 6 {
		t.Fatalf("missing fresh report: got exit %d", got)
	}
}

func TestRunnerCompletenessAcrossRequiredInvocations(t *testing.T) {
	for _, tc := range []struct {
		name, xml  string
		exit, want int
	}{
		{"nonempty", `<testsuite><testcase name="renamed"/></testsuite>`, 0, 0},
		{"zero", `<testsuite tests="99"/>`, 0, 6},
		{"skipped-suite", `<testsuite><testcase name="skipped"><skipped/></testcase></testsuite>`, 0, 6},
		{"failed-report", `<testsuite><testcase name="failed"><failure/></testcase></testsuite>`, 0, 6},
		{"original-failing-exit", `<testsuite><testcase name="passed"/></testsuite>`, 23, 23},
		{"unsupported-summary", `{"tests":2,"passed":2}`, 0, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			check := catalog.Check{Reports: []string{"report.xml"}}
			unit := catalog.Unit{ID: "sample", TimeoutSeconds: 10, Checks: map[string]catalog.Check{"unit": check}}
			inv := planner.Invocation{ID: "sample-unit@macos-arm64", Unit: "sample", Check: "unit", Platform: "macos-arm64", Commands: []catalog.Command{{Cwd: ".", Argv: []string{"fixture"}}}}
			run := func(context.Context, catalog.Command, string) int {
				if os.WriteFile(filepath.Join(root, "report.xml"), []byte(tc.xml), 0600) != nil {
					t.Fatal("report")
				}
				return tc.exit
			}
			before := func(context.Context, string) error { return nil }
			if code := runChecks(context.Background(), root, t.TempDir(), "macos-arm64", []catalog.Unit{unit}, map[string]string{"sample": root}, []planner.Invocation{inv}, true, run, before); code != tc.want {
				t.Fatalf("got %d want %d", code, tc.want)
			}
		})
	}
}
func TestEmptyPackNeverCompletes(t *testing.T) {
	if code := runChecks(context.Background(), t.TempDir(), t.TempDir(), "macos-arm64", nil, nil, nil, true, nil, nil); code != 6 {
		t.Fatal("empty pack passed")
	}
}
