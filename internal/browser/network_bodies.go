package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
)

// GetResponseBody fetches the response body for requestID via Network.getResponseBody.
// Returns the decoded bytes (auto-base64-decoded if Chrome flagged it) and the MIME type
// from the call. Errors when called before loadingFinished or for unsupported targets.
func (p *Page) GetResponseBody(ctx context.Context, requestID string) ([]byte, error) {
	raw, err := p.send(ctx, "Network.getResponseBody", map[string]any{"requestId": requestID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Body          string `json:"body"`
		Base64Encoded bool   `json:"base64Encoded"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.Base64Encoded {
		return base64.StdEncoding.DecodeString(resp.Body)
	}
	return []byte(resp.Body), nil
}

// GetRequestPostData fetches the original request body via Network.getRequestPostData.
// Used for requests where Chrome did not include postData inline in requestWillBeSent.
func (p *Page) GetRequestPostData(ctx context.Context, requestID string) ([]byte, error) {
	raw, err := p.send(ctx, "Network.getRequestPostData", map[string]any{"requestId": requestID})
	if err != nil {
		return nil, err
	}
	var resp struct {
		PostData string `json:"postData"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return []byte(resp.PostData), nil
}
