package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

type lockData struct {
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// Lock represents an acquired lock on a path.
type Lock struct {
	path string
}

// Acquire creates a lock file. Fails if a live lock exists.
// Stale locks (PID not running) are auto-cleaned.
func Acquire(path string, port int) (*Lock, error) {
	if existing, ok := readLock(path); ok && pidAlive(existing.PID) {
		return nil, fmt.Errorf("lock held by pid %d on port %d", existing.PID, existing.Port)
	}
	return write(path, port)
}

// ForceAcquire overwrites any existing lock.
func ForceAcquire(path string, port int) (*Lock, error) {
	_ = os.Remove(path)
	return write(path, port)
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}

func write(path string, port int) (*Lock, error) {
	if err := os.MkdirAll(parentDir(path), 0o755); err != nil {
		return nil, err
	}
	d := lockData{Port: port, PID: os.Getpid(), StartedAt: time.Now().UTC()}
	b, _ := json.Marshal(d)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, fmt.Errorf("write lock: %w", err)
	}
	return &Lock{path: path}, nil
}

func readLock(path string) (lockData, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return lockData{}, false
	}
	var d lockData
	if err := json.Unmarshal(b, &d); err != nil {
		return lockData{}, false
	}
	return d, true
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return errors.Is(err, os.ErrPermission) // EPERM means process exists, just not ours
	}
	return true
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
