package process

import (
	"path/filepath"
	"testing"
)

func TestDynamicShellCommandsRefused(t *testing.T) {
	root := physicalTemp(t)
	for _, argv := range [][]string{{"bash", "-c", "printf unsafe"}, {"bash", "-lc", "printf unsafe"}, {"sh", "-s"}, {"pwsh", "-NoProfile", "-Command", "unsafe"}, {"powershell", "-EncodedCommand", "encoded"}, {"cmd", "/c", "unsafe"}, {"tool.cmd", "argument"}} {
		argv[0] = filepath.Join(root, argv[0])
		if validateArgv(argv) == nil {
			t.Fatal("dynamic shell form accepted")
		}
	}
}
func TestExplicitScriptArgvAccepted(t *testing.T) {
	root := physicalTemp(t)
	for _, argv := range [][]string{{"bash", "--noprofile", "--norc", "script.sh", "value"}, {"pwsh", "-NoProfile", "-NonInteractive", "-File", "script.ps1", "value"}} {
		argv[0] = filepath.Join(root, argv[0])
		if validateArgv(argv) != nil {
			t.Fatal("explicit script refused")
		}
	}
}
