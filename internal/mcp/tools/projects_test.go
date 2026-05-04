package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
	"github.com/mr-white-hat/netra-browser/internal/profile"
)

func TestListProjectsTool(t *testing.T) {
	dir := t.TempDir()
	if _, err := profile.OpenProject(dir, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.OpenProject(dir, "beta"); err != nil {
		t.Fatal(err)
	}
	reg := mcp.NewRegistry()
	RegisterProjects(reg, dir, "alpha")
	out, err := reg.Invoke(context.Background(), "browser_list_projects", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"alpha"`) || !strings.Contains(string(b), `"beta"`) {
		t.Fatalf("missing projects: %s", b)
	}
	if !strings.Contains(string(b), `"is_self":true`) {
		t.Fatalf("expected is_self flag: %s", b)
	}
}

func TestAdoptReleaseTabTools(t *testing.T) {
	dir := t.TempDir()
	proj, err := profile.OpenProject(dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.getTargets": json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page"}]}`),
		},
	})
	sess.SetProject(proj)
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	// Adopt nonexistent should reject.
	out, _ := reg.Invoke(context.Background(), "browser_adopt_tab", json.RawMessage(`{"target_id":"T-bogus"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected reject for nonexistent target: %s", b)
	}

	// Adopt T1.
	out, _ = reg.Invoke(context.Background(), "browser_adopt_tab", json.RawMessage(`{"target_id":"T1"}`))
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("adopt: %s", b)
	}
	if !proj.Owns("T1") {
		t.Fatal("not owned after adopt")
	}

	// list_tabs filtered: T1 visible (owned).
	out, _ = reg.Invoke(context.Background(), "browser_list_tabs", nil)
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"T1"`) {
		t.Fatalf("T1 missing from list: %s", b)
	}

	// Release.
	out, _ = reg.Invoke(context.Background(), "browser_release_tab", json.RawMessage(`{"target_id":"T1"}`))
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("release: %s", b)
	}
	if proj.Owns("T1") {
		t.Fatal("still owned after release")
	}

	// list_tabs filtered: T1 hidden now (not owned, no include_all).
	out, _ = reg.Invoke(context.Background(), "browser_list_tabs", nil)
	b, _ = json.Marshal(out)
	if strings.Contains(string(b), `"T1"`) {
		t.Fatalf("T1 should be hidden: %s", b)
	}
	// include_all: visible.
	out, _ = reg.Invoke(context.Background(), "browser_list_tabs", json.RawMessage(`{"include_all":true}`))
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"T1"`) {
		t.Fatalf("T1 should be visible with include_all: %s", b)
	}
}

func TestNewTabAutoTagsProject(t *testing.T) {
	dir := t.TempDir()
	proj, err := profile.OpenProject(dir, "p1")
	if err != nil {
		t.Fatal(err)
	}
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.createTarget": json.RawMessage(`{"targetId":"T-new"}`),
		},
	})
	sess.SetProject(proj)
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_new_tab", json.RawMessage(`{"url":"https://x"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"target_id":"T-new"`) {
		t.Fatalf("missing target: %s", b)
	}
	if !proj.Owns("T-new") {
		t.Fatal("new tab not auto-tagged into project")
	}
}

// Sanity: list_tabs without project shows everything (no filter), no 'owned' field.
func TestListTabsWithoutProject(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.getTargets": json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page"}]}`),
		},
	})
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_list_tabs", nil)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"T1"`) {
		t.Fatalf("T1 missing: %s", b)
	}
	if strings.Contains(string(b), `"owned"`) {
		t.Fatalf("owned key shouldn't appear without project: %s", b)
	}
}

// Defense in depth: ensure profile dir gets created on first OpenProject usage so
// the projects tool doesn't choke.
func TestListProjectsToolEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	reg := mcp.NewRegistry()
	RegisterProjects(reg, dir, "")
	out, err := reg.Invoke(context.Background(), "browser_list_projects", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"projects":[]`) && !strings.Contains(string(b), `"projects":null`) {
		t.Fatalf("unexpected: %s", b)
	}
	_ = os.Remove(dir) // best-effort
}
