package catalog

import (
	"encoding/json"
	"os"

	"github.com/keeldock-platform/ci-kit/internal/config"
)

// PackPolicy declares captured setup once per platform and required tool versions.
type PackPolicy struct {
	Support json.RawMessage   `json:"support,omitempty"`
	Setup   []Command         `json:"setup"`
	Tools   map[string]string `json:"tools"`
}

// Shared holds only shared units and pack policy, never feature inventory.
type Shared struct {
	Aliases map[string][]string   `json:"aliases,omitempty"`
	Schema  int                   `json:"schema"`
	Units   []Unit                `json:"units"`
	Packs   map[string]PackPolicy `json:"packs"`
}

// DecodeShared validates the bounded shared configuration and its units.
func DecodeShared(data []byte) (Shared, error) {
	var s Shared
	if config.Decode(data, &s, MaxDescriptorBytes) != nil || s.Schema != 1 || len(s.Units) > 10000 || len(s.Packs) > len(Platforms) || len(s.Aliases) > 10000 {
		return Shared{}, ErrInvalid
	}
	for name, targets := range s.Aliases {
		if !aliasID.MatchString(name) || len(targets) == 0 || len(targets) > 256 {
			return Shared{}, ErrInvalid
		}
	}
	for i, u := range s.Units {
		b, e := json.Marshal(u)
		if e != nil {
			return Shared{}, ErrInvalid
		}
		decoded, e := Decode(b)
		if e != nil {
			return Shared{}, e
		}
		decoded.Root = "."
		s.Units[i] = decoded
	}
	for platform, p := range s.Packs {
		if !contains(Platforms, platform) || len(p.Setup) > 32 || len(p.Tools) > 64 || len(p.Support) > 16<<10 {
			return Shared{}, ErrInvalid
		}
		for name, version := range p.Tools {
			if name == "" || len(name) > 128 || version == "" || len(version) > 256 {
				return Shared{}, ErrInvalid
			}
		}
		for _, cmd := range p.Setup {
			if !safePattern(cmd.Cwd) || len(cmd.Argv) == 0 || len(cmd.Argv) > 128 || cmd.TimeoutSeconds < 1 || cmd.TimeoutSeconds > 7200 {
				return Shared{}, ErrInvalid
			}
			for _, a := range cmd.Argv {
				if a == "" || len(a) > 4096 {
					return Shared{}, ErrInvalid
				}
			}
		}
	}
	return s, nil
}

// LoadShared returns an empty versioned configuration when no shared file exists.
func LoadShared(root string) (Shared, error) { return loadSharedPath(root, "ci/units.json", true) }

// LoadSharedPath reads the configured shared catalogue without a default-path fallback.
func LoadSharedPath(root, sharedPath string) (Shared, error) {
	return loadSharedPath(root, sharedPath, false)
}
func loadSharedPath(root, sharedPath string, optional bool) (Shared, error) {
	p, e := config.SafePath(root, sharedPath)
	if e != nil {
		return Shared{}, ErrInvalid
	}
	if _, e = os.Lstat(p); os.IsNotExist(e) && optional {
		return Shared{Schema: 1, Packs: map[string]PackPolicy{}}, nil
	}
	raw, e := readShared(p)
	if e != nil {
		return Shared{}, e
	}
	return DecodeShared(raw)
}
func readShared(p string) ([]byte, error) {

	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() || st.Size() > MaxDescriptorBytes {
		return nil, ErrInvalid
	}
	return os.ReadFile(p)
}
