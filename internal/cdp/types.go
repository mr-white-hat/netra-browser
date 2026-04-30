// Package cdp provides a thin Chrome DevTools Protocol websocket client.
package cdp

import "encoding/json"

// Method is an outbound CDP request.
type Method struct {
	ID        int64  `json:"id"`
	Name      string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// Response is a CDP method reply.
type Response struct {
	ID        int64           `json:"id"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Event is an unsolicited CDP message (no id, has method+params).
type Event struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Error is the CDP error body.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }
