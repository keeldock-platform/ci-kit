package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/collector"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/packs"
)

func runPack(ctx context.Context, root, private, platform string, units []catalog.Unit, origins map[string]string, selected []planner.Invocation, admission bool) (code int) {
	if !packs.Native(platform) {
		return 3
	}
	cfg, e := config.Load(root, "")
	if e != nil {
		return 2
	}
	shared, e := catalog.LoadSharedPath(root, cfg.SharedUnits)
	if e != nil {
		return 2
	}
	policy, ok := shared.Packs[platform]
	if !ok {
		return 2
	}
	_, experiment := ctx.Value(diagnosticKey{}).(func(diagnosticStage))
	if experiment {
		fmt.Println("STAGE setup START")
	}
	index := 0
	host := capturedExecutor(root, private, &index)
	adapter := packs.Host()
	defer func() {
		if adapter != nil && adapter.Close(context.Background()) != nil && code == 0 {
			code = 1
		}
	}()
	if platform == "linux-debian" || platform == "linux-arch" || platform == "web-linux" || platform == "ios-simulator" || platform == "android-emulator" {
		support, err := packs.DecodeSupport(platform, policy.Support)
		if err != nil {
			return 2
		}
		wd, err := os.Getwd()
		if err != nil {
			return 6
		}
		if _, err := host(ctx, []string{"node", filepath.Join(wd, "public-control", "packs", "disk.cjs"), strconv.FormatInt(support.RemainingWorkingSetBytes, 10)}, ".", time.Minute, nil); err != nil {
			return 6
		}
		switch platform {
		case "linux-debian", "linux-arch", "web-linux":
			docker, err := exec.LookPath("docker")
			if err != nil || !filepath.IsAbs(docker) {
				return 2
			}
			wd, err := os.Getwd()
			if err != nil {
				return 6
			}
			kit := filepath.Join(wd, "public-control")
			adapter, err = packs.Container(ctx, platform, *support.Container, host, docker, kit, filepath.Join(private, "source"))
			if err != nil {
				return 3
			}
		case "ios-simulator":
			adapter, err = acquireIOSAdapter(ctx, *support.IOS, host, selected, admission, packs.IOS)
			if err != nil {
				if err == refused {
					return 6
				}
				return 3
			}

		case "android-emulator":
			if err := androidReady(ctx, host); err != nil {
				return 3
			}
		}
	}

	run := func(runCtx context.Context, cmd catalog.Command, source string) int {
		index++
		argv, cwd, env, err := adapter.Wrap(append([]string{}, cmd.Argv...), source, cmd.Cwd)
		if err != nil {
			return 2
		}
		argv, err = absoluteExecutable(argv)
		if err != nil {
			return 2
		}
		timeout := time.Duration(cmd.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 20 * time.Minute
		}
		result, err := process.Run(runCtx, process.Spec{Argv: argv, Root: source, Cwd: cwd, Env: env, ResultDir: filepath.Join(private, "capture-"+strconv.Itoa(index)), Timeout: timeout})
		if result.TimedOut {
			return 4
		}
		if state := contextExit(runCtx); state != 0 {
			return state
		}
		if result.ExitCode > 0 {
			return 1
		}
		if err != nil || result.ExitCode != 0 || !result.CleanupComplete {
			return 6
		}
		if experiment && len(cmd.Argv) >= 2 && cmd.Argv[0] == "node" && cmd.Argv[1] == "ci/canary.mjs" {
			if syntheticCapture(source, result) != nil {
				fmt.Println("STAGE capture FAIL")
				return 6
			}
			fmt.Println("STAGE capture PASS")
		}
		return 0
	}
	// The shared committed pack policy owns setup once. The same captured process
	// adapter contains all setup output, including maintained native setup tools.
	for _, cmd := range policy.Setup {
		if code := run(ctx, cmd, root); code != 0 {
			return code
		}
	}
	for tool, want := range policy.Tools {
		argv, cwd, env, err := adapter.Wrap([]string{tool, "--version"}, root, ".")
		if err != nil {
			return 2
		}
		out, err := host(ctx, argv, cwd, 30*time.Second, env)
		if err != nil || strings.TrimSpace(string(out)) != want {
			return 2
		}
	}
	if platform == "web-linux" {
		argv, cwd, env, err := adapter.Wrap([]string{"node", "/public-control/packs/web-linux/readiness.cjs"}, root, ".")
		if err != nil {
			return 2
		}
		out, err := host(ctx, argv, cwd, 2*time.Minute, env)
		if err != nil || string(out) != "1.65.1" {
			return 3
		}
	}
	if platform == "android-emulator" {
		support, err := packs.DecodeSupport(platform, policy.Support)
		if err != nil {
			return 2
		}
		return runAndroid(ctx, root, private, units, origins, selected, admission, *support.Android, &index)
	}
	return runChecks(ctx, root, private, platform, units, origins, selected, admission, run, adapter.Before)
}
func runChecks(ctx context.Context, root, private, platform string, units []catalog.Unit, origins map[string]string, selected []planner.Invocation, admission bool, run func(context.Context, catalog.Command, string) int, before func(context.Context, string) error) int {
	var e error
	byID := map[string]catalog.Unit{}
	for _, u := range units {
		byID[u.ID] = u
	}
	owned := map[string]bool{}
	census := collector.Census{Feedback: !admission}
	allReports := []collector.Report{}
	reportBytes := 0
	firstFailure := 0
	failedUnits := map[string]bool{}
	failedEntries := map[string]bool{}
	_, diagnosticMode := ctx.Value(diagnosticKey{}).(func(diagnosticStage))
	if diagnosticMode {
		fmt.Println("STAGE setup PASS")
	}
	diagnosticMode = diagnosticMode && !admission
	for slot, inv := range selected {
		u, ok := byID[inv.Unit]
		if !ok {
			return 6
		}
		blocked := false
		if diagnosticMode {
			check := u.Checks[inv.Check]
			for _, dependency := range append(append(append([]string{}, u.DependsOn...), u.OptionalContracts...), check.DependsOn...) {
				blocked = blocked || failedUnits[dependency]
			}
			for _, need := range append(append([]string{}, u.Needs...), check.Needs...) {
				blocked = blocked || failedEntries[need]
			}
			if blocked {
				failedUnits[u.ID] = true
				failedEntries[inv.ID] = true
				diagnostic(ctx, diagnosticStage{Slot: slot, Code: 6, Blocked: true})
				continue
			}
		}
		if u.TimeoutSeconds < 1 || u.TimeoutSeconds > 21600 {
			return 2
		}
		unitCtx, cancel := context.WithTimeout(ctx, time.Duration(u.TimeoutSeconds)*time.Second)
		code := func() int {
			if state := contextExit(unitCtx); state != 0 {
				return state
			}
			if before(unitCtx, inv.Variant) != nil {
				if state := contextExit(unitCtx); state != 0 {
					return state
				}
				return 3
			}
			source := origins[inv.Unit]
			if source == "" {
				return 6
			}
			check, ok := u.Checks[inv.Check]
			if !ok {
				return 6
			}
			hasReports := len(check.Reports) > 0
			if hasReports && len(inv.Tests) == 0 {
				return 6
			}
			if !hasReports && (!slices.Contains([]string{"format", "lint", "types"}, inv.Check) || len(check.Assertions) > 0 || len(inv.Tests) > 0) {
				return 6
			}
			for _, rel := range check.Reports {
				path, e := reportPath(source, rel)
				if e != nil {
					return 6
				}
				st, e := os.Lstat(path)
				if e == nil {
					if !owned[path] || !st.Mode().IsRegular() || os.Remove(path) != nil {
						return 6
					}
				} else if !os.IsNotExist(e) {
					return 6
				}
			}
			for _, cmd := range inv.Commands {
				if cmd.TimeoutSeconds < 0 {
					return 2
				}
				if cmd.TimeoutSeconds == 0 || cmd.TimeoutSeconds > u.TimeoutSeconds {
					cmd.TimeoutSeconds = u.TimeoutSeconds
				}
				if state := contextExit(unitCtx); state != 0 {
					return state
				}
				if code := run(unitCtx, cmd, source); code != 0 {
					return code
				}
			}
			if !hasReports {
				return contextExit(unitCtx)
			}
			shard, count := 1, 1
			if inv.Shard != "" {
				parts := strings.Split(inv.Shard, "/")
				if len(parts) != 2 {
					return 6
				}
				shard, e = strconv.Atoi(parts[0])
				if e != nil {
					return 6
				}
				count, e = strconv.Atoi(parts[1])
				if e != nil {
					return 6
				}
			}
			expectation := collector.ReportExpectation{Unit: inv.Unit, Check: inv.Check, Platform: platform, Variant: inv.Variant, Shard: shard, Shards: count, Assertions: invocationAssertions(check.Assertions, inv.Tests), Tests: inv.Tests, Reports: check.Reports}
			reports := []collector.Report{}
			for _, rel := range check.Reports {
				path, e := reportPath(source, rel)
				if e != nil {
					return 6
				}
				data, e := readRegular(path, collector.MaxReport)
				if e != nil {
					return 6
				}
				reportBytes += len(data)
				if reportBytes > 64<<20 {
					return 6
				}
				tests, e := collector.PassedTests(data)
				if e != nil {
					return 6
				}
				assertions := []string{}
				for _, a := range check.Assertions {
					if slices.Contains(tests, a) {
						assertions = append(assertions, a)
					}
				}
				reports = append(reports, collector.Report{Unit: inv.Unit, Check: inv.Check, Platform: platform, Variant: inv.Variant, Shard: shard, Shards: count, Path: rel, Format: "junit", Data: data, Assertions: assertions})
				owned[path] = true
			}
			// Each fresh invocation is complete in itself, including an independently
			// requested whole-test shard. Admission expands every required shard first.
			if collector.ValidateReports(collector.Census{Entries: []collector.ReportExpectation{expectation}, Feedback: true}, reports) != nil {
				return 6
			}
			if state := contextExit(unitCtx); state != 0 {
				return state
			}
			census.Entries = append(census.Entries, expectation)
			allReports = append(allReports, reports...)
			return 0
		}()
		cancel()
		if diagnosticMode {
			diagnostic(ctx, diagnosticStage{Slot: slot, Code: code})
		}
		if code != 0 {
			if !diagnosticMode || code == 2 || code == 3 || code == 6 || code == 7 || ctx.Err() != nil {
				if diagnosticMode {
					for remaining := slot + 1; remaining < len(selected); remaining++ {
						diagnostic(ctx, diagnosticStage{Slot: remaining, Code: 6, Blocked: true})
					}
				}
				return code
			}
			if firstFailure == 0 {
				firstFailure = code
			}
			failedUnits[u.ID] = true
			failedEntries[inv.ID] = true
		}
	}
	if firstFailure != 0 {
		return firstFailure
	}
	if len(census.Entries) > 0 && collector.ValidateReports(census, allReports) != nil {
		return 6
	}
	return 0
}
func reportPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) || filepath.ToSlash(filepath.Clean(rel)) != rel || strings.Contains(rel, `\`) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", refused
	}
	if _, e := process.SafePath(root, filepath.Dir(rel)); e != nil {
		return "", refused
	}
	path := filepath.Join(root, rel)
	if st, e := os.Lstat(path); e == nil && (!st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0) {
		return "", refused
	} else if e != nil && !os.IsNotExist(e) {
		return "", refused
	}
	return path, nil
}

func invocationAssertions(assertions, tests []string) []string {
	selected := []string{}
	for _, a := range assertions {
		if slices.Contains(tests, a) {
			selected = append(selected, a)
		}
	}
	return selected
}

func absoluteExecutable(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, refused
	}
	if !filepath.IsAbs(argv[0]) {
		path, e := exec.LookPath(argv[0])
		if e != nil || !filepath.IsAbs(path) {
			return nil, refused
		}
		argv[0] = path
	}
	return argv, nil
}
func capturedExecutor(root, private string, index *int) packs.Execute {
	return func(ctx context.Context, argv []string, cwd string, timeout time.Duration, env map[string]string) ([]byte, error) {
		*index++
		argv, e := absoluteExecutable(argv)
		if e != nil {
			return nil, refused
		}
		result, e := process.Run(ctx, process.Spec{Argv: argv, Root: root, Cwd: cwd, Env: env, ResultDir: filepath.Join(private, "venue-"+strconv.Itoa(*index)), Timeout: timeout})
		if e != nil || result.ExitCode != 0 || !result.CleanupComplete {
			return nil, refused
		}
		return readRegular(result.StdoutPath, 16<<20)
	}
}

func contextExit(ctx context.Context) int {
	if ctx.Err() == context.DeadlineExceeded {
		return 4
	}
	if ctx.Err() != nil {
		return 7
	}
	return 0
}

// Validate admission before the constructor can create owned simulator devices.
func acquireIOSAdapter(ctx context.Context, support packs.IOSSupport, host packs.Execute, selected []planner.Invocation, admission bool, create func(context.Context, packs.IOSSupport, packs.Execute) (packs.Adapter, error)) (packs.Adapter, error) {
	if admission {
		seen := map[string]bool{}
		for _, inv := range selected {
			seen[inv.Variant] = true
		}
		for _, device := range support.Devices {
			if !seen[device.Variant] {
				return nil, refused
			}
		}
	}
	return create(ctx, support, host)
}
