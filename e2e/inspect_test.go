//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestE2E_InspectionStack(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/dialog.html")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromeCmd := exec.Command(chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir, "about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	lockPath := filepath.Join(userDir, "active.lock")
	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", lockPath,
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	if err := bin.Start(); err != nil {
		t.Fatal(err)
	}
	defer bin.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !scanner.Scan() {
			t.Fatalf("no resp for %s", method)
		}
		var r map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &r)
		return r
	}
	mustOK := func(label string, r map[string]any) map[string]any {
		res, ok := r["result"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no result: %v", label, r)
		}
		if res["ok"] != true {
			t.Fatalf("%s: not ok: %v", label, res)
		}
		return res
	}

	send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	r := send(2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid := mustOK("new_tab", r)["target_id"].(string)

	// Navigate with domcontentloaded: triggers NewPage/Target.attachToTarget and
	// returns quickly—before the 200ms alert fires—so CDP session is ready.
	r = send(3, "browser_navigate", map[string]any{"target_id": tid, "url": srv.URL + "/", "wait_until": "domcontentloaded"})
	mustOK("navigate", r)

	// Sleep long enough for the setTimeout(alert, 200) to fire and get buffered.
	time.Sleep(500 * time.Millisecond)

	// 4. Verify dialog event is buffered in the ring (WaitFor misses past events).
	r = send(4, "browser_get_recent_events", map[string]any{"target_id": tid, "types": []string{"dialog"}})
	res := mustOK("dialog in ring", r)
	dialogEvs, _ := res["events"].([]any)
	if len(dialogEvs) == 0 {
		t.Fatalf("expected at least one dialog event in ring buffer, got none: %v", res)
	}

	// 5. Handle the pending dialog so JS unblocks (required before eval/screenshot).
	r = send(5, "browser_handle_dialog", map[string]any{"target_id": tid, "action": "accept"})
	mustOK("handle_dialog", r)

	// Brief pause to let Chrome fully unblock after dialog dismissal.
	time.Sleep(100 * time.Millisecond)

	// 6. eval document.title (safe now: no pending dialog)
	r = send(6, "browser_eval", map[string]any{"target_id": tid, "expression": "document.title"})
	res = mustOK("eval title", r)
	if title, _ := res["result"].(string); title != "Dialog Test" {
		t.Fatalf("title eval: got %q", title)
	}

	// 7. screenshot
	r = send(7, "browser_screenshot", map[string]any{"target_id": tid})
	res = mustOK("screenshot", r)
	if _, ok := res["png_base64"].(string); !ok {
		t.Fatalf("no png: %v", res)
	}

	// 8. get cookies (likely empty for httptest origin but call should succeed)
	r = send(8, "browser_get_cookies", map[string]any{"target_id": tid})
	mustOK("get_cookies", r)

	// 9. recent events (navigation) — confirm the call shape works
	r = send(9, "browser_get_recent_events", map[string]any{"target_id": tid, "types": []string{"navigation"}})
	res = mustOK("recent_events", r)
	if _, ok := res["events"].([]any); !ok {
		t.Fatalf("events not an array: %v", res)
	}
}
