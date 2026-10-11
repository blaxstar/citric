package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// decodeRepoToml parses a .citric.toml the way loadRepoPublishConfig does, and
// also reports whether the key leaked into the last [[tool]] table.
func decodeRepoToml(t *testing.T, path string) (RepoPublishConfig, map[string]any) {
	t.Helper()
	var cfg RepoPublishConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		t.Fatalf("decode raw %s: %v", path, err)
	}
	lastTool := map[string]any{}
	if tools, ok := raw["tool"].([]map[string]any); ok && len(tools) > 0 {
		lastTool = tools[len(tools)-1]
	}
	return cfg, lastTool
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".citric.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The core hazard: a top-level key written after a table header becomes a field
// of that table. The file still parses, so the setting is silently ignored —
// citric publish reports "no public_remote set" while it sits right there.
func TestSetRepoTomlKey_StaysAboveTables(t *testing.T) {
	path := writeTemp(t, `# header comment

[[tool]]
name    = "lint"
command = "cargo clippy"
`)

	if err := setRepoTomlKey(path, "public_remote", "git@github.com:me/x.git"); err != nil {
		t.Fatalf("set: %v", err)
	}

	cfg, lastTool := decodeRepoToml(t, path)
	if cfg.PublicRemote != "git@github.com:me/x.git" {
		t.Errorf("PublicRemote = %q, want it set at top level", cfg.PublicRemote)
	}
	if _, leaked := lastTool["public_remote"]; leaked {
		t.Error("public_remote leaked into the [[tool]] table")
	}
	if lastTool["name"] != "lint" {
		t.Errorf("tool table damaged: %v", lastTool)
	}
}

// Repairs a file already broken by the older layout, where the key was written
// below the tool blocks.
func TestSetRepoTomlKey_RescuesStrandedKey(t *testing.T) {
	path := writeTemp(t, `[[tool]]
name    = "lint"
command = "cargo clippy"

public_remote = "git@github.com:me/old.git"
`)

	if err := setRepoTomlKey(path, "public_remote", "git@github.com:me/new.git"); err != nil {
		t.Fatalf("set: %v", err)
	}

	cfg, lastTool := decodeRepoToml(t, path)
	if cfg.PublicRemote != "git@github.com:me/new.git" {
		t.Errorf("PublicRemote = %q, want the new value at top level", cfg.PublicRemote)
	}
	if _, leaked := lastTool["public_remote"]; leaked {
		t.Error("public_remote still stranded inside the [[tool]] table")
	}
	if n := strings.Count(readFile(t, path), "public_remote"); n != 1 {
		t.Errorf("public_remote appears %d times, want exactly 1", n)
	}
}

// The template ships the setting as a commented hint above the tools; setting it
// should fill that line in rather than adding a second one.
func TestSetRepoTomlKey_ReplacesCommentedHint(t *testing.T) {
	path := writeTemp(t, `# header
# public_remote = "git@github.com:you/project.git"
# public_branch = "main"

[[tool]]
name    = "lint"
command = "cargo clippy"
`)

	if err := setRepoTomlKey(path, "public_remote", "git@github.com:me/x.git"); err != nil {
		t.Fatalf("set: %v", err)
	}

	body := readFile(t, path)
	if n := strings.Count(body, "public_remote"); n != 1 {
		t.Errorf("public_remote appears %d times, want 1 (hint replaced, not duplicated)", n)
	}
	if !strings.Contains(body, `# public_branch = "main"`) {
		t.Error("neighbouring commented hint was clobbered")
	}
	cfg, _ := decodeRepoToml(t, path)
	if cfg.PublicRemote != "git@github.com:me/x.git" {
		t.Errorf("PublicRemote = %q", cfg.PublicRemote)
	}
}

func TestSetRepoTomlKey_NoTablesAndMissingFile(t *testing.T) {
	// No tables at all — appending is fine.
	path := writeTemp(t, "# just comments\n")
	if err := setRepoTomlKey(path, "public_branch", "main"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if cfg, _ := decodeRepoToml(t, path); cfg.PublicBranch != "main" {
		t.Errorf("PublicBranch = %q", cfg.PublicBranch)
	}

	// Missing file — created from scratch.
	missing := filepath.Join(t.TempDir(), ".citric.toml")
	if err := setRepoTomlKey(missing, "public_remote", "git@github.com:me/y.git"); err != nil {
		t.Fatalf("set on missing file: %v", err)
	}
	if cfg, _ := decodeRepoToml(t, missing); cfg.PublicRemote != "git@github.com:me/y.git" {
		t.Errorf("PublicRemote = %q", cfg.PublicRemote)
	}
}

// Every generated template must keep its top-level publish keys above the first
// table, or uncommenting them by hand silently does nothing.
func TestRenderTemplate_PublishHintPrecedesTables(t *testing.T) {
	defs := projectDefs(t.TempDir())
	for name, def := range defs {
		body := renderTemplate(def, "example", defs)
		hint := strings.Index(body, "# public_remote =")
		table := strings.Index(body, "\n[[tool]]")
		if hint == -1 {
			t.Errorf("%s: template has no public_remote hint", name)
			continue
		}
		if table != -1 && hint > table {
			t.Errorf("%s: public_remote hint sits below the first [[tool]] table", name)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
