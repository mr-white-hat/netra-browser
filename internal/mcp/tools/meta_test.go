package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestMetaHealthDetached(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterMeta(reg, sess, MetaDeps{})

	out, err := reg.Invoke(context.Background(), "meta_health", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"chrome_alive":false`) || !strings.Contains(string(b), `"ws_alive":false`) {
		t.Fatalf("unexpected: %s", b)
	}
}

// fakeProbeSender lets us simulate a live or broken CDP connection.
type fakeProbeSender struct {
	send func(ctx context.Context, method string, params any) (json.RawMessage, error)
}

func (f *fakeProbeSender) Send(ctx context.Context, m string, p any) (json.RawMessage, error) {
	return f.send(ctx, m, p)
}
func (f *fakeProbeSender) Close() error { return nil }

// Live: Browser.getVersion returns an empty object — both flags should be true.
func TestMetaHealthLive(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeProbeSender{send: func(ctx context.Context, m string, _ any) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}})
	reg := mcp.NewRegistry()
	RegisterMeta(reg, sess, MetaDeps{})
	out, _ := reg.Invoke(context.Background(), "meta_health", nil)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"chrome_alive":true`) || !strings.Contains(string(b), `"ws_alive":true`) {
		t.Fatalf("expected live: %s", b)
	}
}

// Broken: client present but Send errors — both flags must report false.
func TestMetaHealthBroken(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeProbeSender{send: func(ctx context.Context, m string, _ any) (json.RawMessage, error) {
		return nil, errProbe
	}})
	reg := mcp.NewRegistry()
	RegisterMeta(reg, sess, MetaDeps{})
	out, _ := reg.Invoke(context.Background(), "meta_health", nil)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"chrome_alive":false`) || !strings.Contains(string(b), `"ws_alive":false`) {
		t.Fatalf("expected dead: %s", b)
	}
}

var errProbe = jsonError("probe failed")

type jsonError string

func (e jsonError) Error() string { return string(e) }
