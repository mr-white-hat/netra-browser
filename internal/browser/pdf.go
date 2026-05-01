package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
)

type PDFOpts struct {
	Format string // "Letter" | "A4" | "" (default Letter)
}

func (p *Page) RenderPDF(ctx context.Context, opts PDFOpts) (string, error) {
	params := map[string]any{}
	switch opts.Format {
	case "A4":
		params["paperWidth"] = 8.27
		params["paperHeight"] = 11.69
	case "Letter", "":
		params["paperWidth"] = 8.5
		params["paperHeight"] = 11.0
	}

	raw, err := p.send(ctx, "Page.printToPDF", params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	bytes, err := base64.StdEncoding.DecodeString(resp.Data)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "netra-pdf-*.pdf")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(bytes); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}
