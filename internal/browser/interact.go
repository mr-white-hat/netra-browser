package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// Click resolves the locator and synthesizes a mouse click at the element's box center.
func (p *Page) Click(ctx context.Context, l Locator) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := p.send(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"nodeId": nodeID}); err != nil {
		return err
	}
	raw, err := p.send(ctx, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})
	if err != nil {
		return err
	}
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &box); err != nil {
		return err
	}
	if len(box.Model.Content) < 8 {
		return fmt.Errorf("box model too small")
	}
	x := (box.Model.Content[0] + box.Model.Content[2] + box.Model.Content[4] + box.Model.Content[6]) / 4
	y := (box.Model.Content[1] + box.Model.Content[3] + box.Model.Content[5] + box.Model.Content[7]) / 4
	for _, t := range []string{"mousePressed", "mouseReleased"} {
		if _, err := p.send(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": t, "x": x, "y": y, "button": "left", "clickCount": 1,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Fill focuses the located element, clears its value, and types the given text.
func (p *Page) Fill(ctx context.Context, l Locator, value string) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := p.send(ctx, "DOM.focus", map[string]any{"nodeId": nodeID}); err != nil {
		return err
	}
	if _, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression": "document.activeElement && (document.activeElement.value = '')",
	}); err != nil {
		return err
	}
	if _, err := p.send(ctx, "Input.insertText", map[string]any{"text": value}); err != nil {
		return err
	}
	return nil
}

func (p *Page) backendToNodeID(ctx context.Context, backendID int64) (int64, error) {
	raw, err := p.send(ctx, "DOM.pushNodesByBackendIdsToFrontend", map[string]any{
		"backendNodeIds": []int64{backendID},
	})
	if err != nil {
		return 0, err
	}
	var resp struct {
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if len(resp.NodeIDs) == 0 || resp.NodeIDs[0] == 0 {
		return 0, fmt.Errorf("not_found: backend node %d not in document", backendID)
	}
	return resp.NodeIDs[0], nil
}
