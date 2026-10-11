package runner

import "sync"

// EventHub is a tiny per-CL pub/sub used to push "something changed" pings to
// SSE subscribers (build created/started/finished, etc.). It carries no payload
// — subscribers re-fetch authoritative state when pinged.
type EventHub struct {
	mu   sync.Mutex
	subs map[int64]map[chan struct{}]struct{} // clID → set of subscriber channels
}

func NewEventHub() *EventHub {
	return &EventHub{subs: make(map[int64]map[chan struct{}]struct{})}
}

// Subscribe returns a channel that receives a signal whenever the CL changes,
// plus an unsubscribe func. The channel is buffered/coalescing: rapid bursts
// collapse into a single pending signal.
func (h *EventHub) Subscribe(clID int64) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[clID] == nil {
		h.subs[clID] = make(map[chan struct{}]struct{})
	}
	h.subs[clID][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if m := h.subs[clID]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subs, clID)
			}
		}
		h.mu.Unlock()
	}
}

// Publish signals all subscribers for a CL without blocking.
func (h *EventHub) Publish(clID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[clID] {
		select {
		case ch <- struct{}{}:
		default: // a signal is already pending — coalesce
		}
	}
}
