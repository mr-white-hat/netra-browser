package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// HTTPOpts configures the HTTP-SSE handler.
type HTTPOpts struct {
	Token          string   // required if non-empty
	AllowedOrigins []string // CORS allowlist; default: deny non-empty origins
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
