package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mr-white-hat/netra-browser/internal/profile"
)

// ParseDebugURL extracts host+port from inputs like "http://127.0.0.1:9222" or "127.0.0.1:9222".
func ParseDebugURL(s string) (string, int, error) {
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", 0, fmt.Errorf("parse %q: %w", s, err)
	}
	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return "", 0, fmt.Errorf("debug url missing host or port: %s", s)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("port %q: %w", portStr, err)
	}
	return host, p, nil
}

// Attach discovers the Chrome at debugURL, dials its browser-level CDP socket,
// and returns the client plus version + target count.
func Attach(ctx context.Context, debugURL string) (*Client, string, int, error) {
	host, port, err := ParseDebugURL(debugURL)
	if err != nil {
		return nil, "", 0, err
	}
	info, err := profile.Discover(host, port)
	if err != nil {
		return nil, "", 0, fmt.Errorf("discover: %w", err)
	}
	client, err := Dial(ctx, info.WebSocketDebuggerURL)
	if err != nil {
		return nil, "", 0, fmt.Errorf("dial cdp: %w", err)
	}
	raw, err := client.Send(ctx, "Target.getTargets", nil)
	if err != nil {
		_ = client.Close()
		return nil, "", 0, fmt.Errorf("Target.getTargets: %w", err)
	}
	var got struct {
		TargetInfos []any `json:"targetInfos"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &got)
	}
	return client, info.Browser, len(got.TargetInfos), nil
}
