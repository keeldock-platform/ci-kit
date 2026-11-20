package cli

import (
	"context"
	"errors"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"path/filepath"
)

func (a *app) committedConfiguration(root, sha string) (config.Repository, error) {
	path := a.configPath
	if path == "" {
		path = ".kd-ci.json"
	}
	if filepath.IsAbs(path) {
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return config.Repository{}, gh.Fail(2, "configuration")
		}
		path = filepath.ToSlash(rel)
	}
	if _, e := config.SafePath(root, path); e != nil {
		return config.Repository{}, gh.Fail(2, "configuration")
	}
	raw, e := gh.Git(a.ctx, root, 1<<20, "show", sha+":"+path)
	if e != nil {
		return config.Repository{}, gh.Fail(2, "configuration")
	}
	cfg, e := config.DecodeRepository(root, raw)
	if e != nil {
		return cfg, gh.Fail(2, "configuration")
	}
	return cfg, nil
}
func (a *app) committedShared(root, sha, path string) (catalog.Shared, error) {
	raw, e := gh.Git(a.ctx, root, 256<<10, "show", sha+":"+path)
	if e != nil {
		return catalog.Shared{}, gh.Fail(2, "configuration")
	}
	shared, e := catalog.DecodeShared(raw)
	if e != nil {
		return shared, gh.Fail(2, "configuration")
	}
	return shared, nil
}

func (a *app) committedComposition(root, sha string, cfg config.Repository) (string, error) {
	composition, e := a.readCommittedComposition(root, sha, cfg)
	if e != nil {
		return "", e
	}
	digest, e := config.CompositionDigest(composition)
	if e != nil {
		return "", gh.Fail(2, "configuration")
	}
	return digest, nil
}

func (a *app) stateDirectory(root, results string, create bool) (string, error) {
	if _, e := gh.Git(a.ctx, root, 1024, "check-ignore", "--", results); e != nil {
		return "", gh.Fail(3, "policy_refused")
	}
	if create {
		return gh.StateDir(root, results)
	}
	base, e := config.SafePath(root, results)
	if e != nil {
		return "", gh.Fail(3, "policy_refused")
	}
	path, e := config.SafePath(root, filepath.ToSlash(filepath.Join(results, "requests")))
	if e != nil {
		return "", gh.Fail(3, "policy_refused")
	}
	if !process.UserOnly(base, true) || !process.UserOnly(path, true) {
		return "", gh.Fail(6, "missing_evidence")
	}
	return path, nil
}

func (a *app) committedCatalogue(root, sha string, cfg config.Repository) ([]catalog.Unit, error) {
	sources, e := a.committedCompositionSources(root, sha, cfg)
	if e != nil {
		return nil, e
	}
	git, e := gh.TrustedExecutable("git", root)
	if e != nil {
		return nil, e
	}
	var units []catalog.Unit
	if len(sources) == 1 {
		units, e = catalog.LoadAtWithConfigContext(a.ctx, root, sha, cfg.UnitRoots, cfg.SharedUnits, git)
	} else {
		units, _, e = catalog.LoadCompositionAt(a.ctx, sources, git)
	}
	if e != nil {
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return nil, selectionError(e)
		}
		return nil, gh.Fail(2, "configuration")
	}
	return units, nil
}

func selectionError(e error) error {
	if errors.Is(e, context.Canceled) {
		return gh.Fail(7, "interrupted")
	}
	if errors.Is(e, context.DeadlineExceeded) {
		return gh.Fail(4, "deadline")
	}
	return gh.Fail(2, "usage")
}

func (a *app) committedCompositionSources(root, sha string, cfg config.Repository) (map[string]catalog.CompositionSource, error) {
	sources := map[string]catalog.CompositionSource{cfg.Concern: {Root: root, SHA: sha}}
	composition, e := a.readCommittedComposition(root, sha, cfg)
	if e != nil {
		return nil, e
	}
	if len(composition.Sources) == 0 {
		return sources, nil
	}
	machine, e := config.LoadMachine()
	if e != nil {
		return nil, gh.Fail(3, "policy_refused")
	}
	seen := map[string]bool{root: true}
	for _, source := range composition.Sources {
		mapped := machine.CompositionRoots[source.Alias]
		if !filepath.IsAbs(mapped) || source.Alias == cfg.Concern {
			return nil, gh.Fail(3, "policy_refused")
		}
		physical, e := filepath.EvalSymlinks(mapped)
		if e != nil || !config.NoLinks(physical) || seen[physical] {
			return nil, gh.Fail(3, "policy_refused")
		}
		seen[physical] = true
		sources[source.Alias] = catalog.CompositionSource{Root: physical, SHA: source.SHA}
	}
	return sources, nil
}
func (a *app) readCommittedComposition(root, sha string, cfg config.Repository) (config.Composition, error) {
	if cfg.CompositionLock == "" {
		return config.SingleSourceComposition(), nil
	}
	raw, e := gh.Git(a.ctx, root, 1<<20, "show", sha+":"+cfg.CompositionLock)
	if e != nil {
		return config.Composition{}, gh.Fail(2, "configuration")
	}
	composition, e := config.DecodeComposition(raw)
	if e != nil {
		return composition, gh.Fail(2, "configuration")
	}
	return composition, nil
}
