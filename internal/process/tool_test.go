package process

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func toolFixture(t *testing.T) (Spec, map[string]string) {
	t.Helper()
	base := physicalTemp(t)
	root := filepath.Join(base, "source")
	bundle := filepath.Join(base, "control")
	packs := filepath.Join(bundle, "packs")
	lib := filepath.Join(packs, "android-emulator")
	for _, p := range []string{root, bundle, packs, lib} {
		if MakePrivateDir(p) != nil {
			t.Fatal("private fixture directory failed")
		}
	}
	node := filepath.Join(bundle, "node")
	main := filepath.Join(lib, "lifecycle.cjs")
	for _, p := range []string{node, main} {
		if os.WriteFile(p, []byte("synthetic"), 0700) != nil {
			t.Fatal("fixture tool failed")
		}
	}
	helper, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	helper, e = filepath.EvalSymlinks(helper)
	if e != nil {
		t.Fatal(e)
	}
	state := filepath.Join(base, "state.json")
	f, e := CreatePrivateFile(state)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	extra := map[string]string{"INPUT_API-LEVEL": "35", "INPUT_SYSTEM-IMAGE-API-LEVEL": "35", "INPUT_TARGET": "google_apis", "INPUT_ARCH": "x86_64", "INPUT_PROFILE": "pixel_6", "INPUT_CORES": "2", "INPUT_RAM-SIZE": "2048M", "INPUT_HEAP-SIZE": "512M", "INPUT_AVD-NAME": "ci-api-35", "INPUT_FORCE-AVD-CREATION": "true", "INPUT_EMULATOR-BOOT-TIMEOUT": "600", "INPUT_EMULATOR-PORT": "5554", "INPUT_EMULATOR-OPTIONS": androidOptions, "INPUT_DISABLE-ANIMATIONS": "true", "INPUT_DISABLE-SPELLCHECKER": "true", "INPUT_DISABLE-LINUX-HW-ACCEL": "false", "INPUT_ENABLE-HW-KEYBOARD": "false", "INPUT_EMULATOR-BUILD": "13241669", "INPUT_WORKING-DIRECTORY": root, "INPUT_NDK": "", "INPUT_CMAKE": "", "INPUT_CHANNEL": "stable", "INPUT_SCRIPT": androidScript, "INPUT_PRE-EMULATOR-LAUNCH-SCRIPT": "", "CI_PACK_STATE": state, "CI_KIT_HELPER": helper}
	return Spec{Root: root, Cwd: ".", Argv: []string{node, main}}, extra
}
func TestMaintainedToolBridgeAcceptsOnlyReviewedShape(t *testing.T) {
	s, extra := toolFixture(t)
	if validateTool(s, androidTool, extra) != nil {
		t.Fatal("reviewed Android bridge refused")
	}
}
func TestMaintainedToolBridgeRejectsScriptsTokensUnknownToolsAndSourceBundles(t *testing.T) {
	cases := map[string]func(*Spec, map[string]string) *string{
		"script": func(s *Spec, e map[string]string) *string { e["INPUT_SCRIPT"] = "echo unreviewed"; return nil },
		"pre-script": func(s *Spec, e map[string]string) *string {
			e["INPUT_PRE-EMULATOR-LAUNCH-SCRIPT"] = "echo unreviewed"
			return nil
		},
		"token":                   func(s *Spec, e map[string]string) *string { e["SOURCE_READ_01"] = "unreviewed"; return nil },
		"runner-channel":          func(s *Spec, e map[string]string) *string { e["GITHUB_OUTPUT"] = "unreviewed"; return nil },
		"wrong-working-directory": func(s *Spec, e map[string]string) *string { e["INPUT_WORKING-DIRECTORY"] = ".."; return nil },
		"source-bundle": func(s *Spec, e map[string]string) *string {
			s.Argv[1] = filepath.Join(s.Root, "packs", "android-emulator", "lifecycle.cjs")
			return nil
		},
		"unknown-tool":     func(s *Spec, e map[string]string) *string { v := "unreviewed-tool"; return &v },
		"unknown-build":    func(s *Spec, e map[string]string) *string { e["INPUT_EMULATOR-BUILD"] = "latest"; return nil },
		"injected-profile": func(s *Spec, e map[string]string) *string { e["INPUT_PROFILE"] = "pixel; echo unsafe"; return nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, extra := toolFixture(t)
			tool := androidTool
			if v := mutate(&s, extra); v != nil {
				tool = *v
			}
			if validateTool(s, tool, extra) == nil {
				t.Fatal("unreviewed tool authority accepted")
			}
		})
	}
	if _, e := RunTool(context.Background(), Spec{}, "unreviewed-tool", nil); e == nil {
		t.Fatal("unknown tool reached launch")
	}
}
func TestDeviceSelectorsAreTypedAndNeverInherited(t *testing.T) {
	uuid := "12345678-1234-1234-1234-123456789abc"
	t.Setenv("KD_CI_SIMULATOR_UDID", uuid)
	t.Setenv("ANDROID_SERIAL", "emulator-5554")
	env, e := Environment(nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, pair := range env {
		if pair == "KD_CI_SIMULATOR_UDID="+uuid || pair == "ANDROID_SERIAL=emulator-5554" {
			t.Fatal("ambient device selector inherited")
		}
	}
	if _, e := Environment(map[string]string{"KD_CI_SIMULATOR_UDID": uuid, "ANDROID_SERIAL": "emulator-5554"}); e != nil {
		t.Fatal("typed explicit device selector refused")
	}
	for _, extra := range []map[string]string{{"KD_CI_SIMULATOR_UDID": "arbitrary"}, {"ANDROID_SERIAL": "physical-device"}, {"ANDROID_SERIAL": "emulator-5555"}, {"ANDROID_SERIAL": "emulator-05554"}} {
		if _, e := Environment(extra); e == nil {
			t.Fatal("untyped device selector accepted")
		}
	}
}

func TestRawUpstreamEntrypointCannotBypassPreflight(t *testing.T) {
	s, extra := toolFixture(t)
	dir := filepath.Join(filepath.Dir(s.Argv[0]), "lib")
	if MakePrivateDir(dir) != nil {
		t.Fatal("fixture directory failed")
	}
	raw := filepath.Join(dir, "main.js")
	if os.WriteFile(raw, []byte("synthetic"), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	s.Argv[1] = raw
	if validateTool(s, androidTool, extra) == nil {
		t.Fatal("raw upstream action bypassed trusted lifecycle")
	}
}
