package config

import (
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
)

// Machine is fixed operator-installed policy; repository configuration may only tighten it.
type Machine struct {
	SchemaVersion        int               `json:"schema_version"`
	LocalPolicy          string            `json:"local_policy"`
	AllowedLocalConcerns []string          `json:"allowed_local_concerns"`
	SourceRoots          map[string]string `json:"source_roots"`
	CompositionRoots     map[string]string `json:"composition_roots,omitempty"`
	Tools                map[string]string `json:"tools"`
	PlannerDirectory     string            `json:"planner_directory"`
	KitRoot              string            `json:"kit_root"`
}

func LoadMachine() (Machine, error) {
	var m Machine
	dir, e := os.UserConfigDir()
	if e != nil {
		return m, ErrInvalid
	}
	p := filepath.Join(dir, "KeelDock", "kd-ci-machine.json")
	_, e = os.Lstat(p)
	if e != nil || !process.UserOnly(p, false) || !NoLinks(p) || Read(p, &m, 1<<20) != nil || m.SchemaVersion != 1 || m.LocalPolicy != "managed-owner-mac" && m.LocalPolicy != "developer-machine" {
		return m, ErrInvalid
	}
	for k, v := range m.Tools {
		if !Alias(k) || !filepath.IsAbs(v) {
			return m, ErrInvalid
		}
		st, e := os.Stat(v)
		if e != nil || !st.Mode().IsRegular() {
			return m, ErrInvalid
		}
	}
	for alias, path := range m.CompositionRoots {
		if !Alias(alias) || !filepath.IsAbs(path) {
			return m, ErrInvalid
		}
	}
	for _, c := range m.AllowedLocalConcerns {
		if !Alias(c) {
			return m, ErrInvalid
		}
	}
	return m, nil
}
