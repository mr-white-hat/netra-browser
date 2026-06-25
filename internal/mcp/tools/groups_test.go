package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestCreateGroupOpensTabAndDoesNotTouchSessionActive(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDPSender{responses: map[string]json.RawMessage{
		"Target.createTarget": json.RawMessage(`{"targetId":"NEW"}`),
	}}
	sess.SetClient(cdp)
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_create_group", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	got := string(b)
	if !strings.Contains(got, `"group_id":"g1"`) || !strings.Contains(got, `"target_id":"NEW"`) {
		t.Fatalf("expected g1 + tab NEW: %s", got)
	}
	if sess.GroupActive("g1") != "NEW" {
		t.Fatalf("group g1 active should be NEW, got %q", sess.GroupActive("g1"))
	}
	if sess.GroupOf("NEW") != "g1" {
		t.Fatalf("NEW should be owned by g1")
	}
	// Crucially: a group's tab must NOT become the shared session active target,
	// or it would clobber other agents.
	if sess.ActiveTarget() != "" {
		t.Fatalf("session active target should be untouched, got %q", sess.ActiveTarget())
	}
}

func TestCreateGroupNoTabWhenOpenTabFalse(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDPSender{})
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)

	out, _ := reg.Invoke(context.Background(), "browser_create_group", json.RawMessage(`{"open_tab":false}`))
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), `"target_id"`) {
		t.Fatalf("open_tab=false should not open a tab: %s", b)
	}
	if !sess.GroupExists("g1") {
		t.Fatalf("group should still be created")
	}
}

func TestNewTabWithGroupAssignsOwnershipNotSessionActive(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDPSender{responses: map[string]json.RawMessage{
		"Target.createTarget": json.RawMessage(`{"targetId":"T-G2"}`),
	}}
	sess.SetClient(cdp)
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)
	RegisterBrowserTargets(reg, sess)

	g2 := sess.CreateGroup() // g1
	_ = g2
	g2 = sess.CreateGroup() // g2
	out, err := reg.Invoke(context.Background(), "browser_new_tab", json.RawMessage(`{"url":"about:blank","group_id":"`+g2+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"target_id":"T-G2"`) {
		t.Fatalf("expected target T-G2: %s", b)
	}
	if sess.GroupOf("T-G2") != g2 {
		t.Fatalf("T-G2 should be owned by %s, got %q", g2, sess.GroupOf("T-G2"))
	}
	if sess.ActiveTarget() != "" {
		t.Fatalf("grouped new_tab must not set session active target, got %q", sess.ActiveTarget())
	}
}

func TestNewTabUnknownGroupRejected(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDPSender{})
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, _ := reg.Invoke(context.Background(), "browser_new_tab", json.RawMessage(`{"group_id":"g404"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"unknown_group"`) {
		t.Fatalf("expected unknown_group: %s", b)
	}
}

func TestListGroupsShowsTabs(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDPSender{})
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)

	g1 := sess.CreateGroup()
	sess.SetGroupActive(g1, "T-A")

	out, _ := reg.Invoke(context.Background(), "browser_list_groups", nil)
	b, _ := json.Marshal(out)
	got := string(b)
	if !strings.Contains(got, `"group_id":"g1"`) || !strings.Contains(got, `"target_id":"T-A"`) {
		t.Fatalf("list_groups missing group/tab: %s", got)
	}
	if !strings.Contains(got, `"active":true`) {
		t.Fatalf("active tab flag missing: %s", got)
	}
}

func TestCloseGroupReleaseByDefault(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDPSender{}
	sess.SetClient(cdp)
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)

	g1 := sess.CreateGroup()
	sess.AdoptToGroup(g1, "T-A")

	out, _ := reg.Invoke(context.Background(), "browser_close_group", json.RawMessage(`{"group_id":"`+g1+`"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"released":["T-A"]`) {
		t.Fatalf("expected T-A released: %s", b)
	}
	for _, c := range cdp.calls {
		if c == "Target.closeTarget" {
			t.Fatalf("close_tabs defaulted false; should not close the tab")
		}
	}
	if sess.GroupExists(g1) {
		t.Fatalf("group should be gone")
	}
}

func TestCloseGroupClosesTabsWhenRequested(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDPSender{}
	sess.SetClient(cdp)
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)

	g1 := sess.CreateGroup()
	sess.AdoptToGroup(g1, "T-A")

	out, _ := reg.Invoke(context.Background(), "browser_close_group", json.RawMessage(`{"group_id":"`+g1+`","close_tabs":true}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"closed":["T-A"]`) {
		t.Fatalf("expected T-A closed: %s", b)
	}
	sawClose := false
	for _, c := range cdp.calls {
		if c == "Target.closeTarget" {
			sawClose = true
		}
	}
	if !sawClose {
		t.Fatalf("expected Target.closeTarget to be sent")
	}
}

func TestCloseGroupUnknownRejected(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterBrowserGroups(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_close_group", json.RawMessage(`{"group_id":"g999"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"unknown_group"`) {
		t.Fatalf("expected unknown_group: %s", b)
	}
}
