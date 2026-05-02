//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/profile"
)

// binPath is the absolute path to the netra-browser binary built once by
// TestMain. Tests use this instead of `go run ../cmd/netra-browser` for two
// reasons: (1) faster (no per-test compile) and (2) avoids `go run`'s mod
// cache writes, which break t.TempDir cleanup when tests override $HOME.
var binPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "netra-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mkdir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binPath = filepath.Join(tmp, "netra-browser")
	build := exec.Command("go", "build", "-o", binPath, "../cmd/netra-browser")
	build.Stderr = os.Stderr
	build.Stdout = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: build: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func findChrome(t *testing.T) string {
	for _, name := range []string{"chromium", "google-chrome", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no chromium binary found in PATH")
	return ""
}

func freePort(t *testing.T) int {
	for p := 9322; p < 9400; p++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", p))
		if err != nil {
			return p
		}
		resp.Body.Close()
	}
	t.Fatal("no free debug port")
	return 0
}

func waitForChrome(t *testing.T, port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("chrome did not come up")
}

// startInGroup spawns a subprocess in its own process group so killGroup
// can take down the whole tree. Required for chromium (forks
// renderer/GPU/zygote children that survive a plain Process.Kill, holding
// file handles in --user-data-dir and breaking t.TempDir cleanup) and for
// `go run` (which spawns a separate compiled child).
func startInGroup(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	profile.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start subprocess: %v", err)
	}
}

// killGroup tears down the entire process tree.
//
// Critically: SIGTERM first so any deferred shutdown handlers (the bridge's
// LaunchHandle.Stop, which kills the chrome process group it spawned) get to
// run. SIGKILL would skip those handlers and leave Chrome processes orphaned
// in their own pgid, racing t.TempDir cleanup. After a short grace window we
// fall back to SIGKILL.
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = profile.TerminateProcessGroup(cmd.Process.Pid)
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
		_ = profile.KillProcessGroup(cmd.Process.Pid)
		<-done
	}
}

func TestE2E_AttachAndListTabs(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	chromeCmd := exec.Command(chrome,
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDir),
		"about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	startInGroup(t, chromeCmd)
	defer killGroup(chromeCmd)
	waitForChrome(t, port, 10*time.Second)

	lockPath := userDir + "/active.lock"
	bin := exec.Command(binPath,
		"--lock", lockPath,
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	startInGroup(t, bin)
	defer killGroup(bin)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !scanner.Scan() {
			t.Fatal("no response")
		}
		var resp map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v line=%s", err, scanner.Text())
		}
		return resp
	}

	// 1. health (detached)
	r := send(1, "meta_health", nil)
	_ = r // not strict-checked here

	// 2. attach
	r = send(2, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	res := r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("attach failed: %v", r)
	}

	// 3. list tabs — about:blank should be there.
	r = send(3, "browser_list_tabs", nil)
	res = r["result"].(map[string]any)
	tabs := res["tabs"].([]any)
	if len(tabs) == 0 {
		t.Fatalf("expected at least one tab, got %v", res)
	}

	// 4. new_tab
	r = send(4, "browser_new_tab", map[string]any{"url": "about:blank"})
	res = r["result"].(map[string]any)
	if res["target_id"] == nil {
		t.Fatalf("no target_id: %v", r)
	}
	tid := res["target_id"].(string)

	// 5. close_tab
	r = send(5, "browser_close_tab", map[string]any{"target_id": tid})
	res = r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("close failed: %v", r)
	}

	// 6. detach
	_ = send(6, "meta_detach", nil)

	// Allow strings package to remain referenced in case future edits add usage.
	_ = strings.TrimSpace
}
