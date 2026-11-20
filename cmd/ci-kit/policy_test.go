package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/keeldock-platform/ci-kit/internal/protocol"
)

func validEnvironment(t *testing.T) map[string]string {
	t.Helper()
	sha := "1111111111111111111111111111111111111111"
	dispatch, e := protocol.NewDispatch("sample", sha, sha, "full", "feedback", "all", "all", "00000000-0000-4000-8000-000000000001")
	if e != nil {
		t.Fatal(e)
	}
	request, _ := json.Marshal(dispatch.Inputs)
	policy := admissionPolicy{SchemaVersion: 1, CallerRepository: "example/public-ci", CallerRepositoryID: 1, ControlSHA: sha, KitSHA: sha, Actors: []string{"example-actor"}, Concerns: map[string]concernPolicy{"sample": {Environment: "source-read-sample", SourceMapSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}, AcquisitionReviewed: true}
	raw, _ := json.Marshal(policy)
	return map[string]string{"CI_REQUEST": string(request), "CI_ADMISSION_POLICY": string(raw), "CI_REPOSITORY": "example/public-ci", "CI_REPOSITORY_ID": "1", "CI_WORKFLOW_SHA": sha, "CI_KIT_SHA": sha, "CI_WORKFLOW_REF": "example/public-ci/.github/workflows/source-ci.yml@refs/heads/main", "CI_EVENT_NAME": "workflow_dispatch", "CI_REF": "refs/heads/main", "CI_REF_TYPE": "branch", "CI_FORK": "false", "CI_ACTOR": "example-actor", "CI_TRIGGERING_ACTOR": "example-actor", "CI_KIT_OPERATION": "preflight"}
}
func TestPreflightRefusesBeforeEnvironment(t *testing.T) {
	baseline := validEnvironment(t)
	if _, e := readControl(func(k string) string { return baseline[k] }); e != nil {
		t.Fatal(e)
	}
	for _, change := range []struct{ key, value string }{{"CI_REF", "refs/tags/main"}, {"CI_REF_TYPE", "tag"}, {"CI_FORK", "true"}, {"CI_ACTOR", "other"}, {"CI_TRIGGERING_ACTOR", "other"}, {"RUNNER_DEBUG", "1"}, {"CI_EVENT_NAME", "push"}, {"CI_KIT_SHA", "2222222222222222222222222222222222222222"}, {"CI_REPOSITORY_ID", "2"}} {
		t.Run(change.key, func(t *testing.T) {
			env := validEnvironment(t)
			env[change.key] = change.value
			if _, e := readControl(func(k string) string { return env[k] }); e == nil {
				t.Fatal("policy admitted invalid request")
			}
		})
	}
	env := validEnvironment(t)
	var policy admissionPolicy
	json.Unmarshal([]byte(env["CI_ADMISSION_POLICY"]), &policy)
	policy.AcquisitionReviewed = false
	raw, _ := json.Marshal(policy)
	env["CI_ADMISSION_POLICY"] = string(raw)
	if _, e := readControl(func(k string) string { return env[k] }); e == nil {
		t.Fatal("unreviewed acquisition admitted")
	}
}
func TestReportPathsRefuseEscapesAndLinks(t *testing.T) {
	root := t.TempDir()
	if _, e := reportPath(root, "../result.xml"); e == nil {
		t.Fatal("traversal admitted")
	}
	target := filepath.Join(root, "outside")
	if e := os.Mkdir(target, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, filepath.Join(root, "link")); e == nil {
		if _, e := reportPath(root, "link/result.xml"); e == nil {
			t.Fatal("symlink admitted")
		}
	}
}

func distinctSlotEnvironments() map[string]string {
	values := map[string]string{"plan": "source-read-canary-plan"}
	for _, platform := range protocol.Platforms {
		values[platform] = "source-read-canary-" + platform
	}
	return values
}
func environmentPolicyFixture(t *testing.T, concern, entry string, values map[string]string, explicit bool) map[string]string {
	t.Helper()
	env := validEnvironment(t)
	var inputs map[string]string
	if json.Unmarshal([]byte(env["CI_REQUEST"]), &inputs) != nil {
		t.Fatal("fixture request")
	}
	request, e := protocol.NewDispatch(concern, inputs["source_sha"], inputs["base_sha"], inputs["mode"], inputs["purpose"], inputs["platforms"], inputs["selector"], inputs["request_id"])
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(request.Inputs)
	env["CI_REQUEST"] = string(raw)
	var policy admissionPolicy
	if json.Unmarshal([]byte(env["CI_ADMISSION_POLICY"]), &policy) != nil {
		t.Fatal("fixture policy")
	}
	value := policy.Concerns["sample"]
	value.Environments = values
	policy.Concerns = map[string]concernPolicy{concern: value}
	raw, _ = json.Marshal(policy)
	// Preserve explicit empty/null maps rather than letting omitempty erase them.
	if explicit {
		var object map[string]any
		json.Unmarshal(raw, &object)
		object["concerns"].(map[string]any)[concern].(map[string]any)["environments"] = values
		raw, _ = json.Marshal(object)
	}
	env["CI_ADMISSION_POLICY"] = string(raw)
	env["CI_WORKFLOW_REF"] = "example/public-ci/.github/workflows/" + entry + "@refs/heads/main"
	return env
}
func TestCanaryEntryRequiresFixedConcernAndDistinctCompleteEnvironments(t *testing.T) {
	cases := []struct {
		name, concern, entry string
		values               func() map[string]string
		explicit, pass       bool
	}{
		{"canary", canaryConcern, "canary-ci.yml", distinctSlotEnvironments, true, true},
		{"wrong-concern", "sample", "canary-ci.yml", distinctSlotEnvironments, true, false},
		{"canary-fallback", canaryConcern, "canary-ci.yml", func() map[string]string { return nil }, false, false},
		{"canary-concern-via-source-entry-still-needs-map", canaryConcern, "source-ci.yml", func() map[string]string { return nil }, false, false},
		{"missing-platform", canaryConcern, "canary-ci.yml", func() map[string]string { m := distinctSlotEnvironments(); delete(m, "windows-arm64"); return m }, true, false},
		{"duplicate-canary-environment", canaryConcern, "canary-ci.yml", func() map[string]string { m := distinctSlotEnvironments(); m["macos-arm64"] = m["plan"]; return m }, true, false},
		{"duplicate-production-map", "sample", "source-ci.yml", func() map[string]string { m := distinctSlotEnvironments(); m["linux-arch"] = m["plan"]; return m }, true, false},
		{"production-fallback", "sample", "source-ci.yml", func() map[string]string { return nil }, false, true},
		{"production-injective-map", "sample", "source-ci.yml", distinctSlotEnvironments, true, true},
		{"explicit-empty-map", "sample", "source-ci.yml", func() map[string]string { return map[string]string{} }, true, false},
		{"explicit-null-map", "sample", "source-ci.yml", func() map[string]string { return nil }, true, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			env := environmentPolicyFixture(t, test.concern, test.entry, test.values(), test.explicit)
			_, e := readControl(func(k string) string { return env[k] })
			if (e == nil) != test.pass {
				t.Fatal("environment admission did not match the required policy")
			}
		})
	}
}

func TestReportPathUsesPortableCatalogueSeparators(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested", "reports"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := reportPath(root, "nested/reports/unit.xml"); err != nil || got != filepath.Join(root, "nested", "reports", "unit.xml") {
		t.Fatal("portable report path refused")
	}
	for _, bad := range []string{"nested/../reports/unit.xml", `nested\reports\unit.xml`, "/reports/unit.xml"} {
		if _, err := reportPath(root, bad); err == nil {
			t.Fatal("noncanonical path admitted")
		}
	}
}
