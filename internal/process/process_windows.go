//go:build windows

package process

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

type ownership struct {
	job windows.Handle
	pid uint32
}

func prepare(cmd *exec.Cmd) (*ownership, error) {
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return nil, e
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP}
	return &ownership{job: job}, nil
}
func (o *ownership) started(cmd *exec.Cmd) error {
	o.pid = uint32(cmd.Process.Pid)
	process, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, o.pid)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(process)
	if e = windows.AssignProcessToJobObject(o.job, process); e != nil {
		return e
	}
	// CREATE_SUSPENDED prevents the primary thread from running any source code.
	// Its thread is resumed only after successful kill-on-close job assignment.
	snapshot, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if e != nil {
		return e
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	e = windows.Thread32First(snapshot, &entry)
	for e == nil {
		if entry.OwnerProcessID == o.pid {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return openErr
			}
			defer windows.CloseHandle(thread)
			count, callErr := windows.ResumeThread(thread)
			if callErr != nil {
				return callErr
			}
			if count != 1 {
				return ErrCapture
			}
			return nil
		}
		e = windows.Thread32Next(snapshot, &entry)
	}
	return ErrCapture
}
func (o *ownership) stop(force bool) error {
	if force {
		return windows.TerminateJobObject(o.job, 1)
	}
	if o.pid == 0 {
		return nil
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, o.pid)
}
func (o *ownership) empty() (bool, error) {
	var info struct {
		TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
		TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
	}
	e := windows.QueryInformationJobObject(o.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	return info.ActiveProcesses == 0, e
}
func (o *ownership) close() {
	if o.job != 0 {
		windows.CloseHandle(o.job)
		o.job = 0
	}
}
func exitSignal(s *os.ProcessState) string { return "" }
func privateAttributes(directory bool) (*windows.SecurityAttributes, error) {
	token, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return nil, e
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		return nil, e
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	sd, e := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;" + flags + ";FA;;;" + user.User.Sid.String() + ")")
	if e != nil {
		return nil, e
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}
func makePrivateDir(p string) error {
	sa, e := privateAttributes(true)
	if e != nil {
		return e
	}
	path, e := windows.UTF16PtrFromString(p)
	if e != nil {
		return e
	}
	return windows.CreateDirectory(path, sa)
}
func privateFile(p string) (*os.File, error) {
	sa, e := privateAttributes(false)
	if e != nil {
		return nil, e
	}
	path, e := windows.UTF16PtrFromString(p)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(path, windows.GENERIC_WRITE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), p), nil
}

// UserOnly requires a protected DACL containing only the current user's ACE.
func UserOnly(path string, isDir bool) bool {
	st, e := os.Lstat(path)
	if e != nil || st.Mode()&os.ModeSymlink != 0 || st.IsDir() != isDir || (!isDir && !st.Mode().IsRegular()) {
		return false
	}
	token, e := windows.OpenCurrentProcessToken()
	if e != nil {
		return false
	}
	defer token.Close()
	user, e := token.GetTokenUser()
	if e != nil {
		return false
	}
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if e != nil {
		return false
	}
	owner, _, e := sd.Owner()
	if e != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return false
	}

	acl, _, e := sd.DACL()
	if e != nil || acl == nil || acl.AceCount != 1 {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(acl, 0, &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return false
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	return sid.Equals(user.User.Sid)
}

func observeExit(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() {
		h, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
		if e != nil {
			ch <- e
			return
		}
		defer windows.CloseHandle(h)
		status, e := windows.WaitForSingleObject(h, windows.INFINITE)
		if e == nil && status != windows.WAIT_OBJECT_0 {
			e = ErrCapture
		}
		ch <- e
	}()
	return ch
}

func terminationConfirmed(signalErr error, empty bool) bool { return empty && signalErr == nil }
