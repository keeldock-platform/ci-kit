package cli

import (
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"path/filepath"
	"reflect"
	"strings"
)

func (a *app) localCatalogue(root string, cfg config.Repository) ([]catalog.Unit, map[string]string, catalog.Shared, error) {
	composition := config.SingleSourceComposition()
	if cfg.CompositionLock != "" {
		path, e := config.SafePath(root, cfg.CompositionLock)
		if e != nil {
			return nil, nil, catalog.Shared{}, gh.Fail(2, "configuration")
		}
		var raw json.RawMessage
		if config.Read(path, &raw, 1<<20) != nil {
			return nil, nil, catalog.Shared{}, gh.Fail(2, "configuration")
		}
		composition, e = config.DecodeComposition(raw)
		if e != nil {
			return nil, nil, catalog.Shared{}, gh.Fail(2, "configuration")
		}
	}
	roots := map[string]string{cfg.Concern: root}
	if len(composition.Sources) > 0 {
		machine, e := config.LoadMachine()
		if e != nil {
			return nil, nil, catalog.Shared{}, gh.Fail(3, "policy_refused")
		}
		for _, source := range composition.Sources {
			mapped := machine.CompositionRoots[source.Alias]
			if !filepath.IsAbs(mapped) || source.Alias == cfg.Concern {
				return nil, nil, catalog.Shared{}, gh.Fail(3, "policy_refused")
			}
			physical, e := filepath.EvalSymlinks(mapped)
			if e != nil || !config.NoLinks(physical) {
				return nil, nil, catalog.Shared{}, gh.Fail(3, "policy_refused")
			}
			head, e := gh.Git(a.ctx, physical, 1024, "rev-parse", "HEAD")
			if e != nil || strings.TrimSpace(string(head)) != source.SHA {
				return nil, nil, catalog.Shared{}, gh.Fail(3, "policy_refused")
			}
			for _, prior := range roots {
				if prior == physical {
					return nil, nil, catalog.Shared{}, gh.Fail(3, "policy_refused")
				}
			}
			roots[source.Alias] = physical
		}
	}
	var units []catalog.Unit
	origins := map[string]string{}
	var e error
	if len(roots) == 1 {
		units, e = catalog.LoadWithConfig(root, cfg.UnitRoots, cfg.SharedUnits)
		for _, u := range units {
			origins[u.ID] = root
		}
	} else {
		standard, err := config.Load(root, "")
		if err != nil || !reflect.DeepEqual(standard.UnitRoots, cfg.UnitRoots) || standard.SharedUnits != cfg.SharedUnits || standard.CompositionLock != cfg.CompositionLock {
			return nil, nil, catalog.Shared{}, gh.Fail(3, "policy_refused")
		}
		units, origins, e = catalog.LoadCompositionConfigured(a.ctx, roots)
	}
	if e != nil {
		return nil, nil, catalog.Shared{}, selectionError(e)
	}
	merged := catalog.Shared{Schema: 1, Aliases: map[string][]string{}}
	for alias, path := range roots {
		sourceCfg := cfg
		if alias != cfg.Concern {
			sourceCfg, e = config.Load(path, "")
			if e != nil {
				return nil, nil, merged, gh.Fail(2, "configuration")
			}
		}
		shared, err := catalog.LoadSharedPath(path, sourceCfg.SharedUnits)
		if err != nil {
			return nil, nil, merged, gh.Fail(2, "configuration")
		}
		for name, targets := range shared.Aliases {
			if _, exists := merged.Aliases[name]; exists {
				return nil, nil, merged, gh.Fail(2, "configuration")
			}
			merged.Aliases[name] = targets
		}
	}
	if _, e = catalog.AggregateAliases(units, merged); e != nil {
		return nil, nil, merged, gh.Fail(2, "configuration")
	}
	return units, origins, merged, nil
}
func (a *app) compositionDirty(roots map[string]string) (bool, error) {
	seen := map[string]bool{}
	dirty := false
	for _, root := range roots {
		if seen[root] {
			continue
		}
		seen[root] = true
		out, e := gh.Git(a.ctx, root, 1<<20, "status", "--porcelain=v1", "-z", "--untracked-files=all")
		if e != nil {
			return false, e
		}
		dirty = dirty || len(out) > 0
	}
	return dirty, nil
}
func approvedManagedRoot(machine config.Machine, root string) bool {
	for _, concern := range machine.AllowedLocalConcerns {
		if machine.SourceRoots[concern] == root {
			return true
		}
	}
	return false
}
