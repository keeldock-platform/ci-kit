package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCandidateManifestCannotClaimQualification(t *testing.T) {
	m := manifest{SchemaVersion: 1, Kind: "unsigned-cli-candidate", ControlSHA: strings.Repeat("a", 40), RunID: "1", RunAttempt: 1, OS: runtime.GOOS, Architecture: runtime.GOARCH, Binary: "kd-ci", SHA256: strings.Repeat("b", 64), NativeSmoke: map[string]bool{"version": true, "help": true, "offline_refusals": true}}
	raw, e := json.Marshal(m)
	if e != nil || !strings.Contains(string(raw), `"runtime_qualified":false`) || !strings.Contains(string(raw), `"admission_eligible":false`) || strings.Contains(string(raw), "/Users/") {
		t.Fatal("invalid public candidate manifest")
	}
}
func TestCandidateAcceptsInstalledPinnedNativeHost(t *testing.T) {
	if runtime.Version() != expectedGoVersion {
		t.Fatalf("candidate toolchain must match the pinned build: %s", runtime.Version())
	}
	if run([]string{"-os", runtime.GOOS, "-arch", runtime.GOARCH, "-host-only"}) != nil {
		t.Fatal("pinned native candidate host refused")
	}
}

func TestCandidateHostAndMetadataRefuseInvalidInputs(t *testing.T) {
	for _, args := range [][]string{{"-os", "unknown", "-arch", "amd64"}, {"-os", runtime.GOOS, "-arch", "unknown"}, {"-os", runtime.GOOS, "-arch", runtime.GOARCH, "-control-sha", "bad", "-run-id", "1", "-attempt", "1"}} {
		if run(args) == nil {
			t.Fatal("invalid candidate metadata accepted")
		}
	}
}
func TestCandidateBinaryDiscoveryRejectsAmbiguity(t *testing.T) {
	root := t.TempDir()
	if _, e := findBinary(root, "linux"); e == nil {
		t.Fatal("missing candidate accepted")
	}
	for _, dir := range []string{"one", "two"} {
		if os.Mkdir(filepath.Join(root, dir), 0700) != nil {
			t.Fatal("fixture directory")
		}
		if os.WriteFile(filepath.Join(root, dir, "kd-ci"), []byte("synthetic"), 0600) != nil {
			t.Fatal("fixture binary")
		}
	}
	if _, e := findBinary(root, "linux"); e == nil {
		t.Fatal("ambiguous candidates accepted")
	}
}
func TestCandidateFormatInspectionUsesNativeHeaders(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if inspectBinary(executable, runtime.GOOS, runtime.GOARCH) != nil {
		t.Fatal("native test executable rejected")
	}
	other := "amd64"
	if runtime.GOARCH == other {
		other = "arm64"
	}
	if inspectBinary(executable, runtime.GOOS, other) == nil {
		t.Fatal("foreign architecture accepted")
	}
}

func TestReviewedNoticesCopyAndHash(t *testing.T) {
	control, output := t.TempDir(), t.TempDir()
	text := []byte("Synthetic upstream legal notice\n")
	if os.WriteFile(filepath.Join(control, noticesFile), text, 0600) != nil {
		t.Fatal("notice fixture")
	}
	digest, e := copyReviewedNotices(control, output)
	if e != nil {
		t.Fatal(e)
	}
	copied, e := os.ReadFile(filepath.Join(output, noticesFile))
	if e != nil || string(copied) != string(text) {
		t.Fatal("notice copy changed")
	}
	expected, e := hashFile(filepath.Join(control, noticesFile))
	if e != nil || digest != expected {
		t.Fatal("notice digest changed")
	}
	if _, e := copyReviewedNotices(t.TempDir(), t.TempDir()); e == nil {
		t.Fatal("missing legal notices accepted")
	}
}
