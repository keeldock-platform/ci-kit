package config

import (
	"encoding/json"
	"github.com/keeldock-platform/ci-kit/schemas"
	"os"
	"path/filepath"
	"regexp"
)

type Repository struct {
	SchemaVersion       int      `json:"schema_version"`
	Concern             string   `json:"concern"`
	KitAPI              int      `json:"kit_api"`
	UnitRoots           []string `json:"unit_roots"`
	SharedUnits         string   `json:"shared_units"`
	CompositionLock     string   `json:"composition_lock,omitempty"`
	LocalPolicy         string   `json:"local_policy"`
	ResultsDir          string   `json:"results_dir,omitempty"`
	RetentionDays       int      `json:"retention_days,omitempty"`
	EditorSchema        string   `json:"$schema,omitempty"`
	RetentionConfigured bool     `json:"-"`
}
type User struct {
	SchemaVersion int    `json:"schema_version"`
	Color         string `json:"color,omitempty"`
	Progress      *bool  `json:"progress,omitempty"`
	RetentionDays int    `json:"retention_days,omitempty"`
	EditorSchema  string `json:"$schema,omitempty"`
}

var alias = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

func Alias(s string) bool { return alias.MatchString(s) }

// Load reads the selected configuration inside an already identified checkout.
func Load(root, path string) (Repository, error) {
	var empty Repository
	if path == "" {
		path = ".kd-ci.json"
	} else if filepath.IsAbs(path) {
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return empty, ErrInvalid
		}
		path = filepath.ToSlash(rel)
	}
	p, e := SafePath(root, path)
	if e != nil {
		return empty, e
	}
	var raw json.RawMessage
	if Read(p, &raw, 1<<20) != nil {
		return empty, ErrInvalid
	}
	return DecodeRepository(root, raw)
}

// DecodeRepository validates committed configuration bytes without reading a working copy.
func DecodeRepository(root string, data []byte) (Repository, error) {
	var c Repository
	if Decode(data, &c, 1<<20) != nil {
		return c, ErrInvalid
	}
	var raw any
	if json.Unmarshal(data, &raw) != nil || schemas.Validate("urn:keeldock:kd-ci:config:1", raw) != nil {
		return c, ErrInvalid
	}
	var e error
	// Required values are checked separately from defaults so omitted policy cannot weaken it.
	if c.SchemaVersion != 1 || c.KitAPI != 1 || !Alias(c.Concern) || len(c.UnitRoots) == 0 || len(c.UnitRoots) > 10000 || c.SharedUnits == "" || c.LocalPolicy != "managed-owner-mac" && c.LocalPolicy != "developer-machine" {
		return c, ErrInvalid
	}
	seen := map[string]bool{}
	for _, r := range c.UnitRoots {
		if seen[r] {
			return c, ErrInvalid
		}
		seen[r] = true
		if _, e = SafePath(root, r); e != nil {
			return c, e
		}
	}
	for _, r := range []string{c.SharedUnits, c.CompositionLock} {
		if r != "" {
			if _, e = SafePath(root, r); e != nil {
				return c, e
			}
		}
	}
	if c.ResultsDir == "" {
		c.ResultsDir = ".ci-local/results"
	}
	if _, e = SafePath(root, c.ResultsDir); e != nil {
		return c, e
	}
	c.RetentionConfigured = c.RetentionDays != 0
	if c.RetentionDays == 0 {
		c.RetentionDays = 7
	}
	if c.RetentionDays < 1 || c.RetentionDays > 30 {
		return c, ErrInvalid
	}
	return c, nil
}

// LoadUser accepts presentation and retention defaults only.
func LoadUser() (User, error) {
	var c User
	dir, e := os.UserConfigDir()
	if e != nil {
		return c, ErrInvalid
	}
	p := filepath.Join(dir, "KeelDock", "kd-ci.json")
	if _, e = os.Lstat(p); os.IsNotExist(e) {
		return c, nil
	}
	if e = Read(p, &c, 1<<20); e != nil {
		return c, e
	}
	if e = validateFile(p, "urn:keeldock:kd-ci:user-config:1"); e != nil {
		return c, e
	}
	if c.SchemaVersion != 1 || c.Color != "" && c.Color != "auto" && c.Color != "never" || c.RetentionDays < 0 || c.RetentionDays > 30 {
		return c, ErrInvalid
	}
	return c, nil
}

// Canonical returns deterministic JSON bytes used for request digests.
func Canonical(v any) ([]byte, error) { return json.Marshal(v) }

func validateFile(path, urn string) error {
	var v any
	if Read(path, &v, 1<<20) != nil || schemas.Validate(urn, v) != nil {
		return ErrInvalid
	}
	return nil
}
