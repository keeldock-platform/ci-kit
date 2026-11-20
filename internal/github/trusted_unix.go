//go:build !windows

package github

import (
	"os"
	"path/filepath"
	"syscall"
)

func trustedPath(path string) bool {
	for {
		st, e := os.Stat(path)
		if e != nil || st.Mode().Perm()&0022 != 0 {
			return false
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != 0 && sys.Uid != uint32(os.Getuid()) {
			return false
		}
		next := filepath.Dir(path)
		if next == path {
			return true
		}
		path = next
	}
}
