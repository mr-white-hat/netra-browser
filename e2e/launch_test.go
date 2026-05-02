//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestE2E_LaunchMode(t *testing.T) {
	if _, err := exec.LookPath("chromium"); err != nil {
		t.Skip("no chromium")
	}
	tmp := t.TempDir()
	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--launch", "--launch-headless",
		"--profile-dir", filepath.Join(tmp, "profile"),
		"--lock", filepath.Join(tmp, "active.lock"),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	stderr, _ := bin.StderrPipe()
	go io.Copy(os.Stderr, stderr)
	startInGroup(t, bin)
	defer killGroup(bin)

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

	// Give launch+attach time to complete (go run + chrome boot + auto-attach).
	// Poll meta_health until chrome_alive:true or 30s timeout.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r := send(1, "meta_health", nil)
		res, ok := r["result"].(map[string]any)
		if ok && res["chrome_alive"] == true {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("chrome never became alive within 30s")
}

// Ensure fmt is used (imported for potential future test helpers).
var _ = fmt.Sprintf
