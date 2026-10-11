//go:build windows

package proc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDefaultShellIsPowerShell(t *testing.T) {
	if sh := DefaultShell(); sh != "pwsh" && sh != "powershell" {
		t.Fatalf("default shell on Windows = %q, want PowerShell", sh)
	}
	cmd, err := Command(context.Background(), "", "Write-Output hi")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("got %q, %v", out, err)
	}
}

// A timed-out command, and the child it started, must be gone promptly: the
// Job Object ends the whole tree rather than waiting out the child's sleep.
func TestTimeoutEndsTheTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd, err := Command(ctx, "", `Start-Process -NoNewWindow -Wait powershell -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 60'`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := Run(cmd); err == nil {
		t.Fatal("want an error from the timed-out command")
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("timed-out command took %s to return; its child kept it alive", took)
	}
}
