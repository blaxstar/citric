package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// publishRepo builds a repo with three commits: one adding a source file plus
// .citric.toml, one touching ONLY .citric.toml, and one adding another source file.
// Every message carries a Change-Id, as citric's commit-msg hook would stamp.
// It chdirs into the repo for the duration of the test.
func publishRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })

	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e", "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("app.go", "package main\n")
	write(".citric.toml", "[[tool]]\nname = \"lint\"\n")
	git("add", "-A")
	git("commit", "-q", "-m", "feat: the app\n\nChange-Id: I1111111111111111111111111111111111111111")

	write(".citric.toml", "[[tool]]\nname = \"lint\"\ncommand = \"golangci-lint run\"\n")
	git("add", "-A")
	git("commit", "-q", "-m", "chore: configure lint in .citric.toml\n\nChange-Id: I2222222222222222222222222222222222222222")

	write("lib.go", "package main\n")
	git("add", "-A")
	git("commit", "-q", "-m", "feat: a library\n\nChange-Id: I3333333333333333333333333333333333333333")

	return dir
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// .citric.toml configures this citric server and means nothing to anyone outside it,
// so the public mirror must never contain it — in any commit, not just the tip.
func TestRewriteForPublish_StripsCritTomlFromEveryCommit(t *testing.T) {
	dir := publishRepo(t)

	newTip, kept, dropped, err := rewriteForPublish("HEAD", defaultPublishOptions())
	if err != nil {
		t.Fatalf("rewriteForPublish: %v", err)
	}
	if kept == 0 {
		t.Fatal("nothing was published")
	}

	list := gitIn(t, dir, "rev-list", newTip)
	for _, sha := range strings.Fields(list) {
		files := gitIn(t, dir, "ls-tree", "-r", "--name-only", sha)
		for _, f := range strings.Fields(files) {
			if f == ".citric.toml" {
				t.Errorf("commit %s still carries .citric.toml", sha[:8])
			}
		}
	}

	// The middle commit touched nothing else, so it has no content left to publish.
	if dropped != 1 {
		t.Errorf("want the .citric.toml-only commit dropped, got dropped=%d kept=%d", dropped, kept)
	}
	if kept != 2 {
		t.Errorf("want 2 real commits kept, got %d", kept)
	}
}

// Change-Id stripping is the older half of the same contract; keep it covered.
func TestRewriteForPublish_StripsChangeIDs(t *testing.T) {
	dir := publishRepo(t)

	newTip, _, _, err := rewriteForPublish("HEAD", defaultPublishOptions())
	if err != nil {
		t.Fatalf("rewriteForPublish: %v", err)
	}
	msgs := gitIn(t, dir, "log", "--format=%B", newTip)
	if strings.Contains(msgs, "Change-Id:") {
		t.Errorf("published history still contains a Change-Id trailer:\n%s", msgs)
	}
	if !strings.Contains(msgs, "feat: the app") || !strings.Contains(msgs, "feat: a library") {
		t.Errorf("published history lost real commit subjects:\n%s", msgs)
	}
}

// Re-publishing must produce identical SHAs, otherwise every mirror push would
// need --force and the public history would churn.
func TestRewriteForPublish_IsDeterministic(t *testing.T) {
	publishRepo(t)

	first, _, _, err := rewriteForPublish("HEAD", defaultPublishOptions())
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := rewriteForPublish("HEAD", defaultPublishOptions())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("rewrite is not deterministic: %s then %s", first[:8], second[:8])
	}
}

// The rewrite writes objects but must not move the local branch: the internal
// repo keeps its Change-Ids and its .citric.toml.
func TestRewriteForPublish_LeavesLocalBranchAlone(t *testing.T) {
	dir := publishRepo(t)
	before := gitIn(t, dir, "rev-parse", "HEAD")

	if _, _, _, err := rewriteForPublish("HEAD", defaultPublishOptions()); err != nil {
		t.Fatal(err)
	}

	if after := gitIn(t, dir, "rev-parse", "HEAD"); after != before {
		t.Errorf("local HEAD moved: %s → %s", before[:8], after[:8])
	}
	files := gitIn(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if !strings.Contains(files, ".citric.toml") {
		t.Error("local tree lost .citric.toml — the rewrite must only touch published objects")
	}
}

func defaultPublishOptions() publishOptions {
	return publishOptions{exclude: publishExcludePaths}
}

// publish_base: everything up to the base commit becomes one initial commit;
// later commits are replayed on top. Re-running gives the same SHAs.
func TestRewriteForPublish_BaseStartsFreshHistory(t *testing.T) {
	dir := publishRepo(t)
	base := gitIn(t, dir, "rev-parse", "HEAD~1")
	opts := defaultPublishOptions()
	opts.base = base

	tip, kept, _, err := rewriteForPublish("HEAD", opts)
	if err != nil {
		t.Fatal(err)
	}
	if kept != 2 {
		t.Errorf("want the squashed root plus one replayed commit, kept=%d", kept)
	}
	log := gitIn(t, dir, "log", "--format=%s", tip)
	if log != "feat: a library\nInitial public release" {
		t.Errorf("unexpected public history:\n%s", log)
	}
	again, _, _, _ := rewriteForPublish("HEAD", opts)
	if again != tip {
		t.Error("publishing twice must give the same tip, so mirror pushes fast-forward")
	}
}

func TestRewriteForPublish_ExcludesDirectoriesAndRewritesIdentity(t *testing.T) {
	dir := publishRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "internal-docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal-docs", "plan.md"), []byte("secret plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	cmd := exec.Command("git", "-c", "user.name=Dev", "-c", "user.email=dev@corp.example", "commit", "-q", "-m", "docs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}

	opts := defaultPublishOptions()
	opts.exclude = append(opts.exclude, "internal-docs")
	opts.authorName, opts.authorEmail = "Example Org", "oss@example.org"
	tip, _, _, err := rewriteForPublish("HEAD", opts)
	if err != nil {
		t.Fatal(err)
	}
	if files := gitIn(t, dir, "ls-tree", "-r", "--name-only", tip); strings.Contains(files, "internal-docs") {
		t.Errorf("excluded directory was published:\n%s", files)
	}
	if ids := gitIn(t, dir, "log", "--format=%an <%ae> %cn <%ce>", tip); strings.Contains(ids, "corp.example") {
		t.Errorf("original identity leaked:\n%s", ids)
	}
}

func TestForbiddenInPublishFindsContentAndMessages(t *testing.T) {
	dir := publishRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("deploy to intranet.corp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	cmd := exec.Command("git", "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "-m", "mention INTRANET.corp here")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
	opts := defaultPublishOptions()
	cfg := &RepoPublishConfig{PublishForbid: `intranet\.corp`}
	resolved, err := publishOptionsFrom(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tip, _, _, err := rewriteForPublish("HEAD", opts)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := forbiddenInPublish(tip, resolved.forbid, false)
	if err != nil {
		t.Fatal(err)
	}
	var file, msg bool
	for _, h := range hits {
		file = file || strings.Contains(h, "notes.txt")
		msg = msg || strings.Contains(h, "mention INTRANET")
	}
	if !file || !msg {
		t.Errorf("want a file hit and a message hit, got %v", hits)
	}
}
