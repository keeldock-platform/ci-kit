//go:build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestSignalExitRetained(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	root := physicalTemp(t)
	cmd := exec.Command(exe, "-test.run=^TestProcessFixture$", "--", "hang")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	owner, e := prepare(cmd)
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	owner.started(cmd)
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	e = cmd.Wait()
	if e == nil || exitSignal(cmd.ProcessState) != "killed" {
		t.Fatal("signal status lost")
	}
}

func TestExitedGroupTerminationRequiresEmptyProbe(t *testing.T) {
	cmd := exec.Command("/usr/bin/true")
	owner, e := prepare(cmd)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.close()
	if cmd.Start() != nil {
		t.Fatal("fixture start failed")
	}
	owner.started(cmd)
	if e = <-observeExit(cmd); e != nil {
		t.Fatal(e)
	}
	_ = owner.stop(false)
	hard := owner.stop(true)
	if cmd.Wait() != nil {
		t.Fatal("fixture exit lost")
	}
	empty, probe := owner.empty()
	if probe != nil || !empty || !terminationConfirmed(hard, empty) {
		t.Fatalf("exited group cleanup failed: signal=%v empty=%v probe=%v", hard, empty, probe)
	}
	if terminationConfirmed(syscall.EPERM, false) || terminationConfirmed(syscall.EINVAL, true) {
		t.Fatal("unconfirmed termination error accepted")
	}
}
