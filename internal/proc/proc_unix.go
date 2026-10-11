//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

// setupTree starts the command in a new process group and makes cancellation
// signal the whole group.
func setupTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// trackTree kills any of the group still running once the shell has exited.
func trackTree(cmd *exec.Cmd) func() {
	pid := cmd.Process.Pid
	return func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }
}
