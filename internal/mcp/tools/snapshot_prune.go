package tools

import "github.com/mr-white-hat/netra-browser/internal/browser"

// pruneEmptyContainers strips WebArea/generic/group nodes that have no name and
// whose subtree has no name/value either — the kind of structural padding that
// inflates snapshots without helping locator resolution.
//
// Active when --snapshot-prune-aggressive is set. The default snapshot already
// drops empty leaves; this pass is the more aggressive sibling that also trims
// empty containers (collapsing their useful descendants up to the parent).
func pruneEmptyContainers(nodes []browser.SnapshotNode) []browser.SnapshotNode {
	out := make([]browser.SnapshotNode, 0, len(nodes))
	for _, n := range nodes {
		if pruned, keep := pruneNode(n); keep {
			if len(pruned) == 1 {
				out = append(out, pruned[0])
			} else {
				out = append(out, pruned...)
			}
		}
	}
	return out
}

func isEmptyContainer(n browser.SnapshotNode) bool {
	if n.Name != "" || n.Value != "" {
		return false
	}
	switch n.Role {
	case "WebArea", "RootWebArea", "generic", "group", "none", "":
		return true
	}
	return false
}

// pruneNode returns either:
//   - one element (this node, with pruned children) when kept
//   - many elements (the node's pruned children, hoisted) when this node is an empty container
//   - nothing when the whole subtree is empty
func pruneNode(n browser.SnapshotNode) ([]browser.SnapshotNode, bool) {
	pruned := pruneEmptyContainers(n.Children)
	if isEmptyContainer(n) {
		if len(pruned) == 0 {
			return nil, false
		}
		return pruned, true
	}
	n.Children = pruned
	return []browser.SnapshotNode{n}, true
}
