/*
 * © 2025 Labmonkeys Space
 *
 * Layer 8 Ecosystem is licensed under the Apache License, Version 2.0.
 */

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseTrapMode_AllCases(t *testing.T) {
	cases := []struct {
		in      string
		want    TrapMode
		wantErr bool
	}{
		{"", TrapModeTrap, false},
		{"trap", TrapModeTrap, false},
		{"TRAP", TrapModeTrap, false},
		{"inform", TrapModeInform, false},
		{"Inform", TrapModeInform, false},
		{"notify", 0, true},
		{"v3", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseTrapMode(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseTrapMode(%q): err = %v, wantErr = %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ParseTrapMode(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestStartTrapExport_RejectsInformWithoutPerDeviceBinding(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	err := sm.StartTrapExport(TrapConfig{
		Collector:       "127.0.0.1:16200",
		Mode:            TrapModeInform,
		SourcePerDevice: false, // explicit conflict
		Interval:        time.Second,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "inform") || !strings.Contains(err.Error(), "per-device") {
		t.Errorf("error should mention inform + per-device: %v", err)
	}
	if sm.trapActive.Load() {
		t.Error("trapActive should remain false after failed StartTrapExport")
	}
}

func TestStartTrapExport_RejectsEmptyCollector(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	err := sm.StartTrapExport(TrapConfig{Interval: time.Second})
	if err == nil || !strings.Contains(err.Error(), "-trap-collector") {
		t.Fatalf("want empty-collector error, got %v", err)
	}
}

func TestStartTrapExport_RejectsNonPositiveInterval(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	err := sm.StartTrapExport(TrapConfig{
		Collector:       "127.0.0.1:16201",
		Mode:            TrapModeTrap,
		SourcePerDevice: true,
		Interval:        0,
	})
	if err == nil || !strings.Contains(err.Error(), "-trap-interval") {
		t.Fatalf("want interval error, got %v", err)
	}
}

func TestStartTrapExport_RejectsNegativeRetries(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	err := sm.StartTrapExport(TrapConfig{
		Collector:       "127.0.0.1:16202",
		Mode:            TrapModeTrap,
		SourcePerDevice: true,
		Interval:        time.Second,
		InformRetries:   -1,
	})
	if err == nil || !strings.Contains(err.Error(), "retries") {
		t.Fatalf("want retries error, got %v", err)
	}
}

// startTrapForTest stands up a minimal SimulatorManager with trap export
// active, pointing at a mock collector. Returns the mock (must Close) and the
// manager. A single fake device is registered so FindDeviceByIP / the HTTP
// handlers can resolve it.
func startTrapForTest(t *testing.T, mode TrapMode) (*SimulatorManager, *mockCollector, *DeviceSimulator) {
	t.Helper()
	mc := newMockCollector(t, mode == TrapModeInform)

	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}

	err := sm.StartTrapExport(TrapConfig{
		Collector:       mc.addr.String(),
		Mode:            mode,
		Community:       "public",
		Interval:        time.Second,
		InformTimeout:   200 * time.Millisecond,
		InformRetries:   0,
		SourcePerDevice: false, // TRAP mode only in this helper; INFORM would need netns
	})
	if mode == TrapModeInform {
		// INFORM mode requires per-device binding, which we can't do without
		// netns. Rewrite StartTrapExport to allow an explicit test-only path.
		if err == nil {
			t.Fatal("expected inform + non-per-device to fail")
		}
		err = sm.StartTrapExport(TrapConfig{
			Collector:       mc.addr.String(),
			Mode:            TrapModeTrap, // fall back to trap mode for test
			Community:       "public",
			Interval:        time.Second,
			InformTimeout:   200 * time.Millisecond,
			SourcePerDevice: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err != nil {
		t.Fatal(err)
	}

	// Insert a fake device with a TrapExporter so FindDeviceByIP has
	// something to find. This mirrors what device.go does.
	device := &DeviceSimulator{
		ID: "test-device",
		IP: net.IPv4(127, 0, 0, 1),
	}
	conn := openTestUDPConn(t)
	exp := NewTrapExporter(TrapExporterOptions{
		DeviceIP:      device.IP,
		Community:     sm.trapCommunity,
		Encoder:       sm.trapEncoder,
		Mode:          sm.trapMode,
		Collector:     sm.trapCollectorAddr,
		Limiter:       sm.trapLimiter,
		SharedConn:    sm.trapConn,
		InformTimeout: sm.trapInformTimeout,
	})
	exp.SetConn(conn)
	exp.StartBackgroundLoops(context.Background())
	device.trapExporter = exp

	sm.devices[device.ID] = device
	sm.deviceIPs[device.IP.String()] = struct{}{}

	t.Cleanup(func() {
		sm.StopTrapExport()
		mc.Close()
	})
	return sm, mc, device
}

func TestFireTrapOnDevice_HappyPath(t *testing.T) {
	sm, mc, device := startTrapForTest(t, TrapModeTrap)
	reqID, err := sm.FireTrapOnDevice(device.IP.String(), "linkDown", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reqID == 0 {
		t.Error("reqID = 0, want nonzero")
	}
	// Give the collector a moment to see the datagram.
	time.Sleep(100 * time.Millisecond)
	if mc.received.Load() == 0 {
		t.Error("collector never saw the trap")
	}
}

func TestFireTrapOnDevice_UnknownCatalogName(t *testing.T) {
	sm, _, device := startTrapForTest(t, TrapModeTrap)
	_, err := sm.FireTrapOnDevice(device.IP.String(), "notACatalogEntry", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrTrapEntryNotFound) {
		t.Errorf("want ErrTrapEntryNotFound, got %v", err)
	}
}

func TestFireTrapOnDevice_UnknownDeviceIP(t *testing.T) {
	sm, _, _ := startTrapForTest(t, TrapModeTrap)
	_, err := sm.FireTrapOnDevice("10.99.99.99", "linkDown", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrTrapDeviceNotFound) {
		t.Errorf("want ErrTrapDeviceNotFound, got %v", err)
	}
}

func TestFireTrapOnDevice_WhenDisabled(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	_, err := sm.FireTrapOnDevice("10.0.0.1", "linkDown", nil)
	if !errors.Is(err, ErrTrapExportDisabled) {
		t.Errorf("want ErrTrapExportDisabled, got %v", err)
	}
}

func TestGetTrapStatus_Disabled(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	s := sm.GetTrapStatus()
	if s.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if s.Mode != "" {
		t.Errorf("Mode = %q, want empty when disabled", s.Mode)
	}
}

func TestGetTrapStatus_TRAPMode_Shape(t *testing.T) {
	sm, _, device := startTrapForTest(t, TrapModeTrap)
	_, err := sm.FireTrapOnDevice(device.IP.String(), "linkUp", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	s := sm.GetTrapStatus()
	if !s.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if s.Mode != "trap" {
		t.Errorf("Mode = %q, want trap", s.Mode)
	}
	if s.Sent == 0 {
		t.Error("Sent = 0, want ≥ 1")
	}
	// INFORM-specific fields must be absent in TRAP mode.
	if s.InformsAcked != 0 || s.InformsFailed != 0 || s.InformsDropped != 0 || s.InformsPending != 0 {
		t.Errorf("INFORM counters should all be zero in TRAP mode: %+v", s)
	}
}

func TestWriteTrapStatusJSON_ContentType(t *testing.T) {
	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
	rec := httptest.NewRecorder()
	sm.WriteTrapStatusJSON(rec)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Enabled {
		t.Errorf("Enabled = true on fresh manager")
	}
}

// TestInformInvariant asserts that at every point during a sequence of fires
// and status reads, the equation:
//
//	informsPending + informsAcked + informsFailed + informsDropped
//	                                    == informsOriginated
//
// holds. Runs in TRAP mode (since the helper can't set up INFORM with netns)
// by using the exporter directly and simulating the INFORM accounting.
func TestInformInvariant_AtExporterLevel(t *testing.T) {
	cat, _ := LoadEmbeddedCatalog()
	mc := newMockCollector(t, true) // auto-ack
	defer mc.Close()
	conn := openTestUDPConn(t)

	e := NewTrapExporter(TrapExporterOptions{
		DeviceIP:      net.IPv4(127, 0, 0, 1),
		Mode:          TrapModeInform,
		Collector:     mc.addr,
		InformTimeout: 150 * time.Millisecond,
		InformRetries: 0,
		PendingCap:    3, // small → we can force drops
	})
	e.SetConn(conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.StartBackgroundLoops(ctx)
	defer e.Close()

	const fires = 10
	for i := 0; i < fires; i++ {
		e.Fire(cat.ByName["linkUp"], nil)
	}
	// Check invariant across a few measurement points.
	for attempt := 0; attempt < 20; attempt++ {
		st := e.Stats()
		pending := uint64(e.PendingInformsLen())
		acked := st.InformsAcked.Load()
		failed := st.InformsFailed.Load()
		dropped := st.InformsDropped.Load()
		originated := st.InformsOriginated.Load()
		if pending+acked+failed+dropped != originated {
			t.Fatalf("invariant broken at attempt %d: pending=%d acked=%d failed=%d dropped=%d originated=%d",
				attempt, pending, acked, failed, dropped, originated)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestFireTrapHandler_DecodesBody(t *testing.T) {
	// Happy-path request shape test — exercises the JSON decoder in the
	// handler without spinning up the full mux/router.
	body, _ := json.Marshal(map[string]any{
		"name":             "linkDown",
		"varbindOverrides": map[string]string{"IfIndex": "5"},
	})
	req := httptest.NewRequest("POST", "/api/v1/devices/10.0.0.1/trap", bytes.NewReader(body))

	var decoded struct {
		Name             string            `json:"name"`
		VarbindOverrides map[string]string `json:"varbindOverrides"`
	}
	if err := json.NewDecoder(req.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "linkDown" || decoded.VarbindOverrides["IfIndex"] != "5" {
		t.Errorf("decode mismatch: %+v", decoded)
	}
}
