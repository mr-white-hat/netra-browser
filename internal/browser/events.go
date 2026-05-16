package browser

import (
	"context"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// collectedEvents is the fixed set the page subscribes to on attach.
var collectedEvents = []string{
	"Network.requestWillBeSent",
	"Network.responseReceived",
	"Network.loadingFinished",
	"Network.loadingFailed",
	"Page.frameNavigated",
	"Page.javascriptDialogOpening",
	"Page.loadEventFired",
	"Page.domContentEventFired",
	"Runtime.consoleAPICalled",
}

// addEventSub registers a subscriber for the given method and returns its channel.
func (p *Page) addEventSub(method string) chan cdp.BufferedEvent {
	ch := make(chan cdp.BufferedEvent, 16)
	p.subsMu.Lock()
	p.subs = append(p.subs, &eventSub{method: method, ch: ch})
	p.subsMu.Unlock()
	return ch
}

// removeEventSub unregisters a subscriber channel.
func (p *Page) removeEventSub(ch chan cdp.BufferedEvent) {
	p.subsMu.Lock()
	defer p.subsMu.Unlock()
	for i, s := range p.subs {
		if s.ch == ch {
			p.subs = append(p.subs[:i], p.subs[i+1:]...)
			return
		}
	}
}

// fanoutEvent delivers e to all registered page-level subscribers whose method matches.
func (p *Page) fanoutEvent(e cdp.BufferedEvent) {
	p.subsMu.Lock()
	subs := append([]*eventSub(nil), p.subs...)
	p.subsMu.Unlock()
	for _, s := range subs {
		if s.method == e.Method {
			select {
			case s.ch <- e:
			default: // drop on full
			}
		}
	}
}

// startEventCollector subscribes to the fixed event set and pushes each event
// into the page's RingBuffer. Cleanups are stored on the Page so Close can tear
// down both the source subscription and the consumer goroutine for every
// method — preventing the historical leak where each per-tab subscription
// stayed registered with the CDP dispatcher forever.
func (p *Page) startEventCollector(_ context.Context) {
	for _, m := range collectedEvents {
		method := m
		ch, cleanup := p.cdp.SubscribeOnTarget(p.sessionID, method)
		p.closeMu.Lock()
		p.collectorCleanups = append(p.collectorCleanups, cleanup)
		closedCh := p.closedCh
		p.closeMu.Unlock()
		go func() {
			for {
				select {
				case <-closedCh:
					return
				case e, ok := <-ch:
					if !ok {
						return
					}
					if e.Method == "" {
						e.Method = method
					}
					if e.At.IsZero() {
						e.At = time.Now()
					}
					p.events.Add(e)
					p.fanoutEvent(e)
				}
			}
		}()
	}
}

// WaitFor blocks until an event matching method (and optionally predicate) arrives, or timeout fires.
// predicate: if nil, any event of method matches.
func (p *Page) WaitFor(ctx context.Context, method string, predicate func(cdp.BufferedEvent) bool, timeout time.Duration) (*cdp.BufferedEvent, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	src := p.addEventSub(method)
	defer p.removeEventSub(src)

	for {
		select {
		case e, ok := <-src:
			if !ok {
				return nil, context.Canceled
			}
			if predicate != nil && !predicate(e) {
				continue
			}
			return &e, nil
		case <-deadline.C:
			return nil, context.DeadlineExceeded
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// RecentEvents returns events from the buffer with At >= since and Method in types.
// types empty → all methods. since zero → all timestamps.
func (p *Page) RecentEvents(since time.Time, types []string) []cdp.BufferedEvent {
	if p.events == nil {
		return nil
	}
	return p.events.Recent(since, types)
}

// InjectEventForTest places an event directly into the ring buffer. Used by
// tests that don't want to drive a real CDP subscription.
func (p *Page) InjectEventForTest(e cdp.BufferedEvent) {
	if p.events == nil {
		p.events = cdp.NewRingBuffer(1000)
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	p.events.Add(e)
}

// SubscribeMethod registers a subscriber for the given CDP method and returns
// a read-only channel plus a cleanup func. Used by the SSE handler to fan out
// live events to HTTP clients. Caller must invoke cleanup on disconnect.
func (p *Page) SubscribeMethod(method string) (<-chan cdp.BufferedEvent, func()) {
	ch := p.addEventSub(method)
	return ch, func() { p.removeEventSub(ch) }
}

// FanoutEventForTest exposes fanoutEvent so SSE tests can inject events without
// driving a real CDP subscription.
func (p *Page) FanoutEventForTest(e cdp.BufferedEvent) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	p.fanoutEvent(e)
}
