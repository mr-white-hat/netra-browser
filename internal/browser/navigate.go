package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// WaitUntil controls when Navigate returns.
type WaitUntil string

const (
	WaitLoad             WaitUntil = "load"
	WaitDOMContentLoaded WaitUntil = "domcontentloaded"
	WaitNetworkIdle      WaitUntil = "networkidle"
)

// NavigateOpts is the input shape for Page.Navigate.
type NavigateOpts struct {
	URL       string
	WaitUntil WaitUntil
}

// NavigateResult is what callers get back.
type NavigateResult struct {
	URL      string
	Title    string
	FrameID  string
	Snapshot *Snapshot // populated by tool layer if return_snapshot=true
}

// Navigate issues Page.navigate. The wait_until logic is best-effort in unit tests
// (event source unwired); Task 4 wires up real event-blocking, Task 5 adds networkidle.
func (p *Page) Navigate(ctx context.Context, opts NavigateOpts) (*NavigateResult, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL required")
	}
	if opts.WaitUntil == "" {
		opts.WaitUntil = WaitLoad
	}

	raw, err := p.send(ctx, "Page.navigate", map[string]any{"url": opts.URL})
	if err != nil {
		return nil, err
	}
	var resp struct {
		FrameID   string `json:"frameId"`
		ErrorText string `json:"errorText,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode Page.navigate: %w", err)
	}
	if resp.ErrorText != "" {
		return nil, fmt.Errorf("navigate: %s", resp.ErrorText)
	}

	return &NavigateResult{URL: opts.URL, FrameID: resp.FrameID}, nil
}
