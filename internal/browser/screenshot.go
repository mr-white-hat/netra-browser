package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

type ScreenshotOpts struct {
	Locator  *Locator
	FullPage bool
	Format   string // "png" (default) or "jpeg"
}

// Screenshot returns a base64-encoded PNG (or JPEG if Format set).
func (p *Page) Screenshot(ctx context.Context, opts ScreenshotOpts) (string, error) {
	params := map[string]any{}
	if opts.Format == "" {
		opts.Format = "png"
	}
	params["format"] = opts.Format

	if opts.Locator != nil {
		bid, err := p.Resolve(ctx, *opts.Locator)
		if err != nil {
			return "", err
		}
		nodeID, err := p.backendToNodeID(ctx, bid)
		if err != nil {
			return "", err
		}
		raw, err := p.send(ctx, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})
		if err != nil {
			return "", err
		}
		var box struct {
			Model struct {
				Content []float64 `json:"content"`
			} `json:"model"`
		}
		if err := json.Unmarshal(raw, &box); err != nil {
			return "", err
		}
		if len(box.Model.Content) < 8 {
			return "", fmt.Errorf("box model too small")
		}
		params["clip"] = map[string]any{
			"x":      box.Model.Content[0],
			"y":      box.Model.Content[1],
			"width":  box.Model.Content[2] - box.Model.Content[0],
			"height": box.Model.Content[5] - box.Model.Content[1],
			"scale":  1,
		}
	} else if opts.FullPage {
		raw, err := p.send(ctx, "Page.getLayoutMetrics", nil)
		if err != nil {
			return "", err
		}
		var lm struct {
			ContentSize struct {
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
			} `json:"contentSize"`
		}
		if err := json.Unmarshal(raw, &lm); err != nil {
			return "", err
		}
		params["captureBeyondViewport"] = true
		params["clip"] = map[string]any{
			"x": 0, "y": 0,
			"width": lm.ContentSize.Width, "height": lm.ContentSize.Height, "scale": 1,
		}
	}

	raw, err := p.send(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}
