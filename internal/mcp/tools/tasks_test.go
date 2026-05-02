package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestCaptureHARTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, err := reg.Invoke(context.Background(), "task_capture_har", json.RawMessage(`{"duration_ms":100}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"har_path":`) {
		t.Fatalf("missing har_path: %s", b)
	}
}

func TestRenderPDFTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Page.printToPDF": json.RawMessage(`{"data":"JVBERi0xLjQK"}`),
		},
	})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, err := reg.Invoke(context.Background(), "task_render_pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"pdf_path":`) {
		t.Fatalf("missing pdf_path: %s", b)
	}
}

func TestRunWithProxyReturnsNotImplemented(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, _ := reg.Invoke(context.Background(), "task_run_with_proxy",
		json.RawMessage(`{"proxy_url":"http://127.0.0.1:8080","tool_calls":[{"tool":"meta_health"}]}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"not_implemented_v1"`) {
		t.Fatalf("expected not_implemented_v1: %s", b)
	}
}

func TestRunWithProxyMissingArgs(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, _ := reg.Invoke(context.Background(), "task_run_with_proxy", json.RawMessage(`{}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected invalid_args: %s", b)
	}
}
