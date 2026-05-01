package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}
type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type harEntry struct {
	StartedDateTime string         `json:"startedDateTime"`
	Time            float64        `json:"time"`
	Request         harRequest     `json:"request"`
	Response        harResponse    `json:"response"`
	Cache           map[string]any `json:"cache"`
	Timings         map[string]any `json:"timings"`
}
type harRequest struct {
	Method      string              `json:"method"`
	URL         string              `json:"url"`
	HTTPVersion string              `json:"httpVersion"`
	Headers     []map[string]string `json:"headers"`
	QueryString []map[string]string `json:"queryString"`
	Cookies     []map[string]any    `json:"cookies"`
	HeadersSize int                 `json:"headersSize"`
	BodySize    int                 `json:"bodySize"`
}
type harResponse struct {
	Status      int                 `json:"status"`
	StatusText  string              `json:"statusText"`
	HTTPVersion string              `json:"httpVersion"`
	Headers     []map[string]string `json:"headers"`
	Cookies     []map[string]any    `json:"cookies"`
	Content     map[string]any      `json:"content"`
	HeadersSize int                 `json:"headersSize"`
	BodySize    int                 `json:"bodySize"`
}

// CaptureHAR collects network events for `duration` and writes a HAR file. Returns the path.
func (p *Page) CaptureHAR(ctx context.Context, duration time.Duration) (string, error) {
	if duration <= 0 {
		duration = 5 * time.Second
	}
	willBeSent := p.addEventSub("Network.requestWillBeSent")
	defer p.removeEventSub(willBeSent)
	responseReceived := p.addEventSub("Network.responseReceived")
	defer p.removeEventSub(responseReceived)

	type entry struct {
		req json.RawMessage
		res json.RawMessage
		t0  time.Time
		t1  time.Time
	}
	var mu sync.Mutex
	byID := map[string]*entry{}

	deadline := time.NewTimer(duration)
	defer deadline.Stop()

loop:
	for {
		select {
		case e := <-willBeSent:
			var v struct {
				RequestID string          `json:"requestId"`
				Request   json.RawMessage `json:"request"`
			}
			if err := json.Unmarshal(e.Params, &v); err != nil {
				continue
			}
			mu.Lock()
			byID[v.RequestID] = &entry{req: v.Request, t0: e.At}
			mu.Unlock()
		case e := <-responseReceived:
			var v struct {
				RequestID string          `json:"requestId"`
				Response  json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal(e.Params, &v); err != nil {
				continue
			}
			mu.Lock()
			if ent, ok := byID[v.RequestID]; ok {
				ent.res = v.Response
				ent.t1 = e.At
			}
			mu.Unlock()
		case <-deadline.C:
			break loop
		case <-ctx.Done():
			break loop
		}
	}

	mu.Lock()
	defer mu.Unlock()
	har := harLog{
		Version: "1.2",
		Creator: harCreator{Name: "netra-browser", Version: "0.0.1-dev"},
		Entries: make([]harEntry, 0, len(byID)),
	}
	for _, ent := range byID {
		he := harEntry{
			StartedDateTime: ent.t0.UTC().Format(time.RFC3339Nano),
			Time:            float64(ent.t1.Sub(ent.t0).Milliseconds()),
			Cache:           map[string]any{},
			Timings:         map[string]any{"send": 0, "wait": 0, "receive": 0},
		}
		decodeRequest(ent.req, &he.Request)
		decodeResponse(ent.res, &he.Response)
		har.Entries = append(har.Entries, he)
	}

	tmp, err := os.CreateTemp("", "netra-har-*.json")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"log": har}); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func decodeRequest(raw json.RawMessage, dst *harRequest) {
	if len(raw) == 0 {
		return
	}
	var v struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	_ = json.Unmarshal(raw, &v)
	dst.Method = v.Method
	dst.URL = v.URL
	dst.HTTPVersion = "HTTP/1.1"
	dst.Headers = []map[string]string{}
	for k, val := range v.Headers {
		dst.Headers = append(dst.Headers, map[string]string{"name": k, "value": val})
	}
	dst.HeadersSize = -1
	dst.BodySize = -1
	dst.Cookies = []map[string]any{}
	dst.QueryString = []map[string]string{}
}

func decodeResponse(raw json.RawMessage, dst *harResponse) {
	if len(raw) == 0 {
		return
	}
	var v struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
	}
	_ = json.Unmarshal(raw, &v)
	dst.Status = v.Status
	dst.StatusText = fmt.Sprintf("%d", v.Status)
	dst.HTTPVersion = "HTTP/1.1"
	dst.Headers = []map[string]string{}
	for k, val := range v.Headers {
		dst.Headers = append(dst.Headers, map[string]string{"name": k, "value": val})
	}
	dst.Content = map[string]any{"size": 0, "mimeType": ""}
	dst.HeadersSize = -1
	dst.BodySize = -1
	dst.Cookies = []map[string]any{}
}
