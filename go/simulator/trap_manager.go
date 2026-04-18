/*
 * © 2025 Labmonkeys Space
 *
 * Layer 8 Ecosystem is licensed under the Apache License, Version 2.0.
 * You may obtain a copy of the License at:
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// SimulatorManager-level SNMP trap export lifecycle.
//
// Parses the TrapConfig surfaced by CLI flags, loads the catalog (embedded or
// file override), creates the shared TrapScheduler + TrapEncoder, and starts
// the scheduler goroutine. Wires per-device TrapExporters into device
// startup/teardown (see trap_exporter.go) and exposes GetTrapStatus for the
// HTTP API.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// TrapConfig bundles CLI-derived configuration for the trap subsystem.
// Empty Collector disables the feature.
type TrapConfig struct {
	Collector       string
	Mode            TrapMode
	Community       string
	Interval        time.Duration
	GlobalCap       int // 0 = unlimited
	CatalogPath     string
	InformTimeout   time.Duration
	InformRetries   int
	SourcePerDevice bool
}

// ParseTrapMode converts a case-insensitive string to TrapMode. Empty defaults
// to TrapModeTrap so operators that pass -trap-collector without -trap-mode
// get the common case.
func ParseTrapMode(s string) (TrapMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "trap":
		return TrapModeTrap, nil
	case "inform":
		return TrapModeInform, nil
	default:
		return 0, fmt.Errorf("invalid -trap-mode %q (valid: trap, inform)", s)
	}
}

// TrapStatus is the JSON body returned by GET /api/v1/traps/status. Fields
// follow the shape required by spec.md ("Trap status HTTP endpoint").
type TrapStatus struct {
	Enabled                    bool   `json:"enabled"`
	Mode                       string `json:"mode,omitempty"`
	Collector                  string `json:"collector,omitempty"`
	Community                  string `json:"community,omitempty"`
	Sent                       uint64 `json:"sent"`
	InformsPending             uint64 `json:"informs_pending,omitempty"`
	InformsAcked               uint64 `json:"informs_acked,omitempty"`
	InformsFailed              uint64 `json:"informs_failed,omitempty"`
	InformsDropped             uint64 `json:"informs_dropped,omitempty"`
	RateLimiterTokensAvailable int    `json:"rate_limiter_tokens_available,omitempty"`
	DevicesExporting           int    `json:"devices_exporting"`
}

// StartTrapExport validates cfg, loads the catalog, creates the shared
// scheduler, and starts the scheduler goroutine. Idempotent on error —
// partial state is unwound before returning.
//
// Preconditions:
//   - Called after manager construction, before any device creation that
//     should participate in trap export.
//   - Mode=inform requires SourcePerDevice=true (spec: "Explicit conflict
//     fails startup").
func (sm *SimulatorManager) StartTrapExport(cfg TrapConfig) error {
	if sm.trapActive.Load() {
		return fmt.Errorf("trap export: already active; Shutdown before re-initializing")
	}
	if cfg.Collector == "" {
		return fmt.Errorf("trap export: -trap-collector required to enable feature")
	}
	if cfg.Mode == TrapModeInform && !cfg.SourcePerDevice {
		return fmt.Errorf("trap export: -trap-mode inform requires -trap-source-per-device=true")
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("trap export: -trap-interval must be positive, got %s", cfg.Interval)
	}
	if cfg.InformRetries < 0 {
		return fmt.Errorf("trap export: -trap-inform-retries must be non-negative, got %d", cfg.InformRetries)
	}
	if cfg.GlobalCap < 0 {
		return fmt.Errorf("trap export: -trap-global-cap must be non-negative, got %d", cfg.GlobalCap)
	}
	if cfg.Community == "" {
		cfg.Community = "public"
	}
	if cfg.InformTimeout <= 0 {
		cfg.InformTimeout = 5 * time.Second
	}

	addr, err := net.ResolveUDPAddr("udp4", cfg.Collector)
	if err != nil {
		return fmt.Errorf("trap export: invalid collector address %q: %w", cfg.Collector, err)
	}

	var catalog *Catalog
	if cfg.CatalogPath == "" {
		catalog, err = LoadEmbeddedCatalog()
	} else {
		catalog, err = LoadCatalogFromFile(cfg.CatalogPath)
	}
	if err != nil {
		return err
	}

	// Shared fallback socket: used only when per-device binding is off or
	// fails in TRAP mode (INFORM mode disallows fallback).
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return fmt.Errorf("trap export: failed to open shared UDP socket: %w", err)
	}

	var limiter *rate.Limiter
	if cfg.GlobalCap > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.GlobalCap), cfg.GlobalCap)
	}

	scheduler := NewTrapScheduler(SchedulerOptions{
		Catalog:            catalog,
		MeanInterval:       cfg.Interval,
		GlobalCapPerSecond: cfg.GlobalCap,
	})

	sm.mu.Lock()
	sm.trapCatalog = catalog
	sm.trapScheduler = scheduler
	sm.trapEncoder = SNMPv2cEncoder{}
	sm.trapLimiter = limiter
	sm.trapConn = conn
	sm.trapCollectorAddr = addr
	sm.trapCollectorStr = cfg.Collector
	sm.trapMode = cfg.Mode
	sm.trapCommunity = cfg.Community
	sm.trapInterval = cfg.Interval
	sm.trapGlobalCap = cfg.GlobalCap
	sm.trapInformTimeout = cfg.InformTimeout
	sm.trapInformRetries = cfg.InformRetries
	sm.trapSourcePerDevice = cfg.SourcePerDevice
	sm.trapCatalogPath = cfg.CatalogPath
	sm.mu.Unlock()
	sm.trapActive.Store(true)

	modeStr := "trap"
	if cfg.Mode == TrapModeInform {
		modeStr = "inform"
	}
	capStr := "unlimited"
	if cfg.GlobalCap > 0 {
		capStr = fmt.Sprintf("%d/s", cfg.GlobalCap)
	}
	catStr := "<embedded>"
	if cfg.CatalogPath != "" {
		catStr = cfg.CatalogPath
	}
	log.Printf("Trap export: %s → %s (mode=%s, interval=%s, cap=%s, catalog=%s, per-device-source=%v)",
		conn.LocalAddr(), cfg.Collector, modeStr, cfg.Interval, capStr, catStr, cfg.SourcePerDevice)

	go scheduler.Run(context.Background())

	return nil
}

// StopTrapExport stops the scheduler, closes the shared socket, and closes
// every device's TrapExporter. Safe to call when trap export is inactive
// (no-op).
func (sm *SimulatorManager) StopTrapExport() {
	if !sm.trapActive.Load() {
		return
	}
	sm.trapActive.Store(false)

	sm.mu.RLock()
	scheduler := sm.trapScheduler
	devices := make([]*DeviceSimulator, 0, len(sm.devices))
	for _, d := range sm.devices {
		if d.trapExporter != nil {
			devices = append(devices, d)
		}
	}
	conn := sm.trapConn
	sm.mu.RUnlock()

	if scheduler != nil {
		scheduler.Stop()
	}
	for _, d := range devices {
		if d.trapExporter != nil {
			_ = d.trapExporter.Close()
			d.trapExporter = nil
		}
	}
	if conn != nil {
		_ = conn.Close()
	}

	sm.mu.Lock()
	sm.trapScheduler = nil
	sm.trapConn = nil
	sm.mu.Unlock()
}

// startDeviceTrapExporter creates a TrapExporter for device, opens a
// per-device UDP socket when enabled, and registers the exporter with the
// scheduler. Called from device creation sites in device.go (mirrors the
// flow-export hook).
//
// Returns an error for INFORM mode when per-device binding fails (startup
// refusal per spec). TRAP mode falls back to the shared socket with a warning.
func (sm *SimulatorManager) startDeviceTrapExporter(device *DeviceSimulator) error {
	if !sm.trapActive.Load() || device == nil {
		return nil
	}
	sm.mu.RLock()
	mode := sm.trapMode
	opts := TrapExporterOptions{
		DeviceIP:      device.IP,
		Community:     sm.trapCommunity,
		Encoder:       sm.trapEncoder,
		Mode:          mode,
		Collector:     sm.trapCollectorAddr,
		Limiter:       sm.trapLimiter,
		SharedConn:    sm.trapConn,
		InformTimeout: sm.trapInformTimeout,
		InformRetries: sm.trapInformRetries,
		IfIndexFn:     deviceIfIndexFn(device),
	}
	sourcePerDevice := sm.trapSourcePerDevice
	scheduler := sm.trapScheduler
	sm.mu.RUnlock()

	exporter := NewTrapExporter(opts)

	if sourcePerDevice {
		conn := openTrapConnForDevice(device)
		if conn == nil {
			if mode == TrapModeInform {
				return fmt.Errorf("trap export: per-device bind failed for %s (required by INFORM mode)", device.IP)
			}
			log.Printf("trap export: device %s per-device bind failed, falling back to shared socket", device.IP)
		} else {
			exporter.SetConn(conn)
		}
	}

	exporter.StartBackgroundLoops(context.Background())

	device.mu.Lock()
	device.trapExporter = exporter
	device.mu.Unlock()

	if scheduler != nil {
		scheduler.Register(device.IP, exporter)
	}
	return nil
}

// deviceIfIndexFn builds a template-field callback returning a random ifIndex
// drawn from the device's simulated interface set. Falls back to 1 when the
// device has no indexed interfaces (fresh device, or a device type without
// ifTable resources).
func deviceIfIndexFn(device *DeviceSimulator) func() int {
	return func() int {
		if device == nil || device.metricsCycler == nil || device.metricsCycler.ifCounters == nil {
			return 1
		}
		indices := device.metricsCycler.ifCounters.IfIndices()
		if len(indices) == 0 {
			return 1
		}
		// Use math/rand global — we don't need crypto-quality randomness here.
		return indices[rand.Intn(len(indices))]
	}
}

// GetTrapStatus returns a JSON-serializable snapshot of the trap export state.
// Exposed via GET /api/v1/traps/status.
func (sm *SimulatorManager) GetTrapStatus() TrapStatus {
	if !sm.trapActive.Load() {
		return TrapStatus{Enabled: false}
	}

	sm.mu.RLock()
	status := TrapStatus{
		Enabled:   true,
		Collector: sm.trapCollectorStr,
		Community: sm.trapCommunity,
	}
	if sm.trapMode == TrapModeInform {
		status.Mode = "inform"
	} else {
		status.Mode = "trap"
	}
	limiter := sm.trapLimiter

	var sent, pending, acked, failed, dropped uint64
	devicesExporting := 0
	for _, d := range sm.devices {
		if d.trapExporter == nil {
			continue
		}
		devicesExporting++
		st := d.trapExporter.Stats()
		sent += st.Sent.Load()
		if sm.trapMode == TrapModeInform {
			pending += uint64(d.trapExporter.PendingInformsLen())
			acked += st.InformsAcked.Load()
			failed += st.InformsFailed.Load()
			dropped += st.InformsDropped.Load()
		}
	}
	sm.mu.RUnlock()

	status.Sent = sent
	status.DevicesExporting = devicesExporting
	if sm.trapMode == TrapModeInform {
		status.InformsPending = pending
		status.InformsAcked = acked
		status.InformsFailed = failed
		status.InformsDropped = dropped
	}
	if limiter != nil {
		// TokensAt is the approximate instantaneous token count. Conservative
		// snapshot — not synchronized with concurrent Wait calls.
		status.RateLimiterTokensAvailable = int(limiter.Tokens())
	}
	return status
}

// FindDeviceByIP returns the first device with the given IP, or nil if none
// match. Linear scan — trap endpoints are admin-plane so O(N) per request is
// acceptable at 30k-device scale.
func (sm *SimulatorManager) FindDeviceByIP(ip string) *DeviceSimulator {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for _, d := range sm.devices {
		if d.IP.String() == ip {
			return d
		}
	}
	return nil
}

// FireTrapOnDevice implements POST /api/v1/devices/{ip}/trap by looking up the
// device's TrapExporter and invoking Fire with the given catalog name.
// Returns the request-id and nil on success; HTTP status codes are chosen by
// the caller based on the error (see web.go / api.go).
//
// Returns errors tagged so the HTTP layer can map them:
//   - ErrTrapExportDisabled → 503 Service Unavailable
//   - ErrTrapDeviceNotFound → 404
//   - ErrTrapEntryNotFound  → 400
func (sm *SimulatorManager) FireTrapOnDevice(ip, trapName string, overrides map[string]string) (uint32, error) {
	if !sm.trapActive.Load() {
		return 0, ErrTrapExportDisabled
	}
	sm.mu.RLock()
	cat := sm.trapCatalog
	sm.mu.RUnlock()

	entry, ok := cat.ByName[trapName]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrTrapEntryNotFound, trapName)
	}
	device := sm.FindDeviceByIP(ip)
	if device == nil {
		return 0, fmt.Errorf("%w: %q", ErrTrapDeviceNotFound, ip)
	}
	if device.trapExporter == nil {
		return 0, fmt.Errorf("%w: device %s has no trap exporter", ErrTrapExportDisabled, ip)
	}
	id := device.trapExporter.Fire(entry, overrides)
	if id == 0 {
		return 0, fmt.Errorf("trap fire for %s returned 0 reqID (resolve or write failure)", ip)
	}
	atomic.AddUint64(&fireTrapAPIRequests, 1)
	return id, nil
}

// Sentinel errors returned by FireTrapOnDevice for HTTP status mapping.
var (
	ErrTrapExportDisabled  = fmt.Errorf("trap export disabled")
	ErrTrapDeviceNotFound  = fmt.Errorf("device not found")
	ErrTrapEntryNotFound   = fmt.Errorf("trap catalog entry not found")
)

// fireTrapAPIRequests counts POST /api/v1/devices/{ip}/trap hits. Not exposed
// but useful for future diagnostics.
var fireTrapAPIRequests uint64

// WriteTrapStatusJSON writes GetTrapStatus as JSON to w. Extracted for
// testability and because the api.go pattern in this codebase is thin handlers.
func (sm *SimulatorManager) WriteTrapStatusJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sm.GetTrapStatus())
}
