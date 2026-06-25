package mcp

import "testing"

func TestCreateGroupSequentialAndExists(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	g2 := s.CreateGroup()
	if g1 == g2 {
		t.Fatalf("expected distinct group ids, got %q == %q", g1, g2)
	}
	if g1 != "g1" || g2 != "g2" {
		t.Fatalf("expected g1/g2, got %q/%q", g1, g2)
	}
	if !s.GroupExists(g1) || !s.GroupExists(g2) {
		t.Fatalf("created groups should exist")
	}
	if s.GroupExists("g99") {
		t.Fatalf("unknown group should not exist")
	}
}

func TestAdoptToGroupAndReverseIndex(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	g2 := s.CreateGroup()
	s.AdoptToGroup(g1, "T-A")
	if got := s.GroupOf("T-A"); got != g1 {
		t.Fatalf("GroupOf(T-A)=%q want %q", got, g1)
	}
	if tgs := s.GroupTargets(g1); len(tgs) != 1 || tgs[0] != "T-A" {
		t.Fatalf("GroupTargets(g1)=%v want [T-A]", tgs)
	}
	// Re-adopting into another group moves ownership.
	s.AdoptToGroup(g2, "T-A")
	if got := s.GroupOf("T-A"); got != g2 {
		t.Fatalf("after move GroupOf(T-A)=%q want %q", got, g2)
	}
	if len(s.GroupTargets(g1)) != 0 {
		t.Fatalf("g1 should be empty after move, got %v", s.GroupTargets(g1))
	}
}

func TestGroupActive(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	if s.GroupActive(g1) != "" {
		t.Fatalf("new group should have no active target")
	}
	s.SetGroupActive(g1, "T-A")
	if s.GroupActive(g1) != "T-A" {
		t.Fatalf("GroupActive=%q want T-A", s.GroupActive(g1))
	}
	// Setting active also adopts the tab into the group.
	if s.GroupOf("T-A") != g1 {
		t.Fatalf("SetGroupActive should adopt the target into the group")
	}
}

func TestResolveActiveLegacyNoGroup(t *testing.T) {
	s := NewSession()
	// Legacy: no group, no explicit target -> falls back to session active.
	s.SetActiveTarget("T-SESS")
	got, terr := s.ResolveActive("", "")
	if terr != nil {
		t.Fatalf("unexpected error: %+v", terr)
	}
	if got != "T-SESS" {
		t.Fatalf("got %q want T-SESS", got)
	}
	// Legacy with explicit target passes through unchanged.
	got, terr = s.ResolveActive("", "T-X")
	if terr != nil || got != "T-X" {
		t.Fatalf("got %q err %+v want T-X/nil", got, terr)
	}
}

func TestResolveActiveLegacyNoTarget(t *testing.T) {
	s := NewSession()
	_, terr := s.ResolveActive("", "")
	if terr == nil {
		t.Fatalf("expected error when no group and no active target")
	}
	if terr.Code != ErrInvalidArgs {
		t.Fatalf("code=%q want %q", terr.Code, ErrInvalidArgs)
	}
}

func TestResolveActiveUnknownGroup(t *testing.T) {
	s := NewSession()
	_, terr := s.ResolveActive("g7", "")
	if terr == nil || terr.Code != ErrUnknownGroup {
		t.Fatalf("expected unknown_group, got %+v", terr)
	}
}

func TestResolveActiveGroupActiveFallback(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	s.SetGroupActive(g1, "T-A")
	got, terr := s.ResolveActive(g1, "")
	if terr != nil || got != "T-A" {
		t.Fatalf("got %q err %+v want T-A", got, terr)
	}
}

func TestResolveActiveGroupNoActive(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	_, terr := s.ResolveActive(g1, "")
	if terr == nil || terr.Code != ErrInvalidArgs {
		t.Fatalf("expected invalid_args (no active tab in group), got %+v", terr)
	}
}

func TestResolveActiveCrossGroupRejected(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	g2 := s.CreateGroup()
	s.AdoptToGroup(g2, "T-OWNED-BY-2")
	// g1 tries to act on a tab owned by g2 -> cross_group.
	_, terr := s.ResolveActive(g1, "T-OWNED-BY-2")
	if terr == nil || terr.Code != ErrCrossGroup {
		t.Fatalf("expected cross_group, got %+v", terr)
	}
	if terr != nil && terr.TargetID != "T-OWNED-BY-2" {
		t.Fatalf("cross_group error should carry the target id, got %q", terr.TargetID)
	}
}

func TestResolveActiveGroupOwnTargetOK(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	s.AdoptToGroup(g1, "T-A")
	got, terr := s.ResolveActive(g1, "T-A")
	if terr != nil || got != "T-A" {
		t.Fatalf("group acting on its own tab should succeed, got %q err %+v", got, terr)
	}
}

func TestResolveActiveGroupUnownedTargetAdopts(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	// Target not owned by any group: a group acting on it claims it.
	got, terr := s.ResolveActive(g1, "T-FREE")
	if terr != nil || got != "T-FREE" {
		t.Fatalf("got %q err %+v want T-FREE", got, terr)
	}
	if s.GroupOf("T-FREE") != g1 {
		t.Fatalf("acting on a free tab should adopt it into the group")
	}
}

func TestResolveActiveResolvesTabAlias(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	tab := s.TabIDFor("T-REAL") // allocates t1
	s.AdoptToGroup(g1, "T-REAL")
	got, terr := s.ResolveActive(g1, tab)
	if terr != nil || got != "T-REAL" {
		t.Fatalf("alias %q should resolve to T-REAL, got %q err %+v", tab, got, terr)
	}
}

func TestCloseGroupReturnsTargetsAndClears(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	s.AdoptToGroup(g1, "T-A")
	s.AdoptToGroup(g1, "T-B")
	targets, ok := s.CloseGroup(g1)
	if !ok {
		t.Fatalf("CloseGroup should report ok for existing group")
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 released targets, got %v", targets)
	}
	if s.GroupExists(g1) {
		t.Fatalf("group should be gone after close")
	}
	if s.GroupOf("T-A") != "" || s.GroupOf("T-B") != "" {
		t.Fatalf("reverse index should be cleared after close")
	}
	if _, ok := s.CloseGroup("g404"); ok {
		t.Fatalf("closing unknown group should report not-ok")
	}
}

func TestClearResetsGroups(t *testing.T) {
	s := NewSession()
	g1 := s.CreateGroup()
	s.AdoptToGroup(g1, "T-A")
	s.Clear()
	if s.GroupExists(g1) {
		t.Fatalf("Clear should drop all groups")
	}
	if s.GroupOf("T-A") != "" {
		t.Fatalf("Clear should drop reverse index")
	}
	// Group sequence restarts clean after Clear.
	if g := s.CreateGroup(); g != "g1" {
		t.Fatalf("after Clear, first group should be g1, got %q", g)
	}
}
