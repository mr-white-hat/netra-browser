//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestE2E_SessionLocalStorageRoundTrip verifies that task_save_session captures
// localStorage from currently-open tabs and task_load_session restores it.
// Uses example.com (IANA-reserved, no redirect) so cold-load auto-navigate is
// reliable.
func TestE2E_SessionLocalStorageRoundTrip(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()
	homeDir := t.TempDir() // session JSON lands under $HOME/.config/netra-browser/sessions

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

	bin := exec.Command(binPath,
		"--lock", userDir+"/active.lock",
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	bin.Env = append(os.Environ(), "HOME="+homeDir)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	startInGroup(t, bin)
	defer killGroup(bin)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	id := 0
	rpc := func(method string, params any) map[string]any {
		t.Helper()
		id++
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !sc.Scan() {
			t.Fatal("no response")
		}
		var resp map[string]any
		if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v line=%s", err, sc.Text())
		}
		return resp
	}

	// 1. attach
	resp := rpc("meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if r, _ := resp["result"].(map[string]any); r == nil || r["ok"] != true {
		t.Skipf("attach failed (no network in CI?): %v", resp)
	}

	// 2. open example.com and set LS via browser_eval.
	resp = rpc("browser_new_tab", map[string]any{"url": "https://example.com/"})
	r := resp["result"].(map[string]any)
	tid := r["target_id"].(string)
	time.Sleep(3 * time.Second) // let the page load

	// Confirm the network reached example.com (skip the test gracefully if not).
	resp = rpc("browser_eval", map[string]any{"target_id": tid, "expression": "location.host"})
	r = resp["result"].(map[string]any)
	if !strings.Contains(fmt.Sprint(r["result"]), "example.com") {
		t.Skipf("network unavailable, location.host = %v", r["result"])
	}

	rpc("browser_eval", map[string]any{"target_id": tid, "expression": "localStorage.setItem('auth','XYZ123'); localStorage.setItem('theme','dark'); 'ok'"})

	// 3. save_session
	resp = rpc("task_save_session", map[string]any{"name": "ls_e2e"})
	r = resp["result"].(map[string]any)
	if r["ok"] != true {
		t.Fatalf("save: %v", resp)
	}
	origins, _ := r["local_storage_origins"].([]any)
	if len(origins) == 0 || origins[0] != "https://example.com" {
		t.Fatalf("expected example.com origin captured, got %v", origins)
	}

	// 4. clear LS in the page.
	rpc("browser_eval", map[string]any{"target_id": tid, "expression": "localStorage.clear(); 'cleared'"})

	// 5. load_session — should restore via the still-open tab.
	resp = rpc("task_load_session", map[string]any{"name": "ls_e2e"})
	r = resp["result"].(map[string]any)
	applied, _ := r["local_storage_origins_applied"].([]any)
	if len(applied) != 1 || applied[0] != "https://example.com" {
		t.Fatalf("expected one origin applied, got %v", applied)
	}

	// 6. verify in-page LS via browser_eval.
	resp = rpc("browser_eval", map[string]any{"target_id": tid, "expression": "JSON.stringify({a: localStorage.getItem('auth'), t: localStorage.getItem('theme')})"})
	r = resp["result"].(map[string]any)
	got := fmt.Sprint(r["result"])
	if !strings.Contains(got, "XYZ123") || !strings.Contains(got, "dark") {
		t.Fatalf("LS not restored: %s", got)
	}
}
