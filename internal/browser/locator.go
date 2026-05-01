package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Locator is a tagged union — set exactly one of {SnapshotID, Role/Name, Text, CSS, XPath}.
type Locator struct {
	Role       string `json:"role,omitempty"`
	Name       string `json:"name,omitempty"`
	Exact      bool   `json:"exact,omitempty"`
	Text       string `json:"text,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	CSS        string `json:"css,omitempty"`
	XPath      string `json:"xpath,omitempty"`
}

// Resolve returns a backendNodeId for the locator, or an error.
func (p *Page) Resolve(ctx context.Context, l Locator) (int64, error) {
	switch {
	case l.SnapshotID != "":
		return p.resolveSnapshotID(l.SnapshotID)
	case l.Role != "" || l.Name != "":
		return p.resolveRoleName(ctx, l)
	case l.Text != "":
		return p.resolveText(ctx, l.Text, l.Exact)
	case l.CSS != "":
		return p.resolveCSS(ctx, l.CSS)
	case l.XPath != "":
		return p.resolveXPath(ctx, l.XPath)
	}
	return 0, fmt.Errorf("locator empty")
}

func (p *Page) resolveSnapshotID(id string) (int64, error) {
	p.mu.Lock()
	snap := p.snapshot
	p.mu.Unlock()
	if snap == nil {
		return 0, fmt.Errorf("not_found: no snapshot taken yet")
	}
	n, ok := snap.byID[id]
	if !ok {
		return 0, fmt.Errorf("not_found: snapshot id %s", id)
	}
	return n.BackendNodeID, nil
}

func (p *Page) resolveRoleName(ctx context.Context, l Locator) (int64, error) {
	p.mu.Lock()
	snap := p.snapshot
	p.mu.Unlock()
	if snap == nil {
		s, err := p.Snapshot(ctx, SnapshotAccessibility)
		if err != nil {
			return 0, err
		}
		snap = s
	}
	var matches []*SnapshotNode
	var walk func([]SnapshotNode)
	walk = func(ns []SnapshotNode) {
		for i := range ns {
			n := &ns[i]
			if (l.Role == "" || n.Role == l.Role) && nameMatch(n.Name, l.Name, l.Exact) {
				matches = append(matches, n)
			}
			walk(n.Children)
		}
	}
	walk(snap.Nodes)
	if len(matches) == 0 {
		return 0, fmt.Errorf("not_found: role=%q name=%q", l.Role, l.Name)
	}
	if len(matches) > 1 {
		var cands []string
		for _, m := range matches {
			cands = append(cands, fmt.Sprintf("%s {role:%q,name:%q}", m.ID, m.Role, m.Name))
		}
		return 0, fmt.Errorf("ambiguous_locator: %s", strings.Join(cands, "; "))
	}
	return matches[0].BackendNodeID, nil
}

func nameMatch(got, want string, exact bool) bool {
	if want == "" {
		return true
	}
	if exact {
		return got == want
	}
	return strings.Contains(strings.ToLower(got), strings.ToLower(want))
}

func (p *Page) resolveCSS(ctx context.Context, sel string) (int64, error) {
	rawDoc, err := p.send(ctx, "DOM.getDocument", nil)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return 0, err
	}
	rawQ, err := p.send(ctx, "DOM.querySelector", map[string]any{"nodeId": doc.Root.NodeID, "selector": sel})
	if err != nil {
		return 0, err
	}
	var q struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(rawQ, &q); err != nil {
		return 0, err
	}
	if q.NodeID == 0 {
		return 0, fmt.Errorf("not_found: css %s", sel)
	}
	rawD, err := p.send(ctx, "DOM.describeNode", map[string]any{"nodeId": q.NodeID})
	if err != nil {
		return 0, err
	}
	var d struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		return 0, err
	}
	return d.Node.BackendNodeID, nil
}

func (p *Page) resolveText(ctx context.Context, text string, exact bool) (int64, error) {
	expr := fmt.Sprintf(`(function(){
		const xpath = %q;
		const r = document.evaluate(xpath, document, null, XPathResult.ANY_TYPE, null);
		const n = r.iterateNext();
		if (!n) return null;
		const second = r.iterateNext();
		if (second) return "AMBIGUOUS";
		return n;
	})()`, textXPath(text, exact))
	return p.runtimeNode(ctx, expr)
}

func (p *Page) resolveXPath(ctx context.Context, xpath string) (int64, error) {
	expr := fmt.Sprintf(`(function(){
		const r = document.evaluate(%q, document, null, XPathResult.ANY_TYPE, null);
		const n = r.iterateNext();
		if (!n) return null;
		const second = r.iterateNext();
		if (second) return "AMBIGUOUS";
		return n;
	})()`, xpath)
	return p.runtimeNode(ctx, expr)
}

func textXPath(text string, exact bool) string {
	t := strings.ReplaceAll(text, `'`, `\'`)
	if exact {
		return fmt.Sprintf(`//*[normalize-space(.)='%s']`, t)
	}
	return fmt.Sprintf(`//*[contains(normalize-space(.), '%s')]`, t)
}

func (p *Page) runtimeNode(ctx context.Context, expr string) (int64, error) {
	raw, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": false,
	})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Result struct {
			Type     string `json:"type"`
			Value    string `json:"value"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if resp.Result.Type == "string" && resp.Result.Value == "AMBIGUOUS" {
		return 0, fmt.Errorf("ambiguous_locator")
	}
	if resp.Result.ObjectID == "" {
		return 0, fmt.Errorf("not_found")
	}
	rawD, err := p.send(ctx, "DOM.requestNode", map[string]any{"objectId": resp.Result.ObjectID})
	if err != nil {
		return 0, err
	}
	var d struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		return 0, err
	}
	rawDesc, _ := p.send(ctx, "DOM.describeNode", map[string]any{"nodeId": d.NodeID})
	var desc struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	_ = json.Unmarshal(rawDesc, &desc)
	return desc.Node.BackendNodeID, nil
}
