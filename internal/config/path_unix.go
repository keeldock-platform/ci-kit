//go:build !windows

package config

import "os"

func unsafeLink(st os.FileInfo) bool { return st.Mode()&os.ModeSymlink != 0 }

func safeRoot(string) bool { return true }
