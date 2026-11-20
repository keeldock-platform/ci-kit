//go:build windows

package config

import (
	"os"
	"syscall"
)

func unsafeLink(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Win32FileAttributeData)
	return !ok || s.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func safeRoot(path string) bool { return NoLinks(path) }
