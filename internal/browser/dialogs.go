package browser

import (
	"context"
	"fmt"
)

// HandleDialog accepts or dismisses the currently-open JS dialog.
// action ∈ "accept" | "dismiss". text is optional prompt input.
func (p *Page) HandleDialog(ctx context.Context, action, text string) error {
	if action != "accept" && action != "dismiss" {
		return fmt.Errorf("invalid action %q", action)
	}
	params := map[string]any{"accept": action == "accept"}
	if text != "" {
		params["promptText"] = text
	}
	_, err := p.send(ctx, "Page.handleJavaScriptDialog", params)
	return err
}
