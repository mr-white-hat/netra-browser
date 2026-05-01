package profile

import (
	"os/exec"
	"testing"
	"time"
)

func TestLaunchSpawnsChrome(t *testing.T) {
	if _, err := exec.LookPath("chromium"); err != nil {
		t.Skip("chromium not installed")
	}
	tmp := t.TempDir()
	h, err := Launch(LaunchOpts{
		UserDataDir: tmp,
		Headless:    true,
		Args:        []string{"--no-sandbox", "--disable-gpu"},
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer h.Stop()
	if h.DebugURL() == "" {
		t.Fatal("empty DebugURL")
	}
	host, port, err := splitHostPort(h.DebugURL())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Discover(host, port); err == nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("Discover never succeeded against launched chrome")
}
