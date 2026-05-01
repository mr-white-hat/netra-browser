package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestScreenshotReturnsBase64(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.captureScreenshot": json.RawMessage(`{"data":"iVBORw0KGgoAAAANSUhEUgAA"}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	data, err := p.Screenshot(context.Background(), ScreenshotOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if data == "" {
		t.Fatal("empty screenshot data")
	}
}
