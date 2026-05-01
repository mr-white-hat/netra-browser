package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

type SnapshotMode string

const (
	SnapshotAccessibility SnapshotMode = "accessibility"
	SnapshotDOMText       SnapshotMode = "dom_text"
)

type axNode struct {
	NodeID           string   `json:"nodeId"`
	BackendDOMNodeID int64    `json:"backendDOMNodeId"`
	Role             axValue  `json:"role"`
	Name             axValue  `json:"name"`
	Value            axValue  `json:"value"`
	ChildIDs         []string `json:"childIds"`
	Ignored          bool     `json:"ignored"`
}

type axValue struct {
	Value any `json:"value"`
}

func (v axValue) String() string {
	if v.Value == nil {
		return ""
	}
	switch s := v.Value.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(s)
	}
}

// Snapshot returns the requested snapshot of the page and caches it for snapshot_id resolution.
func (p *Page) Snapshot(ctx context.Context, mode SnapshotMode) (*Snapshot, error) {
	if mode == "" {
		mode = SnapshotAccessibility
	}
	if mode == SnapshotDOMText {
		return p.snapshotDOMText(ctx)
	}

	raw, err := p.send(ctx, "Accessibility.getFullAXTree", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode AX tree: %w", err)
	}

	byNodeID := make(map[string]*axNode, len(resp.Nodes))
	for i := range resp.Nodes {
		byNodeID[resp.Nodes[i].NodeID] = &resp.Nodes[i]
	}

	snap := &Snapshot{byID: map[string]*SnapshotNode{}}
	var counter int
	mkID := func() string {
		id := "#a" + strconv.Itoa(counter)
		counter++
		return id
	}

	var visit func(node *axNode, isRoot bool) *SnapshotNode
	visit = func(node *axNode, isRoot bool) *SnapshotNode {
		if node == nil || node.Ignored {
			return nil
		}
		out := &SnapshotNode{
			ID:            mkID(),
			Role:          node.Role.String(),
			Name:          node.Name.String(),
			Value:         node.Value.String(),
			BackendNodeID: node.BackendDOMNodeID,
		}
		for _, cid := range node.ChildIDs {
			if c := visit(byNodeID[cid], false); c != nil {
				out.Children = append(out.Children, *c)
			}
		}
		// prune leaf nodes with no name/value (noise) — but keep the root.
		if !isRoot && out.Name == "" && out.Value == "" && len(out.Children) == 0 {
			counter--
			return nil
		}
		snap.byID[out.ID] = out
		return out
	}

	// Roots are nodes whose nodeId isn't in any childIds list.
	childSet := map[string]struct{}{}
	for _, n := range resp.Nodes {
		for _, c := range n.ChildIDs {
			childSet[c] = struct{}{}
		}
	}
	for i := range resp.Nodes {
		if _, isChild := childSet[resp.Nodes[i].NodeID]; isChild {
			continue
		}
		if v := visit(&resp.Nodes[i], true); v != nil {
			snap.Nodes = append(snap.Nodes, *v)
		}
	}

	p.mu.Lock()
	p.snapshot = snap
	p.mu.Unlock()
	return snap, nil
}

func (p *Page) snapshotDOMText(ctx context.Context) (*Snapshot, error) {
	raw, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "document.body && document.body.innerText",
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	snap := &Snapshot{
		Nodes: []SnapshotNode{{ID: "#t0", Role: "text", Value: resp.Result.Value}},
		byID:  map[string]*SnapshotNode{},
	}
	snap.byID["#t0"] = &snap.Nodes[0]
	p.mu.Lock()
	p.snapshot = snap
	p.mu.Unlock()
	return snap, nil
}
