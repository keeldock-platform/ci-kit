package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailuresNamesAndErrorsStayInPrivateFile(t *testing.T) {
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_RUN_ID", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		t.Setenv(key, "")
	}
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory")
	}
	report := filepath.Join(dir, "report.xml")
	output := filepath.Join(dir, "failures.json")
	data := `<testsuite><testcase classname="private-suite" name="private-name"><failure message="private-error">private-stack</failure></testcase><testcase name="other"><error message="private-second-error"/></testcase></testsuite>`
	if os.WriteFile(report, []byte(data), 0600) != nil {
		t.Fatal("report")
	}
	code, out, err := invoke(t, "failures", "--private-report", report, "--private-output", output, "--json")
	if code != 0 {
		t.Fatalf("private failures refused: %d %s %s", code, out, err)
	}
	b, e := os.ReadFile(output)
	if e != nil || !strings.Contains(string(b), "private-suite/private-name") || !strings.Contains(string(b), "private-error") || !strings.Contains(string(b), "private-second-error") || !process.UserOnly(output, false) {
		t.Fatal("private names/errors unavailable")
	}
	if strings.Contains(out+err, "private-name") || strings.Contains(out+err, "private-error") {
		t.Fatal("private diagnostic reached stdout/stderr")
	}
	code, out, err = invoke(t, "failures", "--private-report", report, "--private-output", filepath.Join(dir, "human.json"))
	if code != 0 || !strings.Contains(out, "2 failures written privately") || strings.Contains(out+err, "private-name") {
		t.Fatal("safe human failure count missing")
	}
	code, _, _ = invoke(t, "failures", "--private-report", report, "--private-output", output, "--json")
	if code == 0 {
		t.Fatal("existing output overwritten")
	}
}
func TestFailuresRefusePublicExecutionAndUnsafeFiles(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("private directory")
	}
	report := filepath.Join(dir, "report.xml")
	os.WriteFile(report, []byte(`<testsuite><testcase name="private-name"><failure message="private-error"/></testcase></testsuite>`), 0600)
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_RUN_ID", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "true")
			output := filepath.Join(dir, "blocked-"+key)
			code, out, err := invoke(t, "failures", "--private-report", report, "--private-output", output, "--json")
			if code != 3 || strings.Contains(out+err, "private-name") {
				t.Fatal("public execution accepted")
			}
			if _, e := os.Stat(output); !os.IsNotExist(e) {
				t.Fatal("private file written in public execution")
			}
		})
	}
	output := filepath.Join(dir, "output.json")
	link := filepath.Join(dir, "link.xml")
	if os.Symlink(report, link) == nil {
		code, _, _ := invoke(t, "failures", "--private-report", link, "--private-output", output, "--json")
		if code == 0 {
			t.Fatal("report link accepted")
		}
	}
}
