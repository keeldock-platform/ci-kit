package catalog

import (
	"context"
	"path/filepath"
	"sort"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

// CompositionSource locates an already available immutable checkout/object set.
// Aliases are supplied by trusted policy; declared unit IDs are never rewritten.
type CompositionSource struct{ Root, SHA string }

// LoadCompositionAt assembles every configured source at its exact existing SHA.
// It never fetches objects, guesses sibling paths or changes a checkout.
func LoadCompositionAt(ctx context.Context, sources map[string]CompositionSource, gitPath string) ([]Unit, map[string]string, error) {
	return assembleComposition(ctx, sources, gitPath, true)
}

// LoadCompositionConfigured assembles current-tree feedback using each source's
// own repository configuration. It cannot provide admission identity.
func LoadCompositionConfigured(ctx context.Context, roots map[string]string) ([]Unit, map[string]string, error) {
	sources := map[string]CompositionSource{}
	for alias, root := range roots {
		sources[alias] = CompositionSource{Root: root}
	}
	return assembleComposition(ctx, sources, "", false)
}

func assembleComposition(ctx context.Context, sources map[string]CompositionSource, gitPath string, immutable bool) ([]Unit, map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if len(sources) == 0 || len(sources) > 32 {
		return nil, nil, ErrInvalid
	}
	aliases := make([]string, 0, len(sources))
	for alias := range sources {
		if !config.Alias(alias) {
			return nil, nil, ErrInvalid
		}
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	canonical := map[string]string{}
	seenRoots := map[string]bool{}
	for _, alias := range aliases {
		source := sources[alias]
		if !filepath.IsAbs(source.Root) {
			return nil, nil, ErrInvalid
		}
		physical, e := filepath.EvalSymlinks(source.Root)
		if e != nil || seenRoots[physical] {
			return nil, nil, ErrInvalid
		}
		seenRoots[physical] = true
		canonical[alias] = physical
	}
	if immutable {
		if !filepath.IsAbs(gitPath) {
			return nil, nil, ErrInvalid
		}
		resolved, e := filepath.EvalSymlinks(gitPath)
		if e != nil {
			return nil, nil, ErrInvalid
		}
		for _, root := range canonical {
			if !OutsideRoot(root, resolved) {
				return nil, nil, ErrInvalid
			}
		}
		gitPath = resolved
	}
	units := []Unit{}
	origins := map[string]string{}
	for _, alias := range aliases {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		source := sources[alias]
		root := canonical[alias]
		var cfg config.Repository
		var current []Unit
		if immutable {
			if !revision.MatchString(source.SHA) {
				return nil, nil, ErrInvalid
			}
			readCtx := context.WithValue(ctx, gitToolKey{}, gitPath)
			raw, e := objectCommand(readCtx, root, 1<<20, "show", source.SHA+":.kd-ci.json")
			if e != nil {
				return nil, nil, e
			}
			cfg, e = config.DecodeRepository(root, raw)
			if e != nil {
				return nil, nil, ErrInvalid
			}
			current, e = loadAtConfigured(ctx, root, source.SHA, cfg.UnitRoots, cfg.SharedUnits, false, false, gitPath)
			if e != nil {
				return nil, nil, e
			}
		} else {
			var e error
			cfg, e = config.Load(root, "")
			if e != nil {
				return nil, nil, ErrInvalid
			}
			current, e = loadConfigured(root, cfg.UnitRoots, cfg.SharedUnits, false, false)
			if e != nil {
				return nil, nil, e
			}
		}
		for _, unit := range current {
			if origins[unit.ID] != "" {
				return nil, nil, ErrInvalid
			}
			origins[unit.ID] = root
			units = append(units, unit)
			if len(units) > 10000 {
				return nil, nil, ErrInvalid
			}
		}
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if Validate(units) != nil {
		return nil, nil, ErrInvalid
	}
	return units, origins, nil
}
