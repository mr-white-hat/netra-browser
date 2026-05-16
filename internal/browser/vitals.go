package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Vitals is the subset of Core Web Vitals we collect server-side. Values
// are either the metric reading or nil if the metric hasn't fired by the
// time we read. INP requires user interaction and will usually be nil for
// passive measurements.
type Vitals struct {
	LCP  *float64 `json:"lcp"`  // Largest Contentful Paint (ms)
	CLS  *float64 `json:"cls"`  // Cumulative Layout Shift (unitless)
	FCP  *float64 `json:"fcp"`  // First Contentful Paint (ms)
	TTFB *float64 `json:"ttfb"` // Time To First Byte (ms)
	INP  *float64 `json:"inp"`  // Interaction to Next Paint (ms)
}

// vitalsInstall is the page-side observer; it stores metrics on
// window.__netra_vitals so GetVitals can read them after waiting.
// FCP/LCP/CLS use PerformanceObserver; TTFB is read from navigation entry;
// INP is approximated as the worst event-loop block we see.
const vitalsInstall = `
(() => {
  if (window.__netra_vitals) return;
  const v = { lcp: null, cls: 0, fcp: null, ttfb: null, inp: null };
  window.__netra_vitals = v;
  try {
    const nav = performance.getEntriesByType('navigation')[0];
    if (nav) v.ttfb = nav.responseStart;
  } catch (_) {}
  try {
    new PerformanceObserver((entries) => {
      for (const e of entries.getEntries()) {
        if (e.name === 'first-contentful-paint') v.fcp = e.startTime;
      }
    }).observe({ type: 'paint', buffered: true });
  } catch (_) {}
  try {
    new PerformanceObserver((entries) => {
      const list = entries.getEntries();
      if (list.length) v.lcp = list[list.length - 1].startTime;
    }).observe({ type: 'largest-contentful-paint', buffered: true });
  } catch (_) {}
  try {
    new PerformanceObserver((entries) => {
      for (const e of entries.getEntries()) {
        if (!e.hadRecentInput) v.cls += e.value;
      }
    }).observe({ type: 'layout-shift', buffered: true });
  } catch (_) {}
  try {
    new PerformanceObserver((entries) => {
      for (const e of entries.getEntries()) {
        const d = e.duration;
        if (v.inp === null || d > v.inp) v.inp = d;
      }
    }).observe({ type: 'event', buffered: true, durationThreshold: 16 });
  } catch (_) {}
})();
`

// installVitals injects the observer. Idempotent — the script self-guards
// via window.__netra_vitals.
func (p *Page) installVitals(ctx context.Context) error {
	_, err := p.Eval(ctx, vitalsInstall)
	return err
}

// GetVitals installs the observer (if not yet installed), waits for the
// requested duration so observers can accumulate, and reads the metrics out.
// waitMS clamps to [0, 60000].
func (p *Page) GetVitals(ctx context.Context, waitMS int) (*Vitals, error) {
	if waitMS < 0 {
		waitMS = 0
	}
	if waitMS > 60000 {
		waitMS = 60000
	}
	if err := p.installVitals(ctx); err != nil {
		return nil, fmt.Errorf("install vitals: %w", err)
	}
	if waitMS > 0 {
		select {
		case <-time.After(time.Duration(waitMS) * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	raw, err := p.Eval(ctx, "JSON.stringify(window.__netra_vitals || {})")
	if err != nil {
		return nil, err
	}
	s, ok := raw.(string)
	if !ok {
		return &Vitals{}, nil
	}
	var v Vitals
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("decode vitals: %w", err)
	}
	return &v, nil
}
