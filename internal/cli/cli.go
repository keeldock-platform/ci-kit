// Package cli composes validated configuration, planning and provider operations.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"io"
	"os"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"github.com/keeldock-platform/ci-kit/schemas"
	"github.com/spf13/cobra"
)

var Version = "0.1.0-dev"
var Commit = "0000000000000000000000000000000000000000"

type app struct {
	ctx                       context.Context
	out, err                  io.Writer
	json, offline, noProgress bool
	color, configPath         string
	httpTimeout               time.Duration
	result                    protocol.Result
	root                      *cobra.Command
}

// Execute handles every normal error through one fixed, schema-validated result.
func Execute(ctx context.Context, args []string, out, errout io.Writer) int {
	a := &app{ctx: ctx, out: out, err: errout, color: "auto", httpTimeout: 30 * time.Second}
	a.root = a.command()
	a.root.SetArgs(args)
	a.root.SetOut(out)
	a.root.SetErr(errout)
	a.json = jsonRequested(args)

	a.result = protocol.New("invalid", Version, false)
	if duplicateScalars(args) || a.json && textRequested(args) {
		a.result.Fail(2, "usage")
		return a.write()
	}
	if e := a.root.ExecuteContext(ctx); e != nil {
		a.json = jsonRequested(args)
		var f *gh.Failure
		if errors.As(e, &f) {
			if ctx.Err() == context.Canceled && f.Code == "deadline" {
				f = &gh.Failure{Exit: 7, Code: "interrupted"}
			}
			a.result.Fail(f.Exit, f.Code)
		} else {
			a.result.Fail(2, "usage")
		}
		return a.write()
	}
	if a.result.Command == "text" {
		return 0
	}
	return a.write()
}
func takesFlagValue(name string) bool {
	switch name {
	case "config", "color", "http-timeout", "base", "sha", "unit", "platform", "mode", "deadline", "variant", "shard", "check", "concern", "request-id", "attempt", "older-than", "experiment-receipt", "private-bundle", "identity", "private-report", "private-output":
		return true
	}
	return false
}
func jsonRequested(args []string) bool {
	requested := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, value, assigned := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if strings.HasPrefix(arg, "--") && takesFlagValue(name) && !assigned {
			i++
			continue
		}
		if arg == "--json" {
			requested = true
		} else if strings.HasPrefix(arg, "--json=") {
			on, e := strconv.ParseBool(value)
			requested = e != nil || on
		}
	}
	return requested
}
func textRequested(args []string) bool {
	command := false
	for i := 0; i < len(args); i++ {
		s := args[i]
		name, _, assigned := strings.Cut(strings.TrimPrefix(s, "--"), "=")
		if strings.HasPrefix(s, "--") && takesFlagValue(name) && !assigned {
			i++
			continue
		}
		if s == "--help" || s == "-h" {
			return true
		}
		if strings.HasPrefix(s, "--help=") || strings.HasPrefix(s, "-h=") {
			_, value, _ := strings.Cut(s, "=")
			on, e := strconv.ParseBool(value)
			if e != nil || on {
				return true
			}
			continue
		}
		if !strings.HasPrefix(s, "-") && !command {
			command = true
			if s == "help" || s == "completion" || s == "__complete" || s == "__completeNoDesc" {
				return true
			}
		}
	}
	return false
}
func duplicateScalars(args []string) bool {
	seen := map[string]bool{}
	repeat := map[string]bool{"unit": true, "platform": true, "variant": true, "check": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if len(arg) > 2 && strings.HasPrefix(arg, "-hh") && strings.Trim(arg[1:], "h") == "" {
			return true
		}
		if arg == "-h" || strings.HasPrefix(arg, "-h=") {
			if seen["help"] {
				return true
			}
			seen["help"] = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, _, assigned := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !repeat[name] {
			if seen[name] {
				return true
			}
			seen[name] = true
		}
		if takesFlagValue(name) && !assigned {
			i++
		}
	}
	return false
}

func (a *app) write() int {
	if a.result.Error != nil {
		a.result.Error.Message = protocol.Message(a.result.Error.Code)
	}
	if a.result.ValidateSemantics() != nil {
		cmd := a.result.Command
		hosted := a.result.Scope == "hosted"
		a.result = protocol.New(cmd, Version, hosted)
		a.result.Fail(6, "missing_evidence")
	}
	b, e := a.result.Bytes()
	if len(b) > 16<<20 {
		a.result = protocol.New(a.result.Command, Version, a.result.Scope == "hosted")
		a.result.Fail(2, "configuration")
		b, _ = a.result.Bytes()
	}
	if e == nil {
		var v any
		e = json.Unmarshal(b, &v)
		if e == nil {
			e = schemas.Validate("urn:keeldock:kd-ci:result:1", v)
		}
	}
	if e == nil && a.result.Local != nil {
		var v any
		b, _ := json.Marshal(a.result.Local.Data)
		json.Unmarshal(b, &v)
		if a.result.OK && a.result.Command != "invalid" {
			name := a.result.Command
			if name == "results purge" {
				name = "purge"
			}
			e = schemas.Validate("urn:keeldock:kd-ci:local-data:1#/$defs/"+name, v)
		} else if string(b) != "{}" {
			e = errors.New("invalid local data")
		}
	}
	if e != nil {
		a.result = protocol.New("invalid", Version, false)
		a.result.Fail(6, "missing_evidence")
		b, _ = a.result.Bytes()
	}
	if a.json {
		fmt.Fprintln(a.out, string(b))
	} else {
		a.writeHuman()
	}
	return a.result.ExitCode
}
func (a *app) command() *cobra.Command {
	root := &cobra.Command{Use: "kd-ci", Short: "Validate, plan and observe checks", SilenceErrors: true, SilenceUsage: true, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return gh.Fail(2, "usage") }, CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true}}
	root.PersistentFlags().StringVar(&a.configPath, "config", "", "Repository configuration path")
	root.PersistentFlags().BoolVar(&a.json, "json", false, "Emit one JSON result")
	root.PersistentFlags().BoolVar(&a.noProgress, "no-progress", false, "Disable progress")
	root.PersistentFlags().StringVar(&a.color, "color", "auto", "Colour: auto or never")
	root.PersistentFlags().BoolVar(&a.offline, "offline", false, "Refuse network operations")
	root.PersistentFlags().DurationVar(&a.httpTimeout, "http-timeout", 30*time.Second, "Request timeout (1s–120s)")
	root.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if a.color != "auto" && a.color != "never" || a.httpTimeout < time.Second || a.httpTimeout > 120*time.Second {
			return gh.Fail(2, "usage")
		}
		return nil
	}
	root.SetHelpFunc(func(c *cobra.Command, args []string) { a.result.Command = "text"; fmt.Fprint(a.out, c.UsageString()) })
	root.AddCommand(a.version(), a.doctor(), a.list(), a.explain(), a.run(), a.dispatch(), a.remote("status"), a.remote("wait"), a.remote("failures"), a.remote("cancel"), a.results(), a.completion(root))
	return root
}
func (a *app) begin(name string, hosted bool) { a.result = protocol.New(name, Version, hosted) }
func (a *app) repository() (string, config.Repository, error) {
	cwd, e := os.Getwd()
	if e != nil {
		return "", config.Repository{}, gh.Fail(2, "configuration")
	}
	b, e := gh.Git(a.ctx, cwd, 1<<20, "rev-parse", "--show-toplevel")
	if e != nil {
		return "", config.Repository{}, gh.Fail(2, "configuration")
	}
	root := strings.TrimSpace(string(b))
	c, e := config.Load(root, a.configPath)
	if e != nil {
		return "", c, gh.Fail(2, "configuration")
	}
	u, e := config.LoadUser()
	if e != nil {
		return "", c, gh.Fail(2, "configuration")
	}
	if !c.RetentionConfigured && u.RetentionDays > 0 {
		c.RetentionDays = u.RetentionDays
	}
	if !a.root.PersistentFlags().Changed("color") && u.Color != "" {
		a.color = u.Color
	}
	if os.Getenv("NO_COLOR") != "" {
		a.color = "never"
	}
	if !a.root.PersistentFlags().Changed("no-progress") && u.Progress != nil {
		a.noProgress = !*u.Progress
	}
	return root, c, nil
}
func (a *app) version() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Show binary and schema versions", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		a.begin("version", false)
		a.result.Local.Data = map[string]any{"binary_version": Version, "commit": Commit, "go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "config_schema": 1, "unit_schema": 1, "result_schema": 1, "kit_api": 1, "production_qualified": false}
		return nil
	}}
}
func unique(ss []string) []string {
	out := append([]string{}, ss...)
	slices.Sort(out)
	return slices.Compact(out)
}
func validPlatforms(ps []string) bool {
	for _, p := range ps {
		if !slices.Contains(protocol.Platforms, p) {
			return false
		}
	}
	return true
}
func (a *app) list() *cobra.Command {
	var ps []string
	c := &cobra.Command{Use: "list", Short: "List validated local units", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		a.begin("list", false)
		if !validPlatforms(ps) {
			return gh.Fail(2, "usage")
		}
		root, cfg, e := a.repository()
		if e != nil {
			return e
		}
		units, _, shared, e := a.localCatalogue(root, cfg)
		if e != nil {
			return gh.Fail(2, "configuration")
		}
		entries, e := catalog.Entries(units)
		if e != nil {
			return gh.Fail(2, "configuration")
		}
		type item struct {
			ID        string   `json:"id"`
			Kind      string   `json:"kind"`
			Owner     string   `json:"owner"`
			Platforms []string `json:"platforms"`
			Members   []string `json:"members"`
		}
		items := []item{}
		for _, u := range units {
			members := []string{}
			platforms := []string{}
			for _, en := range entries {
				if en.Unit == u.ID && (len(ps) == 0 || slices.Contains(ps, en.Platform)) {
					members = append(members, en.ID)
					platforms = append(platforms, en.Platform)
				}
			}
			if len(members) > 0 {
				items = append(items, item{u.ID, "alias", u.Owner, unique(platforms), unique(members)})
				for _, id := range unique(members) {
					p := strings.Split(strings.Split(id, "@")[1], "#")[0]
					items = append(items, item{id, "unit", u.Owner, []string{p}, []string{}})
				}
			}
		}

		aliases, e := catalog.AggregateAliases(units, shared)
		if e != nil {
			return gh.Fail(2, "configuration")
		}
		total := 0
		for _, alias := range aliases {
			members := []string{}
			platforms := []string{}
			owners := []string{}
			for _, target := range alias.Units {
				for _, en := range entries {
					full := en.ID
					if en.Shard != "" {
						full += "#" + en.Shard
					}
					if en.Unit == target || en.ID == target || full == target {
						if len(ps) > 0 && !slices.Contains(ps, en.Platform) {
							continue
						}
						members = append(members, full)
						platforms = append(platforms, en.Platform)
						for _, u := range units {
							if u.ID == en.Unit {
								owners = append(owners, u.Owner)
							}
						}
					}
				}
			}
			members = unique(members)
			total += len(members)
			if total > 100000 {
				return gh.Fail(2, "configuration")
			}
			if len(members) > 0 {
				items = append(items, item{alias.ID, "alias", strings.Join(unique(owners), ", "), unique(platforms), members})
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		for _, it := range items {
			a.result.Local.Units = append(a.result.Local.Units, it.ID)
		}
		a.result.Local.Data = map[string]any{"units": items}
		return nil
	}}
	c.Flags().StringArrayVar(&ps, "platform", nil, "Filter by platform")
	return c
}
func (a *app) explain() *cobra.Command {
	var opts planner.Options
	c := &cobra.Command{Use: "explain", Short: "Explain exact-object affected selection", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		a.begin("explain", false)
		opts.Context = a.ctx
		opts.Platforms = unique(opts.Platforms)
		if !protocol.SHA(opts.Base) || !protocol.SHA(opts.SHA) || len(opts.Units) > 0 && opts.Mode != "affected" {
			return gh.Fail(2, "usage")
		}
		root, _, e := a.repository()
		if e != nil {
			return e
		}
		cfg, e := a.committedConfiguration(root, opts.SHA)
		if e != nil {
			return e
		}
		units, e := a.committedCatalogue(root, opts.SHA, cfg)
		if e != nil {
			return e
		}
		if m, e := config.LoadMachine(); e == nil {
			opts.ToolDirectory = m.PlannerDirectory
			opts.Executables = m.Tools
		}
		shared, e := a.committedShared(root, opts.SHA, cfg.SharedUnits)
		if e != nil {
			return e
		}
		if len(opts.Units) > 0 {
			opts.Units, e = catalog.ExpandAliases(units, shared, opts.Units)
			if e != nil {
				return gh.Fail(2, "usage")
			}
		}
		r, e := planner.Plan(root, units, opts)
		if e != nil {
			return selectionError(e)
		}
		selected := []string{}
		for _, u := range r.Selected {
			selected = append(selected, u.ID)
		}
		mode := opts.Mode
		if len(opts.Units) > 0 {
			mode = "unit"
		}
		a.result.Local.Units = unique(selected)
		a.result.Local.Explanation = unique(r.Reasons)
		a.result.Local.Data = map[string]any{"mode": mode, "seed_units": unique(r.Seeds), "selected_units": unique(selected), "full_fallback": r.FullFallback, "reasons": unique(r.Reasons), "graph_engine": "nx", "base_sha": opts.Base, "source_sha": opts.SHA}
		return nil
	}}
	c.Flags().StringVar(&opts.Base, "base", "", "Exact base commit")
	c.Flags().StringVar(&opts.SHA, "sha", "", "Exact source commit")
	c.Flags().StringArrayVar(&opts.Units, "unit", nil, "Select a feedback unit")
	c.Flags().StringArrayVar(&opts.Platforms, "platform", nil, "Select a feedback platform")
	c.Flags().StringVar(&opts.Mode, "mode", "affected", "affected, full or candidate")
	return c
}
func (a *app) completion(root *cobra.Command) *cobra.Command {
	return &cobra.Command{Use: "completion bash|zsh|fish|powershell", Short: "Print a shell completion script", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		a.result.Command = "text"
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(a.out)
		case "zsh":
			return root.GenZshCompletion(a.out)
		case "fish":
			return root.GenFishCompletion(a.out, true)
		case "powershell":
			return root.GenPowerShellCompletion(a.out)
		}
		a.result.Command = "invalid"
		return gh.Fail(2, "usage")
	}}
}
func (a *app) results() *cobra.Command {
	var older time.Duration
	root := &cobra.Command{Use: "results", Short: "Manage private local captures", RunE: func(*cobra.Command, []string) error { return gh.Fail(2, "usage") }}
	c := &cobra.Command{Use: "purge", Short: "Delete recognised inactive captures", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		a.begin("results purge", false)
		if !c.Flags().Changed("older-than") || older < 24*time.Hour {
			return gh.Fail(2, "usage")
		}
		root, cfg, e := a.repository()
		if e != nil {
			return e
		}
		if _, e = gh.Git(a.ctx, root, 1024, "check-ignore", "--", cfg.ResultsDir); e != nil {
			return gh.Fail(3, "policy_refused")
		}
		p, e := config.SafePath(root, cfg.ResultsDir)
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		deleted, e := purgeRuns(p, older, time.Now())
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		files, e := os.ReadDir(p)
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		a.result.Local.Data = map[string]any{"deleted_result_ids": deleted, "retained_unknown_count": len(files)}
		return nil
	}}
	c.Flags().DurationVar(&older, "older-than", 0, "Minimum capture age (at least 24h)")
	root.AddCommand(c)
	return root
}
func (a *app) doctor() *cobra.Command {
	var concern string
	c := &cobra.Command{Use: "doctor", Short: "Check prerequisites without modifying them", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		a.begin("doctor", false)
		root, cfg, e := a.repository()
		if e != nil {
			return e
		}
		if concern != "" && concern != cfg.Concern {
			return gh.Fail(3, "policy_refused")
		}
		m, e := config.LoadMachine()
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		if m.LocalPolicy == "managed-owner-mac" || cfg.LocalPolicy == "managed-owner-mac" {
			if !slices.Contains(m.AllowedLocalConcerns, cfg.Concern) || m.SourceRoots[cfg.Concern] != root {
				return gh.Fail(3, "policy_refused")
			}
		}
		resultPath, e := config.SafePath(root, cfg.ResultsDir)
		if e != nil {
			return gh.Fail(3, "policy_refused")
		}
		if _, e = os.Lstat(resultPath); e == nil && !process.UserOnly(resultPath, true) {
			return gh.Fail(3, "policy_refused")
		}
		if _, e = gh.Git(a.ctx, root, 1024, "check-ignore", "--", cfg.ResultsDir); e != nil {
			return gh.Fail(3, "policy_refused")
		}
		checks := []map[string]any{}
		names := []string{"git"}
		if !a.offline {
			names = append(names, "gh")
		}
		for _, name := range names {
			version, e := gh.ToolVersion(a.ctx, root, name)
			if e != nil {
				return e
			}
			checks = append(checks, map[string]any{"id": name, "status": "ready", "version": version, "reason": nil})
		}
		checks = append(checks, map[string]any{"id": "local_policy", "status": "ready", "version": nil, "reason": nil}, map[string]any{"id": "results_permissions", "status": "ready", "version": nil, "reason": nil})

		if !a.offline {
			client, e := a.client(root)
			if e != nil {
				return e
			}
			route, e := gh.LoadRoute(cfg.Concern)
			if e != nil {
				return e
			}
			var actor struct {
				Login string `json:"login"`
			}
			if _, e = client.Request(a.ctx, "GET", "user", nil, &actor); e != nil {
				return e
			}
			if actor.Login != route.Caller.Actor {
				return gh.Fail(3, "auth_refused")
			}
			for _, repo := range []gh.RegisteredRepository{route.Source, {Repository: route.Caller.Repository, ID: route.Caller.ID}} {
				var metadata struct {
					ID       int64  `json:"id"`
					FullName string `json:"full_name"`
					Fork     bool   `json:"fork"`
				}
				if _, e = client.Request(a.ctx, "GET", "repos/"+repo.Repository, nil, &metadata); e != nil {
					return e
				}
				if metadata.ID != repo.ID || metadata.FullName != repo.Repository || metadata.Fork {
					return gh.Fail(3, "policy_refused")
				}
			}
			var workflow struct {
				ID          int64 `json:"id"`
				Path, State string
			}
			if _, e = client.Request(a.ctx, "GET", "repos/"+route.Caller.Repository+"/actions/workflows/"+route.Caller.WorkflowID, nil, &workflow); e != nil {
				return e
			}
			if workflow.Path != route.Caller.WorkflowPath || workflow.State != "active" {
				return gh.Fail(3, "policy_refused")
			}
			checks = append(checks, map[string]any{"id": "caller_access", "status": "ready", "version": nil, "reason": nil})
		}
		checks = append(checks, map[string]any{"id": "revocation_freshness", "status": "ready", "version": nil, "reason": func() any {
			if a.offline {
				return "offline policy freshness is not refreshed"
			}
			return "current kit revocations are verified during dispatch"
		}()})
		a.result.Local.Data = map[string]any{"offline": a.offline, "checks": checks}
		return nil
	}}
	c.Flags().StringVar(&concern, "concern", "", "Approved concern alias")
	return c
}
func (a *app) client(root string) (*gh.Client, error) {
	if a.offline {
		return nil, gh.Fail(3, "offline_refused")
	}
	token, e := gh.Token(a.ctx, root, a.httpTimeout)
	if e != nil {
		return nil, e
	}
	return gh.New(token, a.httpTimeout, nil)
}
