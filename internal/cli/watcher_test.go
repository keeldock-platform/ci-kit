package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/process"
	"path/filepath"
	"testing"
)

func TestWaitHasOnePrivatePollingOwner(t *testing.T) {
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(parent, "private")
	if process.MakePrivateDir(dir) != nil {
		t.Fatal("mkdir")
	}
	release, e := acquireWatcher(dir, "123")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = acquireWatcher(dir, "123"); e == nil {
		t.Fatal("duplicate watcher accepted")
	}
	release()
	release()
	next, e := acquireWatcher(dir, "123")
	if e != nil {
		t.Fatal(e)
	}
	next()
	if _, e = acquireWatcher(dir, "../escape"); e == nil {
		t.Fatal("invalid watcher identity")
	}
}
