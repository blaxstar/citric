package runner

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// TestMaxConcurrentPerCL proves the per-CL cap actually limits how many of
// one CL's own tools run simultaneously (see critcache.md) — not just that it
// compiles. Four independent (no depends_on) tools are configured, and
// MaxConcurrent (the global cap) is set high so only the per-CL cap is under
// test. Each tool command drops a marker file for the duration of its run; a
// background poller records the peak number of markers seen at once, which
// must never exceed the configured cap.
func TestMaxConcurrentPerCL(t *testing.T) {
	reposDir := t.TempDir()
	sha := initBareRepo(t, reposDir, "demo", "")

	db, err := store.Open(filepath.Join(t.TempDir(), "citric.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	markerDir := t.TempDir()
	toolCmd := func(name string) string { return markerCmd(markerDir, name, 300) }

	const perCLCap = 2
	cfg := &config.Config{
		Server: config.Server{
			ReposDir:           reposDir,
			DataDir:            t.TempDir(),
			MaxConcurrent:      10, // high — isolate the per-CL cap from the global one
			MaxConcurrentPerCL: perCLCap,
			BuildPath:          testBuildPath(),
		},
		BuildTools: []config.BuildTool{
			{Name: "a", Command: toolCmd("a")},
			{Name: "b", Command: toolCmd("b")},
			{Name: "c", Command: toolCmd("c")},
			{Name: "d", Command: toolCmd("d")},
		},
	}
	r := New(db, cfg)

	clID, err := db.CreateCL(&model.CL{ChangeID: "Itest", Repo: "demo", TargetBranch: "main", Author: "t", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := db.GetCL(clID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreatePatchset(&model.Patchset{CLID: clID, Number: 1, CommitSHA: sha, BaseSHA: sha, Ref: "refs/x"}); err != nil {
		t.Fatal(err)
	}
	ps, err := db.LatestPatchset(clID)
	if err != nil || ps == nil {
		t.Fatalf("latest patchset: %v", err)
	}

	done := make(chan struct{})
	var peak int32
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				entries, _ := os.ReadDir(markerDir)
				if n := int32(len(entries)); n > atomic.LoadInt32(&peak) {
					atomic.StoreInt32(&peak, n)
				}
			}
		}
	}()

	if err := r.Run(cl, ps, nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		builds, err := db.ListBuilds(clID, ps.ID)
		if err != nil {
			t.Fatal(err)
		}
		allDone := len(builds) == len(cfg.BuildTools)
		for _, b := range builds {
			if b.Status != model.BuildStatusPass && b.Status != model.BuildStatusFail {
				allDone = false
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("builds did not finish in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(done)

	if peak == 0 {
		t.Fatal("never observed any tool running — test harness is broken, not just the cap")
	}
	if peak > perCLCap {
		t.Errorf("observed %d tools running simultaneously for one CL, want <= MaxConcurrentPerCL=%d", peak, perCLCap)
	} else {
		t.Logf("peak simultaneous tools for this CL: %d (cap: %d)", peak, perCLCap)
	}
}

// TestQueueMode proves that with Server.QueueMode on, two different CLs'
// tools never run at the same time — even with MaxConcurrent set high enough
// to freely allow it otherwise. Each CL gets 2 independent tools; a
// background poller records, at each sample, which CL(s) have a live marker
// file, and fails if both are ever seen at once.
func TestQueueMode(t *testing.T) {
	reposDir := t.TempDir()
	sha := initBareRepo(t, reposDir, "demo", "")

	db, err := store.Open(filepath.Join(t.TempDir(), "citric.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	markerDir := t.TempDir()
	toolCmd := func(prefix, name string) string { return markerCmd(markerDir, prefix+"-"+name, 200) }

	cfg := &config.Config{
		Server: config.Server{
			ReposDir:           reposDir,
			DataDir:            t.TempDir(),
			MaxConcurrent:      10, // deliberately high — queue mode must ignore this entirely
			MaxConcurrentPerCL: 2,
			QueueMode:          true,
			BuildPath:          testBuildPath(),
		},
	}
	r := New(db, cfg)

	makeCL := func(prefix, changeID string) (*model.CL, *model.Patchset) {
		t.Helper()
		clID, err := db.CreateCL(&model.CL{ChangeID: changeID, Repo: "demo", TargetBranch: "main", Author: "t", Title: "t"})
		if err != nil {
			t.Fatal(err)
		}
		cl, err := db.GetCL(clID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreatePatchset(&model.Patchset{CLID: clID, Number: 1, CommitSHA: sha, BaseSHA: sha, Ref: "refs/x"}); err != nil {
			t.Fatal(err)
		}
		ps, err := db.LatestPatchset(clID)
		if err != nil || ps == nil {
			t.Fatalf("latest patchset: %v", err)
		}
		return cl, ps
	}

	cl1, ps1 := makeCL("cl1", "Itest1")
	cl2, ps2 := makeCL("cl2", "Itest2")

	cfg.BuildTools = []config.BuildTool{
		{Name: "a", Command: toolCmd("cl1", "a")},
		{Name: "b", Command: toolCmd("cl1", "b")},
	}
	done := make(chan struct{})
	var overlapObserved, cl1Seen, cl2Seen int32
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				entries, _ := os.ReadDir(markerDir)
				var sawCL1, sawCL2 bool
				for _, e := range entries {
					switch {
					case len(e.Name()) >= 3 && e.Name()[:3] == "cl1":
						sawCL1 = true
					case len(e.Name()) >= 3 && e.Name()[:3] == "cl2":
						sawCL2 = true
					}
				}
				if sawCL1 {
					atomic.StoreInt32(&cl1Seen, 1)
				}
				if sawCL2 {
					atomic.StoreInt32(&cl2Seen, 1)
				}
				if sawCL1 && sawCL2 {
					atomic.StoreInt32(&overlapObserved, 1)
				}
			}
		}
	}()

	if err := r.Run(cl1, ps1, nil); err != nil {
		t.Fatal(err)
	}
	// cfg.BuildTools is read fresh by selectTools() on each Run call, so
	// swapping it between the two Run calls gives each CL its own tool set
	// without needing per-CL server registries.
	cfg.BuildTools = []config.BuildTool{
		{Name: "a", Command: toolCmd("cl2", "a")},
		{Name: "b", Command: toolCmd("cl2", "b")},
	}
	if err := r.Run(cl2, ps2, nil); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		b1, err := db.ListBuilds(cl1.ID, ps1.ID)
		if err != nil {
			t.Fatal(err)
		}
		b2, err := db.ListBuilds(cl2.ID, ps2.ID)
		if err != nil {
			t.Fatal(err)
		}
		allDone := len(b1) == 2 && len(b2) == 2
		for _, b := range append(b1, b2...) {
			if b.Status != model.BuildStatusPass && b.Status != model.BuildStatusFail {
				allDone = false
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("builds did not finish in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(done)

	if atomic.LoadInt32(&cl1Seen) == 0 || atomic.LoadInt32(&cl2Seen) == 0 {
		t.Fatal("never observed activity from both CLs — test harness is broken, not just queue mode")
	}
	if atomic.LoadInt32(&overlapObserved) == 1 {
		t.Error("observed CL1 and CL2 tools running at the same time with QueueMode on")
	}
}
