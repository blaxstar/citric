package runner

import (
	"fmt"
	"io"
	"sync"
)

// Broadcaster fans out build log lines to all active SSE subscribers for a build.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[int64][]chan string
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: make(map[int64][]chan string)}
}

func (b *Broadcaster) Subscribe(buildID int64) (<-chan string, func()) {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.subs[buildID] = append(b.subs[buildID], ch)
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		chans := b.subs[buildID]
		for i, c := range chans {
			if c == ch {
				b.subs[buildID] = append(chans[:i], chans[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, unsub
}

// Close signals build completion by closing all subscriber channels for buildID.
func (b *Broadcaster) Close(buildID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs[buildID] {
		close(ch)
	}
	delete(b.subs, buildID)
}

func (b *Broadcaster) Broadcast(buildID int64, line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[buildID] {
		select {
		case ch <- line:
		default: // drop if subscriber is slow
		}
	}
}

// lineWriter writes each line to the log file and broadcasts it to SSE subscribers.
type lineWriter struct {
	buildID int64
	w       io.Writer
	b       *Broadcaster
	buf     []byte
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.buf = append(lw.buf, p...)
	for {
		i := indexOf(lw.buf, '\n')
		if i < 0 {
			break
		}
		line := string(lw.buf[:i+1])
		lw.buf = lw.buf[i+1:]
		fmt.Fprint(lw.w, line)
		lw.b.Broadcast(lw.buildID, line)
	}
	return len(p), nil
}

// flush writes out a trailing partial line (output that didn't end in a
// newline) once the command has exited.
func (lw *lineWriter) flush() {
	if len(lw.buf) == 0 {
		return
	}
	line := string(lw.buf)
	fmt.Fprint(lw.w, line)
	lw.b.Broadcast(lw.buildID, line)
	lw.buf = nil
}

func indexOf(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
