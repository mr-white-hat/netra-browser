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
	"strings"
	"testing"
	"time"
)

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
	if err := chromeCmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	lockPath := userDir + "/active.lock"
	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", lockPath,
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	if err := bin.Start(); err != nil {
		t.Fatalf("start bridge: %v", err)
	}
	defer bin.Process.Kill()

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
