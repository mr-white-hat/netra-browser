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
	"strings"
	"testing"
	"time"
)

func TestE2E_FormSubmission(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/form.html")
	})
	mux.HandleFunc("/submitted", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "OK email=%s pw=%s", r.URL.Query().Get("email"), r.URL.Query().Get("password"))
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
		t.Fatalf("start bridge: %v", err)
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

	// 1. attach
	r := send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	mustOK("attach", r)

	// 2. open new tab on the form URL
	r = send(2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	res := mustOK("new_tab", r)
	tid := res["target_id"].(string)
	// new tab opening might not have time to load before we navigate; tiny pause.
	time.Sleep(200 * time.Millisecond)

	// 3. navigate (idempotent re-navigate to anchor session and wait for load)
	r = send(3, "browser_navigate", map[string]any{"target_id": tid, "url": srv.URL + "/", "wait_until": "load"})
	mustOK("navigate", r)

	// 4. fill email
	r = send(4, "browser_fill", map[string]any{
		"target_id": tid,
		"locator":   map[string]any{"css": "#email"},
		"value":     "alice@example.com",
	})
	mustOK("fill email", r)

	// 5. fill password
	r = send(5, "browser_fill", map[string]any{
		"target_id": tid,
		"locator":   map[string]any{"css": "#password"},
		"value":     "hunter2",
	})
	mustOK("fill password", r)

	// 6. click submit
	r = send(6, "browser_click", map[string]any{
		"target_id": tid,
		"locator":   map[string]any{"css": "#submit"},
	})
	mustOK("click submit", r)

	// 7. give navigation time then snapshot in dom_text mode to confirm submission.
	time.Sleep(700 * time.Millisecond)
	r = send(7, "browser_snapshot", map[string]any{"target_id": tid, "mode": "dom_text"})
	res = mustOK("snapshot", r)
	snap := res["snapshot"].([]any)
	if len(snap) == 0 {
		t.Fatal("empty snapshot")
	}
	body := snap[0].(map[string]any)
	val, _ := body["value"].(string)
	if !strings.Contains(val, "alice@example.com") {
		t.Fatalf("submission body missing email: %q", val)
	}
}
