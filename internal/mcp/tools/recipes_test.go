package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestRecipeRecordReplayList(t *testing.T) {
	dir := t.TempDir()
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	// Stand-in tools the recipe will invoke.
	called := []string{}
	calledArgs := []map[string]any{}
	reg.Register("test_step", func(ctx context.Context, params json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(params, &m)
		called = append(called, "test_step")
		calledArgs = append(calledArgs, m)
		return map[string]any{"ok": true}, nil
	})
	RegisterRecipesAt(reg, sess, dir)

	// Record.
	out, err := reg.Invoke(context.Background(), "task_record_recipe",
		json.RawMessage(`{"name":"r1","actions":[{"tool":"test_step","args":{"x":"$EMAIL","y":"static"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"recipe_path"`) {
		t.Fatalf("missing recipe_path: %s", b)
	}

	// List.
	out, _ = reg.Invoke(context.Background(), "task_list_recipes", nil)
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"r1"`) {
		t.Fatalf("recipe missing from list: %s", b)
	}

	// Replay with var substitution.
	out, err = reg.Invoke(context.Background(), "task_replay_recipe",
		json.RawMessage(`{"name":"r1","env":{"EMAIL":"a@b"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"steps_executed":1`) {
		t.Fatalf("steps not executed: %s", b)
	}
	if len(calledArgs) != 1 || calledArgs[0]["x"] != "a@b" {
		t.Fatalf("var substitution failed: %v", calledArgs)
	}
	if calledArgs[0]["y"] != "static" {
		t.Fatalf("static arg mangled: %v", calledArgs[0])
	}
}

func TestRecipeMissingVarFails(t *testing.T) {
	dir := t.TempDir()
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	reg.Register("noop", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	RegisterRecipesAt(reg, sess, dir)
	_, _ = reg.Invoke(context.Background(), "task_record_recipe",
		json.RawMessage(`{"name":"r","actions":[{"tool":"noop","args":{"v":"$MISSING"}}]}`))
	out, _ := reg.Invoke(context.Background(), "task_replay_recipe", json.RawMessage(`{"name":"r"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "$MISSING") {
		t.Fatalf("expected error mentioning missing var: %s", b)
	}
}

func TestRecipeReplayMissing(t *testing.T) {
	dir := t.TempDir()
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterRecipesAt(reg, sess, dir)
	out, _ := reg.Invoke(context.Background(), "task_replay_recipe", json.RawMessage(`{"name":"ghost"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "not_found") {
		t.Fatalf("expected not_found: %s", b)
	}
}
