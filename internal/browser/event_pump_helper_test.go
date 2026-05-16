package browser

import (
	"sync"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// continuouslyEmptyEvents returns a (channel, cleanup) pair shaped like the
// real cdp.SubscribeOnTarget. An empty event is delivered immediately so
// any sub already registered fires; a 50ms ticker keeps emitting after that
// so a sub registered slightly AFTER startEventCollector starts pumping
// still gets matched on a subsequent fanout. Closes a pre-existing race
// where a single buffered event could be drained microseconds before a
// Navigate / WaitFor call registered its sub.
func continuouslyEmptyEvents() (<-chan cdp.BufferedEvent, func()) {
	ch := make(chan cdp.BufferedEvent, 1)
	done := make(chan struct{})
	go func() {
		select {
		case ch <- cdp.BufferedEvent{}:
		case <-done:
			return
		}
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
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
