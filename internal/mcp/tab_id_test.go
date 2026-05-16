package mcp

import "testing"

func TestTabIDForAllocatesAndReuses(t *testing.T) {
	s := NewSession()
	id1 := s.TabIDFor("ABC")
	id2 := s.TabIDFor("ABC")
	if id1 != id2 {
		t.Fatalf("expected stable id; got %q vs %q", id1, id2)
	}
	if id1 != "t1" {
		t.Fatalf("expected first allocation to be t1; got %q", id1)
	}
	id3 := s.TabIDFor("DEF")
	if id3 != "t2" {
		t.Fatalf("expected second target → t2; got %q", id3)
	}
}

func TestResolveTabIDRoundTrips(t *testing.T) {
	s := NewSession()
	target := "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"
	short := s.TabIDFor(target)
	if got := s.ResolveTabID(short); got != target {
		t.Fatalf("resolve %q: got %q want %q", short, got, target)
	}
	// A non-tab-shaped string passes through.
	if got := s.ResolveTabID(target); got != target {
		t.Fatalf("raw target_id should pass through; got %q", got)
	}
	// Unknown "tN" passes through.
	if got := s.ResolveTabID("t999"); got != "t999" {
		t.Fatalf("unknown tab id should pass through; got %q", got)
	}
}

func TestResolveTabIDEmpty(t *testing.T) {
	s := NewSession()
	if got := s.ResolveTabID(""); got != "" {
		t.Fatalf("empty input should stay empty; got %q", got)
	}
}
