package browser

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// WaitNetworkIdle blocks until there are zero in-flight network requests for `quiet` duration.
func (p *Page) WaitNetworkIdle(ctx context.Context, quiet time.Duration) error {
	willBeSent := p.cdp.SubscribeOnTarget(p.sessionID, "Network.requestWillBeSent")
	finished := p.cdp.SubscribeOnTarget(p.sessionID, "Network.loadingFinished")
	failed := p.cdp.SubscribeOnTarget(p.sessionID, "Network.loadingFailed")

	var mu sync.Mutex
	inflight := map[string]struct{}{}

	idle := time.NewTimer(quiet)
	defer idle.Stop()
	resetIdle := func() {
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(quiet)
	}
	bump := func(rid string, add bool) {
		mu.Lock()
		defer mu.Unlock()
		if add {
			inflight[rid] = struct{}{}
		} else {
			delete(inflight, rid)
		}
		if len(inflight) == 0 {
			resetIdle()
		}
	}

	for {
		select {
		case e := <-willBeSent:
			var v struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(e.Params, &v)
			bump(v.RequestID, true)
		case e := <-finished:
			var v struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(e.Params, &v)
			bump(v.RequestID, false)
		case e := <-failed:
			var v struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(e.Params, &v)
			bump(v.RequestID, false)
		case <-idle.C:
			mu.Lock()
			n := len(inflight)
			mu.Unlock()
			if n == 0 {
				return nil
			}
			idle.Reset(quiet)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
