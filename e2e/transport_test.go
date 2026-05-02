//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_HTTPTransport(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	chromeCmd := exec.Command(chrome, "--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir, "about:blank")
	chromeCmd.Stderr = os.Stderr
	startInGroup(t, chromeCmd)
	defer killGroup(chromeCmd)
	waitForChrome(t, port, 10*time.Second)

	httpPort := freePort(t)
	bin := exec.Command(binPath,
		"--lock", filepath.Join(userDir, "active.lock"),
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
		"--listen", fmt.Sprintf("127.0.0.1:%d", httpPort),
	)
	// Drain stderr through a pipe so the bridge subprocess's children don't
	// keep an inherited fd open after we kill the group.
	stderrPipe, _ := bin.StderrPipe()
	go func() {
		if stderrPipe == nil {
			return
		}
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				_, _ = os.Stderr.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	startInGroup(t, bin)
	defer killGroup(bin)
	defer func() {
		if stderrPipe != nil {
			_ = stderrPipe.Close()
		}
	}()

	// Wait for bridge HTTP to come up.
	rpcURL := fmt.Sprintf("http://127.0.0.1:%d/rpc", httpPort)
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Post(rpcURL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":0,"method":"meta_health"}`))
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			lastErr = nil
			break
		}
		if err != nil {
			lastErr = err
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("bridge HTTP never came up: %v", lastErr)
	}

	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var r map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&r)
		return r
	}

	r := send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	res, _ := r["result"].(map[string]any)
	if res == nil || res["ok"] != true {
		t.Fatalf("attach: %v", r)
	}

	r = send(2, "browser_list_tabs", nil)
	res, _ = r["result"].(map[string]any)
	if res == nil || res["ok"] != true {
		t.Fatalf("list_tabs: %v", r)
	}
	tabs, _ := res["tabs"].([]any)
	if len(tabs) == 0 {
		t.Fatalf("expected at least one tab, got: %v", res)
	}
}
