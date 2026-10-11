// Package proc runs build commands through a shell, and makes cancelling or
// timing one out kill everything it started, not just the shell itself.
package proc

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// waitDelay bounds how long Wait lingers on output pipes after the process
// tree is killed, so a stray grandchild holding stdout can't hang a build.
const waitDelay = 5 * time.Second

// Shells lists the accepted values for a tool's shell setting.
var Shells = []string{"sh", "bash", "pwsh", "powershell", "cmd"}

// DefaultShell is sh, or PowerShell on Windows (pwsh when installed, else the
// built-in Windows PowerShell).
func DefaultShell() string {
	if runtime.GOOS != "windows" {
		return "sh"
	}
	if _, err := exec.LookPath("pwsh"); err == nil {
		return "pwsh"
	}
	return "powershell"
}

// Command returns a command that runs line in shell ("" means DefaultShell).
// The command is set up so that cancelling ctx kills its whole process tree.
// Run it with Run, not cmd.Run, so the tree is tracked from the start.
func Command(ctx context.Context, shell, line string) (*exec.Cmd, error) {
	if shell == "" {
		shell = DefaultShell()
	}
	var args []string
	switch strings.ToLower(shell) {
	case "sh", "bash":
		args = []string{shell, "-c", line}
	case "pwsh", "powershell":
		// $ErrorActionPreference makes a failing cmdlet fail the build, the
		// same way a failing command fails `sh -e`-style scripts people expect.
		args = []string{shell, "-NoProfile", "-NonInteractive", "-Command",
			"$ErrorActionPreference = 'Stop'; " + line}
	case "cmd":
		args = []string{"cmd", "/d", "/s", "/c", line}
	default:
		return nil, fmt.Errorf("unknown shell %q (want one of %s)", shell, strings.Join(Shells, ", "))
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = waitDelay
	setupTree(cmd)
	return cmd, nil
}

// Run starts cmd, places it in its own process group (Unix) or Job Object
// (Windows), and waits for it. Whatever is left of the tree when it exits
// (background jobs the command forgot) is killed too.
func Run(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	cleanup := trackTree(cmd)
	err := cmd.Wait()
	cleanup()
	return err
}
