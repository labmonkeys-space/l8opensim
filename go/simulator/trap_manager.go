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
	"sort"
	"strings"
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
//
// CatalogsByType surfaces the per-device-type overlay map so operators can
// see at a glance which vendor catalogs are active. Keys are device-type
// slugs (plus the reserved `_fallback` entry for the universal catalog);
// values describe each catalog's entry count and source (embedded vs file
// override).
type TrapStatus struct {
	Enabled                    bool                          `json:"enabled"`
	Mode                       string                        `json:"mode,omitempty"`
	Collector                  string                        `json:"collector,omitempty"`
	Community                  string                        `json:"community,omitempty"`
	Sent                       uint64                        `json:"sent"`
	InformsPending             uint64                        `json:"informs_pending,omitempty"`
	InformsAcked               uint64                        `json:"informs_acked,omitempty"`
	InformsFailed              uint64                        `json:"informs_failed,omitempty"`
	InformsDropped             uint64                        `json:"informs_dropped,omitempty"`
	RateLimiterTokensAvailable int                           `json:"rate_limiter_tokens_available,omitempty"`
	DevicesExporting           int                           `json:"devices_exporting"`
	CatalogsByType             map[string]CatalogSourceInfo  `json:"catalogs_by_type,omitempty"`
}

// CatalogSourceInfo describes one entry in TrapStatus.CatalogsByType /
// SyslogStatus.CatalogsByType. Shared between trap and syslog since their
// observability shape is identical.
type CatalogSourceInfo struct {
	Entries int    `json:"entries"`
	Source  string `json:"source"` // "embedded", "file:<path>", or "override:<path>"
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

	// Per-device-type overlay scan. Skipped when `-trap-catalog` is set —
	// the flag preserves today's full-replacement contract: every device
	// uses the single file supplied.
	catalogsByType := map[string]*Catalog{
		fallbackCatalogKey: catalog,
	}
	if cfg.CatalogPath == "" {
		perType, scanErr := ScanPerTypeTrapCatalogs(catalog, trapCatalogResourceDir)
		if scanErr != nil {
			return fmt.Errorf("trap export: scanning per-type catalogs: %w", scanErr)
		}
		for slug, c := range perType {
			catalogsByType[slug] = c
		}
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
		CatalogFor:         func(ip net.IP) *Catalog { return sm.CatalogFor(ip.String()) },
		MeanInterval:       cfg.Interval,
		GlobalCapPerSecond: cfg.GlobalCap,
	})

	sm.mu.Lock()
	sm.trapCatalog = catalog
	sm.trapCatalogsByType = catalogsByType
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
// Exposed via GET /api/v1/traps/status. All reads of manager trap state happen
// under the RLock; downstream-only data (status fields, atomic counters) is
// populated from local snapshots after RUnlock.
func (sm *SimulatorManager) GetTrapStatus() TrapStatus {
	if !sm.trapActive.Load() {
		return TrapStatus{Enabled: false}
	}

	sm.mu.RLock()
	mode := sm.trapMode
	status := TrapStatus{
		Enabled:   true,
		Collector: sm.trapCollectorStr,
		Community: sm.trapCommunity,
	}
	if mode == TrapModeInform {
		status.Mode = "inform"
	} else {
		status.Mode = "trap"
	}
	limiter := sm.trapLimiter
	// Snapshot catalogs-by-type under the lock so reads are race-free.
	if len(sm.trapCatalogsByType) > 0 {
		status.CatalogsByType = make(map[string]CatalogSourceInfo, len(sm.trapCatalogsByType))
		for slug, cat := range sm.trapCatalogsByType {
			status.CatalogsByType[slug] = CatalogSourceInfo{
				Entries: len(cat.Entries),
				Source:  trapCatalogSource(slug, sm.trapCatalogPath),
			}
		}
	}

	var sent, pending, acked, failed, dropped uint64
	devicesExporting := 0
	for _, d := range sm.devices {
		if d.trapExporter == nil {
			continue
		}
		devicesExporting++
		st := d.trapExporter.Stats()
		sent += st.Sent.Load()
		if mode == TrapModeInform {
			pending += uint64(d.trapExporter.PendingInformsLen())
			acked += st.InformsAcked.Load()
			failed += st.InformsFailed.Load()
			dropped += st.InformsDropped.Load()
		}
	}
	// Sample limiter tokens under the lock so we can't race with a concurrent
	// Shutdown that nils sm.trapLimiter after sm.mu.Lock.
	var tokens int
	if limiter != nil {
		tokens = int(limiter.Tokens())
	}
	sm.mu.RUnlock()

	status.Sent = sent
	status.DevicesExporting = devicesExporting
	if mode == TrapModeInform {
		status.InformsPending = pending
		status.InformsAcked = acked
		status.InformsFailed = failed
		status.InformsDropped = dropped
	}
	if limiter != nil {
		status.RateLimiterTokensAvailable = tokens
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

// CatalogFor returns the trap catalog to use for the device with the given
// IP. Resolution order: device-IP → type-slug → `trapCatalogsByType[slug]`
// → `trapCatalogsByType["_fallback"]` (the universal). Safe for concurrent
// use; the hot path is O(1) (two map reads). Returns nil only when trap
// export has never been initialised.
func (sm *SimulatorManager) CatalogFor(ip string) *Catalog {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.trapCatalogsByType == nil {
		return sm.trapCatalog
	}
	if slug, ok := sm.deviceTypesByIP[ip]; ok {
		if cat, found := sm.trapCatalogsByType[slug]; found {
			return cat
		}
	}
	return sm.trapCatalogsByType[fallbackCatalogKey]
}

// fallbackCatalogKey is the reserved slug used in trapCatalogsByType and
// syslogCatalogsByType for the universal (non-per-type) catalog.
const fallbackCatalogKey = "_fallback"

// trapCatalogResourceDir is the root of the per-device-type resource tree.
// Must match the path used by the SNMP/SSH/REST resource loader in
// resources.go so per-type trap catalogs live alongside their siblings.
const trapCatalogResourceDir = "resources"

// trapCatalogSource returns the `source` string for a CatalogSourceInfo,
// distinguishing the three paths that can populate an entry: the
// `-trap-catalog` flag override (`override:<path>`), the per-type file on
// disk (`file:resources/<slug>/traps.json`), or the embedded universal
// catalog (`embedded`). `catalogFlagPath` is the value of `-trap-catalog`;
// empty means the flag was not set.
func trapCatalogSource(slug, catalogFlagPath string) string {
	if catalogFlagPath != "" {
		return "override:" + catalogFlagPath
	}
	if slug == fallbackCatalogKey {
		return "embedded"
	}
	return fmt.Sprintf("file:%s/%s/traps.json", trapCatalogResourceDir, slug)
}

// FireTrapOnDevice implements POST /api/v1/devices/{ip}/trap by looking up the
// device's TrapExporter and invoking Fire with the given catalog name.
// Returns the request-id and nil on success; HTTP status codes are chosen by
// the caller based on the error (see web.go / api.go).
//
// Name resolution uses the DEVICE'S catalog (via CatalogFor), not the
// universal catalog — entries defined only in a per-type overlay are
// reachable here for devices of that type. Unknown entries return
// ErrTrapEntryNotFound wrapped in a TrapEntryNotFoundError carrying the
// resolved-catalog identifier and available entry names so the handler
// can render an actionable 400 body.
//
// Returns errors tagged so the HTTP layer can map them:
//   - ErrTrapExportDisabled → 503 Service Unavailable
//   - ErrTrapDeviceNotFound → 404
//   - ErrTrapEntryNotFound  → 400
func (sm *SimulatorManager) FireTrapOnDevice(ip, trapName string, overrides map[string]string) (uint32, error) {
	if !sm.trapActive.Load() {
		return 0, ErrTrapExportDisabled
	}
	device := sm.FindDeviceByIP(ip)
	if device == nil {
		return 0, fmt.Errorf("%w: %q", ErrTrapDeviceNotFound, ip)
	}
	cat := sm.CatalogFor(ip)
	if cat == nil {
		return 0, fmt.Errorf("%w: no catalog resolved for %s", ErrTrapExportDisabled, ip)
	}
	entry, ok := cat.ByName[trapName]
	if !ok {
		return 0, &TrapEntryNotFoundError{
			Name:    trapName,
			Catalog: sm.resolvedCatalogLabel(ip),
			Entries: sortedTrapEntryNames(cat),
		}
	}
	if device.trapExporter == nil {
		return 0, fmt.Errorf("%w: device %s has no trap exporter", ErrTrapExportDisabled, ip)
	}
	id := device.trapExporter.Fire(entry, overrides)
	if id == 0 {
		return 0, fmt.Errorf("trap fire for %s returned 0 reqID (resolve or write failure)", ip)
	}
	return id, nil
}

// resolvedCatalogLabel returns the catalogsByType key that CatalogFor
// resolved for `ip` — either a device-type slug or "_fallback". Used only
// for HTTP error bodies, so a miss (unknown IP) maps to "_fallback" for a
// sensible message.
func (sm *SimulatorManager) resolvedCatalogLabel(ip string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if sm.trapCatalogsByType == nil {
		return fallbackCatalogKey
	}
	if slug, ok := sm.deviceTypesByIP[ip]; ok {
		if _, found := sm.trapCatalogsByType[slug]; found {
			return slug
		}
	}
	return fallbackCatalogKey
}

// sortedTrapEntryNames returns the catalog's entry names in stable
// alphabetical order — useful for deterministic HTTP 400 bodies that
// tests can assert against.
func sortedTrapEntryNames(cat *Catalog) []string {
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

// TrapEntryNotFoundError is returned when POST /trap references a catalog
// entry name that isn't in the device's resolved catalog. Embeds the
// standard ErrTrapEntryNotFound so existing errors.Is checks keep working.
type TrapEntryNotFoundError struct {
	Name    string
	Catalog string
	Entries []string
}

func (e *TrapEntryNotFoundError) Error() string {
	return fmt.Sprintf("%s: %q (catalog: %s)", ErrTrapEntryNotFound.Error(), e.Name, e.Catalog)
}
func (e *TrapEntryNotFoundError) Unwrap() error { return ErrTrapEntryNotFound }

// Sentinel errors returned by FireTrapOnDevice for HTTP status mapping.
var (
	ErrTrapExportDisabled = fmt.Errorf("trap export disabled")
	ErrTrapDeviceNotFound = fmt.Errorf("device not found")
	ErrTrapEntryNotFound  = fmt.Errorf("trap catalog entry not found")
)

// WriteTrapStatusJSON writes GetTrapStatus as JSON to w. Extracted for
// testability and because the api.go pattern in this codebase is thin handlers.
func (sm *SimulatorManager) WriteTrapStatusJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sm.GetTrapStatus())
}
