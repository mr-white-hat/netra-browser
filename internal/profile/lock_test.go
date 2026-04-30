package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.lock")

	l, err := Acquire(path, 9222)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock file not removed: %v", err)
	}
}

func TestLockSecondAcquireFailsUntilForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.lock")

	l1, err := Acquire(path, 9222)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Release()

	if _, err := Acquire(path, 9223); err == nil {
		t.Fatal("expected second acquire to fail")
	}

	l2, err := ForceAcquire(path, 9223)
	if err != nil {
		t.Fatalf("force: %v", err)
	}
	defer l2.Release()
}

func TestLockStaleAutoCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.lock")

	// Write a lock for a definitely-dead PID.
	if err := os.WriteFile(path, []byte(`{"port":9222,"pid":999999,"started_at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(path, 9222)
	if err != nil {
		t.Fatalf("expected stale-lock cleanup, got: %v", err)
	}
	defer l.Release()
}
