package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"os"
	"path/filepath"
)

// analysisProjection uses native local Git only; no remote protocol or source hook can run.
func (a *app) analysisProjection(root, sha string) (string, func(), error) {
	if !protocol.SHA(sha) {
		return "", nil, gh.Fail(2, "usage")
	}
	temp, e := os.MkdirTemp("", "kd-ci-analysis-")
	if e != nil {
		return "", nil, gh.Fail(6, "capture_failed")
	}
	cleanup := func() { os.RemoveAll(temp) }
	temp, e = filepath.EvalSymlinks(temp)
	if e != nil {
		cleanup()
		return "", nil, gh.Fail(6, "capture_failed")
	}
	empty := filepath.Join(temp, "template")
	if e = process.MakePrivateDir(empty); e != nil {
		cleanup()
		return "", nil, gh.Fail(6, "capture_failed")
	}
	target := filepath.Join(temp, "source")
	if e = process.MakePrivateDir(target); e != nil {
		cleanup()
		return "", nil, gh.Fail(6, "capture_failed")
	}
	if _, e = gh.Git(a.ctx, root, 1<<20, "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "clone", "--local", "--shared", "--no-hardlinks", "--no-checkout", "--no-tags", "--template="+empty, "--", root, target); e != nil {
		cleanup()
		return "", nil, e
	}
	if _, e = gh.Git(a.ctx, target, 1<<20, "-c", "core.autocrlf=false", "-c", "filter.lfs.required=false", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.process=", "checkout", "--detach", "--force", sha); e != nil {
		cleanup()
		return "", nil, e
	}
	if _, e = config.SafePath(target, "."); e != nil {
		cleanup()
		return "", nil, gh.Fail(3, "policy_refused")
	}
	return target, cleanup, nil
}

func (a *app) feedbackSelection(root, sha string, cfg config.Repository, units []catalog.Unit, opts planner.Options) (planner.Result, error) {
	composition, e := a.readCommittedComposition(root, sha, cfg)
	if e != nil {
		return planner.Result{}, e
	}
	if len(composition.Sources) > 0 {
		if opts.Quick {
			opts.Quick = false
			if len(opts.Units) > 0 {
				opts.Mode = "unit"
			} else {
				opts.Mode = "full"
			}
		} else if len(opts.Units) == 0 {
			opts.Mode = "full"
		}
	}
	analysis := root
	cleanup := func() {}
	if opts.Quick || opts.Mode == "affected" && len(opts.Units) == 0 {
		analysis, cleanup, e = a.analysisProjection(root, sha)
		if e != nil {
			return planner.Result{}, e
		}
		defer cleanup()
	}
	result, e := planner.Plan(analysis, units, opts)
	if e != nil {
		return result, selectionError(e)
	}
	return result, nil
}
