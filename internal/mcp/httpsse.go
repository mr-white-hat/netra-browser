package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// HTTPOpts configures the HTTP-SSE handler.
type HTTPOpts struct {
	Token          string   // required if non-empty
	AllowedOrigins []string // CORS allowlist; default: deny non-empty origins

	// Session enables the /events SSE stream. Nil keeps the legacy 204 stub.
	Session *Session
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
		if opts.Session == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// SSE clients (EventSource) can't set headers — accept ?token= as a
		// fallback. POSTs to /rpc still require the bearer header.
		if !checkAuthFlexible(r, opts.Token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !checkOrigin(r, opts.AllowedOrigins) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		serveSSE(w, r, opts.Session)
	})
	return mux
}

func checkAuthFlexible(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") == token {
		return true
	}
	return r.URL.Query().Get("token") == token
}

// sseEventTypeMap is the friendly-name → CDP-method mapping the /events
// endpoint accepts in its `types` query param. Mirrors the names used by
// browser_get_recent_events / browser_wait_for in the tools layer.
var sseEventTypeMap = map[string]string{
	"navigation":       "Page.frameNavigated",
	"network_request":  "Network.requestWillBeSent",
	"network_response": "Network.responseReceived",
	"console":          "Runtime.consoleAPICalled",
	"dialog":           "Page.javascriptDialogOpening",
	"load":             "Page.loadEventFired",
	"domcontentloaded": "Page.domContentEventFired",
}

func serveSSE(w http.ResponseWriter, r *http.Request, sess *Session) {
	tid := r.URL.Query().Get("target_id")
	if tid == "" {
		http.Error(w, "target_id required", http.StatusBadRequest)
		return
	}
	typesParam := r.URL.Query().Get("types")
	var methods []string
	if typesParam == "" {
		// Default: every supported type.
		for _, m := range sseEventTypeMap {
			methods = append(methods, m)
		}
	} else {
		for _, t := range strings.Split(typesParam, ",") {
			if m, ok := sseEventTypeMap[strings.TrimSpace(t)]; ok {
				methods = append(methods, m)
			}
		}
		if len(methods) == 0 {
			http.Error(w, "no recognized types", http.StatusBadRequest)
			return
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	page, err := sess.Page(r.Context(), tid)
	if err != nil {
		http.Error(w, "target unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering on the off-chance.
	w.WriteHeader(http.StatusOK)

	// Merge per-method subscriptions onto one channel so the writer loop is simple.
	merged := make(chan cdp.BufferedEvent, 64)
	var cleanups []func()
	var wg sync.WaitGroup
	for _, m := range methods {
		ch, cleanup := page.SubscribeMethod(m)
		cleanups = append(cleanups, cleanup)
		wg.Add(1)
		go func(ch <-chan cdp.BufferedEvent) {
			defer wg.Done()
			for {
				select {
				case e, ok := <-ch:
					if !ok {
						return
					}
					select {
					case merged <- e:
					default:
						// Slow consumer — drop. v0 doesn't track drops; future:
						// emit an SSE event with kind "dropped".
					}
				case <-r.Context().Done():
					return
				}
			}
		}(ch)
	}

	defer func() {
		for _, c := range cleanups {
			c()
		}
		wg.Wait()
		close(merged)
	}()

	// Initial hello so clients know the stream is live before any events arrive.
	fmt.Fprintf(w, "event: ready\ndata: {\"target_id\":%q}\n\n", tid)
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// Comment line — keepalive without polluting the event stream.
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e := <-merged:
			payload := map[string]any{
				"target_id": tid,
				"at_ms":     e.At.UnixMilli(),
				"params":    json.RawMessage(e.Params),
			}
			b, _ := json.Marshal(payload)
			eventName := reverseSSEEventName(e.Method)
			_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, b)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// reverseSSEEventName maps the CDP method back to the friendly type used in the
// SSE event: line. Falls back to the raw CDP name if unmapped.
func reverseSSEEventName(method string) string {
	for friendly, m := range sseEventTypeMap {
		if m == method {
			return friendly
		}
	}
	return method
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
