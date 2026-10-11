package node

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

const (
	shipInterval = time.Second
	shipMaxBytes = 64 << 10
	keepAlive    = 5 * time.Second
)

// logShipper buffers a job's output and posts it to the server about once a
// second, which also serves as the job's keep-alive. The server's reply says
// whether the job was cancelled.
type logShipper struct {
	a       *agent
	path    string
	mu      sync.Mutex
	buf     bytes.Buffer
	lastPut time.Time

	cancelled bool
	done      chan struct{}
	stopped   chan struct{}
}

func newLogShipper(a *agent, buildID int64) *logShipper {
	return &logShipper{
		a:       a,
		path:    "/node/jobs/" + strconv.FormatInt(buildID, 10) + "/log",
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (l *logShipper) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf.Write(p)
	full := l.buf.Len() >= shipMaxBytes
	l.mu.Unlock()
	if full {
		l.ship(context.Background())
	}
	return len(p), nil
}

// start ships on a timer until stop; cancel runs when the server says the
// job was cancelled.
func (l *logShipper) start(ctx context.Context, cancel context.CancelFunc) {
	go func() {
		defer close(l.stopped)
		t := time.NewTicker(shipInterval)
		defer t.Stop()
		for {
			select {
			case <-l.done:
				l.ship(context.Background())
				return
			case <-t.C:
				l.mu.Lock()
				idle := l.buf.Len() == 0 && time.Since(l.lastPut) < keepAlive
				l.mu.Unlock()
				if !idle && l.ship(ctx) {
					cancel()
				}
			}
		}
	}()
}

func (l *logShipper) stop() {
	close(l.done)
	<-l.stopped
}

func (l *logShipper) wasCancelled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cancelled
}

// ship posts whatever is buffered (possibly nothing, as a keep-alive) and
// reports whether the server asked for cancellation. Unsent output stays
// buffered for the next attempt.
func (l *logShipper) ship(ctx context.Context) bool {
	l.mu.Lock()
	chunk := append([]byte(nil), l.buf.Bytes()...)
	l.mu.Unlock()
	resp, err := l.a.post(ctx, l.path, "text/plain", bytes.NewReader(chunk))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var out struct {
		Cancel bool `json:"cancel"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	l.mu.Lock()
	defer l.mu.Unlock()
	if resp.StatusCode == 200 {
		l.buf.Next(len(chunk))
		l.lastPut = time.Now()
	}
	if out.Cancel {
		l.cancelled = true
	}
	return out.Cancel
}
