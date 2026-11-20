package process

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const androidTool = "android-emulator-runner"
const androidScript = `"$CI_KIT_HELPER" native-hook`
const androidOptions = "-no-window -gpu swiftshader_indirect -no-snapshot -noaudio -no-boot-anim -accel on"

var androidProfile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,127}$`)
var numericVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,3}$`)
var digits = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)

// RunTool is solely the reviewed maintained Android action bridge. The trusted
// caller verifies the exact pinned public bundle before calling it. Input keys
// never become a general project environment escape hatch.
func RunTool(ctx context.Context, s Spec, tool string, extra map[string]string) (Result, error) {
	if validateTool(s, tool, extra) != nil {
		return Result{ExitCode: -1}, ErrInvalid
	}
	return run(ctx, s, extra)
}

func validateTool(s Spec, tool string, extra map[string]string) error {
	if tool != androidTool || s.Cwd != "." || len(s.Argv) != 2 {
		return ErrInvalid
	}
	exe := strings.TrimSuffix(strings.ToLower(filepath.Base(s.Argv[0])), ".exe")
	if exe != "node" || !androidLifecycle(s.Argv[1]) {
		return ErrInvalid
	}
	for _, p := range s.Argv {
		if !outsideSource(s.Root, p) || !regularToolPath(p) {
			return ErrInvalid
		}
	}
	if extra["INPUT_WORKING-DIRECTORY"] != s.Root {
		return ErrInvalid
	}
	helper, e := os.Executable()
	if e != nil {
		return ErrInvalid
	}
	helper, e = filepath.EvalSymlinks(helper)
	if e != nil || extra["CI_KIT_HELPER"] != helper || !outsideSource(s.Root, helper) || !regularToolPath(helper) {
		return ErrInvalid
	}
	state := extra["CI_PACK_STATE"]
	if !filepath.IsAbs(state) || filepath.Clean(state) != state || !UserOnly(state, false) || !regularToolPath(state) {
		return ErrInvalid
	}
	return androidInputs(extra)
}

func androidInputs(extra map[string]string) error {
	fixed := map[string]string{
		"INPUT_CORES": "2", "INPUT_RAM-SIZE": "2048M", "INPUT_HEAP-SIZE": "512M",
		"INPUT_FORCE-AVD-CREATION": "true", "INPUT_EMULATOR-BOOT-TIMEOUT": "600", "INPUT_EMULATOR-PORT": "5554",
		"INPUT_EMULATOR-OPTIONS": androidOptions, "INPUT_DISABLE-ANIMATIONS": "true", "INPUT_DISABLE-SPELLCHECKER": "true",
		"INPUT_DISABLE-LINUX-HW-ACCEL": "false", "INPUT_ENABLE-HW-KEYBOARD": "false", "INPUT_CHANNEL": "stable",
		"INPUT_SCRIPT": androidScript, "INPUT_PRE-EMULATOR-LAUNCH-SCRIPT": "",
	}
	variable := map[string]bool{"INPUT_API-LEVEL": true, "INPUT_SYSTEM-IMAGE-API-LEVEL": true, "INPUT_TARGET": true, "INPUT_ARCH": true, "INPUT_PROFILE": true, "INPUT_AVD-NAME": true, "INPUT_EMULATOR-BUILD": true, "INPUT_WORKING-DIRECTORY": true, "INPUT_NDK": true, "INPUT_CMAKE": true, "CI_PACK_STATE": true, "CI_KIT_HELPER": true}
	if len(extra) != len(fixed)+len(variable) {
		return ErrInvalid
	}
	for k, v := range extra {
		if strings.ContainsAny(k+v, "\x00\r\n") {
			return ErrInvalid
		}
		if want, ok := fixed[k]; ok {
			if v != want {
				return ErrInvalid
			}
		} else if !variable[k] {
			return ErrInvalid
		}
	}
	for k, v := range fixed {
		if got, ok := extra[k]; !ok || got != v {
			return ErrInvalid
		}
	}
	for k := range variable {
		if _, ok := extra[k]; !ok {
			return ErrInvalid
		}
	}
	api, e := strconv.Atoi(extra["INPUT_API-LEVEL"])
	if e != nil || api < 21 || api > 99 || strconv.Itoa(api) != extra["INPUT_API-LEVEL"] || extra["INPUT_SYSTEM-IMAGE-API-LEVEL"] != extra["INPUT_API-LEVEL"] {
		return ErrInvalid
	}
	if extra["INPUT_ARCH"] != "x86_64" || extra["INPUT_AVD-NAME"] != "ci-api-"+strconv.Itoa(api) {
		return ErrInvalid
	}
	switch extra["INPUT_TARGET"] {
	case "default", "google_apis", "google_atd", "aosp_atd":
	default:
		return ErrInvalid
	}
	profile := extra["INPUT_PROFILE"]
	if !androidProfile.MatchString(profile) || strings.TrimSpace(profile) != profile {
		return ErrInvalid
	}
	if !digits.MatchString(extra["INPUT_EMULATOR-BUILD"]) {
		return ErrInvalid
	}
	for _, k := range []string{"INPUT_NDK", "INPUT_CMAKE"} {
		v := extra[k]
		if v != "" && (len(v) > 64 || !numericVersion.MatchString(v)) {
			return ErrInvalid
		}
	}
	return nil
}
func regularToolPath(p string) bool {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return false
	}
	if _, e := SafePath(filepath.Dir(p), "."); e != nil {
		return false
	}
	st, e := os.Lstat(p)
	return e == nil && st.Mode().IsRegular()
}
func outsideSource(root, p string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(p) {
		return false
	}
	rel, e := filepath.Rel(root, p)
	return e == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func androidLifecycle(path string) bool {
	return filepath.Base(path) == "lifecycle.cjs" && filepath.Base(filepath.Dir(path)) == "android-emulator" && filepath.Base(filepath.Dir(filepath.Dir(path))) == "packs"
}
