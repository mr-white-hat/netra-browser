package browser

import (
	"context"
	"fmt"
)

// ViewportSpec drives Emulation.setDeviceMetricsOverride.
// A zero ScaleFactor is normalized to 1; a zero Width/Height clears the override.
type ViewportSpec struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"device_scale_factor"`
	Mobile            bool    `json:"mobile"`
}

// SetViewport overrides the page viewport. Pass Width=0, Height=0 to clear.
func (p *Page) SetViewport(ctx context.Context, v ViewportSpec) error {
	if v.Width == 0 && v.Height == 0 {
		_, err := p.send(ctx, "Emulation.clearDeviceMetricsOverride", nil)
		return err
	}
	if v.DeviceScaleFactor == 0 {
		v.DeviceScaleFactor = 1
	}
	_, err := p.send(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             v.Width,
		"height":            v.Height,
		"deviceScaleFactor": v.DeviceScaleFactor,
		"mobile":            v.Mobile,
	})
	return err
}

// SetUserAgent overrides the User-Agent header for subsequent requests.
// Empty string clears the override.
func (p *Page) SetUserAgent(ctx context.Context, ua string) error {
	_, err := p.send(ctx, "Network.setUserAgentOverride", map[string]any{
		"userAgent": ua,
	})
	return err
}

// GeoSpec drives Emulation.setGeolocationOverride. Pass an empty spec
// (zero lat/lon and zero Accuracy) to clear.
type GeoSpec struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy"`
}

// SetGeolocation overrides geolocation. Accuracy defaults to 100m if zero.
// Calling with the zero value clears the override.
func (p *Page) SetGeolocation(ctx context.Context, g GeoSpec) error {
	if g.Latitude == 0 && g.Longitude == 0 && g.Accuracy == 0 {
		_, err := p.send(ctx, "Emulation.clearGeolocationOverride", nil)
		return err
	}
	if g.Accuracy == 0 {
		g.Accuracy = 100
	}
	_, err := p.send(ctx, "Emulation.setGeolocationOverride", map[string]any{
		"latitude":  g.Latitude,
		"longitude": g.Longitude,
		"accuracy":  g.Accuracy,
	})
	return err
}

// SetOffline toggles network connectivity for the page. When offline=true,
// every request immediately fails with net::ERR_INTERNET_DISCONNECTED.
func (p *Page) SetOffline(ctx context.Context, offline bool) error {
	_, err := p.send(ctx, "Network.emulateNetworkConditions", map[string]any{
		"offline":            offline,
		"latency":            0,
		"downloadThroughput": -1,
		"uploadThroughput":   -1,
	})
	return err
}

// DevicePreset is one row in the built-in device emulation table.
type DevicePreset struct {
	Viewport  ViewportSpec
	UserAgent string
}

// DevicePresets is the small device library — accessibility-tree focused
// rather than the 100+ Playwright table. Add presets as agents need them.
var DevicePresets = map[string]DevicePreset{
	"iphone_14": {
		Viewport:  ViewportSpec{Width: 390, Height: 844, DeviceScaleFactor: 3, Mobile: true},
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1",
	},
	"iphone_se": {
		Viewport:  ViewportSpec{Width: 375, Height: 667, DeviceScaleFactor: 2, Mobile: true},
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1",
	},
	"pixel_8": {
		Viewport:  ViewportSpec{Width: 412, Height: 915, DeviceScaleFactor: 2.625, Mobile: true},
		UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	},
	"ipad_pro": {
		Viewport:  ViewportSpec{Width: 1024, Height: 1366, DeviceScaleFactor: 2, Mobile: true},
		UserAgent: "Mozilla/5.0 (iPad; CPU OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1",
	},
	"desktop_1080p": {
		Viewport: ViewportSpec{Width: 1920, Height: 1080, DeviceScaleFactor: 1, Mobile: false},
	},
	"desktop_macbook": {
		Viewport: ViewportSpec{Width: 1440, Height: 900, DeviceScaleFactor: 2, Mobile: false},
	},
}

// EmulateDevice applies a viewport + user-agent preset. Returns an error if
// the device name is unknown so callers can surface it cleanly.
func (p *Page) EmulateDevice(ctx context.Context, name string) error {
	d, ok := DevicePresets[name]
	if !ok {
		return fmt.Errorf("unknown device preset %q", name)
	}
	if err := p.SetViewport(ctx, d.Viewport); err != nil {
		return err
	}
	if d.UserAgent != "" {
		if err := p.SetUserAgent(ctx, d.UserAgent); err != nil {
			return err
		}
	}
	return nil
}
