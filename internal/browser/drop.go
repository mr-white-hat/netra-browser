package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DropFilesMode reports which underlying mechanism DropFiles used.
type DropFilesMode string

const (
	// DropModeHiddenInput: a hidden <input type="file"> was found near the
	// locator and DOM.setFileInputFiles handled the upload (cleanest path,
	// works for ~80% of modern drop zones — react-dropzone, Uppy, Filepond,
	// most a11y-conscious editors).
	DropModeHiddenInput DropFilesMode = "hidden_input"
	// DropModeSyntheticDrag: pure drop zone with no file input fallback. Used
	// CDP Input.dispatchDragEvent to fire native drag events at the located
	// element's box center.
	DropModeSyntheticDrag DropFilesMode = "synthetic_drag"
)

// DropFiles uploads files into a drop zone resolved by `l`. It auto-detects
// the right mechanism: a hidden file input inside the located subtree wins
// (no synthetic drag needed); otherwise CDP's native Input.dispatchDragEvent
// fires the dragEnter/dragOver/drop sequence carrying server-side file paths.
//
// Files are read by Chrome from its own filesystem — the paths must exist on
// the host running the bridge. For remote-Chrome setups where paths aren't
// shared, use the items-with-data form (not yet exposed; future work).
func (p *Page) DropFiles(ctx context.Context, l Locator, paths []string) (DropFilesMode, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no file paths")
	}
	abs := make([]string, len(paths))
	for i, path := range paths {
		ap, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("abs %q: %w", path, err)
		}
		if _, err := os.Stat(ap); err != nil {
			return "", fmt.Errorf("file %q: %w", ap, err)
		}
		abs[i] = ap
	}

	// Resolve the drop target.
	backendID, err := p.Resolve(ctx, l)
	if err != nil {
		return "", err
	}
	dropNodeID, err := p.backendToNodeID(ctx, backendID)
	if err != nil {
		return "", err
	}

	// Fast path: located element IS itself a file input, OR contains one.
	fiNodeID, ok, err := p.findFileInputIn(ctx, dropNodeID)
	if err != nil {
		return "", err
	}
	if ok {
		if _, err := p.send(ctx, "DOM.setFileInputFiles", map[string]any{
			"nodeId": fiNodeID,
			"files":  abs,
		}); err != nil {
			return "", fmt.Errorf("setFileInputFiles: %w", err)
		}
		return DropModeHiddenInput, nil
	}

	// Slow path: synthesize the full drag sequence on the element's center.
	x, y, err := p.boxCenter(ctx, dropNodeID)
	if err != nil {
		return "", err
	}
	if err := p.dispatchDrag(ctx, x, y, abs); err != nil {
		return "", err
	}
	return DropModeSyntheticDrag, nil
}

// findFileInputIn checks whether nodeID itself is an <input type="file"> or
// has one in its subtree. Returns the file input's nodeId if found.
func (p *Page) findFileInputIn(ctx context.Context, nodeID int64) (int64, bool, error) {
	// First check the node itself.
	rawDesc, err := p.send(ctx, "DOM.describeNode", map[string]any{"nodeId": nodeID})
	if err != nil {
		return 0, false, nil // not fatal — fall through to descendant search
	}
	var desc struct {
		Node struct {
			NodeName   string   `json:"nodeName"`
			Attributes []string `json:"attributes"`
		} `json:"node"`
	}
	_ = json.Unmarshal(rawDesc, &desc)
	if desc.Node.NodeName == "INPUT" && hasFileType(desc.Node.Attributes) {
		return nodeID, true, nil
	}

	// Descendant search via DOM.querySelector.
	raw, err := p.send(ctx, "DOM.querySelector", map[string]any{
		"nodeId":   nodeID,
		"selector": "input[type='file']",
	})
	if err != nil {
		return 0, false, nil
	}
	var resp struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, false, nil
	}
	if resp.NodeID == 0 {
		return 0, false, nil
	}
	return resp.NodeID, true, nil
}

func hasFileType(attrs []string) bool {
	// attrs is alternating [name, value, name, value, ...]
	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i] == "type" && attrs[i+1] == "file" {
			return true
		}
	}
	return false
}

// boxCenter returns the viewport center coords of the element's content box.
func (p *Page) boxCenter(ctx context.Context, nodeID int64) (float64, float64, error) {
	if _, err := p.send(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"nodeId": nodeID}); err != nil {
		return 0, 0, err
	}
	raw, err := p.send(ctx, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})
	if err != nil {
		return 0, 0, err
	}
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &box); err != nil {
		return 0, 0, err
	}
	if len(box.Model.Content) < 8 {
		return 0, 0, fmt.Errorf("box model too small")
	}
	x := (box.Model.Content[0] + box.Model.Content[2] + box.Model.Content[4] + box.Model.Content[6]) / 4
	y := (box.Model.Content[1] + box.Model.Content[3] + box.Model.Content[5] + box.Model.Content[7]) / 4
	return x, y, nil
}

// dispatchDrag fires the native dragEnter → dragOver → drop sequence at (x,y)
// carrying the given server-side file paths. Chrome reads the files from disk.
func (p *Page) dispatchDrag(ctx context.Context, x, y float64, paths []string) error {
	dragData := map[string]any{
		// `items` carries inline mime+base64 payloads. Empty here — Chrome
		// reads the actual file bytes from `files` on disk.
		"items":              []any{},
		"files":              paths,
		"dragOperationsMask": 1, // copy
	}
	for _, kind := range []string{"dragEnter", "dragOver", "drop"} {
		if _, err := p.send(ctx, "Input.dispatchDragEvent", map[string]any{
			"type":      kind,
			"x":         x,
			"y":         y,
			"data":      dragData,
			"modifiers": 0,
		}); err != nil {
			return fmt.Errorf("dispatchDragEvent %s: %w", kind, err)
		}
	}
	return nil
}
