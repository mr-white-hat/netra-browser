package cdp

import (
	"context"
	"encoding/json"
	"fmt"
)

// AttachToTarget asks Chrome to give us a flattened session for targetID.
func (c *Client) AttachToTarget(ctx context.Context, targetID string) (string, error) {
	raw, err := c.Send(ctx, "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	})
	if err != nil {
		return "", fmt.Errorf("attachToTarget: %w", err)
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode attachToTarget: %w", err)
	}
	if resp.SessionID == "" {
		return "", fmt.Errorf("attachToTarget returned empty sessionId")
	}
	return resp.SessionID, nil
}

// SendOnTarget sends a CDP method scoped to a session.
func (c *Client) SendOnTarget(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan Response, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	c.writeMu.Lock()
	err := c.conn.WriteJSON(Method{ID: id, Name: method, Params: params, SessionID: sessionID})
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, fmt.Errorf("cdp client closed")
	case r := <-ch:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	}
}

// SubscribeOnTarget returns a channel that receives only events whose SessionID matches.
// TODO: this leaks the underlying source channel; cleanup is Plan D.
func (c *Client) SubscribeOnTarget(sessionID, method string) chan BufferedEvent {
	src := c.Subscribe(method)
	out := make(chan BufferedEvent, 16)
	go func() {
		defer close(out)
		for e := range src {
			if e.SessionID == sessionID {
				select {
				case out <- e:
				default:
				}
			}
		}
	}()
	return out
}
