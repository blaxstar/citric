package brand

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoConfigPath(t *testing.T) {
	dir := t.TempDir()
	if got, want := RepoConfigPath(dir), filepath.Join(dir, ".citric.toml"); got != want {
		t.Errorf("no config: got %s, want %s (the name a new file should get)", got, want)
	}
	touch(t, filepath.Join(dir, ".crit.toml"))
	if got, want := RepoConfigPath(dir), filepath.Join(dir, ".crit.toml"); got != want {
		t.Errorf("legacy only: got %s, want %s", got, want)
	}
	touch(t, filepath.Join(dir, ".citric.toml"))
	if got, want := RepoConfigPath(dir), filepath.Join(dir, ".citric.toml"); got != want {
		t.Errorf("both present: got %s, want %s", got, want)
	}
}

func TestDataFileKeepsLegacyDB(t *testing.T) {
	dir := t.TempDir()
	if got, want := DataFile(dir, ".db"), filepath.Join(dir, "citric.db"); got != want {
		t.Errorf("fresh install: got %s, want %s", got, want)
	}
	touch(t, filepath.Join(dir, "crit.db"))
	if got, want := DataFile(dir, ".db"), filepath.Join(dir, "crit.db"); got != want {
		t.Errorf("existing install must keep its database: got %s, want %s", got, want)
	}
}

func TestGetenvFallsBack(t *testing.T) {
	t.Setenv("CRIT_BRANDTEST", "old")
	if got := Getenv("BRANDTEST"); got != "old" {
		t.Errorf("legacy only: got %q, want old", got)
	}
	t.Setenv("CITRIC_BRANDTEST", "new")
	if got := Getenv("BRANDTEST"); got != "new" {
		t.Errorf("both set: got %q, want new", got)
	}
}

func TestIsServiceUser(t *testing.T) {
	for name, want := range map[string]bool{"citric": true, "crit": true, "alice": false, "": false} {
		if got := IsServiceUser(name); got != want {
			t.Errorf("IsServiceUser(%q) = %v, want %v", name, got, want)
		}
	}
}
