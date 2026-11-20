package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"github.com/keeldock-platform/ci-kit/schemas"
	"strings"
	"testing"
)

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := Execute(context.Background(), args, &out, &err)
	return code, out.String(), err.String()
}
func TestJSONUsageSafeAndSingle(t *testing.T) {
	tests := [][]string{{"--json"}, {"--json", "missing-private-input"}, {"--json", "version", "--unknown=private-marker"}, {"version", "--json", "--json"}, {"--json", "--help"}, {"--json", "completion", "bash"}, {"--json", "--http-timeout=0s", "version"}, {"--json", "--color=invalid", "version"}}
	for _, args := range tests {
		code, out, _ := invoke(t, args...)
		if code != 2 || strings.Contains(out, "private-marker") || strings.Contains(out, "missing-private-input") || strings.Count(out, "\n") != 1 {
			t.Fatalf("unsafe or malformed result: code%d %q", code, out)
		}
		var r protocol.Result
		if json.Unmarshal([]byte(out), &r) != nil || r.Command != "invalid" {
			t.Fatal(out)
		}
	}
}
func TestVersionSchemaAndQualification(t *testing.T) {
	code, out, err := invoke(t, "version", "--json")
	if code != 0 || err != "" {
		t.Fatalf("%d %s", code, err)
	}
	var r protocol.Result
	if json.Unmarshal([]byte(out), &r) != nil {
		t.Fatal(out)
	}
	if r.AdmissionEligible || r.Local == nil {
		t.Fatal("local result admitted")
	}
	b, _ := json.Marshal(r.Local.Data)
	var v any
	json.Unmarshal(b, &v)
	if e := schemas.Validate("urn:keeldock:kd-ci:local-data:1#/$defs/version", v); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), `"production_qualified":true`) {
		t.Fatal("unqualified binary claimed release")
	}
}
func TestRemoteOfflineRefusesBeforeAuthentication(t *testing.T) {
	t.Setenv("GH_PATH", "private-marker")
	for _, name := range []string{"dispatch", "status", "wait", "failures", "cancel"} {
		args := []string{name}
		if name != "dispatch" {
			args = append(args, "1")
		}
		args = append(args, "--offline", "--json")
		code, out, _ := invoke(t, args...)
		if code != 3 || !strings.Contains(out, `"code":"offline_refused"`) || strings.Contains(out, "private-marker") {
			t.Fatalf("%s: %d %s", name, code, out)
		}
	}
}
func TestHelpAndEveryCompletionAreText(t *testing.T) {
	code, out, _ := invoke(t, "--help")
	if code != 0 || !strings.Contains(out, "kd-ci") {
		t.Fatal("help")
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		code, out, _ := invoke(t, "completion", shell)
		if code != 0 || len(out) < 30 {
			t.Fatalf("%s completion", shell)
		}
	}
}
func TestNoUnexpectedPublicCommands(t *testing.T) {
	for _, name := range []string{"auth", "setup", "release", "promote", "rerun", "update", "exec", "secret"} {
		code, _, _ := invoke(t, "--json", name)
		if code != 2 {
			t.Fatalf("unexpected command%s", name)
		}
	}
}
func TestScalarDuplicationAndRepeatableSelectors(t *testing.T) {
	if !duplicateScalars([]string{"--base=a", "--base=b"}) {
		t.Fatal("duplicate scalar")
	}
	if duplicateScalars([]string{"--unit=a", "--unit=a", "--platform=x", "--platform=y", "--variant=a=b", "--variant=c=d"}) {
		t.Fatal("repeatable selector refused")
	}
}
func TestPlannerCancellationUsesFixedExitCategories(t *testing.T) {
	for _, x := range []struct {
		err  error
		exit int
	}{{context.Canceled, 7}, {context.DeadlineExceeded, 4}} {
		e := selectionError(x.err)
		var f *gh.Failure
		if !errors.As(e, &f) || f.Exit != x.exit {
			t.Fatal("planner context mapped incorrectly")
		}
	}
}
func TestJSONBooleanAndTextFlagAssignments(t *testing.T) {
	code, out, _ := invoke(t, "version", "--json", "--help=true")
	if code != 2 || !strings.HasPrefix(out, "{") {
		t.Fatal("help assignment escaped JSON envelope")
	}
	for _, value := range []string{"false", "FALSE", "0"} {
		code, out, _ = invoke(t, "version", "--json="+value, "--help=true")
		if code != 0 || strings.HasPrefix(out, "{") {
			t.Fatal("false JSON option refused text help")
		}
	}
	code, out, _ = invoke(t, "version", "--json", "--config", "completion")
	if code != 0 || !strings.HasPrefix(out, "{") {
		t.Fatal("flag value mistaken for completion command")
	}
	if textRequested([]string{"run", "help"}) {
		t.Fatal("unit alias mistaken for help command")
	}
}
func TestDefaultCargoRefusesInstallationAndUnlockedResolution(t *testing.T) {
	for _, argv := range [][]string{{"cargo", "install", "tool"}, {"cargo", "update", "--locked"}, {"cargo", "publish", "--locked"}, {"cargo", "fetch", "--locked"}, {"cargo", "test"}, {"cargo", "check", "--", "--locked"}, {"cargo", "nextest", "self", "update", "--locked"}} {
		if localCargoPolicy(argv) == nil {
			t.Fatal("unsafe default Cargo command accepted")
		}
	}
	for _, argv := range [][]string{{"cargo", "check", "--locked"}, {"cargo", "clippy", "--locked", "--", "-D", "warnings"}, {"cargo", "nextest", "run", "--locked"}, {"cargo", "fmt", "--", "--check"}} {
		if localCargoPolicy(argv) != nil {
			t.Fatal("provisioned read-only Cargo command refused")
		}
	}
	argv := cargoFlagBeforeSeparator([]string{"cargo", "test", "--locked", "--", "filter"}, "--offline")
	if strings.Join(argv, " ") != "cargo test --locked --offline -- filter" {
		t.Fatal("offline option became test argument")
	}
}

func TestFirstExecutedFailureAndCleanupHaveDistinctOutcomes(t *testing.T) {
	cases := []struct {
		result process.Result
		exit   int
	}{{process.Result{ExitObserved: true, ExitCode: 9, Interrupted: true, CleanupComplete: false}, 1}, {process.Result{ExitCode: 0, CleanupComplete: false}, 1}, {process.Result{Interrupted: true, ExitCode: 1}, 7}, {process.Result{TimedOut: true, ExitCode: 1}, 4}, {process.Result{Overflow: true, ExitCode: 1}, 6}}
	for _, c := range cases {
		e := localProcessError(c.result, process.ErrCapture)
		var failure *gh.Failure
		if !errors.As(e, &failure) || failure.Exit != c.exit {
			t.Fatal("first exit or cleanup outcome lost")
		}
	}
}
func TestScalarPreflightRespectsValuesAndShorthand(t *testing.T) {
	if duplicateScalars([]string{"--config", "--config"}) {
		t.Fatal("a value was counted as a flag")
	}
	if !duplicateScalars([]string{"-h", "--help"}) || !duplicateScalars([]string{"-hh"}) {
		t.Fatal("help shorthand duplicate accepted")
	}
	if jsonRequested([]string{"--config", "--json"}) || jsonRequested([]string{"--", "--json"}) {
		t.Fatal("literal value mistaken for JSON option")
	}
	code, out, _ := invoke(t, "--json", "__complete", "version", "")
	if code != 2 || !strings.HasPrefix(out, "{") {
		t.Fatal("internal completion escaped JSON envelope")
	}
}
