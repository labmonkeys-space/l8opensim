/*
 * © 2025 Labmonkeys Space
 * Apache-2.0 — see LICENSE.
 */

package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Config validation
// ---------------------------------------------------------------------------

func TestStartSyslogExport_RejectsEmptyCollector(t *testing.T) {
	sm := newTestSyslogManager()
	err := sm.StartSyslogExport(SyslogConfig{Interval: time.Second})
	if err == nil || !strings.Contains(err.Error(), "-syslog-collector") {
		t.Fatalf("want empty-collector error, got %v", err)
	}
	if sm.syslogActive.Load() {
		t.Error("syslogActive should remain false after failed StartSyslogExport")
	}
}

func TestStartSyslogExport_RejectsNonPositiveInterval(t *testing.T) {
	sm := newTestSyslogManager()
	err := sm.StartSyslogExport(SyslogConfig{
		Collector: "127.0.0.1:16500",
		Interval:  0,
	})
	if err == nil || !strings.Contains(err.Error(), "-syslog-interval") {
		t.Fatalf("want interval error, got %v", err)
	}
}

func TestStartSyslogExport_RejectsNegativeCap(t *testing.T) {
	sm := newTestSyslogManager()
	err := sm.StartSyslogExport(SyslogConfig{
		Collector: "127.0.0.1:16501",
		Interval:  time.Second,
		GlobalCap: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "-syslog-global-cap") {
		t.Fatalf("want cap error, got %v", err)
	}
}

func TestStartSyslogExport_RejectsUnresolvableCollector(t *testing.T) {
	sm := newTestSyslogManager()
	err := sm.StartSyslogExport(SyslogConfig{
		Collector: "host-does-not-resolve.invalid:514",
		Interval:  time.Second,
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid collector address") && !strings.Contains(err.Error(), "no such host") {
		t.Errorf("error %q: want mention of invalid collector", err)
	}
}

// ---------------------------------------------------------------------------
// Lifecycle happy path
// ---------------------------------------------------------------------------

func TestStartStopSyslogExport_HappyPath(t *testing.T) {
	sm := newTestSyslogManager()
	collector, collectorAddr := newLocalUDPCollector(t)
	_ = collector // collector is cleaned up by t.Cleanup

	err := sm.StartSyslogExport(SyslogConfig{
		Collector:       collectorAddr.String(),
		Format:          SyslogFormat5424,
		Interval:        time.Second,
		SourcePerDevice: false, // tests run without netns
	})
	if err != nil {
		t.Fatalf("StartSyslogExport: %v", err)
	}
	if !sm.syslogActive.Load() {
		t.Fatal("syslogActive should be true after successful Start")
	}
	if sm.syslogCatalog == nil || len(sm.syslogCatalog.Entries) != 6 {
		t.Errorf("embedded catalog not loaded: %v", sm.syslogCatalog)
	}

	// Idempotency: second Start without Stop should fail.
	if err := sm.StartSyslogExport(SyslogConfig{
		Collector: collectorAddr.String(),
		Interval:  time.Second,
	}); err == nil {
		t.Error("second Start should refuse without a Stop")
	}

	sm.StopSyslogExport()
	if sm.syslogActive.Load() {
		t.Error("syslogActive should be false after Stop")
	}
	// Stop is idempotent.
	sm.StopSyslogExport()
}

// ---------------------------------------------------------------------------
// GetSyslogStatus
// ---------------------------------------------------------------------------

func TestGetSyslogStatus_Disabled(t *testing.T) {
	sm := newTestSyslogManager()
	st := sm.GetSyslogStatus()
	if st.Enabled {
		t.Error("Enabled should be false when feature not started")
	}
	if st.Format != "" || st.Collector != "" {
		t.Errorf("Format/Collector should be empty when disabled: %+v", st)
	}
}

func TestGetSyslogStatus_EnabledShape(t *testing.T) {
	sm, _, _ := startSyslogForTest(t, SyslogFormat3164)
	st := sm.GetSyslogStatus()
	if !st.Enabled {
		t.Fatal("Enabled should be true")
	}
	if st.Format != "3164" {
		t.Errorf("Format: got %q, want 3164", st.Format)
	}
	if st.Collector == "" {
		t.Error("Collector should be populated")
	}
	if st.DevicesExporting != 1 {
		t.Errorf("DevicesExporting: got %d, want 1 (test fixture has one device)", st.DevicesExporting)
	}
}

// ---------------------------------------------------------------------------
// FireSyslogOnDevice
// ---------------------------------------------------------------------------

func TestFireSyslogOnDevice_HappyPath(t *testing.T) {
	sm, collector, device := startSyslogForTest(t, SyslogFormat5424)
	if err := sm.FireSyslogOnDevice(device.IP.String(), "interface-down", nil); err != nil {
		t.Fatal(err)
	}
	// Wait briefly for the datagram.
	_ = collector.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 2048)
	n, _, err := collector.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("collector did not receive datagram: %v", err)
	}
	wire := string(buf[:n])
	if !strings.HasPrefix(wire, "<187>1 ") {
		t.Errorf("wire did not start with expected PRI+version: %q", wire)
	}
	if !strings.Contains(wire, "LINKDOWN") {
		t.Errorf("wire missing MsgID: %q", wire)
	}
	// Spec Requirement "On-demand HTTP syslog endpoint" scenario "Valid
	// request returns 202" says "AND the sent counter SHALL increment
	// by 1". Verify via the status endpoint.
	if got := sm.GetSyslogStatus().Sent; got != 1 {
		t.Errorf("SyslogStatus.Sent after one Fire: got %d, want 1", got)
	}
}

func TestFireSyslogOnDevice_UnknownCatalogName(t *testing.T) {
	sm, _, device := startSyslogForTest(t, SyslogFormat5424)
	err := sm.FireSyslogOnDevice(device.IP.String(), "notAnEntry", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrSyslogEntryNotFound) {
		t.Errorf("want ErrSyslogEntryNotFound, got %v", err)
	}
}

func TestFireSyslogOnDevice_UnknownDevice(t *testing.T) {
	sm, _, _ := startSyslogForTest(t, SyslogFormat5424)
	err := sm.FireSyslogOnDevice("10.99.99.99", "interface-up", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrSyslogDeviceNotFound) {
		t.Errorf("want ErrSyslogDeviceNotFound, got %v", err)
	}
}

func TestFireSyslogOnDevice_Disabled(t *testing.T) {
	sm := newTestSyslogManager()
	err := sm.FireSyslogOnDevice("10.0.0.1", "interface-up", nil)
	if err == nil || !errors.Is(err, ErrSyslogExportDisabled) {
		t.Errorf("want ErrSyslogExportDisabled, got %v", err)
	}
}

func TestFireSyslogOnDevice_OverridesApplied(t *testing.T) {
	sm, collector, device := startSyslogForTest(t, SyslogFormat5424)
	err := sm.FireSyslogOnDevice(device.IP.String(), "interface-down", map[string]string{
		"IfIndex": "42",
		"IfName":  "GigabitEthernet7/42",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = collector.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 2048)
	n, _, err := collector.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("no datagram: %v", err)
	}
	wire := string(buf[:n])
	if !strings.Contains(wire, "ifIndex=42") {
		t.Errorf("override IfIndex not present: %q", wire)
	}
	if !strings.Contains(wire, "GigabitEthernet7/42") {
		t.Errorf("override IfName not present: %q", wire)
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests (go through the real mux + handlers)
// ---------------------------------------------------------------------------

// withManager temporarily installs sm as the package-level `manager`
// variable that `fireSyslogHandler` and `syslogStatusHandler` read.
// Restores the previous value on test cleanup.
func withManager(t *testing.T, sm *SimulatorManager) {
	t.Helper()
	prev := manager
	manager = sm
	t.Cleanup(func() { manager = prev })
}

func TestSyslogHTTP_StatusEndpointDisabled(t *testing.T) {
	sm := newTestSyslogManager()
	withManager(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/syslog/status", nil)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code: got %d, want 200", rr.Code)
	}
	var body SyslogStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v — raw=%q", err, rr.Body.String())
	}
	if body.Enabled {
		t.Errorf("Enabled: got %v, want false when feature disabled", body.Enabled)
	}
}

func TestSyslogHTTP_StatusEndpointEnabled(t *testing.T) {
	sm, _, _ := startSyslogForTest(t, SyslogFormat5424)
	withManager(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/syslog/status", nil)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code: got %d, want 200", rr.Code)
	}
	var body SyslogStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Enabled || body.Format != "5424" || body.DevicesExporting != 1 {
		t.Errorf("enabled status unexpected: %+v", body)
	}
}

func TestSyslogHTTP_FireEndpoint202(t *testing.T) {
	sm, collector, device := startSyslogForTest(t, SyslogFormat5424)
	withManager(t, sm)

	body := strings.NewReader(`{"name":"interface-down"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+device.IP.String()+"/syslog", body)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status code: got %d, want 202 — body=%q", rr.Code, rr.Body.String())
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "{}" {
		t.Errorf("body: got %q, want {}", got)
	}
	// Verify the datagram actually reached the collector (end-to-end
	// validation of the manager → exporter → UDP path through the HTTP
	// handler surface).
	_ = collector.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 2048)
	if _, _, err := collector.ReadFromUDP(buf); err != nil {
		t.Errorf("collector did not receive datagram: %v", err)
	}
}

func TestSyslogHTTP_FireEndpoint400UnknownCatalogEntry(t *testing.T) {
	sm, _, device := startSyslogForTest(t, SyslogFormat5424)
	withManager(t, sm)

	body := strings.NewReader(`{"name":"notACatalogEntry"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+device.IP.String()+"/syslog", body)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code: got %d, want 400 — body=%q", rr.Code, rr.Body.String())
	}
}

func TestSyslogHTTP_FireEndpoint404UnknownDevice(t *testing.T) {
	sm, _, _ := startSyslogForTest(t, SyslogFormat5424)
	withManager(t, sm)

	body := strings.NewReader(`{"name":"interface-up"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/10.99.99.99/syslog", body)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status code: got %d, want 404", rr.Code)
	}
}

func TestSyslogHTTP_FireEndpoint503FeatureDisabled(t *testing.T) {
	sm := newTestSyslogManager()
	withManager(t, sm)

	body := strings.NewReader(`{"name":"interface-up"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/10.0.0.1/syslog", body)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code: got %d, want 503", rr.Code)
	}
}

func TestSyslogHTTP_FireEndpoint400MissingName(t *testing.T) {
	sm, _, device := startSyslogForTest(t, SyslogFormat5424)
	withManager(t, sm)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+device.IP.String()+"/syslog", body)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code: got %d, want 400", rr.Code)
	}
}

func TestSyslogHTTP_FireEndpoint400UnknownField(t *testing.T) {
	// `DisallowUnknownFields` fix: a typo'd key surfaces as 400 instead
	// of being silently dropped.
	sm, _, device := startSyslogForTest(t, SyslogFormat5424)
	withManager(t, sm)

	body := strings.NewReader(`{"name":"interface-down","tempalteOverrides":{"IfIndex":"7"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+device.IP.String()+"/syslog", body)
	rr := httptest.NewRecorder()
	setupRoutes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("typo'd field accepted silently: got %d, want 400", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestSyslogManager builds a minimal SimulatorManager suitable for
// exercising the syslog subsystem. No netns, no real devices.
func newTestSyslogManager() *SimulatorManager {
	return &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
	}
}

// startSyslogForTest stands up a SimulatorManager with syslog export
// active, registers one fake device with a SyslogExporter writing via a
// shared socket (no netns). Returns the manager, the collector socket (for
// reading emitted datagrams), and the fake device. Registers t.Cleanup.
func startSyslogForTest(t *testing.T, format SyslogFormat) (*SimulatorManager, *net.UDPConn, *DeviceSimulator) {
	t.Helper()
	sm := newTestSyslogManager()
	collector, collectorAddr := newLocalUDPCollector(t)

	err := sm.StartSyslogExport(SyslogConfig{
		Collector: collectorAddr.String(),
		Format:    format,
		// One hour, not ten seconds: the scheduler's Poisson draw has an
		// unbounded tail, and at 10s the ~1% of runs with a sub-second
		// tail draw made `TestFireSyslogOnDevice_OverridesApplied` read
		// the scheduled datagram instead of the explicit-fire one.
		Interval:        time.Hour,
		SourcePerDevice: false, // no netns available in unit tests
	})
	if err != nil {
		t.Fatalf("StartSyslogExport: %v", err)
	}

	// Build a fake device with a SyslogExporter that writes via the
	// manager's shared socket (SharedConn). This mirrors what
	// startDeviceSyslogExporter does but without touching netns.
	device := &DeviceSimulator{
		ID:      "test-device",
		IP:      net.IPv4(127, 0, 0, 1),
		sysName: "test-host",
	}
	exp := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   device.IP,
		Encoder:    sm.syslogEncoder,
		Collector:  sm.syslogCollectorAddr,
		SharedConn: sm.syslogConn,
		SysName:    device.sysName,
		IfIndexFn:  func() int { return 3 },
		IfNameFn:   func(i int) string { return "GigabitEthernet0/3" },
	})
	device.syslogExporter = exp
	sm.devices[device.ID] = device
	sm.deviceIPs[device.IP.String()] = struct{}{}
	if sm.syslogScheduler != nil {
		sm.syslogScheduler.Register(device.IP, exp)
	}

	t.Cleanup(func() {
		sm.StopSyslogExport()
	})
	return sm, collector, device
}
