//go:build windows

package process

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDirectoryProtectsOrdinaryChildren(t *testing.T) {
	root := physicalTemp(t)
	p := filepath.Join(root, "private")
	if e := MakePrivateDir(p); e != nil {
		t.Fatal(e)
	}
	if !UserOnly(p, true) {
		t.Fatal("private directory ACL missing")
	}
	nested := filepath.Join(p, "nested")
	if e := os.Mkdir(nested, 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(nested, "source")
	if e := os.WriteFile(file, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	if !UserOnly(nested, true) || !UserOnly(file, false) {
		t.Fatal("ordinary source children inherited broad access")
	}
}
