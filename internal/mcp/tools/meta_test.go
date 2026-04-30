package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
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
