package profile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Project is the on-disk record of a bridge's tab ownership.
//
// Multiple bridges can run concurrently against one Chrome — each has its own
// project sidecar so its tools (e.g. browser_list_tabs) only see its own tabs.
// Stale projects (owner pid not alive) are cleaned on bridge startup.
type Project struct {
	Name           string    `json:"name"`
	OwnerPID       int       `json:"owner_pid"`
	OwnedTargetIDs []string  `json:"owned_target_ids"`
	CreatedAt      time.Time `json:"created_at"`

	mu   sync.Mutex
	path string
}

// DefaultProjectsDir returns ~/.config/netra-browser/projects (creating ancestors lazily).
func DefaultProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "netra-browser", "projects"), nil
}

// GenerateProjectName returns a short ID like "proj-3a7f".
func GenerateProjectName() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return "proj-" + hex.EncodeToString(b[:])
}

// OpenProject loads an existing project file or creates a fresh one. The caller's
// PID becomes the owner. Stale projects (owner pid dead) are cleaned via SweepStaleProjects;
// here we treat a stale-named file as if it didn't exist.
func OpenProject(dir, name string) (*Project, error) {
	if name == "" {
		name = GenerateProjectName()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, name+".json")
	if existing, err := readProject(path); err == nil {
		if pidAlive(existing.OwnerPID) && existing.OwnerPID != os.Getpid() {
			return nil, fmt.Errorf("project %q is owned by live pid %d", name, existing.OwnerPID)
		}
		// Reuse the existing record (same pid re-opening, or stale cleanup happens elsewhere).
		existing.OwnerPID = os.Getpid()
		if err := existing.persist(); err != nil {
			return nil, err
		}
		return existing, nil
	}
	p := &Project{
		Name:      name,
		OwnerPID:  os.Getpid(),
		CreatedAt: time.Now().UTC(),
		path:      path,
	}
	if err := p.persist(); err != nil {
		return nil, err
	}
	return p, nil
}

// Path returns the on-disk path for the project file.
func (p *Project) Path() string { return p.path }

// Adopt records that targetID belongs to this project. Idempotent.
func (p *Project) Adopt(targetID string) error {
	if targetID == "" {
		return fmt.Errorf("empty target_id")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.OwnedTargetIDs {
		if t == targetID {
			return nil
		}
	}
	p.OwnedTargetIDs = append(p.OwnedTargetIDs, targetID)
	return p.persist()
}

// Release removes targetID from this project. Idempotent.
func (p *Project) Release(targetID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.OwnedTargetIDs[:0]
	for _, t := range p.OwnedTargetIDs {
		if t != targetID {
			out = append(out, t)
		}
	}
	p.OwnedTargetIDs = out
	return p.persist()
}

// Owns reports whether targetID is in this project's owned list.
func (p *Project) Owns(targetID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.OwnedTargetIDs {
		if t == targetID {
			return true
		}
	}
	return false
}

// Targets returns a copy of the owned target IDs.
func (p *Project) Targets() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.OwnedTargetIDs))
	copy(out, p.OwnedTargetIDs)
	return out
}

// Remove deletes the on-disk project file.
func (p *Project) Remove() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.path == "" {
		return nil
	}
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ListProjects returns every project file in dir (alive and stale).
// Each returned project's path is set; OwnerPID may belong to a dead process.
func ListProjects(dir string) ([]*Project, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Project
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p, err := readProject(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SweepStaleProjects removes project files whose owner pid is no longer alive.
// Returns the count removed.
func SweepStaleProjects(dir string) (int, error) {
	projects, err := ListProjects(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range projects {
		if !pidAlive(p.OwnerPID) {
			if err := os.Remove(p.path); err == nil {
				n++
			}
		}
	}
	return n, nil
}

func readProject(path string) (*Project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Project
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	p.path = path
	return &p, nil
}

func (p *Project) persist() error {
	type wire struct {
		Name           string    `json:"name"`
		OwnerPID       int       `json:"owner_pid"`
		OwnedTargetIDs []string  `json:"owned_target_ids"`
		CreatedAt      time.Time `json:"created_at"`
	}
	w := wire{
		Name:           p.Name,
		OwnerPID:       p.OwnerPID,
		OwnedTargetIDs: p.OwnedTargetIDs,
		CreatedAt:      p.CreatedAt,
	}
	if w.OwnedTargetIDs == nil {
		w.OwnedTargetIDs = []string{}
	}
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, b, 0o600)
}
