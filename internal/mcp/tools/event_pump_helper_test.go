package tools

import (
	"sync"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// continuouslyEmptyEvents returns a (channel, cleanup) pair shaped like the
// real cdp.SubscribeOnTarget. Empty BufferedEvents are emitted on a 1ms
// ticker so any addEventSub registered AFTER startEventCollector starts
// pumping still gets matched on a subsequent fanout. This closes a
// pre-existing test race where a single pre-buffered event could be drained
// (and fanned out to no subscribers) microseconds before a Navigate /
// WaitFor call registered its sub — making tests like TestNavigateTool hang
// roughly 1 in 50 runs under -race. The ticker rate is low enough that the
// 9-per-page collector goroutines don't starve the main test goroutine.
func continuouslyEmptyEvents() (<-chan cdp.BufferedEvent, func()) {
	ch := make(chan cdp.BufferedEvent, 1)
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		// Prime once so a sub already registered at startup gets immediate
		// delivery, then tick.
		select {
		case ch <- cdp.BufferedEvent{}:
		case <-done:
			return
		}
		for {
			select {
			case <-done:
				return
			case <-t.C:
				select {
				case ch <- cdp.BufferedEvent{}:
				default:
				}
			}
		}
	}()
	var once sync.Once
	return ch, func() { once.Do(func() { close(done) }) }
}
