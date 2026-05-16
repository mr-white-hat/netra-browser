// Package browser exposes the higher-level browser primitives that build on cdp.
package browser

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// Sender is what Page needs from a CDP transport. *cdp.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	SendOnTarget(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
	AttachToTarget(ctx context.Context, targetID string) (string, error)
	SubscribeOnTarget(sessionID, method string) (<-chan cdp.BufferedEvent, func())
}

// eventSub is a single WaitFor subscriber.
type eventSub struct {
	method string
	ch     chan cdp.BufferedEvent
}

// Page binds a single target plus its CDP session.
type Page struct {
	cdp       Sender
	targetID  string
	sessionID string

	mu       sync.Mutex
	snapshot *Snapshot // last snapshot for snapshot_id resolution (filled in Task 7)

	events *cdp.RingBuffer

	subsMu sync.Mutex
	subs   []*eventSub

	// collectorCleanups are the per-method SubscribeOnTarget cleanup funcs
	// installed by startEventCollector. Page.Close calls each, unwinding the
	// goroutine + source subscription pair for that method.
	collectorCleanups []func()
	// closedCh fans out a single "page is closing" signal to all collector
	// goroutines so they exit promptly rather than blocking on a stale src.
	closedCh chan struct{}
	closeMu  sync.Mutex
	closed   bool
}

// Close stops every collector goroutine and unsubscribes from CDP events for
// this Page. After Close returns, the Page is unusable; the parent Session
// should drop it from its map. Safe to call multiple times.
func (p *Page) Close() {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return
	}
	p.closed = true
	cleanups := p.collectorCleanups
	p.collectorCleanups = nil
	if p.closedCh != nil {
		close(p.closedCh)
	}
	p.closeMu.Unlock()
	for _, c := range cleanups {
		c()
	}
}

// NewPage attaches to a target and enables Page/DOM/Runtime/Accessibility domains.
func NewPage(ctx context.Context, c Sender, targetID string) (*Page, error) {
	sid, err := c.AttachToTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	p := &Page{cdp: c, targetID: targetID, sessionID: sid, closedCh: make(chan struct{})}
	for _, m := range []string{"Page.enable", "DOM.enable", "Runtime.enable", "Accessibility.enable", "Network.enable"} {
		if _, err := c.SendOnTarget(ctx, sid, m, nil); err != nil {
			return nil, err
		}
	}
	p.events = cdp.NewRingBuffer(1000)
	p.startEventCollector(ctx)
	return p, nil
}

// TargetID returns the underlying target id.
func (p *Page) TargetID() string { return p.targetID }

// SessionID returns the attached session id.
func (p *Page) SessionID() string { return p.sessionID }

// send is the internal helper for routing to the right session.
func (p *Page) send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.cdp.SendOnTarget(ctx, p.sessionID, method, params)
}

// SendCDP issues a raw CDP method on this Page's target session.
// Public escape hatch for tools that need domains the typed Page API doesn't
// wrap (e.g. DOMStorage in task_save_session).
func (p *Page) SendCDP(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.send(ctx, method, params)
}

// Snapshot is filled in Task 7. The type is forward-declared here so locator.go
// (Task 8) can reference Page.snapshot.
type Snapshot struct {
	Nodes []SnapshotNode
	byID  map[string]*SnapshotNode
}

// SnapshotNode is one node in the compact accessibility tree.
type SnapshotNode struct {
	ID       string         `json:"id"`
	Role     string         `json:"role"`
	Name     string         `json:"name,omitempty"`
	Value    string         `json:"value,omitempty"`
	Children []SnapshotNode `json:"children,omitempty"`

	// internal: backend node id from Accessibility.getFullAXTree
	BackendNodeID int64 `json:"-"`
}
