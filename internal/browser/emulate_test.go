package browser

import (
	"context"
	"testing"
)

func TestSetViewportSendsExpectedCDP(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.SetViewport(context.Background(), ViewportSpec{Width: 800, Height: 600}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.calls {
		if c.method == "Emulation.setDeviceMetricsOverride" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected setDeviceMetricsOverride to be called")
	}
}

func TestSetViewportZeroClearsOverride(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.SetViewport(context.Background(), ViewportSpec{}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.calls {
		if c.method == "Emulation.clearDeviceMetricsOverride" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected clearDeviceMetricsOverride for zero spec")
	}
}

func TestEmulateDeviceUnknownReturnsError(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	err := p.EmulateDevice(context.Background(), "nokia_3310")
	if err == nil {
		t.Fatal("expected error for unknown device")
	}
}

func TestEmulateDeviceAppliesViewportAndUA(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.EmulateDevice(context.Background(), "iphone_14"); err != nil {
		t.Fatal(err)
	}
	var sawViewport, sawUA bool
	for _, c := range f.calls {
		if c.method == "Emulation.setDeviceMetricsOverride" {
			sawViewport = true
		}
		if c.method == "Network.setUserAgentOverride" {
			sawUA = true
		}
	}
	if !sawViewport || !sawUA {
		t.Fatalf("expected both viewport and UA calls, viewport=%v ua=%v", sawViewport, sawUA)
	}
}

func TestSetOfflineSendsEmulateNetworkConditions(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.SetOffline(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.calls {
		if c.method == "Network.emulateNetworkConditions" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected emulateNetworkConditions")
	}
}
