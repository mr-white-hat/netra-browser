package profile

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type LaunchOpts struct {
	BinaryPath  string   // path to chromium/chrome; default: discovered from PATH
	UserDataDir string   // default: ~/.config/google-chrome (Linux) etc.
	Headless    bool
	Args        []string // extra args
}

type LaunchHandle struct {
	cmd      *exec.Cmd
	debugURL string
	dir      string
}

func (h *LaunchHandle) DebugURL() string    { return h.debugURL }
func (h *LaunchHandle) UserDataDir() string { return h.dir }

func (h *LaunchHandle) Stop() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	_ = h.cmd.Process.Kill()
	_, _ = h.cmd.Process.Wait()
	return nil
}

// Launch spawns Chrome with a managed remote debugging port and waits for it to come up.
func Launch(opts LaunchOpts) (*LaunchHandle, error) {
	bin := opts.BinaryPath
	if bin == "" {
		var err error
		bin, err = findChrome()
		if err != nil {
			return nil, err
		}
	}
	dir := opts.UserDataDir
	if dir == "" {
		var err error
		dir, err = DefaultUserDataDir()
		if err != nil {
			return nil, err
		}
	}
	port, err := freeTCPPort()
	if err != nil {
		return nil, err
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir=" + dir,
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, opts.Args...)
	args = append(args, "about:blank")

	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	h := &LaunchHandle{cmd: cmd, debugURL: fmt.Sprintf("http://127.0.0.1:%d", port), dir: dir}
	if err := h.waitReady(10 * time.Second); err != nil {
		_ = h.Stop()
		return nil, err
	}
	return h, nil
}

func (h *LaunchHandle) waitReady(timeout time.Duration) error {
	host, port, err := splitHostPort(h.debugURL)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := Discover(host, port); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("chrome did not become ready within %s", timeout)
}

func findChrome() (string, error) {
	for _, n := range []string{"chromium", "chrome", "google-chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no chromium binary found in PATH")
}

// DefaultUserDataDir returns the standard Chrome user-data-dir for this platform.
func DefaultUserDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "google-chrome"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), nil
	case "windows":
		return filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data"), nil
	}
	return filepath.Join(home, ".config", "google-chrome"), nil
}

func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// splitHostPort accepts "http://host:port" or "host:port".
func splitHostPort(s string) (string, int, error) {
	if strings.HasPrefix(s, "http://") {
		s = strings.TrimPrefix(s, "http://")
	}
	if strings.HasPrefix(s, "https://") {
		s = strings.TrimPrefix(s, "https://")
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, err
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, p, nil
}
