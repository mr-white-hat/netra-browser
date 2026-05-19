package tools

import (
	"sync"
	"time"
)

// Defaults holds server-side default behavior knobs configurable via main.go flags.
// Per-call args still win — these only fill in unspecified fields.
type Defaults struct {
	WaitUntil       string // empty | load | domcontentloaded | networkidle
	CallTimeoutMs   int    // <=0 means "library default"
	AggressivePrune bool   // strip empty container nodes from snapshots
}

var (
	defaultsMu sync.RWMutex
	defaults   = Defaults{
		WaitUntil:     "domcontentloaded",
		CallTimeoutMs: 5000,
	}
)

// SetDefaults installs server-side defaults. Pass {} to keep current.
func SetDefaults(d Defaults) {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	if d.WaitUntil != "" {
		defaults.WaitUntil = d.WaitUntil
	}
	if d.CallTimeoutMs > 0 {
		defaults.CallTimeoutMs = d.CallTimeoutMs
	}
	defaults.AggressivePrune = d.AggressivePrune
}

// GetDefaults returns a snapshot of current defaults.
func GetDefaults() Defaults {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	return defaults
}

// DefaultCallTimeout is the duration form of GetDefaults().CallTimeoutMs.
func DefaultCallTimeout() time.Duration {
	d := GetDefaults().CallTimeoutMs
	if d <= 0 {
		return 0
	}
	return time.Duration(d) * time.Millisecond
}
