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

// RingBuffer is a fixed-capacity event store. Oldest events are dropped on overflow.
type RingBuffer struct {
	mu    sync.Mutex
	cap   int
	items []BufferedEvent
}

func NewRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		cap = 1
	}
	return &RingBuffer{cap: cap, items: make([]BufferedEvent, 0, cap)}
}

func (b *RingBuffer) Add(e BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) >= b.cap {
		copy(b.items, b.items[1:])
		b.items = b.items[:len(b.items)-1]
	}
	b.items = append(b.items, e)
}

// Recent returns events with At >= since whose Method is in types.
// If types is empty/nil, all methods match. since=zero matches all.
func (b *RingBuffer) Recent(since time.Time, types []string) []BufferedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BufferedEvent, 0, len(b.items))
	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}
	for _, e := range b.items {
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
