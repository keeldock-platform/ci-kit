package cli

import (
	"filippo.io/age"
	"github.com/keeldock-platform/ci-kit/internal/diagnostics"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedFailuresStayPrivateAndRefusePublicExecution(t *testing.T) {
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_RUN_ID", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		t.Setenv(key, "")
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "private")
	if process.MakePrivateDir(dir) != nil {
		t.Fatal("dir")
	}
	sink := filepath.Join(dir, "sink")
	if process.MakePrivateDir(sink) != nil {
		t.Fatal("sink")
	}
	files := map[string]string{"report-fixture/junit.xml": `<testsuite><testcase name="private-fixture-name"><failure message="private-error-text"/></testcase></testsuite>`, "command-fixture/command.json": `{"argv":["runner","--exact-flag"],"root":"/fixture","cwd":".","result":{"ExitCode":1},"incomplete":false,"capture_errors":[]}`, "command-fixture/stderr": "private-log-tail"}
	files["command-fixture/stdout"] = ""
	malformed := filepath.Join(sink, "report-malformed")
	if process.MakePrivateDir(malformed) != nil {
		t.Fatal("malformed directory")
	}
	bad, _ := process.CreatePrivateFile(filepath.Join(malformed, "junit.xml"))
	bad.WriteString("<broken")
	bad.Close()
	for _, dirName := range []string{"command-fixture", "report-fixture"} {
		if process.MakePrivateDir(filepath.Join(sink, dirName)) != nil {
			t.Fatal("group")
		}
	}
	for name, data := range files {
		f, e := process.CreatePrivateFile(filepath.Join(sink, name))
		if e != nil {
			t.Fatal(e)
		}
		f.WriteString(data)
		f.Close()
	}
	id, _ := age.GenerateX25519Identity()
	keypath := filepath.Join(dir, "identity")
	f, _ := process.CreatePrivateFile(keypath)
	f.WriteString(id.String())
	f.Close()
	cipher := filepath.Join(dir, "recorded.age")
	identity := diagnostics.Identity{Run: "321", Attempt: "1", Platform: "ios-simulator", KitSHA: strings.Repeat("a", 40), SourceSHA: strings.Repeat("b", 40)}
	if diagnostics.Seal(sink, cipher, id.Recipient().String(), identity, 1) != nil {
		t.Fatal("seal")
	}
	outpath := filepath.Join(dir, "report.json")
	code, out, err := invoke(t, "failures", "--private-bundle", cipher, "--identity", keypath, "--private-output", outpath, "--json")
	if code != 0 {
		t.Fatalf("decrypt failed %d %s %s", code, out, err)
	}
	data, e := os.ReadFile(outpath)
	if e != nil || !process.UserOnly(outpath, false) {
		t.Fatal("private output")
	}
	for _, sentinel := range []string{"private-fixture-name", "private-error-text", "private-log-tail", "--exact-flag", "Malformed JUnit"} {
		if !strings.Contains(string(data), sentinel) || strings.Contains(out+err, sentinel) {
			t.Fatal("private diagnostic projection")
		}
	}
	for _, public := range []string{"CI", "GITHUB_ACTIONS", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
		t.Run(public, func(t *testing.T) {
			t.Setenv(public, "true")
			code, out, err := invoke(t, "failures", "--private-bundle", cipher, "--identity", keypath, "--private-output", filepath.Join(dir, public+".json"))
			if code != 3 || strings.Contains(out+err, "private-log-tail") {
				t.Fatal("public decryption accepted")
			}
		})
	}
	for _, name := range []string{"CON", "NUL.txt", "unsafe:stream"} {
		code, _, _ := invoke(t, "failures", "--private-bundle", cipher, "--identity", keypath, "--private-output", filepath.Join(dir, name))
		if code != 3 {
			t.Fatal("unsafe report filename accepted")
		}
	}
	rawCipher, _ := os.ReadFile(cipher)
	app := &app{}
	app.begin("failures", false)
	prefix := filepath.Join(dir, "transaction.json")
	visitor := func(visit func(string, []byte) error) error {
		if e := visit("ios-simulator", rawCipher); e != nil {
			return e
		}
		return gh.Fail(6, "missing_evidence")
	}
	if app.diagnosticReports(prefix, keypath, &identity, visitor) == nil {
		t.Fatal("partial evidence accepted")
	}
	if _, e := os.Lstat(filepath.Join(dir, "transaction-ios-simulator.json")); !os.IsNotExist(e) {
		t.Fatal("partial report published")
	}
	visitor = func(visit func(string, []byte) error) error { return visit("ios-simulator", rawCipher) }
	if app.diagnosticReports(prefix, keypath, &identity, visitor) != nil {
		t.Fatal("retry after partial transport failed")
	}
	if app.diagnosticReports(prefix, keypath, &identity, visitor) == nil {
		t.Fatal("existing report overwritten")
	}

	wrong, _ := age.GenerateX25519Identity()
	wrongpath := filepath.Join(dir, "wrong")
	f, _ = process.CreatePrivateFile(wrongpath)
	f.WriteString(wrong.String())
	f.Close()
	code, _, _ = invoke(t, "failures", "--private-bundle", cipher, "--identity", wrongpath, "--private-output", filepath.Join(dir, "wrong.json"))
	if code == 0 {
		t.Fatal("wrong key accepted")
	}
}
