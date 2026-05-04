package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

	// LocalStorage is keyed by origin (e.g. "https://example.com"). Values are the
	// origin's localStorage entries as a flat string→string map. Optional — older
	// session files saved before Plan H ship as cookies-only and load fine.
	LocalStorage map[string]map[string]string `json:"local_storage,omitempty"`
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
// localStorage capture lives in the tool layer (needs target-attached sessions
// for DOMStorage.* — see internal/mcp/tools/sessions.go).
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
	return WriteSessionFile(name, SessionFile{
		Name:    name,
		SavedAt: time.Now().UTC(),
		Cookies: resp.Cookies,
	})
}

// LoadSession applies the saved cookies. localStorage application lives in the
// tool layer (needs target sessions on each origin).
func LoadSession(ctx context.Context, c BrowserSender, name string) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	sf, err := ReadSessionFile(name)
	if err != nil {
		return err
	}
	_, err = c.Send(ctx, "Storage.setCookies", map[string]any{"cookies": sf.Cookies})
	return err
}

// SessionFile is the on-disk shape exported so the tool layer can read/write
// the full payload (cookies + localStorage) without re-implementing I/O.
type SessionFile = sessionFile

// ReadSessionFile loads a named session from disk.
func ReadSessionFile(name string) (SessionFile, error) {
	if name == "" {
		return SessionFile{}, fmt.Errorf("name required")
	}
	path, err := sessionPath(name)
	if err != nil {
		return SessionFile{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return SessionFile{}, fmt.Errorf("read session: %w", err)
	}
	var sf SessionFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return SessionFile{}, err
	}
	return sf, nil
}

// WriteSessionFile persists the full session payload.
func WriteSessionFile(name string, sf SessionFile) error {
	path, err := sessionPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if sf.SavedAt.IsZero() {
		sf.SavedAt = time.Now().UTC()
	}
	if sf.Name == "" {
		sf.Name = name
	}
	b, _ := json.MarshalIndent(sf, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

// OriginFromURL returns the scheme://host[:port] origin or "" for non-http(s)
// URLs (about:blank, chrome://, file://, data: — none have meaningful LS).
func OriginFromURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
