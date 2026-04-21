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
	"sort"
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
	Enabled                    bool                         `json:"enabled"`
	Format                     string                       `json:"format,omitempty"`
	Collector                  string                       `json:"collector,omitempty"`
	Sent                       uint64                       `json:"sent"`
	SendFailures               uint64                       `json:"send_failures"`
	RateLimiterTokensAvailable int                          `json:"rate_limiter_tokens_available,omitempty"`
	DevicesExporting           int                          `json:"devices_exporting"`
	CatalogsByType             map[string]CatalogSourceInfo `json:"catalogs_by_type,omitempty"`
}

// Sentinel errors returned by FireSyslogOnDevice for HTTP status mapping.
var (
	ErrSyslogExportDisabled = fmt.Errorf("syslog export disabled")
	ErrSyslogDeviceNotFound = fmt.Errorf("device not found")
	ErrSyslogEntryNotFound  = fmt.Errorf("syslog catalog entry not found")
)

// SyslogEntryNotFoundError is returned by FireSyslogOnDevice when the
// catalog entry name is unknown for the device's resolved catalog. Shape
// mirrors TrapEntryNotFoundError.
type SyslogEntryNotFoundError struct {
	Name    string
	Catalog string
	Entries []string
}

func (e *SyslogEntryNotFoundError) Error() string {
	return fmt.Sprintf("%s: %q (catalog: %s)", ErrSyslogEntryNotFound.Error(), e.Name, e.Catalog)
}
func (e *SyslogEntryNotFoundError) Unwrap() error { return ErrSyslogEntryNotFound }

// syslogCatalogSource returns the `source` string for a CatalogSourceInfo
// on the syslog side (mirror of trapCatalogSource).
func syslogCatalogSource(slug, catalogFlagPath string) string {
	if catalogFlagPath != "" {
		return "override:" + catalogFlagPath
	}
	if slug == universalCatalogKey {
		return "embedded"
	}
	return fmt.Sprintf("file:%s/%s/syslog.json", trapCatalogResourceDir, slug)
}

// resolvedSyslogCatalogLabel returns the catalogsByType key resolved for
// the given IP — either a device-type slug or "_universal".
func (sm *SimulatorManager) resolvedSyslogCatalogLabel(ip string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.syslogCatalogsByType == nil {
		return universalCatalogKey
	}
	if slug, ok := sm.deviceTypesByIP[ip]; ok {
		if _, found := sm.syslogCatalogsByType[slug]; found {
			return slug
		}
	}
	return universalCatalogKey
}

// sortedSyslogEntryNames returns the catalog's entry names alphabetically.
func sortedSyslogEntryNames(cat *SyslogCatalog) []string {
	if cat == nil {
		return nil
	}
	out := make([]string, 0, len(cat.Entries))
	for _, e := range cat.Entries {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

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
	// Mirror NewSyslogScheduler's own floor: sub-millisecond intervals
	// busy-loop the scheduler when no global cap is set. Catch it here with
	// a clean startup error rather than let the scheduler constructor panic.
	if cfg.Interval < time.Millisecond {
		return fmt.Errorf("syslog export: -syslog-interval must be >= 1ms, got %s", cfg.Interval)
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

	// Per-device-type overlay scan. Skipped when `-syslog-catalog` is set.
	catalogsByType := map[string]*SyslogCatalog{
		universalCatalogKey: catalog,
	}
	if cfg.CatalogPath == "" {
		perType, scanErr := ScanPerTypeSyslogCatalogs(catalog, trapCatalogResourceDir)
		if scanErr != nil {
			return fmt.Errorf("syslog export: scanning per-type catalogs: %w", scanErr)
		}
		for slug, c := range perType {
			catalogsByType[slug] = c
		}
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
		CatalogFor:         func(ip net.IP) *SyslogCatalog { return sm.SyslogCatalogFor(ip.String()) },
		MeanInterval:       cfg.Interval,
		GlobalCapPerSecond: cfg.GlobalCap,
	})

	sm.mu.Lock()
	sm.syslogCatalog = catalog
	sm.syslogCatalogsByType = catalogsByType
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

	// Snapshot per-device exporters under each device's own mutex so we
	// don't race `startDeviceSyslogExporter` (which writes under d.mu) or
	// direct reads from `GetSyslogStatus` / `FireSyslogOnDevice`.
	sm.mu.RLock()
	scheduler := sm.syslogScheduler
	type devExp struct {
		d   *DeviceSimulator
		exp *SyslogExporter
	}
	captured := make([]devExp, 0, len(sm.devices))
	for _, d := range sm.devices {
		d.mu.Lock()
		exp := d.syslogExporter
		d.syslogExporter = nil
		d.mu.Unlock()
		if exp != nil {
			captured = append(captured, devExp{d: d, exp: exp})
		}
	}
	conn := sm.syslogConn
	sm.mu.RUnlock()

	if scheduler != nil {
		scheduler.Stop()
	}
	for _, ce := range captured {
		_ = ce.exp.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}

	sm.mu.Lock()
	sm.syslogScheduler = nil
	sm.syslogConn = nil
	// Clear per-type catalog state so a subsequent StartSyslogExport
	// rebuilds it from scratch rather than inheriting stale overlays.
	sm.syslogCatalog = nil
	sm.syslogCatalogsByType = nil
	sm.syslogCatalogPath = ""
	sm.mu.Unlock()
}

// startDeviceSyslogExporter creates a SyslogExporter for the device, opens a
// per-device UDP socket when enabled, and registers the exporter with the
// scheduler. Called from device creation sites in device.go (mirrors the
// trap-export hook).
//
// Unlike the trap counterpart, per-device bind failure is never fatal for
// syslog — there is no ack path that requires symmetric source IPs. On bind
// failure we log a warning and fall back to the shared socket. Every error
// scenario inside this function is handled internally (logged + degraded),
// so the function has no error return; the trap-style signature was trimmed
// after a review noted the call-site `err != nil` log branches were dead.
func (sm *SimulatorManager) startDeviceSyslogExporter(device *DeviceSimulator) {
	if !sm.syslogActive.Load() || device == nil {
		return
	}
	// `device.sysName` is written once at device construction and never
	// mutated (the simulator doesn't expose sysName-set via the admin API),
	// so reading it here without holding `device.mu` is safe. Capture it
	// into the exporter options as a plain string; subsequent runtime
	// changes to `device.sysName` — if ever introduced — would not be
	// reflected in emitted syslog messages until the exporter is rebuilt.
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
	if len(sm.syslogCatalogsByType) > 0 {
		status.CatalogsByType = make(map[string]CatalogSourceInfo, len(sm.syslogCatalogsByType))
		for slug, cat := range sm.syslogCatalogsByType {
			status.CatalogsByType[slug] = CatalogSourceInfo{
				Entries: len(cat.Entries),
				Source:  syslogCatalogSource(slug, sm.syslogCatalogPath),
			}
		}
	}

	var sent, failures uint64
	devicesExporting := 0
	for _, d := range sm.devices {
		// Snapshot the exporter pointer under d.mu so a concurrent Stop
		// path (which nils it under d.mu.Lock) can't race this read.
		d.mu.RLock()
		exp := d.syslogExporter
		d.mu.RUnlock()
		if exp == nil {
			continue
		}
		devicesExporting++
		st := exp.Stats()
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
	device := sm.FindDeviceByIP(ip)
	if device == nil {
		return fmt.Errorf("%w: %q", ErrSyslogDeviceNotFound, ip)
	}
	cat := sm.SyslogCatalogFor(ip)
	if cat == nil {
		return fmt.Errorf("%w: no catalog resolved for %s", ErrSyslogExportDisabled, ip)
	}
	entry, ok := cat.ByName[entryName]
	if !ok {
		return &SyslogEntryNotFoundError{
			Name:    entryName,
			Catalog: sm.resolvedSyslogCatalogLabel(ip),
			Entries: sortedSyslogEntryNames(cat),
		}
	}
	// Snapshot the exporter pointer under d.mu so a concurrent Stop can't
	// nil it between the guard check and the Fire call (TOCTOU).
	device.mu.RLock()
	exp := device.syslogExporter
	device.mu.RUnlock()
	if exp == nil {
		return fmt.Errorf("%w: device %s has no syslog exporter", ErrSyslogExportDisabled, ip)
	}
	return exp.Fire(entry, overrides)
}

// WriteSyslogStatusJSON writes GetSyslogStatus as JSON to w. Extracted for
// testability and because the api.go pattern in this codebase is thin handlers.
func (sm *SimulatorManager) WriteSyslogStatusJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sm.GetSyslogStatus())
}

// SyslogCatalogFor returns the syslog catalog to use for the device with the
// given IP. Resolution order: device-IP → type-slug → syslogCatalogsByType
// → syslogCatalogsByType["_universal"] (the universal). Symmetric with
// `CatalogFor` on the trap side.
func (sm *SimulatorManager) SyslogCatalogFor(ip string) *SyslogCatalog {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.syslogCatalogsByType == nil {
		return sm.syslogCatalog
	}
	if slug, ok := sm.deviceTypesByIP[ip]; ok {
		if cat, found := sm.syslogCatalogsByType[slug]; found {
			return cat
		}
	}
	// See CatalogFor above — mirror the prefer-universal-then-legacy
	// fallback so non-nil is guaranteed while any catalog is live.
	if cat := sm.syslogCatalogsByType[universalCatalogKey]; cat != nil {
		return cat
	}
	return sm.syslogCatalog
}

