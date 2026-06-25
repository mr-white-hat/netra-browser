package mcp

import (
	"fmt"
	"sort"
)

// tabGroup is one agent's private lane: a set of owned tabs plus its own
// "active" tab. Each Claude agent calls browser_create_group once and then
// works only within the group it was given. Group state lives in-memory on the
// Session and is keyed by a short id ("g1", "g2", ...).
type tabGroup struct {
	id     string
	active string
	owned  map[string]bool
}

// GroupInfo is the externally-visible snapshot of a group (used by
// browser_list_groups).
type GroupInfo struct {
	ID     string   `json:"group_id"`
	Active string   `json:"active_target"`
	Tabs   []string `json:"tabs"`
}

// CreateGroup mints a new, empty group and returns its id ("g1", "g2", ...).
// Ids are never reused for the life of the session.
func (s *Session) CreateGroup() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups == nil {
		s.groups = map[string]*tabGroup{}
		s.targetGroup = map[string]string{}
	}
	s.nextGroupSeq++
	id := fmt.Sprintf("g%d", s.nextGroupSeq)
	s.groups[id] = &tabGroup{id: id, owned: map[string]bool{}}
	return id
}

// GroupExists reports whether a group with the given id is live.
func (s *Session) GroupExists(id string) bool {
	if id == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.groups[id]
	return ok
}

// AdoptToGroup assigns target to groupID, moving it out of any group that
// previously owned it. No-op if the group does not exist.
func (s *Session) AdoptToGroup(groupID, target string) {
	if target == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adoptLocked(groupID, target)
}

// adoptLocked performs adoption assuming s.mu is held.
func (s *Session) adoptLocked(groupID, target string) {
	g, ok := s.groups[groupID]
	if !ok {
		return
	}
	if prev, had := s.targetGroup[target]; had && prev != groupID {
		if pg, ok := s.groups[prev]; ok {
			delete(pg.owned, target)
			if pg.active == target {
				pg.active = ""
			}
		}
	}
	g.owned[target] = true
	s.targetGroup[target] = groupID
}

// GroupOf returns the id of the group that owns target, or "" if none.
func (s *Session) GroupOf(target string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.targetGroup[target]
}

// GroupTargets returns the sorted list of tabs owned by groupID.
func (s *Session) GroupTargets(groupID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groups[groupID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(g.owned))
	for t := range g.owned {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// GroupActive returns the group's current active tab, or "" if unset/unknown.
func (s *Session) GroupActive(groupID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if g, ok := s.groups[groupID]; ok {
		return g.active
	}
	return ""
}

// SetGroupActive makes target the active tab for groupID and adopts it into the
// group. No-op if the group does not exist.
func (s *Session) SetGroupActive(groupID, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[groupID]
	if !ok {
		return
	}
	if target != "" {
		s.adoptLocked(groupID, target)
	}
	g.active = target
}

// Groups returns a snapshot of every live group.
func (s *Session) Groups() []GroupInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.groups))
	for id := range s.groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]GroupInfo, 0, len(ids))
	for _, id := range ids {
		g := s.groups[id]
		tabs := make([]string, 0, len(g.owned))
		for t := range g.owned {
			tabs = append(tabs, t)
		}
		sort.Strings(tabs)
		out = append(out, GroupInfo{ID: id, Active: g.active, Tabs: tabs})
	}
	return out
}

// CloseGroup removes groupID and returns the tabs it owned (so the caller can
// decide whether to close or release them). Reports ok=false for unknown ids.
func (s *Session) CloseGroup(groupID string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[groupID]
	if !ok {
		return nil, false
	}
	targets := make([]string, 0, len(g.owned))
	for t := range g.owned {
		targets = append(targets, t)
		delete(s.targetGroup, t)
	}
	sort.Strings(targets)
	delete(s.groups, groupID)
	return targets, true
}

// ResolveActive is the single group-aware entry point every tool uses to decide
// which tab a call operates on. Resolution order:
//
//	explicit target_id (alias-resolved) > group's active tab > session active tab
//
// Enforcement when a group_id is supplied:
//   - unknown group_id            -> unknown_group
//   - target owned by ANOTHER group -> cross_group
//   - target free (unowned)       -> adopted into this group
//   - no target and group has no active tab -> invalid_args
//
// When group_id is empty the legacy single-active-target behavior is preserved,
// so existing single-agent callers are unaffected.
func (s *Session) ResolveActive(groupID, targetID string) (string, *ToolError) {
	targetID = s.ResolveTabID(targetID)

	if groupID == "" {
		if targetID == "" {
			targetID = s.ActiveTarget()
		}
		if targetID == "" {
			return "", &ToolError{Code: ErrInvalidArgs, Message: "no target_id and no active target"}
		}
		return targetID, nil
	}

	if !s.GroupExists(groupID) {
		return "", &ToolError{Code: ErrUnknownGroup, Message: "unknown group_id: " + groupID}
	}

	if targetID == "" {
		if a := s.GroupActive(groupID); a != "" {
			return a, nil
		}
		return "", &ToolError{Code: ErrInvalidArgs, Message: "group " + groupID + " has no active tab; pass target_id or open a tab in the group"}
	}

	if owner := s.GroupOf(targetID); owner != "" && owner != groupID {
		return "", &ToolError{Code: ErrCrossGroup, Message: "tab is owned by group " + owner + ", not " + groupID, TargetID: targetID}
	}
	// Free tab: claim it for this group so subsequent implicit calls resolve.
	s.AdoptToGroup(groupID, targetID)
	return targetID, nil
}
