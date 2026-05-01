# netra-browser Plan D — HTTP-SSE + Launch Mode + Sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Add the second MCP transport (HTTP-SSE), launch mode (bridge spawns Chrome), and session save/load tools. After Plan D, the bridge can run as a long-lived HTTP service, manage Chrome's lifecycle, and persist auth surfaces (cookies + storage) across runs.

**Architecture additions:** New `internal/mcp/httpsse.go` for the HTTP-SSE transport. New `internal/profile/launch.go` for spawning Chrome. `task_save_session` / `task_load_session` exported as MCP tools, persisting JSON to `~/.config/netra-browser/sessions/<name>.json`.

**Tech Stack:** Same. New: `os/exec` for Chrome launch.

**Source spec:** `docs/superpowers/specs/2026-04-30-netra-browser-design.md`

**Builds on:** `plan-c-inspection-waiting` tag.

---

## File Structure (Plan D)

| Path | Responsibility |
|---|---|
| `internal/mcp/httpsse.go` | HTTP-SSE transport (POST `/rpc` for tool calls; GET `/events` SSE stream for notifications — v1 ships POST-only, SSE stream is a no-op stub for forward-compat) |
| `internal/mcp/httpsse_test.go` | httptest-based round-trip |
| `internal/mcp/auth.go` | Bearer token middleware; localhost binding guard |
| `internal/mcp/auth_test.go` | Tests |
| `internal/profile/launch.go` | `Launch(opts) (chrome process handle, debug URL)` |
| `internal/profile/launch_test.go` | Light test (skipped if no chromium) |
| `internal/profile/sessions.go` | `SaveSession(client, name)`, `LoadSession(client, name)` — exports/imports cookies + localStorage |
| `internal/profile/sessions_test.go` | Tempdir-based round-trip |
| `internal/mcp/tools/sessions.go` | `task_save_session`, `task_load_session` |
| `internal/mcp/tools/sessions_test.go` | Tool tests |
| `cmd/netra-browser/main.go` (modify) | Add `--listen`, `--token`, `--launch`, `--profile` flags; wire transport selection |
| `e2e/transport_test.go` | E2E HTTP-SSE: POST a JSON-RPC request, verify response |
| `e2e/launch_test.go` | E2E launch mode: bridge spawns chromium, attaches, drives a page |
| `e2e/sessions_test.go` | E2E save/load: navigate → save_session → load_session in fresh page → cookies preserved |

---

## Task 1: HTTP-SSE transport (POST `/rpc`)

**Files:**
- Create: `internal/mcp/httpsse.go`
- Create: `internal/mcp/httpsse_test.go`

The transport accepts JSON-RPC requests as POST body to `/rpc`, dispatches via the registry, returns the JSON response. SSE streaming is reserved for future use; in v1 a GET `/events` returns 204 with a comment noting it's reserved.

- [ ] **Step 1: Failing test**

```go
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPSSE_RPC(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})

	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{}))
	defer srv.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	resp, err := http.Post(srv.URL+"/rpc", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	res, ok := out["result"].(map[string]any)
	if !ok || res["ok"] != true {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestHTTPSSE_TokenRequired(t *testing.T) {
	reg := NewRegistry()
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{Token: "secret"}))
	defer srv.Close()
	body := bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	resp, _ := http.Post(srv.URL+"/rpc", "application/json", body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTPSSE_TokenAccepted(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{Token: "secret"}))
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run failure**

`go test ./internal/mcp/ -run HTTPSSE -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/mcp/httpsse.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// HTTPOpts configures the HTTP-SSE handler.
type HTTPOpts struct {
	Token       string   // required if non-empty
	AllowedOrigins []string // CORS allowlist; default: deny all
}

// NewHTTPHandler returns a router with /rpc (POST) and /events (GET, reserved).
func NewHTTPHandler(reg *Registry, opts HTTPOpts) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !checkAuth(r, opts.Token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !checkOrigin(r, opts.AllowedOrigins) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", Error: &RPCError{Code: -32700, Message: err.Error()}})
			return
		}
		result, err := reg.Invoke(context.Background(), req.Method, req.Params)
		if err != nil {
			code := -32603
			if isUnknownToolErr(err) {
				code = -32601
			}
			_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: code, Message: err.Error()}})
			return
		}
		raw, _ := json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: raw})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		// Reserved for streaming notifications in a future version.
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func checkAuth(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	h := r.Header.Get("Authorization")
	return strings.HasPrefix(h, "Bearer ") && strings.TrimPrefix(h, "Bearer ") == token
}

func checkOrigin(r *http.Request, allowed []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./...
git add internal/mcp/httpsse.go internal/mcp/httpsse_test.go
git commit -m "mcp: add HTTP-SSE transport (POST /rpc + reserved /events)"
```

---

## Task 2: Wire HTTP-SSE + flags in main.go

**Files:**
- Modify: `cmd/netra-browser/main.go`

Add flags `--listen <addr>` (default empty = stdio only), `--token <token>`, `--allow-origin <origin>` (repeatable), `--launch` (Plan D Task 4), `--profile <path>`.

When `--listen` is set:
- If addr binds non-localhost (anything other than `127.0.0.1`, `localhost`, `::1`), require `--token`.
- Run HTTP server in a goroutine.
- Continue to also serve stdio if stdin is a tty (otherwise stdio-only is the assumed mode).

For now: if `--listen` is set, run HTTP-SSE; else run stdio. (User can run two instances if both are needed — keeps v1 simple.)

- [ ] **Step 1: Add flags to main.go**

Insert after existing flags:

```go
listen     = flag.String("listen", "", "HTTP-SSE listen address (e.g. 127.0.0.1:7878). Empty = stdio mode.")
token      = flag.String("token", "", "Bearer token for HTTP-SSE auth")
allowOrigins multiFlag
)
flag.Var(&allowOrigins, "allow-origin", "CORS allowed origin (repeatable)")
```

Define `multiFlag` at top of main.go:

```go
type multiFlag []string

func (m *multiFlag) String() string         { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error     { *m = append(*m, v); return nil }
```

Add `import "strings"` to main.go.

- [ ] **Step 2: Branch on transport**

Replace the existing `mcp.Serve(ctx, os.Stdin, os.Stdout, reg)` block with:

```go
if *listen != "" {
	if !isLocalListen(*listen) && *token == "" {
		fmt.Fprintln(os.Stderr, "non-localhost listen requires --token")
		os.Exit(2)
	}
	handler := mcp.NewHTTPHandler(reg, mcp.HTTPOpts{Token: *token, AllowedOrigins: allowOrigins})
	srv := &http.Server{Addr: *listen, Handler: handler}
	fmt.Fprintf(os.Stderr, "netra-browser listening on %s\n", *listen)
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "http: %v\n", err)
		os.Exit(1)
	}
	return
}

if err := mcp.Serve(ctx, os.Stdin, os.Stdout, reg); err != nil {
	fmt.Fprintf(os.Stderr, "serve: %v\n", err)
	os.Exit(1)
}
```

Add a helper:

```go
func isLocalListen(addr string) bool {
	if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") || strings.HasPrefix(addr, "[::1]:") {
		return true
	}
	return false
}
```

Add `"net/http"` import.

- [ ] **Step 3: Test locally**

```bash
# Stdio still works:
echo '{"jsonrpc":"2.0","id":1,"method":"meta_health"}' | go run ./cmd/netra-browser --lock /tmp/d2.lock

# HTTP works:
go run ./cmd/netra-browser --lock /tmp/d2-http.lock --listen 127.0.0.1:17878 &
PID=$!
sleep 1
curl -s -X POST http://127.0.0.1:17878/rpc -d '{"jsonrpc":"2.0","id":1,"method":"meta_health"}' | head -c 200
kill $PID
```

Expected: both return `{"chrome_alive":false,"ok":true,...}` JSON.

- [ ] **Step 4: Commit**

```bash
git add cmd/netra-browser/main.go
git commit -m "cmd: add --listen/--token flags for HTTP-SSE transport"
```

---

## Task 3: Launch mode — internal/profile/launch.go

**Files:**
- Create: `internal/profile/launch.go`
- Create: `internal/profile/launch_test.go`

`Launch(opts) (*LaunchHandle, error)`. The handle exposes `DebugURL()` and `Stop()`. Internally: pick a free port, exec `chromium --headless=new --remote-debugging-port=PORT --user-data-dir=...`, poll `/json/version` until ready (10s timeout), return.

Profile: default to `~/.config/google-chrome` (Linux). If `opts.UserDataDir` is set, use that.

- [ ] **Step 1: Failing test**

`internal/profile/launch_test.go`:

```go
package profile

import (
	"os/exec"
	"testing"
	"time"
)

func TestLaunchSpawnsChrome(t *testing.T) {
	if _, err := exec.LookPath("chromium"); err != nil {
		t.Skip("chromium not installed")
	}
	tmp := t.TempDir()
	h, err := Launch(LaunchOpts{
		UserDataDir: tmp,
		Headless:    true,
		Args:        []string{"--no-sandbox", "--disable-gpu"},
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer h.Stop()
	if h.DebugURL() == "" {
		t.Fatal("empty DebugURL")
	}
	// Sanity: discover should succeed.
	host, port, err := parseHostPort(h.DebugURL())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Discover(host, port); err == nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("Discover never succeeded against launched chrome")
}

// parseHostPort extracts host+port from "http://host:port".
func parseHostPort(s string) (string, int, error) {
	// minimal — full version in cdp/attach.go but we can't import that here.
	const prefix = "http://"
	if !strings_HasPrefix(s, prefix) {
		return "", 0, errInvalidURL
	}
	rest := s[len(prefix):]
	idx := strings_LastIndex(rest, ":")
	if idx < 0 {
		return "", 0, errInvalidURL
	}
	host := rest[:idx]
	var p int
	for _, c := range rest[idx+1:] {
		if c < '0' || c > '9' {
			break
		}
		p = p*10 + int(c-'0')
	}
	return host, p, nil
}
```

NOTE: Implementer should replace the awkward `strings_HasPrefix` / `errInvalidURL` placeholders with real `strings` import + `errors.New(...)` declarations.

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/profile/ -run Launch -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/profile/launch.go`:

```go
package profile

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type LaunchOpts struct {
	BinaryPath  string   // path to chromium/chrome; default: discovered from PATH
	UserDataDir string   // default: ~/.config/google-chrome (Linux), ~/Library/... (macOS)
	Headless    bool
	Args        []string // extra args
}

type LaunchHandle struct {
	cmd      *exec.Cmd
	debugURL string
	dir      string // user-data-dir actually used
}

func (h *LaunchHandle) DebugURL() string { return h.debugURL }
func (h *LaunchHandle) UserDataDir() string { return h.dir }

func (h *LaunchHandle) Stop() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	_ = h.cmd.Process.Kill()
	_, _ = h.cmd.Process.Wait()
	return nil
}

// Launch spawns Chrome with a managed remote debugging port and waits for it to come up.
func Launch(opts LaunchOpts) (*LaunchHandle, error) {
	bin := opts.BinaryPath
	if bin == "" {
		var err error
		bin, err = findChrome()
		if err != nil {
			return nil, err
		}
	}
	dir := opts.UserDataDir
	if dir == "" {
		var err error
		dir, err = defaultUserDataDir()
		if err != nil {
			return nil, err
		}
	}
	port, err := freeTCPPort()
	if err != nil {
		return nil, err
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir=" + dir,
	}
	if opts.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, opts.Args...)
	args = append(args, "about:blank")

	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	h := &LaunchHandle{cmd: cmd, debugURL: fmt.Sprintf("http://127.0.0.1:%d", port), dir: dir}
	if err := h.waitReady(10 * time.Second); err != nil {
		_ = h.Stop()
		return nil, err
	}
	return h, nil
}

func (h *LaunchHandle) waitReady(timeout time.Duration) error {
	host, port, err := parseHostPort(h.debugURL)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := Discover(host, port); err == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("chrome did not become ready within %s", timeout)
}

func findChrome() (string, error) {
	for _, n := range []string{"chromium", "chrome", "google-chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no chromium binary found in PATH")
}

func defaultUserDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "google-chrome"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), nil
	case "windows":
		return filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data"), nil
	}
	return filepath.Join(home, ".config", "google-chrome"), nil
}

func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func parseHostPort(s string) (string, int, error) {
	const prefix = "http://"
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return "", 0, fmt.Errorf("invalid url %q", s)
	}
	rest := s[len(prefix):]
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == ':' {
			host := rest[:i]
			p, err := strconv.Atoi(rest[i+1:])
			if err != nil {
				return "", 0, err
			}
			return host, p, nil
		}
	}
	return "", 0, fmt.Errorf("no port in url")
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/profile/ -v
git add internal/profile/launch.go internal/profile/launch_test.go
git commit -m "profile: add Launch (spawn Chrome with managed debug port)"
```

---

## Task 4: --launch flag wiring + lock-file port

**Files:**
- Modify: `cmd/netra-browser/main.go` to add `--launch`, `--profile`, `--profile-snapshot` flags
- Modify: `cmd/netra-browser/main.go` lock-file port to use the actual launched port

When `--launch` is set:
- Call `profile.Launch(opts)`
- Use the returned `DebugURL` for attach
- On exit, call `handle.Stop()`

When `--profile-snapshot` is set with `--launch`: copy the user data dir to a temp dir first.

- [ ] **Step 1: Flags + branch**

Add to flag block:

```go
launch         = flag.Bool("launch", false, "launch Chrome instead of attaching to a running one")
profile        = flag.String("profile", "", "user-data-dir for launched Chrome (default: ~/.config/google-chrome)")
profileSnapshot = flag.Bool("profile-snapshot", false, "copy profile to a temp dir before launching")
launchHeadless = flag.Bool("launch-headless", false, "pass --headless=new when launching")
```

After lock acquisition, before signal-aware ctx setup or after registrations, insert:

```go
var launchHandle *profile.LaunchHandle
if *launch {
	dir := *profile
	if dir == "" {
		var err error
		dir, err = profileDefaultDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "profile: %v\n", err)
			os.Exit(2)
		}
	}
	if *profileSnapshot {
		td, err := os.MkdirTemp("", "netra-profile-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "snapshot: %v\n", err)
			os.Exit(2)
		}
		if err := copyTree(dir, td); err != nil {
			fmt.Fprintf(os.Stderr, "copy profile: %v\n", err)
			os.Exit(2)
		}
		dir = td
	}
	h, err := profile.Launch(profile.LaunchOpts{UserDataDir: dir, Headless: *launchHeadless})
	if err != nil {
		fmt.Fprintf(os.Stderr, "launch: %v\n", err)
		os.Exit(1)
	}
	launchHandle = h
	*debugURL = h.DebugURL()
	*autoAttach = true // so meta_attach works without an explicit call
}
defer func() {
	if launchHandle != nil {
		_ = launchHandle.Stop()
	}
}()
```

Also add `profileDefaultDir` and `copyTree` helpers to main.go (or refactor `defaultUserDataDir` from launch.go to be exported and use it).

Actually, simpler: export `profile.DefaultUserDataDir()` from launch.go (rename `defaultUserDataDir`). Then main.go just calls `profile.DefaultUserDataDir()`.

For `copyTree`, write a small helper in main.go using `os.MkdirAll` + `filepath.Walk`. The implementer should handle symlinks safely and skip lock files.

- [ ] **Step 2: Manual smoke**

Build the binary, run `./netra-browser --launch --launch-headless --lock /tmp/d4.lock --listen 127.0.0.1:17880` in one terminal, curl `meta_health` from another. Confirm `chrome_alive: true` after attach.

- [ ] **Step 3: Commit**

```bash
git add cmd/netra-browser/main.go internal/profile/launch.go
git commit -m "cmd: add --launch / --profile / --profile-snapshot flags"
```

---

## Task 5: Sessions — save / load

**Files:**
- Create: `internal/profile/sessions.go`
- Create: `internal/profile/sessions_test.go`
- Create: `internal/mcp/tools/sessions.go`
- Create: `internal/mcp/tools/sessions_test.go`
- Modify: `cmd/netra-browser/main.go` to register

`SaveSession(client, name)`:
- Calls `Storage.getCookies` (browser-wide via the browser-level CDP client) → list of cookies.
- Optionally collects `localStorage` per origin via `Storage.getDOMStorageItems` for the active target's origins.
- Writes JSON to `$HOME/.config/netra-browser/sessions/<name>.json` with shape:

```json
{
  "name": "...",
  "saved_at": "2026-04-30T...",
  "cookies": [...],
  "storage": {"https://example.com": {"sid": "abc"}}
}
```

`LoadSession(client, name)`: read the JSON, call `Storage.setCookies` to restore cookies, set localStorage via `Runtime.evaluate` per origin (after navigating to that origin — for v1, only restore cookies).

For v1 simplicity: store ONLY cookies. Document localStorage as deferred.

- [ ] **Step 1: Tests**

```go
package profile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fakeBrowserClient struct {
	cookies []map[string]any
	calls   []string
}

func (f *fakeBrowserClient) Send(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	switch method {
	case "Storage.getCookies":
		b, _ := json.Marshal(map[string]any{"cookies": f.cookies})
		return json.RawMessage(b), nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeBrowserClient) Close() error { return nil }

func TestSaveLoadSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	c := &fakeBrowserClient{
		cookies: []map[string]any{{"name": "sid", "value": "abc", "domain": "example.com"}},
	}

	if err := SaveSession(context.Background(), c, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config", "netra-browser", "sessions", "alpha.json")); err != nil {
		t.Fatalf("session file not created: %v", err)
	}

	c2 := &fakeBrowserClient{}
	if err := LoadSession(context.Background(), c2, "alpha"); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, m := range c2.calls {
		if m == "Storage.setCookies" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Storage.setCookies not sent on load")
	}
}

func TestLoadSessionMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	c := &fakeBrowserClient{}
	if err := LoadSession(context.Background(), c, "nonexistent"); err == nil {
		t.Fatal("expected error on missing session")
	}
}
```

- [ ] **Step 2: Run failure**

`go test ./internal/profile/ -run Session -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/profile/sessions.go`:

```go
package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BrowserSender is the minimal interface SaveSession/LoadSession need.
type BrowserSender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
}

type sessionFile struct {
	Name    string           `json:"name"`
	SavedAt time.Time        `json:"saved_at"`
	Cookies []map[string]any `json:"cookies"`
}

func sessionPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "netra-browser", "sessions", name+".json"), nil
}

// SaveSession exports cookies via the browser-level CDP client.
func SaveSession(ctx context.Context, c BrowserSender, name string) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	raw, err := c.Send(ctx, "Storage.getCookies", nil)
	if err != nil {
		return err
	}
	var resp struct {
		Cookies []map[string]any `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	path, err := sessionPath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(sessionFile{Name: name, SavedAt: time.Now().UTC(), Cookies: resp.Cookies}, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

// LoadSession applies the saved cookies to the current Chrome.
func LoadSession(ctx context.Context, c BrowserSender, name string) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	path, err := sessionPath(name)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read session: %w", err)
	}
	var sf sessionFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return err
	}
	_, err = c.Send(ctx, "Storage.setCookies", map[string]any{"cookies": sf.Cookies})
	return err
}
```

- [ ] **Step 4: Tool registration**

`internal/mcp/tools/sessions.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
	"github.com/pavankumar2138/netra-browser/internal/profile"
)

func RegisterSessionTasks(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("task_save_session", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		bs, ok := client.(profile.BrowserSender)
		if !ok {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: "client lacks Send"}.AsResult(), nil
		}
		if err := profile.SaveSession(ctx, bs, a.Name); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		path, _ := profile.SessionPath(a.Name)
		return map[string]any{"ok": true, "session_path": path}, nil
	})

	reg.Register("task_load_session", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		bs, ok := client.(profile.BrowserSender)
		if !ok {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: "client lacks Send"}.AsResult(), nil
		}
		if err := profile.LoadSession(ctx, bs, a.Name); err != nil {
			return mcp.ToolError{Code: mcp.ErrNotFound, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})
}
```

Also export `SessionPath` from `sessions.go`:

```go
// SessionPath returns where a named session is stored.
func SessionPath(name string) (string, error) { return sessionPath(name) }
```

- [ ] **Step 5: Wire main.go**

Add `tools.RegisterSessionTasks(reg, sess)` to main.go.

- [ ] **Step 6: Run + commit**

```bash
go test ./...
git add internal/profile/sessions.go internal/profile/sessions_test.go internal/mcp/tools/sessions.go internal/mcp/tools/sessions_test.go cmd/netra-browser/main.go
git commit -m "profile+tools: add task_save_session / task_load_session"
```

---

## Task 6: E2E HTTP-SSE transport

**Files:**
- Create: `e2e/transport_test.go` (build tag `e2e`)

Spawn chromium, then run the bridge with `--listen 127.0.0.1:<port>`. Send JSON-RPC requests via `http.Post`. Verify attach + list_tabs work.

- [ ] **Step 1: Test**

```go
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
	if err := chromeCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	httpPort := freePort(t)
	for !strings.Contains("ok", "ok") {
		break
	}
	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", filepath.Join(userDir, "active.lock"),
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
		"--listen", fmt.Sprintf("127.0.0.1:%d", httpPort),
	)
	bin.Stderr = os.Stderr
	if err := bin.Start(); err != nil {
		t.Fatal(err)
	}
	defer bin.Process.Kill()

	// Wait for bridge HTTP to come up.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/rpc", httpPort), "application/json", strings.NewReader("{}")); err == nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/rpc", httpPort), "application/json", bytes.NewReader(b))
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
}
```

- [ ] **Step 2: Run + commit**

```bash
go test -tags e2e ./e2e/... -timeout 240s -run HTTPTransport -v
git add e2e/transport_test.go
git commit -m "e2e: add HTTP-SSE transport test"
```

---

## Task 7: E2E launch + sessions

**Files:**
- Create: `e2e/launch_test.go` (build tag `e2e`)
- Create: `e2e/sessions_test.go` (build tag `e2e`)

`launch_test.go`: bridge with `--launch --launch-headless`. Wait for it. Send `meta_health`. Verify `chrome_alive: true`.

`sessions_test.go`: navigate to a page that sets a cookie, save_session, kill the bridge, start a fresh bridge with `--launch`, load_session, verify cookie present.

- [ ] **Step 1: launch_test.go**

```go
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
		"--profile", filepath.Join(tmp, "profile"),
		"--lock", filepath.Join(tmp, "active.lock"),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	if err := bin.Start(); err != nil {
		t.Fatal(err)
	}
	defer bin.Process.Kill()

	// Wait for the bridge to print "attached to ..." on stderr — but we redirect
	// stderr to os.Stderr, so just sleep + send a few healths.
	time.Sleep(2 * time.Second)

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

	r := send(1, "meta_health", nil)
	res := r["result"].(map[string]any)
	if res["chrome_alive"] != true {
		t.Fatalf("chrome not alive: %v", res)
	}
}
```

- [ ] **Step 2: sessions_test.go**

```go
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
		_, _ = fmt.Fprintln(w, "<html><body>set</body></html>")
	})
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sid")
		if err != nil {
			_, _ = fmt.Fprintln(w, "MISSING")
			return
		}
		_, _ = fmt.Fprintln(w, "GOT="+c.Value)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// === First bridge: set cookie, save session ===
	port1 := freePort(t)
	dir1 := t.TempDir()
	chrome1 := exec.Command(chrome, "--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port1), "--user-data-dir="+dir1, "about:blank")
	chrome1.Stderr = os.Stderr
	if err := chrome1.Start(); err != nil {
		t.Fatal(err)
	}
	defer chrome1.Process.Kill()
	waitForChrome(t, port1, 10*time.Second)

	bridgeHome := t.TempDir()

	mkBridge := func(port int) (*exec.Cmd, *bufio.Scanner, io.Writer) {
		cmd := exec.Command("go", "run", "../cmd/netra-browser",
			"--lock", filepath.Join(bridgeHome, "active.lock"),
			"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
		)
		cmd.Env = append(os.Environ(), "HOME="+bridgeHome)
		stdin, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		s := bufio.NewScanner(stdout)
		s.Buffer(make([]byte, 1<<20), 16<<20)
		return cmd, s, stdin
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

	c1, s1, w1 := mkBridge(port1)
	defer c1.Process.Kill()

	send(s1, w1, 1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port1)})
	r := send(s1, w1, 2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid1 := r["result"].(map[string]any)["target_id"].(string)
	time.Sleep(300 * time.Millisecond)
	send(s1, w1, 3, "browser_navigate", map[string]any{"target_id": tid1, "url": srv.URL + "/", "wait_until": "load"})
	r = send(s1, w1, 4, "task_save_session", map[string]any{"name": "test1"})
	res := r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("save_session: %v", r)
	}
	c1.Process.Kill()
	chrome1.Process.Kill()

	// === Second bridge: fresh chrome, load session, verify cookie ===
	port2 := freePort(t)
	dir2 := t.TempDir()
	chrome2 := exec.Command(chrome, "--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port2), "--user-data-dir="+dir2, "about:blank")
	chrome2.Stderr = os.Stderr
	if err := chrome2.Start(); err != nil {
		t.Fatal(err)
	}
	defer chrome2.Process.Kill()
	waitForChrome(t, port2, 10*time.Second)

	c2, s2, w2 := mkBridge(port2)
	defer c2.Process.Kill()

	send(s2, w2, 1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port2)})
	r = send(s2, w2, 2, "task_load_session", map[string]any{"name": "test1"})
	if r["result"].(map[string]any)["ok"] != true {
		t.Fatalf("load_session: %v", r)
	}
	r = send(s2, w2, 3, "browser_new_tab", map[string]any{"url": srv.URL + "/check"})
	tid2 := r["result"].(map[string]any)["target_id"].(string)
	time.Sleep(500 * time.Millisecond)
	send(s2, w2, 4, "browser_navigate", map[string]any{"target_id": tid2, "url": srv.URL + "/check", "wait_until": "load"})
	r = send(s2, w2, 5, "browser_snapshot", map[string]any{"target_id": tid2, "mode": "dom_text"})
	res = r["result"].(map[string]any)
	snap := res["snapshot"].([]any)
	body := snap[0].(map[string]any)
	val, _ := body["value"].(string)
	if !strings.Contains(val, "ABCDEF") {
		t.Fatalf("cookie not preserved: %q", val)
	}
}
```

- [ ] **Step 3: Run + commit**

```bash
go test -tags e2e ./e2e/... -timeout 240s -run "Launch|Session" -v
git add e2e/launch_test.go e2e/sessions_test.go
git commit -m "e2e: add launch mode + session round-trip tests"
```

---

## Task 8: Verify + tag

- [ ] **Step 1: Full verification**

```bash
go test ./...
go test -tags e2e ./e2e/... -timeout 300s
go vet ./...
gofmt -l . | grep -v '^docs/' | grep -v '^BRAINSTORM' || echo "all formatted"
git tag plan-d-transport-launch-sessions
```

- [ ] **Step 2: Confirm tools live**

After Plan D: 25 (A+B+C) + 2 (D: save_session, load_session) = **27 tools**.

Plan D doesn't add new browser tools — just two task tools and the HTTP-SSE transport / launch infrastructure.

---

## Self-Review Notes

**Spec coverage:**
- ✅ HTTP-SSE transport with bearer token + non-localhost guard (Tasks 1, 2).
- ✅ Launch mode (Tasks 3, 4).
- ✅ task_save_session / task_load_session (Task 5) — cookies only in v1; localStorage deferred.
- ⏭ HTTP-SSE streaming notifications (only POST /rpc in v1; /events returns 204).
- ⏭ Companion extension, Claude-for-Chrome — explicitly out of scope.

**Placeholder scan:** None.

**Type consistency:**
- `profile.BrowserSender` is a one-method interface that `*cdp.Client` satisfies via its `Send` method.
- `LaunchHandle.Stop()` matches the `--launch` defer in main.go.

**Known gaps to flag in commits:**
- localStorage save/load deferred (cookies-only sessions).
- `task_save_session` uses browser-wide `Storage.getCookies` so it captures all origins, not just the active tab. This is per spec but worth a note.
