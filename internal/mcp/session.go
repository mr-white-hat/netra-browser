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
}

func NewSession() *Session { return &Session{pages: map[string]*browser.Page{}} }

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
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	s.activeTarget = ""
	s.pages = map[string]*browser.Page{}
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

// DropPage clears a cached page (so next access re-attaches).
func (s *Session) DropPage(targetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pages, targetID)
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
