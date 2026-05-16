package cdp

import (
	"encoding/json"
	"sync"
	"time"
)

// BufferedEvent is what we keep in memory per target.
type BufferedEvent struct {
	At        time.Time
	Method    string
	Params    json.RawMessage
	SessionID string
}

// RingBuffer is a fixed-capacity event store backed by a true head/tail ring.
// Add is O(1); the previous implementation did an O(n) slice shift on every
// overflow, which dominated CPU on chatty network pages.
type RingBuffer struct {
	mu    sync.Mutex
	items []BufferedEvent
	head  int // index of the oldest element
	size  int // current number of valid elements
}

func NewRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		cap = 1
	}
	return &RingBuffer{items: make([]BufferedEvent, cap)}
}

func (b *RingBuffer) Add(e BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cap := len(b.items)
	if b.size < cap {
		b.items[(b.head+b.size)%cap] = e
		b.size++
		return
	}
	// Full: overwrite the oldest slot and advance head.
	b.items[b.head] = e
	b.head = (b.head + 1) % cap
}

// Recent returns events with At >= since whose Method is in types, in oldest-to-newest order.
// If types is empty/nil, all methods match. since=zero matches all.
func (b *RingBuffer) Recent(since time.Time, types []string) []BufferedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	cap := len(b.items)
	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}
	out := make([]BufferedEvent, 0, b.size)
	for i := 0; i < b.size; i++ {
		e := b.items[(b.head+i)%cap]
		if !since.IsZero() && e.At.Before(since) {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[e.Method]; !ok {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
