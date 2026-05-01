package browser

import (
	"context"
	"encoding/json"
)

func (p *Page) GetCookies(ctx context.Context, urls []string) ([]map[string]any, error) {
	params := map[string]any{}
	if len(urls) > 0 {
		params["urls"] = urls
	}
	raw, err := p.send(ctx, "Network.getCookies", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Cookies []map[string]any `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Cookies, nil
}

func (p *Page) SetCookies(ctx context.Context, cookies []map[string]any) error {
	_, err := p.send(ctx, "Network.setCookies", map[string]any{"cookies": cookies})
	return err
}
