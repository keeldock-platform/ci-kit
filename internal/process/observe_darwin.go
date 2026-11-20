//go:build darwin

package process

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// Darwin's waitid is not wrapped by x/sys. The standard syscall boundary uses
// P_PID=1 and an oversized aligned siginfo buffer; no siginfo fields are decoded.
// WNOWAIT keeps the leader's PID reserved until group termination has completed.
func observeExit(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		var info [32]uint64
		for {
			_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(cmd.Process.Pid), uintptr(unsafe.Pointer(&info[0])), uintptr(syscall.WEXITED|syscall.WNOWAIT), 0, 0)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				ch <- errno
			} else {
				ch <- nil
			}
			return
		}
	}()
	return ch
}
