package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/keeldock-platform/ci-kit/internal/acquire"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	provider "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
)

func execute(ctx context.Context) int {
	c, e := readControl(os.Getenv)
	if e != nil {
		return 3
	}

	switch os.Getenv("CI_KIT_OPERATION") {
	case "preflight":
		values := map[string]string{"environment": c.Concern.environment("plan")}
		for _, p := range protocol.AdmittedPlatforms {
			values["env-"+p] = c.Concern.environment(p)
		}
		if output(values) != nil {
			return 6
		}
		return 0
	case "plan", "pack":
		return privateWork(ctx, c)
	case "collect":
		return collect(ctx, c)
	}
	return 2
}
func privateWork(ctx context.Context, c control) (code int) {
	code = 6
	recipient, err := diagnosticRecipient()
	if err != nil {
		return 6
	}
	parent := os.Getenv("RUNNER_TEMP")
	if !filepath.IsAbs(parent) {
		return 3
	}
	private, e := os.MkdirTemp(parent, "ci-private-")
	if e != nil {
		return 6
	}
	defer func() {
		cleanupErr := os.RemoveAll(private)

		if cleanupErr != nil && code == 0 {
			code = 1
		}
	}()
	sink := filepath.Join(private, "diagnostics")
	if process.MakePrivateDir(sink) != nil {
		return 6
	}
	ctx = process.WithCaptureSink(ctx, sink)
	stage := "source map and tool resolution"
	defer func() {
		if code != 0 {
			process.RetainAttempt(ctx, process.Spec{Argv: []string{"ci-kit", stage}, Root: private, Cwd: "."}, fmt.Errorf("private stage failed with exit %d", code))
			if sealFailure(sink, recipient, c, code) == nil {
				if output(map[string]string{"diagnostics": "ready"}) != nil {
					fmt.Println("DIAGNOSTICS FAIL")
				}
			} else {
				fmt.Println("DIAGNOSTICS FAIL")
			}
		}
	}()
	raw := []byte(os.Getenv("SOURCE_MAP"))
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != c.Concern.SourceMapSHA256 {
		return 3
	}
	m, e := acquire.ParseMap(raw, c.Request.Inputs["concern"])
	if e != nil || os.Getenv("SOURCE_READ_01") == "" {
		return 3
	}
	git, e := exec.LookPath("git")
	if e != nil || !filepath.IsAbs(git) {
		return 2
	}
	helper, e := os.Executable()
	if e != nil {
		return 6
	}
	req := acquire.Request{Map: m, RootSHA: c.Request.Inputs["source_sha"], BaseSHA: c.Request.Inputs["base_sha"], Workspace: filepath.Join(private, "source"), Git: git, CredentialHelper: helper}
	req.VerifyRepository = func(ctx context.Context, s acquire.Source) (err error) {

		client, e := provider.New(os.Getenv(s.SecretSlot), 30*time.Second, nil)
		if e != nil {
			return refused
		}
		var meta struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
			Fork     bool   `json:"fork"`
		}
		status, requestErr := client.Request(ctx, "GET", "repos/"+s.Owner+"/"+s.Repository, nil, &meta)
		e = requestErr
		if e != nil || status != 200 {
			process.RetainAttempt(ctx, process.Spec{Argv: []string{"github-api", "GET", "repos/" + s.Owner + "/" + s.Repository}, Root: private, Cwd: "."}, fmt.Errorf("repository metadata request failed: HTTP %d: %v", status, e))
		}
		if e != nil || meta.ID != s.RepositoryID || meta.FullName != s.Owner+"/"+s.Repository || meta.Fork {
			return refused
		}
		return nil
	}
	req.VerifySignatures = func(ctx context.Context, root, sha, base string) (err error) {

		wd, e := os.Getwd()
		if e != nil {
			return refused
		}
		return provider.VerifySignaturesWithAuthority(ctx, root, sha, base, provider.SignerAuthority{KitRoot: filepath.Join(wd, "public-control"), KitSHA: c.Policy.KitSHA})
	}
	compositionDigest := ""
	sourceSHAs := map[string]string{m.Root: c.Request.Inputs["source_sha"]}
	req.ReadComposition = func(ctx context.Context, root string) ([]acquire.LockedSource, error) {
		cfg, e := config.Load(root, "")
		if e != nil || cfg.Concern != c.Request.Inputs["concern"] || cfg.KitAPI != 1 {
			return nil, refused
		}
		composition := config.SingleSourceComposition()

		if cfg.CompositionLock != "" {
			path, e := config.SafePath(root, cfg.CompositionLock)
			if e != nil {
				return nil, refused
			}
			raw, e := readRegular(path, 1<<20)
			if e != nil {
				return nil, refused
			}
			composition, e = config.DecodeComposition(raw)
			if e != nil {
				return nil, refused
			}
		}
		compositionDigest, e = config.CompositionDigest(composition)
		if e != nil {
			return nil, refused
		}
		result := make([]acquire.LockedSource, 0, len(composition.Sources))
		for _, source := range composition.Sources {
			sourceSHAs[source.Alias] = source.SHA
			result = append(result, acquire.LockedSource{Alias: source.Alias, SHA: source.SHA, BaseSHA: source.BaseSHA})
		}
		return result, nil
	}

	stage = "source acquisition"
	checkout, e := acquire.Acquire(ctx, req)
	if e != nil || compositionDigest == "" {
		return 3
	}
	defer func() {
		if checkout.Close() != nil && code == 0 {
			code = 1
		}
	}()
	for i := 1; i <= 32; i++ {
		_ = os.Unsetenv("SOURCE_READ_" + fmt.Sprintf("%02d", i))
	}
	_ = os.Unsetenv("SOURCE_MAP")

	root := checkout.Sources[m.Root]
	sources := map[string]catalog.CompositionSource{}
	for alias, path := range checkout.Sources {
		sha := sourceSHAs[alias]
		if sha == "" {
			return 3
		}
		sources[alias] = catalog.CompositionSource{Root: path, SHA: sha}
	}
	stage = "catalogue validation"
	units, origins, e := catalog.LoadCompositionAt(ctx, sources, git)
	if e != nil {
		return 2
	}
	opts := planner.Options{Context: ctx, AdmittedOnly: true, Base: c.Request.Inputs["base_sha"], SHA: c.Request.Inputs["source_sha"], Mode: c.Request.Inputs["mode"], Admission: c.Request.Inputs["purpose"] == "admission", Quick: c.Request.Inputs["mode"] == "quick", Qualified: c.Policy.AffectedQualified}
	wd, e := os.Getwd()
	if e != nil {
		return 6
	}
	opts.ToolDirectory = filepath.Join(wd, "public-control", "planner")
	if c.Request.Inputs["platforms"] != "all" {
		opts.Platforms = strings.Split(c.Request.Inputs["platforms"], ",")
	}
	var entries []catalog.Entry
	if selector := c.Request.Inputs["selector"]; selector != "all" {
		entries, e = catalog.ResolveSelector(units, selector)
		if e != nil {
			return 3
		}
		for _, v := range entries {
			opts.Units = append(opts.Units, v.ID)
		}
	}
	if len(checkout.Sources) > 1 && !opts.Quick && len(entries) == 0 {
		opts.Mode = "full"
	}
	opts.Executables = map[string]string{"git": git}
	for _, name := range []string{"node", "cargo"} {
		if path, e := exec.LookPath(name); e == nil && filepath.IsAbs(path) {
			opts.Executables[name] = path
		}
	}
	stage = "planning"
	plan, e := planner.Plan(root, units, opts)
	if e != nil {
		return 2
	}
	if len(entries) > 0 {
		selected := []planner.Invocation{}
		byPlatform := map[string][]planner.Invocation{}
		wanted := map[string]bool{}
		targets := map[string]bool{}
		matched := map[string]bool{}
		for _, entry := range entries {
			key := entry.ID + "\x00" + entry.Variant + "\x00" + entry.Shard
			wanted[key] = true
			targets[entry.ID] = true
		}
		for _, inv := range plan.Selected {
			key := inv.ID + "\x00" + inv.Variant + "\x00" + inv.Shard
			if targets[inv.ID] && !wanted[key] {
				continue
			}
			if wanted[key] {
				matched[key] = true
			}
			selected = append(selected, inv)
			byPlatform[inv.Platform] = append(byPlatform[inv.Platform], inv)
		}
		if len(matched) != len(wanted) {
			return 3
		}
		plan.Selected = selected
		plan.Packs = nil
		for _, p := range catalog.Platforms {
			if len(byPlatform[p]) > 0 {
				plan.Packs = append(plan.Packs, planner.Pack{Platform: p, Units: byPlatform[p]})
			}
		}
	}
	if len(plan.Packs) == 0 {
		return 6
	}
	if os.Getenv("CI_KIT_OPERATION") == "plan" {
		flags := map[string]string{}
		for _, p := range catalog.Platforms {
			flags[p] = "false"
		}
		for _, p := range plan.Packs {
			flags[p.Platform] = "true"
		}
		if output(flags) != nil {
			return 6
		}
		return 0
	}
	platform := os.Getenv("CI_PLATFORM")
	if !slices.Contains(catalog.Platforms, platform) {
		return 2
	}
	var selected []planner.Invocation
	for _, p := range plan.Packs {
		if p.Platform == platform {
			selected = p.Units
		}
	}
	if len(selected) == 0 {
		return 6
	}
	stage = "pack execution"
	return runPack(ctx, root, private, platform, units, origins, selected, opts.Admission)
}
func readRegular(path string, max int64) ([]byte, error) {
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() > max {
		return nil, refused
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, refused
	}
	return b, nil
}
func collect(ctx context.Context, c control) int {
	var needs map[string]struct {
		Result  string            `json:"result"`
		Outputs map[string]string `json:"outputs"`
	}
	if config.Decode([]byte(os.Getenv("CI_NEEDS")), &needs, 65536) != nil || len(needs) != len(protocol.AdmittedPlatforms)+2 || needs["preflight"].Result != "success" || needs["plan"].Result != "success" {
		return 6
	}
	expected := map[string]string{"preflight": "success", "plan": "success", "collect": "current"}
	required := 0
	for _, p := range protocol.AdmittedPlatforms {
		flag := needs["plan"].Outputs[p]
		n, ok := needs["pack-"+p]
		if !ok || (flag != "true" && flag != "false") {
			return 6
		}
		want := "skipped"
		if flag == "true" {
			want = "success"
			required++
		}
		expected["pack-"+p] = want
		if n.Result != want {
			if n.Result == "failure" {
				return 1
			}
			return 6
		}
	}
	if required == 0 {
		return 6
	}
	run, e := strconv.ParseInt(os.Getenv("CI_RUN_ID"), 10, 64)
	if e != nil || run <= 0 {
		return 3
	}
	attempt, e := strconv.Atoi(os.Getenv("CI_ATTEMPT"))
	if e != nil || attempt <= 0 {
		return 3
	}
	client, e := provider.New(os.Getenv("GH_TOKEN"), 30*time.Second, nil)
	if e != nil {
		return 3
	}
	type providerJob struct {
		ID         int64  `json:"id"`
		RunID      int64  `json:"run_id"`
		RunAttempt int    `json:"run_attempt"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	jobs := []providerJob{}
	total := -1
	for page := 1; page <= 10; page++ {
		var response struct {
			Total int           `json:"total_count"`
			Jobs  []providerJob `json:"jobs"`
		}
		path := "repos/" + c.Policy.CallerRepository + "/actions/runs/" + strconv.FormatInt(run, 10) + "/attempts/" + strconv.Itoa(attempt) + "/jobs?per_page=100&page=" + strconv.Itoa(page)
		if _, e = client.Request(ctx, "GET", path, nil, &response); e != nil {
			return 6
		}
		if total < 0 {
			total = response.Total
		}
		if total != response.Total || total > len(expected)+4 || total < len(expected) || len(response.Jobs) > 100 {
			return 6
		}
		jobs = append(jobs, response.Jobs...)
		if len(jobs) == total {
			break
		}
		if len(jobs) > total || len(response.Jobs) < 100 || page == 10 {
			return 6
		}
	}
	if len(jobs) != total {
		return 6
	}
	seen := map[string]bool{}
	ids := map[int64]bool{}
	for _, j := range jobs {
		slot, platform, ok := protocol.ParseJobName(j.Name)
		if !ok {
			return 6
		}
		if platform != "" && !protocol.AdmittedPlatform(platform) {
			continue
		}
		key := slot
		switch slot {
		case "checks":
			key = "pack-" + platform
		case "finalizer":
			key = "collect"
		}
		want, ok := expected[key]
		if !ok || seen[key] || ids[j.ID] || j.ID <= 0 || j.RunID != run || j.RunAttempt != attempt {
			return 6
		}
		seen[key] = true
		ids[j.ID] = true
		if want == "current" {
			if j.Status != "in_progress" || j.Conclusion != "" {
				return 6
			}
		} else if j.Status != "completed" || j.Conclusion != want {
			return 6
		}
	}
	for key := range expected {
		if !seen[key] {
			return 6
		}
	}
	// The exact current finalizer is deliberately nonterminal in this snapshot.
	// External status requires this same run/attempt finalizer to finish success.
	return 0
}
