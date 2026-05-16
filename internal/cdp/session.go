package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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

// WatchTargets enables target discovery on the root session and invokes
// onDestroyed for every Target.targetDestroyed event. Returns a stop func that
// drains the subscription. Use this so the bridge can drop server-side state
// for tabs the user closes manually (without going through browser_close_tab).
func (c *Client) WatchTargets(ctx context.Context, onDestroyed func(targetID string)) (func(), error) {
	if _, err := c.Send(ctx, "Target.setDiscoverTargets", map[string]any{"discover": true}); err != nil {
		return nil, fmt.Errorf("setDiscoverTargets: %w", err)
	}
	ch, cleanup := c.Subscribe("Target.targetDestroyed")
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				var v struct {
					TargetID string `json:"targetId"`
				}
				if err := json.Unmarshal(e.Params, &v); err == nil && v.TargetID != "" {
					onDestroyed(v.TargetID)
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			cleanup()
		})
	}, nil
}

// SubscribeOnTarget returns a channel that receives only events whose SessionID
// matches, plus a cleanup func. Callers MUST invoke cleanup when done — the
// internal filter goroutine and the source subscription both leak otherwise.
// cleanup is idempotent.
func (c *Client) SubscribeOnTarget(sessionID, method string) (<-chan BufferedEvent, func()) {
	src, stopSrc := c.Subscribe(method)
	out := make(chan BufferedEvent, 16)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case e, ok := <-src:
				if !ok {
					return
				}
				if e.SessionID == sessionID {
					select {
					case out <- e:
					case <-done:
						return
					default:
						// drop on full
					}
				}
			}
		}
	}()
	cleanup := func() {
		once.Do(func() {
			close(done)
			stopSrc()
		})
	}
	return out, cleanup
}
