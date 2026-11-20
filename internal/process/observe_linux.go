//go:build linux

package process

import (
	"golang.org/x/sys/unix"
	"os/exec"
)

// WNOWAIT observes exit while retaining the leader's PID reservation until all
// process-group signals have been sent. Wait then reaps the original leader.
func observeExit(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		for {
			e := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if e == unix.EINTR {
				continue
			}
			ch <- e
			return
		}
	}()
	return ch
}
