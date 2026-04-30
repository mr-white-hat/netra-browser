package mcp

import (
	"context"
	"encoding/json"
	"sync"
)

// CDPSender is the interface the session holds onto.
// Concrete impl is *cdp.Client; tests pass fakes.
type CDPSender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	Close() error
}

// CDPCloser is kept as an alias for older code that imported the name.
type CDPCloser = CDPSender

// Session is per-MCP-process state: one active CDP connection, one active target.
type Session struct {
	mu           sync.RWMutex
	client       CDPSender
	activeTarget string
}

func NewSession() *Session { return &Session{} }

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
