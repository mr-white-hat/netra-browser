package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mr-white-hat/netra-browser/internal/browser"
)

// CDPSender is the interface the session holds onto.
// Concrete impl is *cdp.Client; tests pass fakes.
type CDPSender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	Close() error
}

// CDPCloser is kept as an alias for older code that imported the name.
type CDPCloser = CDPSender

// ProjectScope is the subset of *profile.Project that Session needs. Nil = no scoping
// (every tab is visible, no auto-tagging on new tabs).
type ProjectScope interface {
	Adopt(targetID string) error
	Release(targetID string) error
	Owns(targetID string) bool
	Targets() []string
}

// Session is per-MCP-process state: one active CDP connection, one active target.
type Session struct {
	mu           sync.RWMutex
	client       CDPSender
	activeTarget string
	pages        map[string]*browser.Page
	project      ProjectScope
	// clientCloser is an optional hook installed at attach time (e.g. the
	// target-lifecycle watcher) that must be stopped before the underlying
	// client is closed.
	clientCloser func()
	// Friendly short tab IDs (t1, t2, ...) layered on top of CDP target IDs
	// so agents don't have to round-trip the 32-char hex string. Allocated
	// lazily on first request per target_id; never reused once allocated.
	nextTabSeq int
	tabIDs     map[string]string // target_id → "tN"
	// Per-agent tab groups (see group.go). Each agent works in its own group
	// with its own active tab, so concurrent agents never clobber a shared
	// "active target". groups: id → group; targetGroup: target_id → owning id.
	groups       map[string]*tabGroup
	targetGroup  map[string]string
	nextGroupSeq int
}

func NewSession() *Session {
	return &Session{
		pages:       map[string]*browser.Page{},
		tabIDs:      map[string]string{},
		groups:      map[string]*tabGroup{},
		targetGroup: map[string]string{},
	}
}

// TabIDFor returns the short id (e.g. "t1") assigned to targetID, allocating
// one on first call. Returns "" for an empty input.
func (s *Session) TabIDFor(targetID string) string {
	if targetID == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.tabIDs[targetID]; ok {
		return id
	}
	s.nextTabSeq++
	id := fmt.Sprintf("t%d", s.nextTabSeq)
	s.tabIDs[targetID] = id
	return id
}

// ResolveTabID accepts either a CDP target_id or a friendly tab id ("t1") and
// returns the underlying target_id. Unknown inputs pass through so an
// unrelated string still produces a deterministic "no such target" downstream.
func (s *Session) ResolveTabID(idOrTab string) string {
	if idOrTab == "" {
		return ""
	}
	if len(idOrTab) < 2 || idOrTab[0] != 't' {
		return idOrTab
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for tid, label := range s.tabIDs {
		if label == idOrTab {
			return tid
		}
	}
	return idOrTab
}

// SetProject installs the per-bridge project scope. Pass nil to disable scoping.
func (s *Session) SetProject(p ProjectScope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.project = p
}

// Project returns the installed project scope (may be nil).
func (s *Session) Project() ProjectScope {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.project
}

func (s *Session) SetClient(c CDPSender) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = c
}

// SetClientCloser registers a func to be invoked just before the active client
// is closed (typically a target-lifecycle watcher's stop func). Overwrites any
// previously registered closer.
func (s *Session) SetClientCloser(stop func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientCloser = stop
}

func (s *Session) Client() CDPSender {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *Session) IsAttached() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client != nil
}

func (s *Session) Clear() {
	s.mu.Lock()
	pages := s.pages
	s.pages = map[string]*browser.Page{}
	closer := s.clientCloser
	s.clientCloser = nil
	if s.client != nil {
		// Stop the lifecycle watcher (if any) before closing the underlying
		// websocket — otherwise the watcher's read returns an error and races
		// against Close.
		if closer != nil {
			closer()
		}
		_ = s.client.Close()
		s.client = nil
	}
	s.activeTarget = ""
	s.groups = map[string]*tabGroup{}
	s.targetGroup = map[string]string{}
	s.nextGroupSeq = 0
	s.mu.Unlock()
	for _, p := range pages {
		p.Close()
	}
}

// Page returns a cached *browser.Page for targetID, creating it on first touch.
func (s *Session) Page(ctx context.Context, targetID string) (*browser.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, fmt.Errorf("not attached")
	}
	if p, ok := s.pages[targetID]; ok {
		return p, nil
	}
	bs, ok := s.client.(browser.Sender)
	if !ok {
		return nil, fmt.Errorf("CDP client does not satisfy browser.Sender")
	}
	p, err := browser.NewPage(ctx, bs, targetID)
	if err != nil {
		return nil, err
	}
	s.pages[targetID] = p
	return p, nil
}

// DropPage clears a cached page (so next access re-attaches) and tears down
// its collector goroutines + CDP subscriptions. Safe to call for an unknown
// targetID.
func (s *Session) DropPage(targetID string) {
	s.mu.Lock()
	p, ok := s.pages[targetID]
	if ok {
		delete(s.pages, targetID)
	}
	s.mu.Unlock()
	if ok {
		p.Close()
	}
}

func (s *Session) SetActiveTarget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeTarget = id
}

func (s *Session) ActiveTarget() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeTarget
}
