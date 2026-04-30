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

// Subscribe returns a channel that receives BufferedEvents whose Method matches.
// Channel is buffered (16); if full, events are dropped for that subscriber.
func (c *Client) Subscribe(method string) chan BufferedEvent {
	ch := make(chan BufferedEvent, 16)
	v, _ := c.subs.LoadOrStore(method, &eventSubList{})
	v.(*eventSubList).add(ch)
	return ch
}

// Unsubscribe removes a channel previously returned by Subscribe.
func (c *Client) Unsubscribe(method string, ch chan BufferedEvent) {
	if v, ok := c.subs.Load(method); ok {
		v.(*eventSubList).remove(ch)
	}
}

func (c *Client) dispatchEvent(e Event) {
	be := BufferedEvent{At: time.Now(), Method: e.Method, Params: e.Params, SessionID: e.SessionID}
	if v, ok := c.subs.Load(e.Method); ok {
		v.(*eventSubList).fanout(be)
	}
}

type eventSubList struct {
	mu    sync.Mutex
	chans []chan BufferedEvent
}

func (l *eventSubList) add(ch chan BufferedEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chans = append(l.chans, ch)
}
func (l *eventSubList) remove(ch chan BufferedEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, c := range l.chans {
		if c == ch {
			l.chans = append(l.chans[:i], l.chans[i+1:]...)
			return
		}
	}
}
func (l *eventSubList) fanout(e BufferedEvent) {
	l.mu.Lock()
	chans := append([]chan BufferedEvent(nil), l.chans...)
	l.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- e:
		default: // drop on full
		}
	}
}
