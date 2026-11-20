package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/packs"
)

type androidState struct {
	API                     int                  `json:"api"`
	SchemaVersion           int                  `json:"schema_version"`
	Root                    string               `json:"root"`
	CaptureDirectory        string               `json:"capture_directory"`
	Private                 string               `json:"private"`
	Units                   []catalog.Unit       `json:"units"`
	Origins                 map[string]string    `json:"origins"`
	Selected                []planner.Invocation `json:"selected"`
	Admission               bool                 `json:"admission"`
	Diagnostic              bool                 `json:"diagnostic"`
	Packages                map[string]string    `json:"packages"`
	ToolSHA256              map[string]string    `json:"tool_sha256"`
	Result                  string               `json:"result"`
	CommandLineToolsVersion string               `json:"command_line_tools_version"`
}

func androidReady(ctx context.Context, run packs.Execute) error {
	return androidReadyAt(ctx, "/dev/kvm")
}
func androidReadyAt(ctx context.Context, device string) (err error) {
	defer func() {
		if err != nil {
			process.RetainAttempt(ctx, process.Spec{Argv: []string{"android-kvm-readiness", device}, Root: filepath.Dir(device), Cwd: "."}, err)
		}
	}()
	st, err := os.Stat(device)
	if err != nil {
		return fmt.Errorf("Android KVM stat: %w", err)
	}
	if st.Mode()&os.ModeDevice == 0 {
		return fmt.Errorf("Android KVM path is not a device: %s", device)
	}
	f, err := os.OpenFile(device, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("Android KVM open: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("Android KVM close: %w", err)
	}
	return nil
}
func runAndroid(ctx context.Context, root, private string, units []catalog.Unit, origins map[string]string, selected []planner.Invocation, admission bool, support packs.AndroidSupport, index *int) (code int) {
	phase := 1
	defer func() {
		if code != 0 && diagnosticRequested(ctx) {
			fmt.Printf("ANDROID_GATE %d\n", phase)
		}
	}()
	wd, e := os.Getwd()
	if e != nil {
		return 6
	}
	bundle := filepath.Join(wd, "public-android-tool")
	host := capturedExecutor(root, private, index)
	git, e := exec.LookPath("git")
	if e != nil {
		return 2
	}
	out, e := host(ctx, []string{git, "-C", bundle, "rev-parse", "HEAD"}, ".", time.Minute, nil)
	if e != nil || strings.TrimSpace(string(out)) != "a421e43855164a8197daf9d8d40fe71c6996bb0d" {
		return 3
	}
	for i, dir := range []string{bundle, filepath.Join(wd, "public-control")} {
		phase = 2 + i
		clean, e := host(ctx, []string{git, "-C", dir, "status", "--porcelain", "--untracked-files=all"}, ".", time.Minute, nil)
		if e != nil || len(clean) != 0 {
			return 3
		}
	}
	node, e := exec.LookPath("node")
	if e != nil || !filepath.IsAbs(node) {
		return 2
	}
	helper, e := os.Executable()
	if e != nil {
		return 6
	}
	phase = 4
	byVariant := map[string]packs.AndroidImage{}
	for _, image := range support.Images {
		byVariant[image.Variant] = image
	}
	groups := map[int][]planner.Invocation{}
	seen := map[string]bool{}
	for _, inv := range selected {
		image, ok := byVariant[inv.Variant]
		if !ok {
			return 2
		}
		seen[inv.Variant] = true
		groups[image.API] = append(groups[image.API], inv)
	}
	if admission {
		for variant := range byVariant {
			if !seen[variant] {
				return 6
			}
		}
	}
	for _, image := range support.Images {
		phase = 5
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:5554", time.Second); err == nil {
			conn.Close()
			return 3
		}
		invocations := groups[image.API]
		if len(invocations) == 0 {
			continue
		}
		delete(groups, image.API)
		state := androidState{SchemaVersion: 1, API: image.API, Root: root, Private: private, Units: units, Origins: origins, Selected: invocations, Admission: admission, Diagnostic: diagnosticRequested(ctx) && !admission, CaptureDirectory: filepath.Join(private, "android-api-"+strconv.Itoa(image.API)), Packages: support.Packages, CommandLineToolsVersion: support.CommandLineToolsVersion, ToolSHA256: support.ToolSHA256, Result: filepath.Join(private, "android-result-"+strconv.Itoa(image.API)+".json")}
		if e := process.MakePrivateDir(state.CaptureDirectory); e != nil {
			return 6
		}
		statePath := filepath.Join(private, "android-state-"+strconv.Itoa(image.API)+".json")
		raw, e := json.Marshal(state)
		if e != nil || len(raw) > 16<<20 {
			return 6
		}
		f, e := process.CreatePrivateFile(statePath)
		if e != nil {
			return 6
		}
		_, e = f.Write(raw)
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return 6
		}
		extra := map[string]string{
			"INPUT_API-LEVEL": strconv.Itoa(image.API), "INPUT_SYSTEM-IMAGE-API-LEVEL": strconv.Itoa(image.API), "INPUT_TARGET": image.Target, "INPUT_ARCH": "x86_64", "INPUT_PROFILE": image.Profile,
			"INPUT_CORES": "2", "INPUT_RAM-SIZE": "2048M", "INPUT_HEAP-SIZE": "512M", "INPUT_AVD-NAME": "ci-api-" + strconv.Itoa(image.API), "INPUT_FORCE-AVD-CREATION": "true", "INPUT_EMULATOR-BOOT-TIMEOUT": "600", "INPUT_EMULATOR-PORT": "5554",
			"INPUT_EMULATOR-OPTIONS": "-no-window -gpu swiftshader_indirect -no-snapshot -noaudio -no-boot-anim -accel on", "INPUT_DISABLE-ANIMATIONS": "true", "INPUT_DISABLE-SPELLCHECKER": "true", "INPUT_DISABLE-LINUX-HW-ACCEL": "false", "INPUT_ENABLE-HW-KEYBOARD": "false",
			"INPUT_EMULATOR-BUILD": support.EmulatorBuild, "INPUT_WORKING-DIRECTORY": root, "INPUT_NDK": support.NDK, "INPUT_CMAKE": support.CMake, "INPUT_CHANNEL": "stable", "INPUT_SCRIPT": "\"$CI_KIT_HELPER\" native-hook", "INPUT_PRE-EMULATOR-LAUNCH-SCRIPT": "", "CI_PACK_STATE": statePath, "CI_KIT_HELPER": helper,
		}
		phase = 6
		sdk := os.Getenv("ANDROID_HOME")
		if !filepath.IsAbs(sdk) {
			return 3
		}
		prep := map[string]string{}
		for path, digest := range support.ToolSHA256 {
			if strings.HasPrefix(path, "cmdline-tools/") {
				prep[path] = digest
			}
		}
		phase = 7
		if packs.VerifySDKFiles(ctx, sdk, prep) != nil {
			return 3
		}
		phase = 8
		*index++
		result, e := process.RunTool(ctx, process.Spec{Argv: []string{node, filepath.Join(wd, "public-control", "packs", "android-emulator", "lifecycle.cjs")}, Root: root, Cwd: ".", ResultDir: filepath.Join(private, "android-capture-"+strconv.Itoa(*index)), Timeout: 50 * time.Minute}, "android-emulator-runner", extra)
		var hook struct {
			Code            *int `json:"code"`
			CaptureControls int  `json:"capture_controls"`
			Gate            int  `json:"gate"`
		}
		raw, readErr := readRegular(state.Result, 1024)
		if readErr == nil && config.Decode(raw, &hook, 1024) == nil && hook.Code != nil && *hook.Code >= 0 && *hook.Code <= 8 {
			if hook.Gate < 0 || hook.Gate > 28 {
				return 6
			}
			if *hook.Code != 0 {
				if diagnosticRequested(ctx) {
					fmt.Printf("ANDROID_NATIVE_GATE %d\n", hook.Gate)
				}
				return *hook.Code
			}
			if e != nil || result.ExitCode != 0 || !result.CleanupComplete {
				return 6
			}
			if state.Diagnostic {
				expected := 0
				for _, inv := range invocations {
					for _, cmd := range inv.Commands {
						if isSyntheticCanary(cmd) {
							expected++
						}
					}
				}
				if expected == 0 || hook.CaptureControls != expected {
					return 6
				}
				for i := 0; i < expected; i++ {
					fmt.Println("STAGE capture PASS")
				}
				if !androidCompletedSlots(ctx, selected, invocations) {
					return 6
				}
			}
		} else {
			return 6
		}
		byID := map[string]catalog.Unit{}
		for _, unit := range units {
			byID[unit.ID] = unit
		}
		cleaned := map[string]bool{}
		for _, inv := range invocations {
			for _, rel := range byID[inv.Unit].Checks[inv.Check].Reports {
				path, err := reportPath(origins[inv.Unit], rel)
				if err != nil {
					return 6
				}
				if cleaned[path] {
					continue
				}
				if os.Remove(path) != nil {
					return 6
				}
				cleaned[path] = true
			}
		}
	}
	return 0
}
func nativeHook(ctx context.Context) int {
	path := os.Getenv("CI_PACK_STATE")
	if !filepath.IsAbs(path) {
		return 3
	}
	raw, e := readRegular(path, 16<<20)
	if e != nil {
		return 3
	}
	var state androidState
	if config.Decode(raw, &state, 16<<20) != nil || state.SchemaVersion != 1 || state.API < 21 || state.API > 99 || state.Private != filepath.Dir(path) || !filepath.IsAbs(state.Root) || len(state.Selected) == 0 || len(state.Selected) > 100000 || filepath.Dir(state.Result) != state.Private || filepath.Dir(state.CaptureDirectory) != state.Private {
		return 3
	}
	for _, root := range state.Origins {
		rel, e := filepath.Rel(filepath.Join(state.Private, "source"), root)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return 3
		}
		if _, e = process.SafePath(root, "."); e != nil {
			return 3
		}
	}
	ctx = process.WithCaptureSink(ctx, filepath.Join(state.Private, "diagnostics"))
	index := 100000
	host := capturedExecutor(state.Root, state.CaptureDirectory, &index)
	code := 6
	captureControls := 0
	if _, ok := state.Origins[""]; ok {
		return 3
	}
	rootKnown := false
	for _, root := range state.Origins {
		if root == state.Root {
			rootKnown = true
		}
	}
	if !rootKnown {
		return 3
	}
	sdk := os.Getenv("ANDROID_HOME")
	if !filepath.IsAbs(sdk) {
		return writeHookGate(state.Result, 21)
	}
	out, e := host(ctx, []string{filepath.Join(sdk, "cmdline-tools", "latest", "bin", "sdkmanager"), "--list_installed"}, ".", 2*time.Minute, nil)
	adb := filepath.Join(sdk, "platform-tools", "adb")
	stateOut, stateErr := host(ctx, []string{adb, "-s", "emulator-5554", "get-state"}, ".", 30*time.Second, nil)
	bootOut, bootErr := host(ctx, []string{adb, "-s", "emulator-5554", "shell", "getprop", "sys.boot_completed"}, ".", 30*time.Second, nil)
	apiOut, apiErr := host(ctx, []string{adb, "-s", "emulator-5554", "shell", "getprop", "ro.build.version.sdk"}, ".", 30*time.Second, nil)
	abiOut, abiErr := host(ctx, []string{adb, "-s", "emulator-5554", "shell", "getprop", "ro.product.cpu.abi"}, ".", 30*time.Second, nil)
	nameOut, nameErr := host(ctx, []string{adb, "-s", "emulator-5554", "emu", "avd", "name"}, ".", 30*time.Second, nil)

	if stateErr != nil || strings.TrimSpace(string(stateOut)) != "device" {
		return writeHookGate(state.Result, 22)
	}
	if bootErr != nil || strings.TrimSpace(string(bootOut)) != "1" {
		return writeHookGate(state.Result, 23)
	}
	if apiErr != nil || strings.TrimSpace(string(apiOut)) != strconv.Itoa(state.API) {
		return writeHookGate(state.Result, 24)
	}
	if abiErr != nil || strings.TrimSpace(string(abiOut)) != "x86_64" {
		return writeHookGate(state.Result, 25)
	}
	if nameErr != nil || !androidAVDName(nameOut, state.API) {
		return writeHookGate(state.Result, 26)
	}
	if e != nil || packs.VerifyAndroidPackages(out, state.Packages) != nil {
		return writeHookOutcome(state.Result, 6, 0, 27)
	}
	if packs.VerifySDKFiles(ctx, sdk, state.ToolSHA256) != nil {
		return writeHookOutcome(state.Result, 6, 0, 28)
	}
	{
		run := androidCommandRunner(state, &index, &captureControls)
		code = runChecks(state.executionContext(ctx), state.Root, state.Private, "android-emulator", state.Units, state.Origins, state.Selected, state.Admission, run, func(context.Context, string) error { return nil })
	}
	return writeHookResultWithCaptures(state.Result, code, captureControls)
}
func writeHookResult(path string, code int) int { return writeHookResultWithCaptures(path, code, 0) }
func writeHookResultWithCaptures(path string, code, captures int) int {
	return writeHookOutcome(path, code, captures, 0)
}
func writeHookGate(path string, gate int) int { return writeHookOutcome(path, 3, 0, gate) }
func writeHookOutcome(path string, code, captures, gate int) int {
	f, e := process.CreatePrivateFile(path)
	if e != nil {
		return 6
	}
	raw, _ := json.Marshal(struct {
		Code            int `json:"code"`
		CaptureControls int `json:"capture_controls"`
		Gate            int `json:"gate"`
	}{code, captures, gate})
	_, e = f.Write(raw)
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return 6
	}
	return code
}

func nativePreflight(ctx context.Context) int {
	path := os.Getenv("CI_PACK_STATE")
	if !filepath.IsAbs(path) {
		return 3
	}
	raw, e := readRegular(path, 16<<20)
	if e != nil {
		return 3
	}
	var state androidState
	if config.Decode(raw, &state, 16<<20) != nil || state.SchemaVersion != 1 || state.Private != filepath.Dir(path) || filepath.Dir(state.Result) != state.Private || filepath.Dir(state.CaptureDirectory) != state.Private {
		return 3
	}
	sdk := os.Getenv("ANDROID_HOME")
	if !filepath.IsAbs(sdk) {
		return writeHookGate(state.Result, 11)
	}
	metadataPath, e := config.SafePath(sdk, "cmdline-tools/latest/source.properties")
	if e != nil {
		return writeHookGate(state.Result, 12)
	}
	metadata, e := readRegular(metadataPath, 65536)
	if e != nil || packs.VerifyAndroidCommandLine(metadata, state.CommandLineToolsVersion) != nil {
		return writeHookGate(state.Result, 13)
	}
	ctx = process.WithCaptureSink(ctx, filepath.Join(state.Private, "diagnostics"))
	index := 200000
	host := capturedExecutor(state.Root, state.CaptureDirectory, &index)
	out, e := host(ctx, []string{filepath.Join(sdk, "cmdline-tools", "latest", "bin", "sdkmanager"), "--list_installed"}, ".", 2*time.Minute, nil)
	if e != nil {
		return writeHookGate(state.Result, 14)
	}
	if packs.VerifyAndroidPackages(out, state.Packages) != nil {
		return writeHookGate(state.Result, 15)
	}
	if packs.VerifySDKFiles(ctx, sdk, state.ToolSHA256) != nil {
		return writeHookGate(state.Result, 16)
	}
	return 0
}

func diagnosticRequested(ctx context.Context) bool {
	_, enabled := ctx.Value(diagnosticKey{}).(func(diagnosticStage))
	return enabled
}
func (s androidState) executionContext(ctx context.Context) context.Context {
	if s.Diagnostic && !s.Admission {
		return diagnosticContext(ctx)
	}
	return ctx
}

func androidCommandRunner(state androidState, index, captureControls *int) func(context.Context, catalog.Command, string) int {
	return func(runCtx context.Context, cmd catalog.Command, root string) int {
		*index++
		argv, e := absoluteExecutable(append([]string{}, cmd.Argv...))
		if e != nil {
			process.RetainAttempt(runCtx, process.Spec{Argv: cmd.Argv, Root: root, Cwd: cmd.Cwd}, e)
			return 2
		}
		timeout := time.Duration(cmd.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 20 * time.Minute
		}
		r, e := process.Run(runCtx, process.Spec{Argv: argv, Root: root, Cwd: cmd.Cwd, ResultDir: filepath.Join(state.CaptureDirectory, "android-unit-"+strconv.Itoa(*index)), Timeout: timeout, Env: map[string]string{"ANDROID_SERIAL": "emulator-5554"}})
		if r.TimedOut {
			return 4
		}
		if state := contextExit(runCtx); state != 0 {
			return state
		}
		if r.ExitCode > 0 {
			return 1
		}
		if e != nil || r.ExitCode != 0 || !r.CleanupComplete {
			return 6
		}
		if state.Diagnostic && isSyntheticCanary(cmd) {
			if syntheticCapture(root, r) != nil {
				return 6
			}
			*captureControls++
		}
		return 0
	}
}

func androidCompletedSlots(ctx context.Context, selected, completed []planner.Invocation) bool {
	slots := []int{}
	seen := map[int]bool{}
	for _, done := range completed {
		match := -1
		for slot, inv := range selected {
			if inv.ID == done.ID && inv.Variant == done.Variant && inv.Shard == done.Shard {
				if match >= 0 {
					return false
				}
				match = slot
			}
		}
		if match < 0 || seen[match] {
			return false
		}
		seen[match] = true
		slots = append(slots, match)
	}
	for _, slot := range slots {
		diagnostic(ctx, diagnosticStage{Slot: slot, Code: 0})
	}
	return true
}

func androidAVDName(data []byte, api int) bool {
	if len(data) == 0 || len(data) > 128 || api < 21 || api > 99 {
		return false
	}
	framed := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.Contains(framed, "\r") {
		return false
	}
	return strings.TrimSuffix(framed, "\n") == "ci-api-"+strconv.Itoa(api)+"\nOK"
}
