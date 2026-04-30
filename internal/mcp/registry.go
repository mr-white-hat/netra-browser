package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Handler is invoked for a single tool call.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Registry maps tool names to handlers.
type Registry struct {
	mu sync.RWMutex
	h  map[string]Handler
}

func NewRegistry() *Registry { return &Registry{h: map[string]Handler{}} }

func (r *Registry) Register(name string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.h[name] = h
}

func (r *Registry) Invoke(ctx context.Context, name string, params json.RawMessage) (any, error) {
	r.mu.RLock()
	h, ok := r.h[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return h(ctx, params)
}

// Names returns the list of registered tools.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.h))
	for n := range r.h {
		out = append(out, n)
	}
	return out
}
