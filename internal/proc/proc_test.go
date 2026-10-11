//go:build !windows

package proc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCancelKillsGrandchildren(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// The shell starts a background grandchild that would write the marker
	// after the timeout if it outlived the kill.
	cmd, err := Command(ctx, "sh", "(sleep 1; touch "+marker+") & sleep 5")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := Run(cmd); err == nil {
		t.Fatal("want an error from the timed-out command")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timed-out command took %s to return", time.Since(start))
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("grandchild survived the timeout")
	}
}

func TestShells(t *testing.T) {
	cmd, err := Command(context.Background(), "", "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "hi" {
		t.Fatalf("default shell: %q %v", out, err)
	}
	if _, err := Command(context.Background(), "fish", "echo"); err == nil {
		t.Error("want an error for an unknown shell")
	}
}
