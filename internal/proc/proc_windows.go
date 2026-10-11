//go:build windows

package proc

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// treeJobs maps a running command to the Job Object holding its tree.
var treeJobs sync.Map // *exec.Cmd -> windows.Handle

// setupTree makes cancellation terminate the command's Job Object, which ends
// every process in it, however deeply nested.
func setupTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if v, ok := treeJobs.Load(cmd); ok {
			return windows.TerminateJobObject(v.(windows.Handle), 1)
		}
		return cmd.Process.Kill()
	}
}

// trackTree puts the started process in a fresh Job Object configured to kill
// its members when the job is closed; closing it after the build therefore
// also reaps background processes the command left behind. Children the
// shell spawns from here on join the job automatically.
func trackTree(cmd *exec.Cmd) func() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, h)
		windows.CloseHandle(h)
	}
	if err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	treeJobs.Store(cmd, job)
	return func() {
		treeJobs.Delete(cmd)
		windows.CloseHandle(job)
	}
}
