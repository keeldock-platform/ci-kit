package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/config"
	gh "github.com/keeldock-platform/ci-kit/internal/github"
	"github.com/keeldock-platform/ci-kit/internal/process"
	"github.com/keeldock-platform/ci-kit/internal/protocol"
	"os"
	"sync"
)

// acquireWatcher permits one polling owner; an unclean crash requires operator reconciliation.
func acquireWatcher(dir, id string) (func(), error) {
	if !protocol.Numeric(id) || !process.UserOnly(dir, true) {
		return nil, gh.Fail(3, "policy_refused")
	}
	path, e := config.SafePath(dir, id+".watch")
	if e != nil {
		return nil, gh.Fail(3, "policy_refused")
	}
	f, e := process.CreatePrivateFile(path)
	if e != nil {
		return nil, gh.Fail(3, "policy_refused")
	}
	if e = f.Close(); e != nil {
		return nil, gh.Fail(3, "policy_refused")
	}
	var once sync.Once
	return func() { once.Do(func() { os.Remove(path) }) }, nil
}
