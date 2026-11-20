package github

import (
	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Expectation struct {
	SchemaVersion     int               `json:"schema_version"`
	CompositionSHA256 string            `json:"composition_sha256"`
	Registration      Registration      `json:"registration"`
	Dispatch          protocol.Dispatch `json:"dispatch"`
	RunID             string            `json:"run_id"`
	Attempt           int               `json:"attempt"`
	CreatedAt         time.Time         `json:"created_at"`
}

func (e Expectation) Validate() error {
	if !protocol.Digest(e.CompositionSHA256) || e.SchemaVersion != 1 || e.Registration.Validate() != nil || e.Dispatch.Validate() != nil || !protocol.Numeric(e.RunID) || e.Attempt < 1 || e.Dispatch.Inputs["concern"] != e.Registration.Concern {
		return Fail(6, "missing_evidence")
	}
	return nil
}
func StateDir(root, results string) (string, error) {
	p, e := config.SafePath(root, results)
	if e != nil {
		return "", Fail(3, "policy_refused")
	}
	p = filepath.Join(p, "requests")
	if e = EnsurePrivateDirectory(root, p); e != nil {
		return "", e
	}
	if !process.UserOnly(p, true) {
		return "", Fail(3, "policy_refused")
	}
	return p, nil
}
func SaveExpectation(dir string, x Expectation) error {
	if x.Validate() != nil {
		return Fail(6, "missing_evidence")
	}
	p, e := config.SafePath(dir, x.RunID+".json")
	if e != nil {
		return Fail(3, "policy_refused")
	}
	if _, e = os.Lstat(p); e == nil {
		return Fail(3, "policy_refused")
	}
	b, e := config.Canonical(x)
	if e != nil {
		return Fail(6, "missing_evidence")
	}
	f, e := process.CreatePrivateFile(p)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil || ce != nil {
		return Fail(6, "missing_evidence")
	}
	return nil
}
func ReadExpectation(dir, id string) (Expectation, error) {
	var x Expectation
	if !protocol.Numeric(id) {
		return x, Fail(2, "usage")
	}
	p, e := config.SafePath(dir, id+".json")
	if e != nil {
		return x, Fail(3, "policy_refused")
	}
	_, e = os.Lstat(p)
	if e != nil || !process.UserOnly(p, false) || config.Read(p, &x, 1<<20) != nil || x.Validate() != nil || x.RunID != id {
		return x, Fail(6, "missing_evidence")
	}
	return x, nil
}
func SavePending(dir string, r Registration, d protocol.Dispatch, composition string) error {
	if r.Validate() != nil || d.Validate() != nil || !protocol.Digest(composition) {
		return Fail(3, "policy_refused")
	}
	p, e := config.SafePath(dir, d.Inputs["request_id"]+".pending.json")
	if e != nil {
		return Fail(3, "policy_refused")
	}
	b, e := config.Canonical(struct {
		Registration      Registration      `json:"registration"`
		Dispatch          protocol.Dispatch `json:"dispatch"`
		CompositionSHA256 string            `json:"composition_sha256"`
	}{r, d, composition})
	if e != nil {
		return Fail(6, "missing_evidence")
	}
	f, e := process.CreatePrivateFile(p)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e != nil || ce != nil {
		return Fail(6, "missing_evidence")
	}
	return nil
}

// EnsurePrivateDirectory creates only missing descendants with non-inherited user-only access.
func EnsurePrivateDirectory(root, path string) error {
	rel, e := filepath.Rel(root, path)
	if e != nil {
		return Fail(3, "policy_refused")
	}
	if _, e = config.SafePath(root, filepath.ToSlash(rel)); e != nil {
		return Fail(3, "policy_refused")
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		if _, e = os.Lstat(cur); os.IsNotExist(e) {
			if e = process.MakePrivateDir(cur); e != nil {
				return Fail(3, "policy_refused")
			}
		} else if e != nil || !process.UserOnly(cur, true) {
			return Fail(3, "policy_refused")
		}
	}
	return nil
}
