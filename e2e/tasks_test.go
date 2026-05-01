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

func TestE2E_HighLevelTasks(t *testing.T) {
	chrome := findChrome(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<!doctype html><html><body><h1>Hello</h1><img src="/img.png"></body></html>`)
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	port := freePort(t)
	userDir := t.TempDir()
	chromeCmd := exec.Command(chrome, "--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir, "about:blank")
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", filepath.Join(userDir, "active.lock"),
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	stderrPipe, _ := bin.StderrPipe()
	go io.Copy(os.Stderr, stderrPipe)
	if err := bin.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { bin.Process.Kill(); bin.Process.Wait() }()

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
			t.Fatal("no resp")
		}
		var r map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &r)
		return r
	}
	mustOK := func(label string, r map[string]any) map[string]any {
		res, ok := r["result"].(map[string]any)
		if !ok || res["ok"] != true {
			t.Fatalf("%s: %v", label, r)
		}
		return res
	}

	send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	r := send(2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid := mustOK("new_tab", r)["target_id"].(string)
	time.Sleep(300 * time.Millisecond)
	send(3, "browser_navigate", map[string]any{"target_id": tid, "url": srv.URL + "/", "wait_until": "load"})

	// Capture HAR — re-navigate inside the capture window so we see fresh requests.
	r = send(4, "task_capture_har", map[string]any{"target_id": tid, "url": srv.URL + "/", "duration_ms": 1500})
	res := mustOK("capture_har", r)
	harPath, _ := res["har_path"].(string)
	defer os.Remove(harPath)
	b, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version":"1.2"`) && !strings.Contains(string(b), `"version": "1.2"`) {
		t.Fatalf("HAR doesn't look right: %s", b[:min(200, len(b))])
	}

	// Render PDF
	r = send(5, "task_render_pdf", map[string]any{"target_id": tid})
	res = mustOK("render_pdf", r)
	pdfPath, _ := res["pdf_path"].(string)
	defer os.Remove(pdfPath)
	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pdfBytes), "%PDF") {
		t.Fatalf("not a PDF: %q", pdfBytes[:min(20, len(pdfBytes))])
	}
}
