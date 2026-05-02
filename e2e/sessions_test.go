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

func TestE2E_SessionRoundTrip(t *testing.T) {
	chrome := findChrome(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "ABCDEF", Path: "/"})
		fmt.Fprintln(w, "<html><body>set</body></html>")
	})
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sid")
		if err != nil {
			fmt.Fprintln(w, "MISSING")
			return
		}
		fmt.Fprintln(w, "GOT="+c.Value)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	bridgeHome := t.TempDir()

	// bridgeIdx is used to generate unique lock-file names so that re-using
	// the same debug port after the first chrome is killed does not collide.
	bridgeIdx := 0

	mkChrome := func(t *testing.T) (port int, kill func()) {
		t.Helper()
		port = freePort(t)
		dir := t.TempDir()
		cmd := exec.Command(chrome, "--headless=new", "--no-sandbox", "--disable-gpu",
			fmt.Sprintf("--remote-debugging-port=%d", port), "--user-data-dir="+dir, "about:blank")
		cmd.Stderr = os.Stderr
		startInGroup(t, cmd)
		waitForChrome(t, port, 10*time.Second)
		return port, func() { killGroup(cmd) }
	}

	mkBridge := func(t *testing.T, port int) (cmd *exec.Cmd, scanner *bufio.Scanner, stdin io.Writer, kill func()) {
		t.Helper()
		bridgeIdx++
		cmd = exec.Command(binPath,
			"--lock", filepath.Join(bridgeHome, fmt.Sprintf("active-%d.lock", bridgeIdx)),
			"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
		)
		cmd.Env = append(os.Environ(), "HOME="+bridgeHome)
		stdinPipe, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()
		go io.Copy(os.Stderr, stderrPipe)
		startInGroup(t, cmd)
		s := bufio.NewScanner(stdout)
		s.Buffer(make([]byte, 1<<20), 16<<20)
		return cmd, s, stdinPipe, func() { killGroup(cmd) }
	}

	send := func(s *bufio.Scanner, w io.Writer, id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(w, string(b)+"\n")
		if !s.Scan() {
			t.Fatalf("no resp from %s", method)
		}
		var r map[string]any
		_ = json.Unmarshal(s.Bytes(), &r)
		return r
	}
	mustOK := func(label string, r map[string]any) map[string]any {
		res, ok := r["result"].(map[string]any)
		if !ok || res["ok"] != true {
			t.Fatalf("%s: %v", label, r)
		}
		return res
	}

	// === First bridge: navigate to "/" (sets cookie), save_session ===
	port1, killChrome1 := mkChrome(t)
	cmd1, s1, w1, killBridge1 := mkBridge(t, port1)
	defer killChrome1()
	defer killBridge1()
	_ = cmd1

	send(s1, w1, 1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port1)})
	r := send(s1, w1, 2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid1 := mustOK("new_tab", r)["target_id"].(string)
	time.Sleep(300 * time.Millisecond)
	send(s1, w1, 3, "browser_navigate", map[string]any{
		"target_id": tid1, "url": srv.URL + "/", "wait_until": "load",
	})
	r = send(s1, w1, 4, "task_save_session", map[string]any{"name": "test1"})
	mustOK("save_session", r)
	killBridge1()
	killChrome1()

	// === Second bridge: fresh chrome, load_session, verify cookie ===
	port2, killChrome2 := mkChrome(t)
	cmd2, s2, w2, killBridge2 := mkBridge(t, port2)
	defer killChrome2()
	defer killBridge2()
	_ = cmd2

	send(s2, w2, 1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port2)})
	r = send(s2, w2, 2, "task_load_session", map[string]any{"name": "test1"})
	mustOK("load_session", r)
	r = send(s2, w2, 3, "browser_new_tab", map[string]any{"url": srv.URL + "/check"})
	tid2 := mustOK("new_tab2", r)["target_id"].(string)
	time.Sleep(500 * time.Millisecond)
	send(s2, w2, 4, "browser_navigate", map[string]any{
		"target_id": tid2, "url": srv.URL + "/check", "wait_until": "load",
	})
	r = send(s2, w2, 5, "browser_snapshot", map[string]any{"target_id": tid2, "mode": "dom_text"})
	res := mustOK("snapshot", r)
	snap := res["snapshot"].([]any)
	body := snap[0].(map[string]any)
	val, _ := body["value"].(string)
	if !strings.Contains(val, "ABCDEF") {
		t.Fatalf("cookie not preserved: %q", val)
	}
}
