package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// Eval runs a JS expression in the page and returns the value (or error).
func (p *Page) Eval(ctx context.Context, expression string) (any, error) {
	raw, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.ExceptionDetails != nil {
		return nil, fmt.Errorf("eval threw: %s", resp.ExceptionDetails.Text)
	}
	if len(resp.Result.Value) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(resp.Result.Value, &v); err != nil {
		return nil, fmt.Errorf("eval result undecodable: %w", err)
	}
	return v, nil
}
