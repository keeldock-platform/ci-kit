package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

func TestFailureBundleRoundTripAndCiphertextCanary(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	// Use the shared native helpers: protected DACL on Windows, 0700/0600 on Unix.
	private := filepath.Join(dir, "private")
	if process.MakePrivateDir(private) != nil {
		t.Fatal("private")
	}
	sink := filepath.Join(private, "diagnostics")
	if process.MakePrivateDir(sink) != nil {
		t.Fatal("sink")
	}
	key, e := age.GenerateX25519Identity()
	if e != nil {
		t.Fatal(e)
	}
	keypath := filepath.Join(private, "key")
	f, e := process.CreatePrivateFile(keypath)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString(key.String() + "\n")
	f.Close()
	// The child emits a unique plaintext sentinel to both streams and fails.
	canary := "fixture-" + filepath.Base(dir) + "-private-diagnostic"
	ctx := process.WithCaptureSink(context.Background(), sink)
	argv := []string{os.Args[0], "-test.run=^TestDiagnosticChild$", "--", canary}
	r, e := process.Run(ctx, process.Spec{Argv: argv, Root: private, Cwd: ".", ResultDir: filepath.Join(private, "capture"), Timeout: time.Minute})
	if e == nil || r.ExitCode != 19 {
		t.Fatal("failing child lost")
	}
	report := filepath.Join(private, "report.xml")
	f, e = process.CreatePrivateFile(report)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString(`<testsuite><testcase name="private-test"><failure message="actual assertion error"/></testcase></testsuite>`)
	f.Close()
	process.RetainReport(ctx, report)
	identity := Identity{"123", "2", "windows-arm64", strings.Repeat("a", 40), strings.Repeat("b", 40)}
	cipher := filepath.Join(private, "failure.age")
	if e = Seal(sink, cipher, key.Recipient().String(), identity, 19); e != nil {
		t.Fatal(e)
	}
	encrypted, e := os.ReadFile(cipher)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted, []byte(canary)) || bytes.Contains(encrypted, []byte(key.String())) || bytes.Contains(encrypted, []byte("actual assertion error")) {
		t.Fatal("plaintext in public ciphertext")
	}
	b, e := Open(bytes.NewReader(encrypted), keypath)
	if e != nil || b.Identity != identity || b.ExitCode != 19 {
		t.Fatal("roundtrip")
	}
	var raw []byte
	for _, data := range b.Files {
		raw = append(raw, data...)
	}
	if !bytes.Contains(raw, []byte(canary)) || !bytes.Contains(raw, []byte("actual assertion error")) || !bytes.Contains(raw, []byte("argv")) {
		t.Fatal("missing positive controls")
	}
	for _, ciphertext := range [][]byte{encrypted[:len(encrypted)-1], append(append([]byte{}, encrypted[:len(encrypted)-1]...), encrypted[len(encrypted)-1]^1)} {
		if _, e := Open(bytes.NewReader(ciphertext), keypath); e == nil {
			t.Fatal("unauthenticated data accepted")
		}
	}
	other, _ := age.GenerateX25519Identity()
	otherpath := filepath.Join(private, "other")
	f, _ = process.CreatePrivateFile(otherpath)
	f.WriteString(other.String())
	f.Close()
	if _, e = Open(bytes.NewReader(encrypted), otherpath); e == nil {
		t.Fatal("wrong key accepted")
	}
	if Seal(sink, cipher, "invalid", identity, 19) == nil {
		t.Fatal("invalid recipient accepted")
	}
	link := filepath.Join(sink, "source-link")
	if os.Symlink(report, link) == nil && Seal(sink, filepath.Join(private, "linked.age"), key.Recipient().String(), identity, 19) == nil {
		t.Fatal("source symlink bundled")
	}
}
func TestDiagnosticChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	marker := os.Args[len(os.Args)-1]
	os.Stdout.WriteString(marker + " stdout\n")
	os.Stderr.WriteString(marker + " stderr\n")
	os.Exit(19)
}

func TestRecordedFailureFixture(t *testing.T) {
	target := os.Getenv("KD_CI_RECORDED_FIXTURE")
	if target == "" {
		t.Skip("explicit local fixture recording only")
	}
	if os.Getenv("GITHUB_ACTIONS") != "" || !filepath.IsAbs(target) {
		t.Fatal("fixture recording requires local absolute directory")
	}
	if process.MakePrivateDir(target) != nil {
		t.Fatal("fresh private fixture directory required")
	}
	sink := filepath.Join(target, "sink")
	if process.MakePrivateDir(sink) != nil {
		t.Fatal("sink")
	}
	ctx := process.WithCaptureSink(context.Background(), sink)
	argv := []string{os.Args[0], "-test.run=^TestDiagnosticChild$", "--", recordedCanary()}
	r, e := process.Run(ctx, process.Spec{Argv: argv, Root: target, Cwd: ".", ResultDir: filepath.Join(target, "capture"), Timeout: time.Minute})
	if e == nil || r.ExitCode != 19 {
		t.Fatal("fixture must really fail")
	}
	report := filepath.Join(target, "test.xml")
	f, e := process.CreatePrivateFile(report)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString(`<testsuite><testcase classname="recorded-fixture" name="assertion-fails"><failure message="expected 42; got 41">synthetic stack frame</failure></testcase></testsuite>`)
	f.Close()
	process.RetainReport(ctx, report)
	raw, e := os.ReadFile("../../.github/diagnostics.json")
	if e != nil {
		t.Fatal(e)
	}
	var cfg struct {
		Recipient string `json:"recipient"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("recipient")
	}
	identity := Identity{Run: "900001", Attempt: "1", Platform: "ios-simulator", KitSHA: strings.Repeat("a", 40), SourceSHA: strings.Repeat("b", 40)}
	if Seal(sink, filepath.Join(target, "recorded.age"), cfg.Recipient, identity, 19) != nil {
		t.Fatal("seal")
	}
	// The originating raw captures remain local and user-only for positive-control verification.
}

func TestSealRefusesPlantedPublicationAndMalformedEnvelope(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	private := filepath.Join(dir, "private")
	if process.MakePrivateDir(private) != nil {
		t.Fatal("private")
	}
	sink := filepath.Join(private, "sink")
	if process.MakePrivateDir(sink) != nil {
		t.Fatal("sink")
	}
	ctx := process.WithCaptureSink(context.Background(), sink)
	process.RetainAttempt(ctx, process.Spec{Argv: []string{"missing-compiler", "--exact-flag"}, Root: private, Cwd: "."}, process.ErrInvalid)
	key, _ := age.GenerateX25519Identity()
	path := filepath.Join(private, "planted.age")
	f, _ := process.CreatePrivateFile(path)
	f.WriteString("benign planted plaintext")
	f.Close()
	identity := Identity{"123", "1", "windows-arm64", strings.Repeat("a", 40), strings.Repeat("b", 40)}
	if Seal(sink, path, key.Recipient().String(), identity, 1) == nil {
		t.Fatal("planted file eligible")
	}
	if _, e = os.Stat(path + ".partial"); !os.IsNotExist(e) {
		t.Fatal("unsealed partial retained")
	}
	invalid := Bundle{SchemaVersion: 1, Identity: identity, ExitCode: 1, Files: map[string][]byte{}}
	if Validate(invalid) == nil {
		t.Fatal("empty evidence accepted")
	}
	invalid.Files = map[string][]byte{"../command.json": []byte(`{}`)}
	if Validate(invalid) == nil {
		t.Fatal("traversal accepted")
	}
}

func recordedCanary() string {
	if marker := os.Getenv("KD_CI_DIAG_CANARY"); marker != "" {
		return marker
	}
	return "recorded-failure-sentinel"
}
