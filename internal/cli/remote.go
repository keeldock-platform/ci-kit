package cli

import (
	"context"
	"errors"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/diagnostics"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"github.com/spf13/cobra"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (a *app) dispatch() *cobra.Command {
	var concern, id string
	var opts planner.Options
	var admission bool
	c := &cobra.Command{Use: "dispatch", Short: "Request one registered feedback or admission run", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		a.begin("dispatch", true)
		opts.Context = a.ctx
		opts.Platforms = unique(opts.Platforms)
		a.result.Operation = &protocol.Operation{}
		if a.offline {
			return gh.Fail(3, "offline_refused")
		}
		if !slices.Contains([]string{"affected", "full", "candidate"}, opts.Mode) || !config.Alias(concern) || !protocol.SHA(opts.Base) || !protocol.SHA(opts.SHA) || len(opts.Units) == 0 && !c.Flags().Changed("mode") || len(opts.Units) > 0 && c.Flags().Changed("mode") || admission && (len(opts.Units) > 0 || len(opts.Platforms) > 0 || len(opts.Variants) > 0 || len(opts.Checks) > 0 || opts.Shard != "" || opts.Quick) || opts.Quick && (opts.Mode == "full" || opts.Mode == "candidate") {
			return gh.Fail(2, "usage")
		}
		if id == "" {
			var e error
			id, e = protocol.NewUUID()
			if e != nil {
				return gh.Fail(6, "missing_evidence")
			}
		}
		if !protocol.UUID(id) {
			return gh.Fail(2, "usage")
		}
		a.result.Identity.RequestID = &id
		root, cfg, e := a.repository()
		if e != nil {
			return e
		}
		if cfg.Concern != concern {
			return gh.Fail(3, "policy_refused")
		}
		route, e := gh.LoadRoute(concern)
		if e != nil {
			return e
		}
		reg, e := gh.BoundRegistration(a.ctx, root, opts.Base, concern, route)
		if e != nil {
			return e
		}
		opts.Admission = admission
		opts.Qualified = reg.Qualified
		if admission && !reg.Qualified {
			opts.Mode = "full"
		}
		mode := opts.Mode
		if len(opts.Units) > 0 {
			mode = "unit"
		}
		if opts.Quick {
			mode = "quick"
		}
		if !slices.Contains([]string{"unit", "quick", "affected", "full", "candidate"}, mode) || !validPlatforms(opts.Platforms) {
			return gh.Fail(2, "usage")
		}
		if a.configPath != "" {
			selected := a.configPath
			var e error
			if filepath.IsAbs(a.configPath) {
				selected, e = filepath.Rel(root, a.configPath)
			}
			if e != nil || filepath.ToSlash(filepath.Clean(selected)) != ".kd-ci.json" {
				return gh.Fail(3, "policy_refused")
			}
		}
		sourceConfig, e := a.committedConfiguration(root, opts.SHA)
		if e != nil {
			return e
		}
		if sourceConfig.Concern != concern {
			return gh.Fail(3, "policy_refused")
		}
		units, e := a.committedCatalogue(root, opts.SHA, sourceConfig)
		if e != nil {
			return e
		}
		shared, e := a.committedShared(root, opts.SHA, sourceConfig.SharedUnits)
		if e != nil {
			return e
		}
		if len(opts.Units) > 0 {
			opts.Units, e = catalog.ExpandAliases(units, shared, opts.Units)
			if e != nil {
				return gh.Fail(2, "usage")
			}
		}

		if machine, e := config.LoadMachine(); e == nil {
			opts.ToolDirectory = machine.PlannerDirectory
			opts.Executables = machine.Tools
		}
		selector := "all"
		platforms := "all"
		if !admission {
			plan, e := a.feedbackSelection(root, opts.SHA, sourceConfig, units, opts)
			if e != nil {
				return e
			}
			selected := []catalog.Entry{}
			selectedPlatforms := []string{}
			for _, inv := range plan.Selected {
				selectedPlatforms = append(selectedPlatforms, inv.Platform)
				selected = append(selected, catalog.Entry{ID: inv.ID, Unit: inv.Unit, Check: inv.Check, Platform: inv.Platform, Variant: inv.Variant, Shard: inv.Shard})
			}
			selectedPlatforms = unique(selectedPlatforms)
			if len(selectedPlatforms) == 0 {
				return gh.Fail(2, "configuration")
			}
			for _, p := range selectedPlatforms {
				if !slices.Contains(reg.Platforms, p) {
					return gh.Fail(3, "policy_refused")
				}
			}
			for _, p := range opts.Platforms {
				if !slices.Contains(selectedPlatforms, p) {
					return gh.Fail(2, "usage")
				}
			}
			platforms = strings.Join(selectedPlatforms, ",")
			restricted := len(opts.Units) > 0 || len(opts.Variants) > 0 || len(opts.Checks) > 0 || opts.Shard != ""
			if mode == "affected" || mode == "quick" || mode == "unit" || restricted {
				selector, e = catalog.Selector(units, selected)
				if e != nil {
					return gh.Fail(2, "selection_too_large")
				}
			}
		}

		purpose := "feedback"
		if admission {
			purpose = "admission"
		}
		payload, e := protocol.NewDispatch(concern, opts.SHA, opts.Base, mode, purpose, platforms, selector, id)
		if e != nil {
			return gh.Fail(2, "usage")
		}
		composition, e := a.committedComposition(root, opts.SHA, sourceConfig)
		if e != nil {
			return e
		}
		client, e := a.client(root)
		if e != nil {
			return e
		}
		if e = client.Preflight(a.ctx, reg, root, opts.SHA, opts.Base); e != nil {
			return e
		}
		dir, e := a.stateDirectory(root, cfg.ResultsDir, true)
		if e != nil {
			return e
		}
		if e = gh.SavePending(dir, reg, payload, composition); e != nil {
			return e
		}
		x, run, e := client.DispatchRun(a.ctx, reg, payload, composition)
		a.result.Identity = gh.ResultIdentity(x, run)
		a.result.Identity.RequestID = &id
		if protocol.Numeric(x.RunID) {
			a.result.Operation.DispatchAccepted = true
		}
		if e != nil {
			return e
		}
		a.result.Operation.DispatchAccepted = true
		a.result.State = "queued"
		if run.Status == "in_progress" {
			a.result.State = "running"
		}
		if e = gh.SaveExpectation(dir, x); e != nil {
			return e
		}
		return nil
	}}
	c.Flags().StringVar(&concern, "concern", "", "Approved concern alias")
	c.Flags().StringVar(&opts.SHA, "sha", "", "Exact source commit")
	c.Flags().StringVar(&opts.Base, "base", "", "Expected admitted base")
	c.Flags().StringArrayVar(&opts.Units, "unit", nil, "Select a feedback unit")
	c.Flags().StringVar(&opts.Mode, "mode", "affected", "affected, full or candidate")
	c.Flags().StringArrayVar(&opts.Platforms, "platform", nil, "Select a feedback platform")
	c.Flags().BoolVar(&admission, "admission", false, "Request complete admission coverage")
	c.Flags().StringVar(&id, "request-id", "", "Existing request UUIDv4 for reconciliation")
	c.Flags().BoolVar(&opts.Quick, "quick", false, "Run related feedback tests")
	c.Flags().StringArrayVar(&opts.Variants, "variant", nil, "Select a declared AXIS=VALUE")
	c.Flags().StringArrayVar(&opts.Checks, "check", nil, "Select a feedback check")
	c.Flags().StringVar(&opts.Shard, "shard", "", "Select a feedback index/count shard")
	return c
}
func (a *app) remote(name string) *cobra.Command {
	var concern, privateReport, privateOutput, privateBundle, diagnosticKey, experimentReceipt string
	var attempt int
	var deadline time.Duration
	def := 2 * time.Hour
	if name == "cancel" {
		def = 5 * time.Minute
	}
	c := &cobra.Command{Use: name + " RUN_ID", Short: "Observe a registered run tuple", Args: func(c *cobra.Command, args []string) error {
		if name == "failures" && experimentReceipt != "" {
			return cobra.ExactArgs(1)(c, args)
		}
		if name == "failures" && (privateReport != "" || privateBundle != "" || privateOutput != "" && len(args) == 0) {
			return cobra.NoArgs(c, args)
		}
		return cobra.ExactArgs(1)(c, args)
	}, RunE: func(c *cobra.Command, args []string) error {
		if name == "failures" && experimentReceipt != "" {
			if concern != "" || privateBundle != "" || privateReport != "" {
				return gh.Fail(2, "usage")
			}
			return a.experimentFailureFiles(args[0], experimentReceipt, diagnosticKey, privateOutput, attempt)
		}
		if name == "failures" && (privateReport != "" || privateBundle != "" || privateOutput != "" && len(args) == 0) {
			if concern != "" || attempt != 0 {
				return gh.Fail(2, "usage")
			}
			if privateBundle != "" {
				if privateReport != "" {
					return gh.Fail(2, "usage")
				}
				return a.privateBundle(privateBundle, diagnosticKey, privateOutput)
			}
			return a.privateFailures(privateReport, privateOutput)
		}
		a.begin(name, true)
		if name == "cancel" {
			a.result.Operation = &protocol.Operation{}
		}
		if a.offline {
			return gh.Fail(3, "offline_refused")
		}
		if !config.Alias(concern) || !protocol.Numeric(args[0]) || attempt < 0 || c.Flags().Changed("attempt") && attempt == 0 {
			return gh.Fail(2, "usage")
		}
		if name == "wait" && (deadline <= 0 || deadline > 6*time.Hour) || name == "cancel" && (deadline <= 0 || deadline > 5*time.Minute) {
			return gh.Fail(2, "usage")
		}
		root, cfg, e := a.repository()
		if e != nil {
			return e
		}
		if cfg.Concern != concern {
			return gh.Fail(3, "policy_refused")
		}
		route, e := gh.LoadRoute(concern)
		if e != nil {
			return e
		}
		a.result.Identity.RunID = protocol.Ptr(args[0])
		a.result.Identity.Concern = protocol.Ptr(concern)
		dir, e := a.stateDirectory(root, cfg.ResultsDir, false)
		if e != nil {
			return e
		}
		x, e := gh.ReadExpectation(dir, args[0])
		if e != nil {
			return e
		}
		selectedConfig, e := a.committedConfiguration(root, x.Dispatch.Inputs["source_sha"])
		if e != nil {
			return e
		}
		composition, e := a.committedComposition(root, x.Dispatch.Inputs["source_sha"], selectedConfig)
		if e != nil || composition != x.CompositionSHA256 {
			return gh.Fail(3, "policy_refused")
		}
		bound, e := gh.BoundRegistration(a.ctx, root, x.Dispatch.Inputs["base_sha"], concern, route)
		if e != nil || !reflect.DeepEqual(bound, x.Registration) {
			return gh.Fail(3, "policy_refused")
		}
		if attempt > 0 && attempt != x.Attempt {
			return gh.Fail(3, "policy_refused")
		}
		client, e := a.client(root)
		if e != nil {
			return e
		}
		if e = client.VerifyActor(a.ctx, x.Registration.Caller.Actor); e != nil {
			return e
		}
		ctx := a.ctx
		cancel := func() {}
		if name == "wait" || name == "cancel" {
			ctx, cancel = context.WithTimeout(ctx, deadline)
		}
		defer cancel()
		var r protocol.Result
		switch name {
		case "status", "failures":
			r, e = client.Status(ctx, x, name, Version)
		case "wait":
			r, e = client.Wait(ctx, x, name, Version)
		case "cancel":
			record, recordErr := gh.CancellationRecorder(dir, x)
			if recordErr != nil {
				return recordErr
			}
			r, e = client.Cancel(ctx, x, Version, record)
		}
		a.result = r
		if name == "failures" && e == nil && r.ExitCode == 1 {
			if !diagnosticsLocal() {
				return gh.Fail(3, "policy_refused")
			}
			failed := []string{}
			for _, stage := range r.Stages {
				if stage.State == "failed" && (stage.Slot == "checks" || stage.Slot == "plan") {
					slot := stage.Slot
					if stage.Platform != nil {
						slot = *stage.Platform
					}
					failed = append(failed, slot)
				}
			}
			output := privateOutput
			if output == "" {
				output = filepath.Join(dir, "failures-"+x.RunID+"-"+strconv.Itoa(x.Attempt)+".json")
			}
			expected := diagnostics.Identity{Run: x.RunID, Attempt: strconv.Itoa(x.Attempt), KitSHA: x.Registration.KitSHA, SourceSHA: x.Dispatch.Inputs["source_sha"]}
			err := a.diagnosticReports(output, diagnosticKey, &expected, func(visit func(string, []byte) error) error {
				return client.WalkDiagnosticArtifacts(ctx, x, failed, visit)
			})
			if err != nil {
				return err
			}

		}
		if e != nil && errors.Is(ctx.Err(), context.Canceled) {
			return gh.Fail(7, "interrupted")
		}
		return e
	}}
	c.Flags().StringVar(&concern, "concern", "", "Approved concern alias")
	if name == "failures" {
		c.Flags().StringVar(&experimentReceipt, "experiment-receipt", "", "Read-only synthetic diagnostic receipt (never dispatches)")
		c.Flags().StringVar(&privateBundle, "private-bundle", "", "Decrypt a recorded local age failure fixture")
		c.Flags().StringVar(&diagnosticKey, "identity", "", "Coordinator age identity (local only)")
		c.Flags().StringVar(&privateReport, "private-report", "", "Read a local user-only JUnit report")
		c.Flags().StringVar(&privateOutput, "private-output", "", "Create a user-only file listing failed names and errors")
	}
	c.Flags().IntVar(&attempt, "attempt", 0, "Pin an explicit run attempt")
	if name == "wait" || name == "cancel" {
		c.Flags().DurationVar(&deadline, "deadline", def, "Maximum observation duration")
	}
	return c
}
