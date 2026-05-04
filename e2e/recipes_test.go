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

// TestE2E_RecipeRoundTrip records a tiny recipe (navigate to a data URL),
// replays it, and asserts the success marker is verified.
func TestE2E_RecipeRoundTrip(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()
	recipesHome := t.TempDir() // recipes will land under $HOME/.config/netra-browser/recipes

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

	// Run the bridge with HOME pointed at our temp so recipes don't pollute the user's config.
	bin := exec.Command(binPath,
		"--lock", userDir+"/active.lock",
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	bin.Env = append(os.Environ(), "HOME="+recipesHome)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	startInGroup(t, bin)
	defer killGroup(bin)

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	rpc := func(id int, method string, params any) map[string]any {
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

	// 1. attach.
	resp := rpc(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if r, _ := resp["result"].(map[string]any); r == nil || r["ok"] != true {
		t.Fatalf("attach: %v", resp)
	}

	// 2. open a target so navigate has somewhere to go.
	resp = rpc(2, "browser_new_tab", map[string]any{"url": "about:blank"})
	tid := resp["result"].(map[string]any)["target_id"].(string)

	// 3. record a small recipe.
	resp = rpc(3, "task_record_recipe", map[string]any{
		"name": "datauri-recipe",
		"actions": []map[string]any{
			{"tool": "browser_navigate", "args": map[string]any{
				"url": "data:text/html,<html><body><h1>RECIPE_OK</h1></body></html>",
			}},
		},
		"success_marker": map[string]any{"text": "RECIPE_OK"},
	})
	if r, _ := resp["result"].(map[string]any); r == nil || r["ok"] != true {
		t.Fatalf("record: %v", resp)
	}

	// 4. list_recipes — should contain the recipe.
	resp = rpc(4, "task_list_recipes", nil)
	recipes := resp["result"].(map[string]any)["recipes"].([]any)
	found := false
	for _, r := range recipes {
		row := r.(map[string]any)
		if row["name"] == "datauri-recipe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recipe not listed: %v", recipes)
	}

	// 5. replay — success_verified should be true.
	resp = rpc(5, "task_replay_recipe", map[string]any{"name": "datauri-recipe", "target_id": tid})
	r := resp["result"].(map[string]any)
	if r["ok"] != true {
		t.Fatalf("replay not ok: %v", resp)
	}
	if r["success_verified"] != true {
		t.Fatalf("expected success_verified=true: %v", resp)
	}
}
