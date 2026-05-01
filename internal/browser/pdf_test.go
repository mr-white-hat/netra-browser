package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

func TestRenderPDFWritesFile(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4 fake")
	pdfB64 := base64.StdEncoding.EncodeToString(pdfBytes)
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.printToPDF": json.RawMessage(`{"data":"` + pdfB64 + `"}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	path, err := p.RenderPDF(context.Background(), PDFOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(pdfBytes) {
		t.Fatalf("file contents wrong: %q", b)
	}
}

func TestRenderPDFA4Format(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.printToPDF": json.RawMessage(`{"data":"JVBERi0xLjQK"}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	path, err := p.RenderPDF(context.Background(), PDFOpts{Format: "A4"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
