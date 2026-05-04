//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestE2E_ProjectIsolation runs two bridges against ONE Chrome with different
// --project names and verifies their browser_list_tabs views don't bleed.
// Bridge A's tabs should be invisible to bridge B's default list and vice versa.
func TestE2E_ProjectIsolation(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()
	projectsDir := t.TempDir()

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

	startBridge := func(project, lockName string) (*exec.Cmd, io.WriteCloser, *bufio.Scanner) {
		bin := exec.Command(binPath,
			"--lock", userDir+"/"+lockName,
			"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
			"--project", project,
			"--projects-dir", projectsDir,
		)
		stdin, _ := bin.StdinPipe()
		stdout, _ := bin.StdoutPipe()
		bin.Stderr = os.Stderr
		startInGroup(t, bin)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		return bin, stdin, sc
	}

	rpc := func(t *testing.T, stdin io.Writer, sc *bufio.Scanner, id int, method string, params any) map[string]any {
		t.Helper()
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

	// Bridge A: project "alpha"
	binA, stdinA, scA := startBridge("alpha", "a.lock")
	defer killGroup(binA)
	resp := rpc(t, stdinA, scA, 1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if r, _ := resp["result"].(map[string]any); r == nil || r["ok"] != true {
		t.Fatalf("attach A: %v", resp)
	}
	resp = rpc(t, stdinA, scA, 2, "browser_new_tab", map[string]any{"url": "about:blank"})
	tabA := resp["result"].(map[string]any)["target_id"].(string)

	// Bridge B: project "beta"
	binB, stdinB, scB := startBridge("beta", "b.lock")
	defer killGroup(binB)
	resp = rpc(t, stdinB, scB, 1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if r, _ := resp["result"].(map[string]any); r == nil || r["ok"] != true {
		t.Fatalf("attach B: %v", resp)
	}
	resp = rpc(t, stdinB, scB, 2, "browser_new_tab", map[string]any{"url": "about:blank"})
	tabB := resp["result"].(map[string]any)["target_id"].(string)

	// Bridge A's filtered list_tabs should NOT include tabB.
	resp = rpc(t, stdinA, scA, 3, "browser_list_tabs", nil)
	tabsA := resp["result"].(map[string]any)["tabs"].([]any)
	for _, ti := range tabsA {
		row := ti.(map[string]any)
		if row["target_id"] == tabB {
			t.Fatalf("bridge A leaked tab from project beta: %v", tabsA)
		}
	}
	if !containsTab(tabsA, tabA) {
		t.Fatalf("bridge A missing its own tab %s in %v", tabA, tabsA)
	}

	// Bridge B's filtered list_tabs should NOT include tabA.
	resp = rpc(t, stdinB, scB, 3, "browser_list_tabs", nil)
	tabsB := resp["result"].(map[string]any)["tabs"].([]any)
	for _, ti := range tabsB {
		row := ti.(map[string]any)
		if row["target_id"] == tabA {
			t.Fatalf("bridge B leaked tab from project alpha: %v", tabsB)
		}
	}
	if !containsTab(tabsB, tabB) {
		t.Fatalf("bridge B missing its own tab %s in %v", tabB, tabsB)
	}

	// include_all=true: bridge A should now see both.
	resp = rpc(t, stdinA, scA, 4, "browser_list_tabs", map[string]any{"include_all": true})
	tabsAll := resp["result"].(map[string]any)["tabs"].([]any)
	if !containsTab(tabsAll, tabA) || !containsTab(tabsAll, tabB) {
		t.Fatalf("include_all=true did not surface both tabs: %v", tabsAll)
	}

	// list_projects on bridge A: alpha is_self=true, beta visible.
	resp = rpc(t, stdinA, scA, 5, "browser_list_projects", nil)
	projects := resp["result"].(map[string]any)["projects"].([]any)
	gotAlpha, gotBeta := false, false
	for _, p := range projects {
		row := p.(map[string]any)
		if row["name"] == "alpha" {
			gotAlpha = true
			if row["is_self"] != true {
				t.Fatalf("alpha not is_self on bridge A: %v", row)
			}
		}
		if row["name"] == "beta" {
			gotBeta = true
		}
	}
	if !gotAlpha || !gotBeta {
		t.Fatalf("missing projects in list: %v", projects)
	}
}

func containsTab(tabs []any, id string) bool {
	for _, t := range tabs {
		row, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if row["target_id"] == id {
			return true
		}
	}
	return false
}
