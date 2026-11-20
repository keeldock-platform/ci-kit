package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"gopkg.in/yaml.v3"
)

func TestProductionRefusesExperimentAndUnadmittedAcquisition(t *testing.T) {
	for _, platform := range []string{"ios-simulator", "android-emulator"} {
		t.Run(platform, func(t *testing.T) {
			env := validEnvironment(t)
			var fields map[string]string
			if json.Unmarshal([]byte(env["CI_REQUEST"]), &fields) != nil {
				t.Fatal("fixture")
			}
			d, e := protocol.NewDispatch("sample", fields["source_sha"], fields["base_sha"], "full", "feedback", platform, "all", fields["request_id"])
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(d.Inputs)
			env["CI_REQUEST"] = string(raw)
			if _, e := readControl(func(k string) string { return env[k] }); e == nil {
				t.Fatal("unadmitted request received authority")
			}
			env = validEnvironment(t)
			env["CI_KIT_OPERATION"] = "pack"
			env["CI_PLATFORM"] = platform
			if _, e := readControl(func(k string) string { return env[k] }); e == nil {
				t.Fatal("direct unadmitted pack received authority")
			}
		})
	}
	env := validEnvironment(t)
	env["CI_ROUTE"] = "experiment"
	env["CI_EXPERIMENT_POLICY"] = env["CI_ADMISSION_POLICY"]
	if _, e := readControl(func(k string) string { return env[k] }); e == nil {
		t.Fatal("experiment route accepted")
	}
	env = validEnvironment(t)
	env["CI_REF"] = "refs/heads/experiment/ci-probe"
	if _, e := readControl(func(k string) string { return env[k] }); e == nil {
		t.Fatal("experiment branch accepted")
	}
}

func TestParkedWorkflowsHaveNoSourceAuthorityOrAdmissionDependency(t *testing.T) {
	root := filepath.Join("..", "..", ".github", "workflows")
	read := func(name string) map[string]any {
		t.Helper()
		raw, e := os.ReadFile(filepath.Join(root, name))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if yaml.Unmarshal(raw, &v) != nil {
			t.Fatal("workflow parse")
		}
		return v
	}
	validate := read("validate.yml")
	jobs := validate["jobs"].(map[string]any)
	for _, p := range []string{"ios-simulator", "android-emulator"} {
		job := jobs["pack-"+p].(map[string]any)
		for _, key := range []string{"uses", "secrets", "environment", "env", "with"} {
			if _, ok := job[key]; ok {
				t.Fatal("parked job contains execution authority", key)
			}
		}
		if job["if"] != "github.repository == ''" {
			t.Fatal("parked job can run")
		}
		needs := jobs["collect"].(map[string]any)["needs"].([]any)
		for _, need := range needs {
			if need == "pack-"+p {
				t.Fatal("parked platform gates admission")
			}
		}
		direct := read("pack-" + p + ".yml")
		call := direct["on"].(map[string]any)["workflow_call"]
		if call != nil {
			t.Fatal("parked reusable accepts inputs or secrets")
		}
	}
	if _, e := os.Stat(filepath.Join(root, "experiment-matrix.yml")); !os.IsNotExist(e) {
		t.Fatal("experiment entry remains")
	}
}

func TestProductionPlannerRequiresExactlyAdmittedPlatforms(t *testing.T) {
	// Static type checks need no executable or reports: this exercises actual
	// admission expansion, including the native scope, without executing source.
	platforms := append([]string{}, protocol.Platforms...)
	c := catalog.Check{Platforms: platforms, Commands: map[string][]catalog.Command{"default": {{Cwd: ".", Argv: []string{"public-type-check"}}}}}
	u := catalog.Unit{Schema: 1, Owner: "test-maintainers", Root: ".", Paths: []string{"sample/**"}, TimeoutSeconds: 300, Resources: catalog.Resources{Weight: 1}, Cache: catalog.Cache{Policy: "none"}, Outputs: catalog.Outputs{Visibility: "private-runner-only"}, Pack: "workspace", CostClass: "normal", ID: "sample", Platforms: platforms, RequiredIn: []string{"full"}, Checks: map[string]catalog.Check{"types": c}}
	plan, e := planner.Plan(t.TempDir(), []catalog.Unit{u}, planner.Options{Context: context.Background(), Mode: "full", Admission: true, AdmittedOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, pack := range plan.Packs {
		seen[pack.Platform] = true
		if !protocol.AdmittedPlatform(pack.Platform) {
			t.Fatal("parked platform selected")
		}
	}
	if len(seen) != 9 {
		t.Fatal("admitted platform omitted")
	}
	for _, p := range protocol.AdmittedPlatforms {
		if !seen[p] {
			t.Fatal("admitted platform missing")
		}
	}
	// Required prerequisites may not silently disappear across the split.
	c.Needs = []string{"sample-types@windows-x64"}
	c.Platforms = []string{"linux-debian"}
	u.Checks["types"] = c
	// This invalid self/cross-platform prerequisite must remain non-admitting.
	if _, e := planner.Plan(t.TempDir(), []catalog.Unit{u}, planner.Options{Mode: "full", Admission: true, AdmittedOnly: true}); e == nil {
		t.Fatal("unavailable prerequisite admitted")
	}
}

func TestWindowsProductionRetainsControlGates(t *testing.T) {
	for _, platform := range []string{"windows-x64", "windows-arm64"} {
		t.Run(platform, func(t *testing.T) {
			env := validEnvironment(t)
			var fields map[string]string
			if json.Unmarshal([]byte(env["CI_REQUEST"]), &fields) != nil {
				t.Fatal("fixture")
			}
			d, e := protocol.NewDispatch("sample", fields["source_sha"], fields["base_sha"], "full", "feedback", platform, "all", fields["request_id"])
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(d.Inputs)
			env["CI_REQUEST"] = string(raw)
			env["CI_KIT_OPERATION"] = "pack"
			env["CI_PLATFORM"] = platform
			if _, e := readControl(func(k string) string { return env[k] }); e != nil {
				t.Fatal("native platform refused", e)
			}
			for _, change := range []struct{ key, value string }{{"CI_REF", "refs/heads/experiment/ci-probe"}, {"CI_ROUTE", "experiment"}, {"CI_WORKFLOW_SHA", "dddddddddddddddddddddddddddddddddddddddd"}, {"RUNNER_DEBUG", "1"}} {
				prior := env[change.key]
				env[change.key] = change.value
				if _, e := readControl(func(k string) string { return env[k] }); e == nil {
					t.Fatal("Windows bypassed control gate", change.key)
				}
				env[change.key] = prior
			}
		})
	}
}
