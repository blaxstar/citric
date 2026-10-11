package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/proc"
)

// defaultVerifyTimeout caps how long a single .citric.toml tool may run during
// local pre-push verification when the tool doesn't declare its own timeout.
// Keeps a slow suite from making `citric push` feel broken (see brokengate.md).
const defaultVerifyTimeout = 30 * time.Second

type verifyResult struct {
	name    string
	status  string // PASS, FAIL, SKIP
	elapsed time.Duration
	detail  string
}

// verifyBeforePush runs the repo's own .citric.toml tools locally before a
// push, so a commit that fails lint/test/etc. is caught before it becomes a
// patchset on the server. It reuses internal/config's .citric.toml parser
// verbatim — the point (per brokengate.md) is that local and server checks
// cannot drift into two different definitions of "passing".
//
// Returns an error (which aborts the push) if any tool failed. A tool that
// isn't installed on this machine, or that runs past its budget, is reported
// as SKIP rather than FAIL — a missing toolchain must not block every push.
func verifyBeforePush() error {
	cfg, err := config.LoadRepoBuildConfig(brand.RepoConfigPath("."))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not parse .citric.toml (%v) — skipping local verification\n", err)
		return nil
	}
	if len(cfg.Tools) == 0 {
		return nil // nothing declared to verify locally
	}

	fmt.Println("running .citric.toml tools locally…")

	results := make([]verifyResult, 0, len(cfg.Tools))
	failed := 0
	for _, tool := range cfg.Tools {
		if tool.Command == "" {
			continue
		}
		r := runVerifyTool(tool)
		if r.status == "FAIL" {
			failed++
		}
		results = append(results, r)
	}

	printVerifyResults(results)

	if failed > 0 {
		return fmt.Errorf("push aborted: %d tool%s failed. --no-verify to override", failed, plural(failed))
	}
	return nil
}

func runVerifyTool(tool config.RepoTool) verifyResult {
	timeout := defaultVerifyTimeout
	if tool.Timeout != "" {
		if d, err := time.ParseDuration(tool.Timeout); err == nil {
			timeout = d
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd, err := proc.Command(ctx, tool.Shell, tool.Command)
	if err != nil {
		return verifyResult{name: tool.Name, status: "FAIL", detail: err.Error()}
	}
	env := os.Environ()
	for k, v := range tool.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	start := time.Now()
	runErr := proc.Run(cmd)
	elapsed := time.Since(start)

	r := verifyResult{name: tool.Name, elapsed: elapsed}
	switch {
	case runErr == nil:
		r.status = "PASS"
	case ctx.Err() == context.DeadlineExceeded:
		r.status = "SKIP"
		r.detail = "(timeout)"
	case isCommandNotFound(runErr):
		r.status = "SKIP"
		r.detail = "(not on PATH)"
	default:
		r.status = "FAIL"
		r.detail = tailLines(out.String(), 10)
	}
	return r
}

func printVerifyResults(results []verifyResult) {
	width := 0
	for _, r := range results {
		if len(r.name) > width {
			width = len(r.name)
		}
	}
	for _, r := range results {
		fmt.Printf("  %-*s  %-4s  %s\n", width, r.name, r.status, r.elapsed.Round(100*time.Millisecond))
		if r.detail == "" {
			continue
		}
		if r.status == "FAIL" {
			for _, line := range strings.Split(r.detail, "\n") {
				fmt.Printf("      %s\n", line)
			}
		} else {
			fmt.Printf("      %s\n", r.detail)
		}
	}
}

// isCommandNotFound reports whether err represents a shell "command not
// found" (exit 127) — the practical signal that a tool isn't installed on
// this machine, since .citric.toml commands are often multi-step shell strings
// rather than a single resolvable binary name (so exec.LookPath on the first
// token isn't reliable).
func isCommandNotFound(err error) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 127
	}
	return false
}

// tailLines returns the last n non-empty-trimmed lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
