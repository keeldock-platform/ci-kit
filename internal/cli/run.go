package cli

import (
	"context"
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/collector"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

func (a *app) run() *cobra.Command {
	var opts planner.Options
	var deadline time.Duration
	c := &cobra.Command{Use: "run UNIT", Short: "Execute local feedback through private capture", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		a.begin("run", false)
		opts.Context = a.ctx
		opts.Platforms = unique(opts.Platforms)
		opts.Units = []string{args[0]}
		opts.Mode = "unit"
		if len(opts.Platforms) > 1 || c.Flags().Changed("deadline") && deadline <= 0 || deadline < 0 || opts.Quick && (!protocol.SHA(opts.Base) || !protocol.SHA(opts.SHA)) {
			return gh.Fail(2, "usage")
		}
		root, cfg, e := a.repository()
		if e != nil {
			return e
		}
		machine, e := config.LoadMachine()
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		policy := machine.LocalPolicy
		if cfg.LocalPolicy == "managed-owner-mac" {
			policy = "managed-owner-mac"
		}
		if policy == "managed-owner-mac" && (!slices.Contains(machine.AllowedLocalConcerns, cfg.Concern) || machine.SourceRoots[cfg.Concern] != root) {
			return gh.Fail(3, "policy_refused")
		}
		units, origins, shared, e := a.localCatalogue(root, cfg)
		if e != nil {
			return gh.Fail(2, "configuration")
		}
		opts.ToolDirectory = machine.PlannerDirectory
		opts.Executables = machine.Tools

		opts.Units, e = catalog.ExpandAliases(units, shared, opts.Units)
		if e != nil {
			return gh.Fail(2, "usage")
		}
		if len(uniqueRootPaths(origins)) > 1 && opts.Quick {
			head, e := gh.Git(a.ctx, root, 1024, "rev-parse", "HEAD")
			dirty, err := a.compositionDirty(origins)
			if e != nil || err != nil || strings.TrimSpace(string(head)) != opts.SHA || dirty {
				return gh.Fail(2, "usage")
			}
			if _, e = gh.Git(a.ctx, root, 1024, "cat-file", "-e", opts.Base+"^{commit}"); e != nil {
				return gh.Fail(2, "usage")
			}
			opts.Quick = false
			opts.Mode = "unit"
			a.result.Local.Explanation = append(a.result.Local.Explanation, "Related mapping is unavailable across the locked composition; complete selected units are used.")
		}
		plan, e := planner.Plan(root, units, opts)
		if e != nil {
			return selectionError(e)
		}
		if len(plan.Selected) == 0 {
			return gh.Fail(2, "usage")
		}
		// A local invocation executes one host venue; aliases never simulate other platforms.
		selected := []planner.Invocation{}
		explicit := len(opts.Platforms) > 0 || strings.Contains(args[0], "@")
		for _, inv := range plan.Selected {
			if hostSupports(inv.Platform) {
				selected = append(selected, inv)
			} else if explicit {
				return gh.Fail(3, "policy_refused")
			}
		}
		if len(selected) == 0 {
			return gh.Fail(3, "policy_refused")
		}
		plan.Selected = selected

		// Validate every command and report before spawning the first child.
		byID := map[string]catalog.Unit{}
		for _, u := range units {
			byID[u.ID] = u
		}
		for _, inv := range selected {
			if policy == "managed-owner-mac" && !approvedManagedRoot(machine, origins[inv.Unit]) {
				return gh.Fail(3, "policy_refused")
			}
			u := byID[inv.Unit]
			if a.offline && u.Checks[inv.Check].RequiresNetwork {
				return gh.Fail(3, "offline_refused")
			}
			if deadline > time.Duration(u.TimeoutSeconds)*time.Second {
				return gh.Fail(2, "usage")
			}
			for _, cmd := range inv.Commands {
				if _, e = prepareCommand(origins[inv.Unit], machine, cmd, a.offline, policy); e != nil {
					return e
				}
			}
			if slices.Contains([]string{"unit", "integration", "e2e", "visual"}, inv.Check) && len(u.Checks[inv.Check].Reports) == 0 {
				return gh.Fail(2, "configuration")
			}
		}
		resultRoot, e := config.SafePath(root, cfg.ResultsDir)
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		if _, e = gh.Git(a.ctx, root, 1024, "check-ignore", "--", cfg.ResultsDir); e != nil {
			return gh.Fail(3, "policy_refused")
		}
		if e = gh.EnsurePrivateDirectory(root, resultRoot); e != nil {
			return e
		}
		id, e := protocol.NewUUID()
		if e != nil {
			return gh.Fail(6, "capture_failed")
		}
		logRoot := filepath.Join(resultRoot, id)
		if e = process.MakePrivateDir(logRoot); e != nil {
			return gh.Fail(3, "policy_refused")
		}
		a.result.Local.LogDir = &logRoot
		active, e := process.CreatePrivateFile(filepath.Join(logRoot, "active"))
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		if e = active.Close(); e != nil {
			return gh.Fail(3, "policy_refused")
		}
		defer os.Remove(filepath.Join(logRoot, "active"))
		dirty, e := a.compositionDirty(origins)
		if e != nil {
			return e
		}
		a.result.Local.Dirty = dirty

		completed := []string{}
		selectedIDs := []string{}
		for _, inv := range selected {
			selectedIDs = append(selectedIDs, inv.ID)
		}
		a.result.Local.Units = unique(selectedIDs)
		for i, inv := range selected {
			u := byID[inv.Unit]
			limit := time.Duration(u.TimeoutSeconds) * time.Second
			if deadline > 0 {
				limit = deadline
			}
			unitctx, cancel := context.WithTimeout(a.ctx, limit)
			if e = archiveReports(a.ctx, origins[inv.Unit], logRoot, u.Checks[inv.Check].Reports, i); e != nil {
				cancel()
				return e
			}
			started := time.Now()
			for j, command := range inv.Commands {
				argv, e := prepareCommand(origins[inv.Unit], machine, command, a.offline, policy)
				if e != nil {
					cancel()
					return e
				}
				env := map[string]string{"PATH": trustedPath(machine), "CARGO_NET_OFFLINE": "true", "npm_config_offline": "true", "npm_config_update_notifier": "false", "NPM_CONFIG_AUDIT": "false", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD": "1", "RUSTUP_AUTO_INSTALL": "0"}
				if a.offline {
					env["CARGO_NET_OFFLINE"] = "true"
					env["npm_config_offline"] = "true"
					env["npm_config_update_notifier"] = "false"
					env["NPM_CONFIG_AUDIT"] = "false"
					env["PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD"] = "1"
				}
				bound := limit
				if command.TimeoutSeconds > 0 && time.Duration(command.TimeoutSeconds)*time.Second < bound {
					bound = time.Duration(command.TimeoutSeconds) * time.Second
				}
				res, e := process.Run(unitctx, process.Spec{Root: origins[inv.Unit], Cwd: command.Cwd, Argv: argv, ResultDir: filepath.Join(logRoot, fmt.Sprintf("%04d-%04d", i, j)), Env: env, Timeout: bound})
				a.result.Local.ChildExit = protocol.Ptr(res.ExitCode)
				if res.Signal != "" {
					a.result.Local.ChildSignal = &res.Signal
				}
				if !res.CleanupComplete {
					a.result.Stages = append(a.result.Stages, protocol.Stage{Slot: "cleanup", Platform: protocol.Ptr(inv.Platform), State: "failed", FailureClass: protocol.Ptr("cleanup")})
				}
				if failure := localProcessError(res, e); failure != nil {
					cancel()
					return failure
				}
			}
			cancel()
			if e = validateLocalReports(origins[inv.Unit], u, inv, started); e != nil {
				return e
			}
			completed = append(completed, inv.ID)
		}
		a.result.State = "passed"
		a.result.Local.Data = map[string]any{"selected_units": unique(selectedIDs), "completed_units": unique(completed)}
		return nil
	}}
	c.Flags().StringArrayVar(&opts.Platforms, "platform", nil, "Select host platform")
	c.Flags().DurationVar(&deadline, "deadline", 0, "Shorten the unit deadline")
	c.Flags().BoolVar(&opts.Quick, "quick", false, "Select related feedback tests")
	c.Flags().StringVar(&opts.Base, "base", "", "Exact quick-mode base commit")
	c.Flags().StringVar(&opts.SHA, "sha", "", "Exact quick-mode source commit")
	c.Flags().StringArrayVar(&opts.Variants, "variant", nil, "Select a declared AXIS=VALUE")
	c.Flags().StringArrayVar(&opts.Checks, "check", nil, "Select a declared check family")
	c.Flags().StringVar(&opts.Shard, "shard", "", "Select an index/count shard")
	return c
}
func hostSupports(p string) bool {
	switch runtime.GOOS {
	case "darwin":
		return p == "macos-"+map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	case "windows":
		return p == "windows-"+map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	case "linux":
		if p == "web-linux" {
			return runtime.GOARCH == "amd64"
		}
		if strings.HasPrefix(p, "server-linux-") {
			return p == "server-linux-"+map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
		}
		if runtime.GOARCH != "amd64" {
			return false
		}
		b, e := os.ReadFile("/etc/os-release")
		if e != nil {
			return false
		}
		s := string(b)
		if p == "linux-arch" {
			return strings.Contains(s, "ID=arch")
		}
		return p == "linux-debian" && (strings.Contains(s, "ID=debian") || strings.Contains(s, "ID=ubuntu") || strings.Contains(s, `ID="ubuntu"`))
	}
	return false
}
func trustedPath(m config.Machine) string {
	dirs := []string{}
	for _, p := range m.Tools {
		dirs = append(dirs, filepath.Dir(p))
	}
	return strings.Join(unique(dirs), string(os.PathListSeparator))
}
func prepareCommand(root string, m config.Machine, command catalog.Command, offline bool, policy string) ([]string, error) {
	if len(command.Argv) == 0 {
		return nil, gh.Fail(2, "configuration")
	}
	argv := append([]string{}, command.Argv...)
	name := filepath.Base(argv[0])
	name = strings.TrimSuffix(name, ".exe")
	p, ok := m.Tools[name]
	if !ok || filepath.IsAbs(argv[0]) && argv[0] != p {
		return nil, gh.Fail(3, "policy_refused")
	}
	if !gh.TrustedToolPath(p, root) {
		return nil, gh.Fail(3, "policy_refused")
	}
	argv[0] = p
	if _, e := config.SafePath(root, command.Cwd); e != nil {
		return nil, gh.Fail(3, "policy_refused")
	}
	for _, a := range argv {
		if strings.ContainsRune(a, 0) {
			return nil, gh.Fail(2, "configuration")
		}
	}
	if policy == "managed-owner-mac" {
		if name != "cargo" || len(argv) < 2 || !slices.Contains([]string{"check", "clippy", "test"}, argv[1]) || !slices.Contains(argv, "--locked") {
			return nil, gh.Fail(3, "policy_refused")
		}
	}
	switch name {
	case "cargo":
		if e := localCargoPolicy(argv); e != nil {
			return nil, e
		}
	case "bash", "sh", "zsh":
		if len(argv) < 2 || strings.HasPrefix(argv[1], "-") {
			return nil, gh.Fail(3, "policy_refused")
		}
		if !reviewedScript(root, command.Cwd, argv[1]) {
			return nil, gh.Fail(3, "policy_refused")
		}
	case "pwsh", "powershell":
		if len(argv) < 4 || argv[1] != "-NoProfile" || argv[2] != "-File" || !reviewedScript(root, command.Cwd, argv[3]) {
			return nil, gh.Fail(3, "policy_refused")
		}

	case "npm":
		if len(argv) < 2 {
			return nil, gh.Fail(3, "policy_refused")
		}
		if slices.Contains([]string{"install", "ci", "update", "publish", "exec", "npx", "uninstall", "remove", "rm", "add", "i", "link", "unlink", "dedupe", "prune", "rebuild", "reinstall", "install-test", "install-ci-test"}, argv[1]) {
			return nil, gh.Fail(3, "policy_refused")
		}

	}
	if offline {
		switch name {
		case "cargo":
			if !slices.Contains(argv, "--locked") {
				return nil, gh.Fail(3, "policy_refused")
			}
			if !slices.Contains(argv, "--offline") {
				argv = cargoFlagBeforeSeparator(argv, "--offline")
			}
		case "npm", "node", "playwright":
		case "git", "curl", "wget":
			return nil, gh.Fail(3, "offline_refused")
		}
	}
	if runtime.GOOS == "windows" && name == "npm" {
		node, ok := m.Tools["node"]
		script, ok2 := m.Tools["npm-cli"]
		if !ok || !ok2 || !gh.TrustedToolPath(node, root) || !gh.TrustedToolPath(script, root) || !strings.HasSuffix(strings.ToLower(script), "npm-cli.js") {
			return nil, gh.Fail(3, "policy_refused")
		}
		argv = append([]string{node, script}, argv[1:]...)
	}

	return argv, nil
}
func validateLocalReports(root string, u catalog.Unit, inv planner.Invocation, started time.Time) error {
	check := u.Checks[inv.Check]
	if len(check.Reports) == 0 {
		return nil
	}
	tests := inv.Tests
	if len(tests) == 0 {
		tests = check.Census
	}
	shard, shards := 1, 1
	if inv.Shard != "" {
		if _, e := fmt.Sscanf(inv.Shard, "%d/%d", &shard, &shards); e != nil || check.Shards == nil || shard < 1 || shard > len(check.Shards.Assignments) || shards != check.Shards.Count || inv.Shard != fmt.Sprintf("%d/%d", shard, shards) {
			return gh.Fail(6, "missing_evidence")
		}
		assignment := check.Shards.Assignments[shard-1]
		if len(inv.Tests) == 0 {
			tests = assignment
		}
		for _, test := range tests {
			if !slices.Contains(assignment, test) {
				return gh.Fail(6, "missing_evidence")
			}
		}
	}
	reports := []collector.Report{}
	assertions := []string{}
	for _, a := range check.Assertions {
		if slices.Contains(tests, a) {
			assertions = append(assertions, a)
		}
	}
	expect := collector.ReportExpectation{Unit: inv.Unit, Check: inv.Check, Platform: inv.Platform, Variant: inv.Variant, Shard: shard, Shards: shards, Assertions: assertions, Tests: tests, Reports: check.Reports}
	for _, rel := range check.Reports {
		p, e := config.SafePath(root, rel)
		if e != nil {
			return gh.Fail(6, "missing_evidence")
		}
		st, e := os.Lstat(p)
		if e != nil || !st.Mode().IsRegular() || st.Size() > collector.MaxReport || st.ModTime().Before(started) {
			return gh.Fail(6, "missing_evidence")
		}
		f, e := os.Open(p)
		if e != nil {
			return gh.Fail(6, "missing_evidence")
		}
		b, e := io.ReadAll(io.LimitReader(f, collector.MaxReport+1))
		f.Close()
		if e != nil {
			return gh.Fail(6, "missing_evidence")
		}
		reports = append(reports, collector.Report{Unit: inv.Unit, Check: inv.Check, Platform: inv.Platform, Variant: inv.Variant, Shard: shard, Shards: shards, Path: rel, Format: "junit", Data: b, Assertions: func() []string {
			actual, _ := collector.PassedTests(b)
			out := []string{}
			for _, a := range assertions {
				if slices.Contains(actual, a) {
					out = append(out, a)
				}
			}
			return out
		}()})
	}
	if collector.ValidateReports(collector.Census{Entries: []collector.ReportExpectation{expect}, Feedback: true}, reports) != nil {
		return gh.Fail(6, "missing_evidence")
	}
	return nil
}

func archiveReports(ctx context.Context, root, logs string, reports []string, index int) error {
	for i, rel := range reports {
		p, e := config.SafePath(root, rel)
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || !st.Mode().IsRegular() || st.Size() > collector.MaxReport {
			return gh.Fail(3, "policy_refused")
		}
		if _, e = gh.Git(ctx, root, 1024, "check-ignore", "--", rel); e != nil {
			return gh.Fail(3, "policy_refused")
		}
		src, e := os.Open(p)
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		dst, e := process.CreatePrivateFile(filepath.Join(logs, fmt.Sprintf("previous-%04d-%04d.xml", index, i)))
		if e != nil {
			src.Close()
			return gh.Fail(3, "policy_refused")
		}
		_, e = io.Copy(dst, io.LimitReader(src, collector.MaxReport+1))
		src.Close()
		ce := dst.Close()
		if e != nil || ce != nil {
			return gh.Fail(3, "policy_refused")
		}
		if os.Remove(p) != nil {
			return gh.Fail(3, "policy_refused")
		}
	}
	return nil
}

func reviewedScript(root, cwd, script string) bool {
	p := script
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, cwd, p)
	}
	rel, e := filepath.Rel(root, p)
	if e != nil {
		return false
	}
	p, e = config.SafePath(root, filepath.ToSlash(rel))
	if e != nil {
		return false
	}
	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() {
		return false
	}
	_, e = gh.Git(context.Background(), root, 1024, "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel))
	return e == nil
}

func localCargoPolicy(argv []string) error {
	if len(argv) < 2 {
		return gh.Fail(3, "policy_refused")
	}
	op := argv[1]
	if op == "fmt" || op == "--version" || op == "-V" || op == "version" || op == "locate-project" {
		return nil
	}
	if !slices.Contains([]string{"check", "clippy", "test", "build", "doc", "nextest", "metadata", "tree", "rustc", "rustdoc"}, op) {
		return gh.Fail(3, "policy_refused")
	}
	if op == "nextest" && (len(argv) < 3 || !slices.Contains([]string{"run", "list"}, argv[2])) {
		return gh.Fail(3, "policy_refused")
	}
	for _, arg := range argv[2:] {
		if arg == "--" {
			break
		}
		if arg == "--locked" {
			return nil
		}
	}
	return gh.Fail(3, "policy_refused")
}
func cargoFlagBeforeSeparator(argv []string, flag string) []string {
	out := make([]string, 0, len(argv)+1)
	for i, arg := range argv {
		if arg == "--" {
			out = append(out, argv[:i]...)
			out = append(out, flag)
			return append(out, argv[i:]...)
		}
	}
	out = append(out, argv...)
	return append(out, flag)
}

func localProcessError(result process.Result, err error) error {
	failed := result.ExitCode > 0 || result.Signal != ""
	if result.ExitObserved && failed {
		return gh.Fail(1, "check_failed")
	}
	if result.TimedOut {
		return gh.Fail(4, "deadline")
	}
	if result.Interrupted {
		return gh.Fail(7, "interrupted")
	}
	if result.Overflow {
		return gh.Fail(6, "capture_failed")
	}
	if failed || !result.CleanupComplete {
		return gh.Fail(1, "check_failed")
	}
	if err != nil {
		return gh.Fail(6, "capture_failed")
	}
	return nil
}

func uniqueRootPaths(origins map[string]string) []string {
	paths := []string{}
	for _, path := range origins {
		paths = append(paths, path)
	}
	return unique(paths)
}
