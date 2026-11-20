package main

import (
	"context"
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"testing"
)

func experimentEnvironment(t *testing.T) map[string]string {
	e := environmentPolicyFixture(t, "canary", "canary-ci.yml", distinctSlotEnvironments(), true)
	e["CI_ROUTE"] = "experiment"
	e["CI_REF"] = "refs/heads/experiment/ci-probe"
	e["CI_WORKFLOW_REF"] = "example/public-ci/.github/workflows/experiment-ci.yml@" + e["CI_REF"]
	e["CI_EXPERIMENT_POLICY"] = e["CI_ADMISSION_POLICY"]
	delete(e, "CI_ADMISSION_POLICY")
	return e
}
func TestExperimentRouteIsSeparateAndSyntheticOnly(t *testing.T) {
	e := experimentEnvironment(t)
	if _, err := readControl(func(k string) string { return e[k] }); err != nil {
		t.Fatal("valid experiment refused")
	}
	for _, change := range []struct{ k, v string }{{"CI_ROUTE", ""}, {"CI_REF", "refs/heads/main"}, {"CI_REF", "refs/tags/experiment/ci-probe"}, {"CI_ACTOR", "other"}, {"CI_FORK", "true"}, {"RUNNER_DEBUG", "1"}, {"CI_EVENT_NAME", "pull_request"}, {"CI_WORKFLOW_REF", "example/public-ci/.github/workflows/source-ci.yml@refs/heads/experiment/ci-probe"}} {
		t.Run(change.k+change.v, func(t *testing.T) {
			e := experimentEnvironment(t)
			e[change.k] = change.v
			if _, err := readControl(func(k string) string { return e[k] }); err == nil {
				t.Fatal("bad route admitted")
			}
		})
	}
	e = experimentEnvironment(t)
	d, err := protocol.NewDispatch("sample", "1111111111111111111111111111111111111111", "1111111111111111111111111111111111111111", "full", "feedback", "all", "all", "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(d.Inputs)
	e["CI_REQUEST"] = string(b)
	if _, err = readControl(func(k string) string { return e[k] }); err == nil {
		t.Fatal("production concern admitted by experiment")
	}
}
func TestDiagnosticPreservesFirstFailureAndBlocksDependents(t *testing.T) {
	root := t.TempDir()
	names := []string{"first", "dependent", "independent"}
	units := []catalog.Unit{}
	invs := []planner.Invocation{}
	origins := map[string]string{}
	for _, name := range names {
		units = append(units, catalog.Unit{ID: name, TimeoutSeconds: 5, Checks: map[string]catalog.Check{"format": {}}})
		invs = append(invs, planner.Invocation{ID: name + "-format@macos-arm64", Unit: name, Check: "format", Commands: []catalog.Command{{Argv: []string{name}}}})
		origins[name] = root
	}
	units[1].DependsOn = []string{"first"}
	calls := []string{}
	stages := []diagnosticStage{}
	ctx := context.WithValue(context.Background(), diagnosticKey{}, func(s diagnosticStage) { stages = append(stages, s) })
	code := runChecks(ctx, root, t.TempDir(), "macos-arm64", units, origins, invs, false, func(_ context.Context, c catalog.Command, _ string) int {
		calls = append(calls, c.Argv[0])
		if c.Argv[0] == "first" {
			return 1
		}
		return 0
	}, func(context.Context, string) error { return nil })
	if code != 1 || len(calls) != 2 || calls[1] != "independent" || len(stages) != 3 || stages[0].Code != 1 || !stages[1].Blocked || stages[2].Code != 0 {
		t.Fatalf("diagnostic lost outcome: code=%d calls=%v stages=%v", code, calls, stages)
	}
	calls = nil
	code = runChecks(ctx, root, t.TempDir(), "macos-arm64", units, origins, invs, false, func(_ context.Context, c catalog.Command, _ string) int { calls = append(calls, c.Argv[0]); return 3 }, func(context.Context, string) error { return nil })
	if code != 3 || len(calls) != 1 {
		t.Fatal("security prerequisite failure continued")
	}
}

func TestAndroidDiagnosticHandoffCannotEnableAdmissionContinuation(t *testing.T) {
	for _, pair := range []struct{ diagnostic, admission, want bool }{{true, false, true}, {false, false, false}, {true, true, false}, {false, true, false}} {
		raw, err := json.Marshal(androidState{Diagnostic: pair.diagnostic, Admission: pair.admission})
		if err != nil {
			t.Fatal(err)
		}
		var state androidState
		if json.Unmarshal(raw, &state) != nil {
			t.Fatal("state round trip")
		}
		ctx := state.executionContext(context.Background())
		_, enabled := ctx.Value(diagnosticKey{}).(func(diagnosticStage))
		if enabled != pair.want {
			t.Fatal("diagnostic scope changed across native hook boundary")
		}
	}
}

func TestSharedSyntheticEnvironmentStaysExperimentOnly(t *testing.T) {
	e := experimentEnvironment(t)
	var p admissionPolicy
	if json.Unmarshal([]byte(e["CI_EXPERIMENT_POLICY"]), &p) != nil {
		t.Fatal("fixture")
	}
	p.CallerRepository = "keeldock-platform/platform-ci"
	p.CallerRepositoryID = 1430960557
	for slot := range p.Concerns["canary"].Environments {
		p.Concerns["canary"].Environments[slot] = "ci-experiment"
	}
	raw, _ := json.Marshal(p)
	e["CI_EXPERIMENT_POLICY"] = string(raw)
	e["CI_REPOSITORY"] = p.CallerRepository
	e["CI_REPOSITORY_ID"] = "1430960557"
	e["CI_WORKFLOW_REF"] = p.CallerRepository + "/.github/workflows/devlane-synthetic.yml@" + e["CI_REF"]
	get := func(k string) string { return e[k] }
	if _, err := readControl(get); err != nil {
		t.Fatal("synthetic allowance refused")
	}
	e["CI_ROUTE"] = ""
	e["CI_ADMISSION_POLICY"] = string(raw)
	e["CI_REF"] = "refs/heads/main"
	e["CI_WORKFLOW_REF"] = p.CallerRepository + "/.github/workflows/canary-ci.yml@refs/heads/main"
	if _, err := readControl(get); err == nil {
		t.Fatal("production accepted shared environment")
	}
}

func TestAndroidCompletedSlotsRetainVariantAndShardIdentity(t *testing.T) {
	selected := []planner.Invocation{{ID: "native-unit@android-emulator", Variant: "api35", Shard: "1/2"}, {ID: "native-unit@android-emulator", Variant: "api36", Shard: "1/2"}, {ID: "native-unit@android-emulator", Variant: "api35", Shard: "2/2"}}
	rows := []diagnosticStage{}
	ctx := context.WithValue(context.Background(), diagnosticKey{}, func(s diagnosticStage) { rows = append(rows, s) })
	if !androidCompletedSlots(ctx, selected, selected[:1]) || len(rows) != 1 || rows[0].Slot != 0 {
		t.Fatal("unexecuted API or shard reported")
	}
	rows = nil
	unknown := planner.Invocation{ID: selected[0].ID, Variant: "api37", Shard: "1/2"}
	if androidCompletedSlots(ctx, selected, []planner.Invocation{unknown}) || len(rows) != 0 {
		t.Fatal("unknown invocation reported")
	}
	if androidCompletedSlots(ctx, selected, []planner.Invocation{selected[0], selected[0]}) || len(rows) != 0 {
		t.Fatal("duplicate invocation reported")
	}
}
