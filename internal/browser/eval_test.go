package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEvalReturnsValue(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"result":{"type":"number","value":42}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	v, err := p.Eval(context.Background(), "1+1")
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("nil value")
	}
}

func TestEvalSurfacesException(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"exceptionDetails":{"text":"ReferenceError"},"result":{"type":"undefined"}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	if _, err := p.Eval(context.Background(), "missing"); err == nil {
		t.Fatal("expected exception error")
	}
}

// Object/array values must come back decoded, never as JSON-encoded strings
// (regression guard for the old "fallback to string(value)" path).
func TestEvalDecodesObjectsAndArrays(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"result":{"type":"object","value":{"a":1,"b":[2,3]}}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	v, err := p.Eval(context.Background(), "({a:1,b:[2,3]})")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T (%v)", v, v)
	}
	if m["a"].(float64) != 1 {
		t.Fatalf("a: %v", m["a"])
	}
	arr, ok := m["b"].([]any)
	if !ok {
		t.Fatalf("expected array, got %T", m["b"])
	}
	if len(arr) != 2 {
		t.Fatalf("array len: %d", len(arr))
	}
}

// String values are returned as Go string, not as the JSON-quoted form.
func TestEvalDecodesStringNotQuoted(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"result":{"type":"string","value":"hello"}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	v, err := p.Eval(context.Background(), `"hello"`)
	if err != nil {
		t.Fatal(err)
	}
	if v != "hello" {
		t.Fatalf("expected 'hello', got %T (%v)", v, v)
	}
}
