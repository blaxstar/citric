package runner

import (
	"testing"
	"time"
)

func TestLabelsMatch(t *testing.T) {
	cases := []struct {
		runsOn, have []string
		want         bool
	}{
		{nil, []string{"linux"}, true},
		{[]string{"windows"}, []string{"linux", "amd64"}, false},
		{[]string{"Windows"}, []string{"windows", "amd64"}, true},
		{[]string{"linux", "gpu"}, []string{"linux", "amd64"}, false},
		{[]string{"linux", "gpu"}, []string{"linux", "amd64", "gpu"}, true},
	}
	for _, c := range cases {
		if got := LabelsMatch(c.runsOn, c.have); got != c.want {
			t.Errorf("LabelsMatch(%v, %v) = %v, want %v", c.runsOn, c.have, got, c.want)
		}
	}
}

func TestClaimSkipsJobsTheExecutorCantRun(t *testing.T) {
	d := newDispatcher()
	win := &job{runsOn: []string{"windows"}, remote: RemoteJob{BuildID: 1}}
	any := &job{remote: RemoteJob{BuildID: 2}}
	d.pending = []*job{win, any}

	got := d.claim([]string{"linux", "amd64"}, "n1", 0, nil)
	if got != any {
		t.Fatalf("linux executor should get the unconstrained job, got %+v", got)
	}
	if len(d.pending) != 1 || d.pending[0] != win {
		t.Fatalf("windows job should stay queued, pending = %v", d.pending)
	}
	if d.running[2] == nil || d.running[2].node != "n1" {
		t.Fatal("claimed job should be tracked as running on n1")
	}
}

func TestClaimWakesOnNewWork(t *testing.T) {
	d := newDispatcher()
	got := make(chan *job, 1)
	go func() { got <- d.claim(nil, "n1", 2*time.Second, nil) }()
	time.Sleep(50 * time.Millisecond)
	j := &job{remote: RemoteJob{BuildID: 7}}
	d.mu.Lock()
	d.pending = append(d.pending, j)
	d.changed()
	d.mu.Unlock()
	select {
	case c := <-got:
		if c != j {
			t.Fatalf("got %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting claim was not woken by new work")
	}
}
