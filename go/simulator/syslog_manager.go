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

// SimulatorManager-level UDP syslog export lifecycle.
//
// Parses the SyslogConfig surfaced by CLI flags, loads the catalog (embedded
// or file override), creates the shared SyslogScheduler + SyslogEncoder, and
// starts the scheduler goroutine. Wires per-device SyslogExporters into
// device startup/teardown (see device.go) and exposes GetSyslogStatus +
// FireSyslogOnDevice for the HTTP API.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// SyslogConfig bundles CLI-derived configuration for the syslog subsystem.
// Empty Collector disables the feature.
type SyslogConfig struct {
	Collector       string
	Format          SyslogFormat
	Interval        time.Duration
	GlobalCap       int // 0 = unlimited
	CatalogPath     string
	SourcePerDevice bool
}

// SyslogStatus is the JSON body returned by GET /api/v1/syslog/status. Field
// shape matches spec Requirement "Syslog status HTTP endpoint".
type SyslogStatus struct {
	Enabled                    bool   `json:"enabled"`
	Format                     string `json:"format,omitempty"`
	Collector                  string `json:"collector,omitempty"`
	Sent                       uint64 `json:"sent"`
	SendFailures               uint64 `json:"send_failures"`
	RateLimiterTokensAvailable int    `json:"rate_limiter_tokens_available,omitempty"`
	DevicesExporting           int    `json:"devices_exporting"`
}

// Sentinel errors returned by FireSyslogOnDevice for HTTP status mapping.
var (
	ErrSyslogExportDisabled = fmt.Errorf("syslog export disabled")
	ErrSyslogDeviceNotFound = fmt.Errorf("device not found")
	ErrSyslogEntryNotFound  = fmt.Errorf("syslog catalog entry not found")
)

// StartSyslogExport validates cfg, loads the catalog, creates the shared
// scheduler, and starts the scheduler goroutine. Partial state on failure
// is unwound before returning.
//
// Preconditions:
//   - Called after manager construction, before any device creation that
//     should participate in syslog export.
func (sm *SimulatorManager) StartSyslogExport(cfg SyslogConfig) error {
	if sm.syslogActive.Load() {
		return fmt.Errorf("syslog export: already active; Shutdown before re-initializing")
	}
	if cfg.Collector == "" {
		return fmt.Errorf("syslog export: -syslog-collector required to enable feature")
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("syslog export: -syslog-interval must be positive, got %s", cfg.Interval)
	}
	if cfg.GlobalCap < 0 {
		return fmt.Errorf("syslog export: -syslog-global-cap must be non-negative, got %d", cfg.GlobalCap)
	}
	if cfg.Format == "" {
		cfg.Format = SyslogFormat5424
	}
	encoder, err := NewSyslogEncoder(cfg.Format)
	if err != nil {
		return fmt.Errorf("syslog export: %w", err)
	}

	addr, err := net.ResolveUDPAddr("udp4", cfg.Collector)
	if err != nil {
		return fmt.Errorf("syslog export: invalid collector address %q: %w", cfg.Collector, err)
	}

	var catalog *SyslogCatalog
	if cfg.CatalogPath == "" {
		catalog, err = LoadEmbeddedSyslogCatalog()
	} else {
		catalog, err = LoadSyslogCatalogFromFile(cfg.CatalogPath)
	}
	if err != nil {
		return err
	}

	// Shared fallback socket: used when per-device binding is off or fails.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return fmt.Errorf("syslog export: failed to open shared UDP socket: %w", err)
	}

	var limiter *rate.Limiter
	if cfg.GlobalCap > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.GlobalCap), cfg.GlobalCap)
	}

	scheduler := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:            catalog,
		MeanInterval:       cfg.Interval,
		GlobalCapPerSecond: cfg.GlobalCap,
	})

	sm.mu.Lock()
	sm.syslogCatalog = catalog
	sm.syslogScheduler = scheduler
	sm.syslogEncoder = encoder
	sm.syslogLimiter = limiter
	sm.syslogConn = conn
	sm.syslogCollectorAddr = addr
	sm.syslogCollectorStr = cfg.Collector
	sm.syslogFormat = cfg.Format
	sm.syslogInterval = cfg.Interval
	sm.syslogGlobalCap = cfg.GlobalCap
	sm.syslogSourcePerDevice = cfg.SourcePerDevice
	sm.syslogCatalogPath = cfg.CatalogPath
	sm.mu.Unlock()
	sm.syslogActive.Store(true)

	capStr := "unlimited"
	if cfg.GlobalCap > 0 {
		capStr = fmt.Sprintf("%d/s", cfg.GlobalCap)
	}
	catStr := "<embedded>"
	if cfg.CatalogPath != "" {
		catStr = cfg.CatalogPath
	}
	log.Printf("Syslog export: %s → %s (format=%s, interval=%s, cap=%s, catalog=%s, per-device-source=%v)",
		conn.LocalAddr(), cfg.Collector, cfg.Format, cfg.Interval, capStr, catStr, cfg.SourcePerDevice)

	go scheduler.Run(context.Background())

	return nil
}

// StopSyslogExport stops the scheduler, closes the shared socket, and closes
// every device's SyslogExporter. Safe to call when syslog export is inactive
// (no-op).
func (sm *SimulatorManager) StopSyslogExport() {
	if !sm.syslogActive.Load() {
		return
	}
	sm.syslogActive.Store(false)

	sm.mu.RLock()
	scheduler := sm.syslogScheduler
	devices := make([]*DeviceSimulator, 0, len(sm.devices))
	for _, d := range sm.devices {
		if d.syslogExporter != nil {
			devices = append(devices, d)
		}
	}
	conn := sm.syslogConn
	sm.mu.RUnlock()

	if scheduler != nil {
		scheduler.Stop()
	}
	for _, d := range devices {
		if d.syslogExporter != nil {
			_ = d.syslogExporter.Close()
			d.syslogExporter = nil
		}
	}
	if conn != nil {
		_ = conn.Close()
	}

	sm.mu.Lock()
	sm.syslogScheduler = nil
	sm.syslogConn = nil
	sm.mu.Unlock()
}

// startDeviceSyslogExporter creates a SyslogExporter for the device, opens a
// per-device UDP socket when enabled, and registers the exporter with the
// scheduler. Called from device creation sites in device.go (mirrors the
// trap-export hook).
//
// Unlike the trap counterpart, per-device bind failure is never fatal for
// syslog — there is no ack path that requires symmetric source IPs. On bind
// failure we log a warning and fall back to the shared socket.
func (sm *SimulatorManager) startDeviceSyslogExporter(device *DeviceSimulator) error {
	if !sm.syslogActive.Load() || device == nil {
		return nil
	}
	sm.mu.RLock()
	opts := SyslogExporterOptions{
		DeviceIP:   device.IP,
		Encoder:    sm.syslogEncoder,
		Collector:  sm.syslogCollectorAddr,
		SharedConn: sm.syslogConn,
		SysName:    device.sysName,
		IfIndexFn:  deviceIfIndexFn(device),
		IfNameFn:   deviceIfNameFn(device),
	}
	sourcePerDevice := sm.syslogSourcePerDevice
	scheduler := sm.syslogScheduler
	sm.mu.RUnlock()

	exporter := NewSyslogExporter(opts)

	if sourcePerDevice {
		conn := openSyslogConnForDevice(device)
		if conn == nil {
			log.Printf("syslog export: device %s per-device bind failed, falling back to shared socket", device.IP)
		} else {
			exporter.SetConn(conn)
		}
	}

	device.mu.Lock()
	device.syslogExporter = exporter
	device.mu.Unlock()

	if scheduler != nil {
		scheduler.Register(device.IP, exporter)
	}
	return nil
}

// deviceIfNameFn returns the ifName for a given ifIndex on the device.
//
// For PR3 scope we synthesise a generic Cisco-style name (`GigabitEthernet0/N`)
// rather than looking up the real ifDescr from the device's SNMP OID table.
// A follow-up PR can replace this with a real lookup against the device's
// response cache when per-device-type catalog realism becomes in scope; for
// the v1 generic catalog the synthesised name is adequate and keeps this
// change decoupled from the device-simulation internals.
func deviceIfNameFn(_ *DeviceSimulator) func(int) string {
	return func(ifIndex int) string {
		if ifIndex <= 0 {
			return ""
		}
		return fmt.Sprintf("GigabitEthernet0/%d", ifIndex)
	}
}

// GetSyslogStatus returns a JSON-serializable snapshot of the syslog export
// state. Exposed via GET /api/v1/syslog/status.
func (sm *SimulatorManager) GetSyslogStatus() SyslogStatus {
	if !sm.syslogActive.Load() {
		return SyslogStatus{Enabled: false}
	}

	sm.mu.RLock()
	status := SyslogStatus{
		Enabled:   true,
		Format:    string(sm.syslogFormat),
		Collector: sm.syslogCollectorStr,
	}
	limiter := sm.syslogLimiter

	var sent, failures uint64
	devicesExporting := 0
	for _, d := range sm.devices {
		if d.syslogExporter == nil {
			continue
		}
		devicesExporting++
		st := d.syslogExporter.Stats()
		sent += st.Sent.Load()
		failures += st.SendFailures.Load()
	}
	var tokens int
	if limiter != nil {
		tokens = int(limiter.Tokens())
	}
	sm.mu.RUnlock()

	status.Sent = sent
	status.SendFailures = failures
	status.DevicesExporting = devicesExporting
	if limiter != nil {
		status.RateLimiterTokensAvailable = tokens
	}
	return status
}

// FireSyslogOnDevice implements POST /api/v1/devices/{ip}/syslog by looking
// up the device's SyslogExporter and invoking Fire with the given catalog
// name. On-demand fires bypass the global rate limiter (pre-flight 1.4
// resolution) — the scheduler is the only consumer of the limiter; direct
// Fire calls write immediately.
//
// Returns errors tagged so the HTTP layer can map them:
//   - ErrSyslogExportDisabled → 503 Service Unavailable
//   - ErrSyslogDeviceNotFound → 404
//   - ErrSyslogEntryNotFound  → 400
func (sm *SimulatorManager) FireSyslogOnDevice(ip, entryName string, overrides map[string]string) error {
	if !sm.syslogActive.Load() {
		return ErrSyslogExportDisabled
	}
	sm.mu.RLock()
	cat := sm.syslogCatalog
	sm.mu.RUnlock()

	entry, ok := cat.ByName[entryName]
	if !ok {
		return fmt.Errorf("%w: %q", ErrSyslogEntryNotFound, entryName)
	}
	device := sm.FindDeviceByIP(ip)
	if device == nil {
		return fmt.Errorf("%w: %q", ErrSyslogDeviceNotFound, ip)
	}
	if device.syslogExporter == nil {
		return fmt.Errorf("%w: device %s has no syslog exporter", ErrSyslogExportDisabled, ip)
	}
	return device.syslogExporter.Fire(entry, overrides)
}

// WriteSyslogStatusJSON writes GetSyslogStatus as JSON to w. Extracted for
// testability and because the api.go pattern in this codebase is thin handlers.
func (sm *SimulatorManager) WriteSyslogStatusJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sm.GetSyslogStatus())
}

