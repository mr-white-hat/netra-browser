package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// CaptureTrace starts a Chrome trace recording, holds it for `duration`, then
// drains the resulting event stream to a temp file and returns the path.
// Categories defaults to a sane Web performance set when nil.
func (p *Page) CaptureTrace(ctx context.Context, duration time.Duration, categories []string) (string, error) {
	if duration <= 0 {
		duration = 5 * time.Second
	}
	if len(categories) == 0 {
		categories = []string{
			"devtools.timeline",
			"loading",
			"v8.execute",
			"disabled-by-default-v8.cpu_profiler",
			"disabled-by-default-devtools.timeline",
		}
	}

	// Subscribe BEFORE start so we don't miss the dataCollected stream.
	dataCh := p.addEventSub("Tracing.dataCollected")
	defer p.removeEventSub(dataCh)
	doneCh := p.addEventSub("Tracing.tracingComplete")
	defer p.removeEventSub(doneCh)

	if _, err := p.cdp.Send(ctx, "Tracing.start", map[string]any{
		"categories":                   joinCategories(categories),
		"transferMode":                 "ReportEvents",
		"options":                      "sampling-frequency=10000",
		"bufferUsageReportingInterval": 1000,
	}); err != nil {
		return "", fmt.Errorf("Tracing.start: %w", err)
	}

	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	select {
	case <-deadline.C:
	case <-ctx.Done():
		_, _ = p.cdp.Send(context.Background(), "Tracing.end", nil)
		return "", ctx.Err()
	}

	if _, err := p.cdp.Send(ctx, "Tracing.end", nil); err != nil {
		return "", fmt.Errorf("Tracing.end: %w", err)
	}

	// Collect events until tracingComplete fires (or a safety timeout).
	var events []json.RawMessage
	complete := time.NewTimer(10 * time.Second)
	defer complete.Stop()
collect:
	for {
		select {
		case e := <-dataCh:
			var v struct {
				Value []json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal(e.Params, &v); err == nil {
				events = append(events, v.Value...)
			}
		case <-doneCh:
			break collect
		case <-complete.C:
			break collect
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	f, err := os.CreateTemp("", "netra-trace-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(map[string]any{
		"traceEvents": events,
		"metadata": map[string]any{
			"capturedAt": time.Now().UTC().Format(time.RFC3339),
			"categories": categories,
		},
	}); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func joinCategories(c []string) string {
	out := ""
	for i, s := range c {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
