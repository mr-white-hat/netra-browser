//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startTestPage hosts the given HTML at GET / on a free localhost port and
// returns the URL. The returned cleanup shuts the server down.
func startTestPage(t *testing.T, body string) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, body)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	url := "http://" + ln.Addr().String() + "/"
	return url, func() { _ = srv.Close() }
}

func newDropFilesBridge(t *testing.T) (rpcFn func(method string, params any) map[string]any, kill func()) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	chromeCmd := exec.Command(chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDir),
		"about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	startInGroup(t, chromeCmd)
	waitForChrome(t, port, 10*time.Second)

	bin := exec.Command(binPath,
		"--lock", userDir+"/active.lock",
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	startInGroup(t, bin)
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

	rpc("meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})

	return rpc, func() {
		killGroup(bin)
		killGroup(chromeCmd)
	}
}

// TestE2E_DropFiles_HiddenInput: drop zone with a hidden <input type=file>
// inside it. Tool must take the fast path.
func TestE2E_DropFiles_HiddenInput(t *testing.T) {
	tmp := t.TempDir()
	payload := filepath.Join(tmp, "payload.txt")
	if err := os.WriteFile(payload, []byte("HELLO_FAST_PATH"), 0o644); err != nil {
		t.Fatal(err)
	}
	pageURL, stop := startTestPage(t, `<!doctype html><html><body>
<style>#dz{width:300px;height:200px;border:2px dashed #888;padding:20px;}</style>
<div id=dz>
  drop here
  <input type=file id=fi style="position:absolute;left:-9999px">
</div>
<div id=marker></div>
<script>
document.getElementById('fi').addEventListener('change', e => {
  for (const f of e.target.files) {
    document.getElementById('marker').textContent = 'fast: ' + f.name + ' ' + f.size;
  }
});
</script>
</body></html>`)
	defer stop()

	rpc, killAll := newDropFilesBridge(t)
	defer killAll()

	resp := rpc("browser_new_tab", map[string]any{"url": pageURL})
	tid := resp["result"].(map[string]any)["target_id"].(string)
	time.Sleep(800 * time.Millisecond)

	// Pre-check: the input is actually in the DOM.
	resp = rpc("browser_eval", map[string]any{"target_id": tid, "expression": "document.querySelectorAll('#dz input[type=file]').length"})
	if got := fmt.Sprint(resp["result"].(map[string]any)["result"]); got != "1" {
		t.Fatalf("test fixture broken — expected 1 file input inside #dz, got %s", got)
	}

	resp = rpc("browser_drop_files", map[string]any{
		"target_id":  tid,
		"locator":    map[string]any{"css": "#dz"},
		"file_paths": []string{payload},
	})
	r := resp["result"].(map[string]any)
	if r["ok"] != true {
		t.Fatalf("drop_files failed: %v", resp)
	}
	if r["mode"] != "hidden_input" {
		t.Fatalf("expected hidden_input mode, got %v (full: %v)", r["mode"], r)
	}

	resp = rpc("browser_eval", map[string]any{"target_id": tid, "expression": "document.getElementById('marker').textContent"})
	got, _ := resp["result"].(map[string]any)["result"].(string)
	if !strings.Contains(got, "fast:") || !strings.Contains(got, "payload.txt") {
		t.Fatalf("change handler did not fire: %q", got)
	}
}

// TestE2E_DropFiles_SyntheticDrag: pure drop zone, no <input type=file>
// anywhere. Tool must use Input.dispatchDragEvent. verify arg confirms the
// drop actually landed.
func TestE2E_DropFiles_SyntheticDrag(t *testing.T) {
	tmp := t.TempDir()
	payload := filepath.Join(tmp, "payload.bin")
	if err := os.WriteFile(payload, []byte("SYNTHETIC_DRAG_PAYLOAD_BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	pageURL, stop := startTestPage(t, `<!doctype html><html><body>
<style>#dz{width:400px;height:240px;border:2px dashed #888;margin:50px;padding:20px;}</style>
<div id=dz>drop here</div>
<div id=marker></div>
<script>
const dz=document.getElementById('dz');
['dragenter','dragover','drop'].forEach(t=>dz.addEventListener(t,e=>{
  e.preventDefault();
  if (t==='drop' && e.dataTransfer) {
    for (const f of e.dataTransfer.files) {
      document.getElementById('marker').textContent = 'dropped: ' + f.name + ' ' + f.size;
    }
  }
}));
</script>
</body></html>`)
	defer stop()

	rpc, killAll := newDropFilesBridge(t)
	defer killAll()

	resp := rpc("browser_new_tab", map[string]any{"url": pageURL})
	tid := resp["result"].(map[string]any)["target_id"].(string)
	time.Sleep(800 * time.Millisecond)

	resp = rpc("browser_drop_files", map[string]any{
		"target_id":  tid,
		"locator":    map[string]any{"css": "#dz"},
		"file_paths": []string{payload},
		"verify":     map[string]any{"text": "dropped: payload.bin", "timeout_ms": 3000},
	})
	r := resp["result"].(map[string]any)
	if r["ok"] != true {
		t.Fatalf("drop_files failed: %v", resp)
	}
	if r["mode"] != "synthetic_drag" {
		t.Fatalf("expected synthetic_drag mode, got %v (full: %v)", r["mode"], r)
	}
	if r["verified"] != true {
		t.Fatalf("expected verified=true, got %v (full: %v)", r["verified"], r)
	}
}
