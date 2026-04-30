package mcp

// Standard tool error codes.
const (
	ErrChromeDisconnected = "chrome_disconnected"
	ErrChromeDead         = "chrome_dead"
	ErrTargetDestroyed    = "target_destroyed"
	ErrTimeout            = "timeout"
	ErrAmbiguousLocator   = "ambiguous_locator"
	ErrNotFound           = "not_found"
	ErrNotAttached        = "not_attached"
	ErrInvalidArgs        = "invalid_args"
)

// ToolError is the application-level error returned inside a tool result.
// Tools never return JSON-RPC errors for these — they return a result with ok:false.
type ToolError struct {
	Code     string `json:"error_code"`
	Message  string `json:"message"`
	TargetID string `json:"target_id,omitempty"`
}

// AsResult turns a ToolError into the standard {ok:false, ...} map.
func (e ToolError) AsResult() map[string]any {
	m := map[string]any{
		"ok":         false,
		"error_code": e.Code,
		"message":    e.Message,
	}
	if e.TargetID != "" {
		m["target_id"] = e.TargetID
	}
	return m
}
