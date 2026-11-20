package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
)

// Routing is an independently installed private operator policy, never candidate configuration.
type Routing struct {
	SchemaVersion int              `json:"schema_version"`
	Concerns      map[string]Route `json:"concerns"`
}
type Route struct {
	Source             RegisteredRepository `json:"source"`
	Caller             RegisteredCaller     `json:"caller"`
	RegistrationSHA256 string               `json:"registration_sha256"`
}

func LoadRoute(concern string) (Route, error) {
	var p Routing
	dir, e := os.UserConfigDir()
	if e != nil {
		return Route{}, Fail(3, "policy_refused")
	}
	path := filepath.Join(dir, "KeelDock", "kd-ci-routing.json")
	_, e = os.Lstat(path)
	if e != nil || !process.UserOnly(path, false) || !config.NoLinks(path) {
		return Route{}, Fail(3, "policy_refused")
	}
	if config.Read(path, &p, 1<<20) != nil || p.SchemaVersion != 1 || len(p.Concerns) > 128 {
		return Route{}, Fail(3, "policy_refused")
	}
	r, ok := p.Concerns[concern]
	if !ok {
		return r, Fail(3, "policy_refused")
	}
	return r, nil
}
func BoundRegistration(ctx context.Context, root, base, concern string, route Route) (Registration, error) {
	r, e := RegistrationAt(ctx, root, base)
	if e != nil {
		return r, e
	}
	raw, e := Git(ctx, root, 16<<10, "show", base+":ci/registration.json")
	if e != nil {
		return r, e
	}
	h := sha256.Sum256(raw)
	if r.Concern != concern || r.Source != route.Source || r.Caller != route.Caller || hex.EncodeToString(h[:]) != route.RegistrationSHA256 {
		return r, Fail(3, "policy_refused")
	}
	return r, nil
}
