package acquire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquisitionRefusesMissingTrustReadersBeforeCreatingTree(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	workspace := filepath.Join(root, "source")
	_, e = Acquire(context.Background(), Request{Map: mapping(), RootSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), Workspace: workspace, Git: "/trusted/git", CredentialHelper: "/trusted/helper"})
	if e == nil {
		t.Fatal("unverified acquisition accepted")
	}
	if _, e = os.Stat(workspace); !os.IsNotExist(e) {
		t.Fatal("source tree created before trust validation")
	}
}
func TestAcquisitionCleansAfterRepositoryIdentityRefusal(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	workspace := filepath.Join(root, "source")
	t.Setenv("SOURCE_READ_01", credentialEnv(t)["SOURCE_READ_01"])
	called := false
	_, e = Acquire(context.Background(), Request{Map: mapping(), RootSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), Workspace: workspace, Git: "/trusted/git", CredentialHelper: "/trusted/helper", VerifyRepository: func(context.Context, Source) error { called = true; return errors.New("identity mismatch") }, VerifySignatures: func(context.Context, string, string, string) error { return nil }, ReadComposition: func(context.Context, string) ([]LockedSource, error) {
		t.Fatal("composition read before root trust")
		return nil, nil
	}})
	if e == nil || !called {
		t.Fatal("identity refusal lost")
	}
	if _, e = os.Stat(workspace); !os.IsNotExist(e) {
		t.Fatal("failed acquisition retained tree")
	}
}
func TestAcquisitionRefusesDebugAndTypedDuplicateSlotsBeforeNetwork(t *testing.T) {
	for _, mode := range []string{"debug", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			m := mapping()
			if mode == "debug" {
				t.Setenv("RUNNER_DEBUG", "1")
			} else {
				s := m.Sources[0]
				s.Alias = "dependency"
				s.RepositoryID = 43
				m.Sources = append(m.Sources, s)
			}
			called := false
			_, e = Acquire(context.Background(), Request{Map: m, RootSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), Workspace: filepath.Join(root, "source"), Git: "/trusted/git", CredentialHelper: "/trusted/helper", VerifyRepository: func(context.Context, Source) error { called = true; return nil }, VerifySignatures: func(context.Context, string, string, string) error { return nil }, ReadComposition: func(context.Context, string) ([]LockedSource, error) { return nil, nil }})
			if e == nil || called {
				t.Fatal("unsafe acquisition reached network reader")
			}
		})
	}
}

func TestCloseCannotDeleteAnUnownedOrRetargetedTree(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "keep")
	if os.WriteFile(marker, []byte("synthetic"), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	for _, c := range []*Checkout{{Root: root}, {Root: root, ownedRoot: filepath.Join(root, "other")}} {
		if c.Close() == nil {
			t.Fatal("unowned cleanup accepted")
		}
	}
	if _, e := os.Stat(marker); e != nil {
		t.Fatal("unowned data deleted")
	}
}
func TestDebugRefusalUsesProviderBooleanSemantics(t *testing.T) {
	for _, v := range []string{"true", "TRUE", " True ", "1", "unexpected"} {
		if !debugEnabled(v) {
			t.Fatal("enabled or unknown debug value accepted")
		}
	}
	for _, v := range []string{"", "false", "FALSE", "0"} {
		if debugEnabled(v) {
			t.Fatal("disabled debug value refused")
		}
	}
}
