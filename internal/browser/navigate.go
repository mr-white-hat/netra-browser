package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
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

// Navigate issues Page.navigate and blocks until the requested load event fires.
func (p *Page) Navigate(ctx context.Context, opts NavigateOpts) (*NavigateResult, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL required")
	}
	if opts.WaitUntil == "" {
		opts.WaitUntil = WaitLoad
	}

	var waitMethod string
	switch opts.WaitUntil {
	case WaitLoad:
		waitMethod = "Page.loadEventFired"
	case WaitDOMContentLoaded:
		waitMethod = "Page.domContentEventFired"
	case WaitNetworkIdle:
		// Plan B Task 5 implements networkidle properly.
		waitMethod = "Page.loadEventFired"
	default:
		return nil, fmt.Errorf("unknown wait_until: %s", opts.WaitUntil)
	}

	events := p.cdp.SubscribeOnTarget(p.sessionID, waitMethod)

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

	select {
	case <-events:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if opts.WaitUntil == WaitNetworkIdle {
		if err := p.WaitNetworkIdle(ctx, 500*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return &NavigateResult{URL: opts.URL, FrameID: resp.FrameID}, nil
}
