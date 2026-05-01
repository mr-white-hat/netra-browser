// Package browser exposes the higher-level browser primitives that build on cdp.
package browser

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

// Sender is what Page needs from a CDP transport. *cdp.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	SendOnTarget(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
	AttachToTarget(ctx context.Context, targetID string) (string, error)
	SubscribeOnTarget(sessionID, method string) chan cdp.BufferedEvent
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
}

// NewPage attaches to a target and enables Page/DOM/Runtime/Accessibility domains.
func NewPage(ctx context.Context, c Sender, targetID string) (*Page, error) {
	sid, err := c.AttachToTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	p := &Page{cdp: c, targetID: targetID, sessionID: sid}
	for _, m := range []string{"Page.enable", "DOM.enable", "Runtime.enable", "Accessibility.enable"} {
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
