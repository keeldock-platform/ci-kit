package packs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainerSupportBindsDigestAndCommittedSnapshot(t *testing.T) {
	value := Support{SchemaVersion: 1, RemainingWorkingSetBytes: 1 << 30, Container: &ContainerSupport{Image: images["linux-debian"], Snapshot: "20261101T000000Z", Packages: []string{"git=1:2.47.3-1"}}}
	raw, _ := json.Marshal(value)
	if _, e := DecodeSupport("linux-debian", raw); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Support){func(s *Support) { s.Container.Image = "debian:13" }, func(s *Support) { s.Container.Snapshot = "" }, func(s *Support) { s.Container.Packages = []string{"git"} }, func(s *Support) { s.Container.Packages = []string{"git;echo"} }, func(s *Support) { s.RemainingWorkingSetBytes = 0 }} {
		var sample Support
		json.Unmarshal(raw, &sample)
		change(&sample)
		bad, _ := json.Marshal(sample)
		if _, e := DecodeSupport("linux-debian", bad); e == nil {
			t.Fatal("unsupported mutable input admitted")
		}
	}
}
func TestAndroidInstalledInventoryRefusesDriftAndMissingPackage(t *testing.T) {
	want := map[string]string{"emulator": "36.6.1", "platform-tools": "37.0.0", "platforms;android-35": "2"}
	input := []byte("Installed packages:\nPath | Version | Description\nemulator | 36.6.1 | Android Emulator\nplatform-tools | 37.0.0 | Android SDK Platform-Tools\nplatforms;android-35 | 2 | Android SDK Platform\n")
	if VerifyAndroidPackages(input, want) != nil {
		t.Fatal("valid inventory refused")
	}
	if VerifyAndroidPackages([]byte(strings.ReplaceAll(string(input), "36.6.1", "36.6.2")), want) == nil {
		t.Fatal("tool drift admitted")
	}
	if VerifyAndroidPackages([]byte("emulator | 36.6.1 | Android Emulator"), want) == nil {
		t.Fatal("missing package admitted")
	}
}
func TestSDKByteVerifierRejectsChangedAndLinkedTools(t *testing.T) {
	root := t.TempDir()
	payload := []byte("synthetic executable bytes")
	path := filepath.Join(root, "tool")
	if e := os.WriteFile(path, payload, 0600); e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(payload)
	want := map[string]string{"tool": hex.EncodeToString(digest[:])}
	if VerifySDKFiles(context.Background(), root, want) != nil {
		t.Fatal("valid tool refused")
	}
	if e := os.WriteFile(path, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if VerifySDKFiles(context.Background(), root, want) == nil {
		t.Fatal("changed tool admitted")
	}
	if e := os.Symlink(path, filepath.Join(root, "link")); e == nil {
		want = map[string]string{"link": hex.EncodeToString(digest[:])}
		if VerifySDKFiles(context.Background(), root, want) == nil {
			t.Fatal("linked tool admitted")
		}
	}
}
func TestIOSSupportRequiresDeviceTypesAndBothFamilies(t *testing.T) {
	value := Support{SchemaVersion: 1, RemainingWorkingSetBytes: 1 << 30, IOS: &IOSSupport{DeveloperDirectory: "/Applications/Xcode_26.0.app/Contents/Developer", XcodeVersion: "26.0", BuildVersion: "23A123", SDKVersion: "26.0", Runtime: "com.apple.CoreSimulator.SimRuntime.iOS-26-0", Devices: []IOSDevice{{Variant: "phone", DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPhone-Example", Family: "iphone"}, {Variant: "tablet", DeviceType: "com.apple.CoreSimulator.SimDeviceType.iPad-Example", Family: "ipad"}}}}
	raw, _ := json.Marshal(value)
	if _, e := DecodeSupport("ios-simulator", raw); e != nil {
		t.Fatal(e)
	}
	value.IOS.Devices = value.IOS.Devices[:1]
	raw, _ = json.Marshal(value)
	if _, e := DecodeSupport("ios-simulator", raw); e == nil {
		t.Fatal("missing device family admitted")
	}
}
