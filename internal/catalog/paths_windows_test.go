//go:build windows

package catalog

import "testing"

func TestCanonicalCrossVolumeToolIsOutsideSource(t *testing.T) {
	if !OutsideRoot("D:\\source", "C:\\tools\\git.exe") {
		t.Fatal("separate canonical volume was rejected")
	}
	if OutsideRoot("D:\\source", "D:\\source\\tools\\git.exe") {
		t.Fatal("source-contained tool accepted")
	}
	if !OutsideRoot("D:\\source", "D:\\tools\\git.exe") {
		t.Fatal("same-volume external tool rejected")
	}
}
