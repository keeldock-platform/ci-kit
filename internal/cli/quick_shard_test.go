package cli

import (
	"github.com/keeldock-platform/ci-kit/internal/catalog"
	"github.com/keeldock-platform/ci-kit/internal/planner"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQuickRelatedMultiTestShardReports(t *testing.T) {
	root := t.TempDir()
	start := time.Now().Add(-time.Second)
	if e := os.WriteFile(filepath.Join(root, "report.xml"), []byte(`<testsuite><testcase classname="suite" name="one"/></testsuite>`), 0600); e != nil {
		t.Fatal(e)
	}
	check := catalog.Check{Census: []string{"suite/one", "suite/two", "suite/three"}, Assertions: []string{"suite/one", "suite/two", "suite/three"}, Reports: []string{"report.xml"}, Shards: &catalog.ShardPolicy{Count: 2, Assignments: [][]string{{"suite/one", "suite/two"}, {"suite/three"}}}}
	unit := catalog.Unit{ID: "sample", Checks: map[string]catalog.Check{"unit": check}}
	inv := planner.Invocation{Unit: "sample", Check: "unit", Platform: "macos-arm64", Shard: "1/2", Tests: []string{"suite/one"}}
	if e := validateLocalReports(root, unit, inv, start); e != nil {
		t.Fatalf("valid quick subset: %v", e)
	}
	for _, bad := range []planner.Invocation{
		{Unit: "sample", Check: "unit", Shard: "1/3", Tests: []string{"suite/one"}},
		{Unit: "sample", Check: "unit", Shard: "1/2junk", Tests: []string{"suite/one"}},
		{Unit: "sample", Check: "unit", Shard: "1/2", Tests: []string{"suite/three"}},
		{Unit: "sample", Check: "unit", Shard: "1/2", Tests: []string{"suite/one", "suite/two"}},
	} {
		if validateLocalReports(root, unit, bad, start) == nil {
			t.Fatal("invalid shard or incomplete full shard accepted")
		}
	}
}
