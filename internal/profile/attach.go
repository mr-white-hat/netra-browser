// Package profile manages Chrome attach/launch and session export.
package profile

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DebugInfo is the parsed /json/version response.
type DebugInfo struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	UserAgent            string `json:"User-Agent"`
}

// Discover queries http://host:port/json/version on a running Chrome.
func Discover(host string, port int) (*DebugInfo, error) {
	url := fmt.Sprintf("http://%s:%d/json/version", host, port)
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	var info DebugInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	if info.WebSocketDebuggerURL == "" {
		return nil, fmt.Errorf("empty webSocketDebuggerUrl")
	}
	return &info, nil
}
