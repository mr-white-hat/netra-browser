package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectOpenAdoptRelease(t *testing.T) {
	dir := t.TempDir()
	p, err := OpenProject(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "alpha" {
		t.Fatalf("name: %s", p.Name)
	}
	if _, err := os.Stat(p.Path()); err != nil {
		t.Fatalf("not persisted: %v", err)
	}
	if err := p.Adopt("T1"); err != nil {
		t.Fatal(err)
	}
	if err := p.Adopt("T1"); err != nil {
		t.Fatalf("idempotent adopt failed: %v", err)
	}
	if err := p.Adopt("T2"); err != nil {
		t.Fatal(err)
	}
	if !p.Owns("T1") || !p.Owns("T2") {
		t.Fatalf("missing target")
	}
	if err := p.Release("T1"); err != nil {
		t.Fatal(err)
	}
	if p.Owns("T1") {
		t.Fatal("T1 still owned after release")
	}
	if err := p.Release("missing"); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}

	// Re-load: expect T2 only.
	loaded, err := OpenProject(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Owns("T2") || loaded.Owns("T1") {
		t.Fatalf("reload mismatch: %v", loaded.Targets())
	}
}

func TestProjectAutoNameWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	p, err := OpenProject(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Name) == 0 {
		t.Fatal("expected auto-generated name")
	}
}

func TestSweepStaleProjects(t *testing.T) {
	dir := t.TempDir()
	// Stale: pid 999999 won't exist.
	stalePath := filepath.Join(dir, "stale.json")
	if err := os.WriteFile(stalePath, []byte(`{"name":"stale","owner_pid":999999,"owned_target_ids":[],"created_at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Alive: my own pid.
	if _, err := OpenProject(dir, "live"); err != nil {
		t.Fatal(err)
	}

	n, err := SweepStaleProjects(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 swept, got %d", n)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("stale file still present")
	}
	if _, err := os.Stat(filepath.Join(dir, "live.json")); err != nil {
		t.Fatal("live file removed")
	}
}

func TestOpenProjectRefusesLiveOwner(t *testing.T) {
	dir := t.TempDir()
	// Simulate another live owner by writing a sidecar with PID = parent process (which is alive).
	path := filepath.Join(dir, "claimed.json")
	otherPID := os.Getppid()
	if otherPID == os.Getpid() || !pidAlive(otherPID) {
		t.Skip("no usable foreign live PID")
	}
	if err := os.WriteFile(path, []byte(`{"name":"claimed","owner_pid":`+itoa(otherPID)+`,"owned_target_ids":[],"created_at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProject(dir, "claimed"); err == nil {
		t.Fatal("expected refusal when owner pid is alive")
	}
}

func TestListProjects(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenProject(dir, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProject(dir, "b"); err != nil {
		t.Fatal(err)
	}
	got, err := ListProjects(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("unexpected: %+v", got)
	}
}

// minimal int->string to avoid pulling strconv just for this test helper
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
