package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/process"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPurgeGroupedRunsRetainsActiveAndUnknown(t *testing.T) {
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "private-results")
	if e = process.MakePrivateDir(root); e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-72 * time.Hour)
	ids := []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"}
	for _, id := range ids {
		p := filepath.Join(root, id)
		if process.MakePrivateDir(p) != nil {
			t.Fatal("mkdir")
		}
		leaf := filepath.Join(p, "0000-0000")
		process.MakePrivateDir(leaf)
		for _, name := range []string{"stdout", "stderr"} {
			f, e := process.CreatePrivateFile(filepath.Join(leaf, name))
			if e != nil {
				t.Fatal(e)
			}
			f.Close()
			os.Chtimes(filepath.Join(leaf, name), past, past)
		}
	}
	for i, name := range []string{"active", "unknown"} {
		f, e := process.CreatePrivateFile(filepath.Join(root, ids[i+1], name))
		if e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	deleted, e := purgeRuns(root, 24*time.Hour, time.Now())
	if e != nil || len(deleted) != 1 || deleted[0] != ids[0] {
		t.Fatalf("%v %v", deleted, e)
	}
	for _, id := range ids[1:] {
		if _, e = os.Stat(filepath.Join(root, id)); e != nil {
			t.Fatal("active/unknown removed")
		}
	}
}
