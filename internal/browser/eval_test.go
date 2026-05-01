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
