//go:build !windows

package process

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

type ownership struct{ pid int }

func prepare(cmd *exec.Cmd) (*ownership, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &ownership{}, nil
}
func (o *ownership) started(cmd *exec.Cmd) error { o.pid = cmd.Process.Pid; return nil }
func (o *ownership) stop(force bool) error {
	if o.pid == 0 {
		return nil
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	e := syscall.Kill(-o.pid, sig)
	if e == syscall.ESRCH {
		return nil
	}
	return e
}
func (o *ownership) empty() (bool, error) {
	if o.pid == 0 {
		return true, nil
	}
	e := syscall.Kill(-o.pid, 0)
	if e == syscall.ESRCH {
		return true, nil
	}
	if e == syscall.EPERM {
		return false, nil
	} // Group exists but is not yet confirmed empty.
	if e != nil {
		return false, e
	}
	return false, nil
}
func (o *ownership) close() {}
func exitSignal(s *os.ProcessState) string {
	if w, ok := s.Sys().(syscall.WaitStatus); ok && w.Signaled() {
		return w.Signal().String()
	}
	return ""
}
func makePrivateDir(p string) error { return os.Mkdir(p, 0700) }
func privateFile(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
}

// UserOnly rejects symlinks, foreign ownership and group/other access.
func UserOnly(path string, isDir bool) bool {
	st, e := os.Lstat(path)
	if e != nil || st.Mode()&os.ModeSymlink != 0 || st.IsDir() != isDir || (!isDir && !st.Mode().IsRegular()) || st.Mode().Perm()&0077 != 0 {
		return false
	}
	native, ok := st.Sys().(*syscall.Stat_t)
	return ok && native.Uid == uint32(os.Geteuid())
}

// Darwin returns EPERM when a group contains only unreaped exited members.
// It is benign only after the direct child was reaped and an independent group
// probe confirmed ESRCH. Live descendants or probe failure never satisfy this.
func terminationConfirmed(signalErr error, empty bool) bool {
	return empty && (signalErr == nil || (runtime.GOOS == "darwin" && signalErr == syscall.EPERM))
}
