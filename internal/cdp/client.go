package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// errValue wraps an error so atomic.Value always stores the same concrete type.
type errValue struct{ err error }

// Client is a long-lived CDP websocket connection.
type Client struct {
	conn       *websocket.Conn
	writeMu    sync.Mutex
	nextID     atomic.Int64
	pending    sync.Map // int64 -> chan Response
	subs       sync.Map // string method -> *eventSubList (filled in Task 5)
	closeOnce  sync.Once
	closed     chan struct{}
	closeError atomic.Value // errValue
}

// Dial opens a websocket and starts the read pump.
func Dial(ctx context.Context, wsURL string) (*Client, error) {
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := d.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial cdp: %w", err)
	}
	c := &Client{conn: conn, closed: make(chan struct{})}
	go c.readPump()
	return c, nil
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeError.Store(errValue{errors.New("cdp client closed")})
		_ = c.conn.Close()
		close(c.closed)
	})
	return nil
}

// Send issues a CDP method and waits for the matching response.
func (c *Client) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan Response, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	c.writeMu.Lock()
	err := c.conn.WriteJSON(Method{ID: id, Name: method, Params: params})
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		if v := c.closeError.Load(); v != nil {
			return nil, v.(errValue).err
		}
		return nil, errors.New("cdp client closed")
	case r := <-ch:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	}
}

// readPump dispatches inbound frames to pending responses or to event subscribers.
func (c *Client) readPump() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.closeError.Store(errValue{err})
			c.closeOnce.Do(func() { _ = c.conn.Close(); close(c.closed) })
			return
		}
		// Distinguish Response (has id) from Event (has method, no id).
		var probe struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.ID != nil {
			var r Response
			if err := json.Unmarshal(raw, &r); err != nil {
				continue
			}
			if v, ok := c.pending.Load(r.ID); ok {
				ch := v.(chan Response)
				select {
				case ch <- r:
				default:
				}
			}
			continue
		}
		if probe.Method != "" {
			var e Event
			if err := json.Unmarshal(raw, &e); err != nil {
				continue
			}
			c.dispatchEvent(e)
		}
	}
}

// dispatchEvent is filled in Task 5; for now, no-op.
func (c *Client) dispatchEvent(e Event) {}
