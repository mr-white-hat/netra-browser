package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BrowserSender is the minimal interface SaveSession/LoadSession need.
type BrowserSender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
}

type sessionFile struct {
	Name    string           `json:"name"`
	SavedAt time.Time        `json:"saved_at"`
	Cookies []map[string]any `json:"cookies"`
}

// SessionPath returns where a named session is stored.
func SessionPath(name string) (string, error) { return sessionPath(name) }

func sessionPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "netra-browser", "sessions", name+".json"), nil
}

// SaveSession exports cookies via the browser-level CDP client.
func SaveSession(ctx context.Context, c BrowserSender, name string) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	raw, err := c.Send(ctx, "Storage.getCookies", nil)
	if err != nil {
		return err
	}
	var resp struct {
		Cookies []map[string]any `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	path, err := sessionPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(sessionFile{Name: name, SavedAt: time.Now().UTC(), Cookies: resp.Cookies}, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

// LoadSession applies the saved cookies to the current Chrome.
func LoadSession(ctx context.Context, c BrowserSender, name string) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	path, err := sessionPath(name)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read session: %w", err)
	}
	var sf sessionFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return err
	}
	_, err = c.Send(ctx, "Storage.setCookies", map[string]any{"cookies": sf.Cookies})
	return err
}
