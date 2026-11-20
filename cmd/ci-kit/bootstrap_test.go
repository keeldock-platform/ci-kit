package main

import (
	"context"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceFreeCandidateBootstrapDoesNotGrantSourceAuthority(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Fatal("required bootstrap interpreter missing")
	}
	raw, e := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "kit-bootstrap", "action.yml"))
	if e != nil {
		t.Fatal(e)
	}
	start := "        python3 -I - <<'PYTHON'\n"
	parts := strings.SplitN(string(raw), start, 2)
	if len(parts) != 2 {
		t.Fatal("bootstrap authority script missing")
	}
	scriptParts := strings.SplitN(parts[1], "        PYTHON\n", 2)
	if len(scriptParts) != 2 {
		t.Fatal("bootstrap authority script not bounded")
	}
	lines := strings.Split(scriptParts[0], "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "        ")
	}
	script := strings.Join(lines, "\n")
	sha := strings.Repeat("1", 40)
	base := map[string]string{"CI_BOOTSTRAP_PRIVATE": "false", "CI_BOOTSTRAP_SHA": sha, "CI_BOOTSTRAP_POLICY_ONLY": "true", "CI_BOOTSTRAP_WORKFLOW_SHA": sha, "CI_BOOTSTRAP_WORKFLOW_REF": "keeldock-platform/ci-kit/.github/workflows/ci-policy.yml@refs/heads/ci/candidate", "CI_BOOTSTRAP_REPOSITORY": "keeldock-platform/ci-kit", "CI_BOOTSTRAP_REPOSITORY_ID": "1430960557", "CI_BOOTSTRAP_EVENT": "workflow_dispatch", "CI_BOOTSTRAP_REF": "refs/heads/ci/candidate", "CI_BOOTSTRAP_REF_TYPE": "branch", "CI_BOOTSTRAP_FORK": "false"}
	cases := []struct {
		name   string
		change map[string]string
		want   int
	}{
		{"candidate-public-policy", nil, 0},
		{"source-bearing-candidate", map[string]string{"CI_BOOTSTRAP_POLICY_ONLY": "false"}, 3},
		{"tag", map[string]string{"CI_BOOTSTRAP_REF": "refs/tags/main", "CI_BOOTSTRAP_REF_TYPE": "tag"}, 3},
		{"invalid-kit-sha", map[string]string{"CI_BOOTSTRAP_SHA": "bad"}, 3},
		{"wrong-public-repository-id", map[string]string{"CI_BOOTSTRAP_REPOSITORY_ID": "0"}, 3},
		{"fork", map[string]string{"CI_BOOTSTRAP_FORK": "true"}, 3},
		{"debug", map[string]string{"RUNNER_DEBUG": "1"}, 3},
		{"wrong-workflow", map[string]string{"CI_BOOTSTRAP_WORKFLOW_REF": "keeldock-platform/ci-kit/.github/workflows/source-ci.yml@refs/heads/ci/candidate"}, 3},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			env := []string{}
			values := map[string]string{}
			for k, v := range base {
				values[k] = v
			}
			for k, v := range test.change {
				values[k] = v
			}
			for k, v := range values {
				env = append(env, k+"="+v)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, python, "-I", "-")
			ordinary, e := process.Environment(nil)
			if e != nil {
				t.Fatal(e)
			}
			cmd.Env = append(ordinary, env...)
			cmd.Stdin = strings.NewReader(script)
			out, e := cmd.CombinedOutput()
			code := 0
			if e != nil {
				failure, ok := e.(*exec.ExitError)
				if !ok {
					t.Fatal("bootstrap interpreter failed")
				}
				code = failure.ExitCode()
			}
			if code != test.want {
				t.Fatalf("bootstrap classification=%d want=%d output=%s", code, test.want, out)
			}
		})
	}
}

func TestBootstrapCanaryEnvironmentIsolation(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Fatal("required bootstrap interpreter missing")
	}
	raw, e := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "kit-bootstrap", "action.yml"))
	if e != nil {
		t.Fatal(e)
	}
	parts := strings.SplitN(string(raw), "        python3 -I - <<'PYTHON'\n", 2)
	if len(parts) != 2 {
		t.Fatal("bootstrap script missing")
	}
	parts = strings.SplitN(parts[1], "        PYTHON\n", 2)
	if len(parts) != 2 {
		t.Fatal("bootstrap script boundary missing")
	}
	lines := strings.Split(parts[0], "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "        ")
	}
	script := strings.Join(lines, "\n")
	cases := []struct {
		name, concern, entry string
		slots                map[string]string
		explicit             bool
		want                 int
	}{
		{"canary-map", canaryConcern, "canary-ci.yml", distinctSlotEnvironments(), true, 0},
		{"canary-fallback", canaryConcern, "canary-ci.yml", nil, false, 3},
		{"wrong-concern", "sample", "canary-ci.yml", distinctSlotEnvironments(), true, 3},
		{"production-fallback", "sample", "source-ci.yml", nil, false, 0},
	}
	duplicate := distinctSlotEnvironments()
	duplicate["linux-arch"] = duplicate["plan"]
	cases = append(cases, struct {
		name, concern, entry string
		slots                map[string]string
		explicit             bool
		want                 int
	}{"duplicate-map", "sample", "source-ci.yml", duplicate, true, 3})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := environmentPolicyFixture(t, test.concern, test.entry, test.slots, test.explicit)

			values := map[string]string{"CI_BOOTSTRAP_POLICY_ONLY": "false", "CI_BOOTSTRAP_SHA": fixture["CI_KIT_SHA"], "CI_BOOTSTRAP_POLICY": fixture["CI_ADMISSION_POLICY"], "CI_REQUEST": fixture["CI_REQUEST"]}
			for _, field := range []string{"WORKFLOW_SHA", "WORKFLOW_REF", "REPOSITORY", "REPOSITORY_ID", "REF", "REF_TYPE", "ACTOR", "TRIGGERING_ACTOR", "FORK"} {
				values["CI_BOOTSTRAP_"+field] = fixture["CI_"+field]
			}
			values["CI_BOOTSTRAP_EVENT"] = fixture["CI_EVENT_NAME"]

			env, e := process.Environment(nil)
			if e != nil {
				t.Fatal(e)
			}
			for key, value := range values {
				env = append(env, key+"="+value)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, python, "-I", "-")
			cmd.Env = env
			cmd.Stdin = strings.NewReader(script)
			out, e := cmd.CombinedOutput()
			code := 0
			if e != nil {
				failure, ok := e.(*exec.ExitError)
				if !ok {
					t.Fatal("bootstrap interpreter failed")
				}
				code = failure.ExitCode()
			}
			if code != test.want {
				t.Fatalf("bootstrap policy code=%d want=%d output=%s", code, test.want, out)
			}
		})
	}
}
