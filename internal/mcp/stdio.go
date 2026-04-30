package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Serve reads newline-delimited JSON-RPC requests from r, invokes the registry,
// and writes responses to w. Returns nil on clean EOF, error on protocol failure.
func Serve(ctx context.Context, r io.Reader, w io.Writer, reg *Registry) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024) // 16 MiB lines
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(Response{JSONRPC: "2.0", Error: &RPCError{Code: -32700, Message: "parse error: " + err.Error()}})
			continue
		}

		result, err := reg.Invoke(ctx, req.Method, req.Params)
		if err != nil {
			code := -32603 // internal
			if errors.Is(err, errUnknownTool) || isUnknownToolErr(err) {
				code = -32601
			}
			_ = enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: code, Message: err.Error()}})
			continue
		}

		raw, err := json.Marshal(result)
		if err != nil {
			_ = enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32603, Message: "marshal result: " + err.Error()}})
			continue
		}
		_ = enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: raw})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdio scan: %w", err)
	}
	return nil
}

var errUnknownTool = errors.New("unknown tool")

func isUnknownToolErr(err error) bool {
	return err != nil && len(err.Error()) >= 12 && err.Error()[:12] == "unknown tool"
}
