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

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FlowEncoder is the protocol-agnostic interface satisfied by both NetFlow v9
// and IPFIX encoders. uptimeMs is the device uptime in milliseconds at export
// time; IPFIX encoders may use it to compute absolute timestamps.
type FlowEncoder interface {
	EncodePacket(domainID uint32, seqNo uint32, uptimeMs uint32,
		records []FlowRecord, includeTemplate bool, buf []byte) (int, error)
	// PacketSizes returns the three per-packet size constants that Tick() needs
	// to compute batch capacity correctly for each protocol:
	//   baseOverhead — message/packet header + data-set/flowset header (bytes)
	//   templateSize — template set/flowset byte length
	//   recordSize   — bytes per flow record on the wire
	PacketSizes() (baseOverhead int, templateSize int, recordSize int)
}

// FlowTickStats holds per-tick export counters returned by Tick.
// tickAllFlowExporters sums these across all devices and adds them to the
// cumulative atomic counters on SimulatorManager.
type FlowTickStats struct {
	PacketsSent    uint64
	BytesSent      uint64
	RecordsSent    uint64
	LastTemplateMs int64 // unix ms of the most-recent template send this tick; 0 if none
}

// FlowExporter is owned by one DeviceSimulator. It ties the FlowCache and
// encoder together and is driven by the shared SimulatorManager ticker goroutine.
// It has no goroutines of its own — see SimulatorManager.startFlowTicker.
//
// The optional per-device conn (set by the device lifecycle when
// flowSourcePerDevice is enabled) lets each exporter send UDP packets with
// a source IP matching the simulated device, so collectors like OpenNMS
// Telemetryd can attribute flows to the correct node. When conn is nil,
// Tick falls back to the shared SimulatorManager socket.
type FlowExporter struct {
	cache            *FlowCache
	profile          *FlowProfile
	rng              *rand.Rand
	seqNo            uint32
	domainID         uint32        // device IPv4 as uint32 (RFC 7011 §3.1)
	startTime        time.Time     // reference point for SysUptime
	lastTempl        time.Time     // last template transmission time
	templateInterval time.Duration
	// conn is the per-device UDP socket (nil = use shared conn). atomic.Pointer
	// so Tick (ticker goroutine) and Close (device-shutdown paths) can read and
	// clear it without racing. Callers must use Load/Store/Swap — never touch
	// the field by address.
	conn atomic.Pointer[net.UDPConn]
}

// NewFlowExporter creates a FlowExporter for device, using profile to drive
// synthetic flow generation. The RNG is seeded from the device's domainID so
// each device produces distinct but deterministic traffic patterns.
func NewFlowExporter(device *DeviceSimulator, profile *FlowProfile, activeTimeout, inactiveTimeout, templateInterval time.Duration) *FlowExporter {
	var domainID uint32
	if ip4 := device.IP.To4(); ip4 != nil {
		domainID = binary.BigEndian.Uint32(ip4)
	}
	return &FlowExporter{
		cache:            NewFlowCache(activeTimeout, inactiveTimeout, profile.MaxFlows),
		profile:          profile,
		rng:              rand.New(rand.NewSource(int64(domainID))),
		domainID:         domainID,
		startTime:        time.Now(),
		templateInterval: templateInterval,
	}
}

// Close releases the per-device UDP socket, if one was opened. Safe to call
// on a nil or already-closed FlowExporter; safe to call multiple times and
// concurrently with Tick (Swap atomically claims the conn so only one caller
// ever observes it non-nil).
func (fe *FlowExporter) Close() error {
	if fe == nil {
		return nil
	}
	conn := fe.conn.Swap(nil)
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// Tick is called by the shared SimulatorManager ticker goroutine on every
// flowTickInterval. It replenishes the flow cache to ConcurrentFlows, expires
// aged records, and emits one or more UDP datagrams to collectorAddr.
//
// When fe.conn is non-nil (per-device mode) it is used for the WriteTo; the
// passed-in conn is the shared fallback used when the per-device socket
// could not be opened or per-device mode is disabled.
//
// bufPool must supply []byte slices of at least 1500 bytes.
// Write errors are ignored (best-effort delivery; collector may be down).
// The returned FlowTickStats are summed by tickAllFlowExporters into the
// cumulative atomic counters on SimulatorManager.
func (fe *FlowExporter) Tick(now time.Time, encoder FlowEncoder, conn *net.UDPConn, collectorAddr *net.UDPAddr, bufPool *sync.Pool) FlowTickStats {
	uptimeMs := uint32(now.Sub(fe.startTime).Milliseconds())
	deviceIP := domainIDtoIP(fe.domainID)

	// Prefer the per-device socket (source IP = device IP) when set; fall back
	// to the shared SimulatorManager socket so callers that don't use
	// per-device binding (tests, ns-disabled deployments) still work.
	// atomic Load pairs with Swap in Close — Tick never observes a torn pointer.
	writeConn := fe.conn.Load()
	if writeConn == nil {
		writeConn = conn
	}
	if writeConn == nil {
		return FlowTickStats{}
	}

	// Replenish cache to the configured ConcurrentFlows level.
	fe.cache.GenerateFlows(fe.profile, deviceIP, fe.rng, now, uptimeMs)

	// Collect all records that crossed an active or inactive timeout boundary.
	expired := fe.cache.Expire(now)

	sendTemplate := fe.seqNo == 0 || now.Sub(fe.lastTempl) >= fe.templateInterval
	if len(expired) == 0 && !sendTemplate {
		return FlowTickStats{}
	}

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	var stats FlowTickStats

	// Paginate: send as many records as fit in each 1500-byte UDP datagram.
	// Capacity depends on the active encoder's protocol (NF9: 45B/record,
	// IPFIX: 53B/record), so we ask the encoder for its sizes rather than
	// hard-coding NF9 constants here.
	baseOverhead, templSize, recSize := encoder.PacketSizes()
	for {
		overhead := baseOverhead
		if sendTemplate {
			overhead += templSize
		}
		var batch []FlowRecord
		if len(buf) >= overhead+recSize {
			cap := (len(buf) - overhead) / recSize
			if cap >= len(expired) {
				batch = expired
				expired = nil
			} else {
				batch = expired[:cap]
				expired = expired[cap:]
			}
		}

		if len(batch) == 0 && !sendTemplate {
			break
		}

		n, err := encoder.EncodePacket(fe.domainID, fe.seqNo, uptimeMs, batch, sendTemplate, buf)
		if err != nil || n == 0 {
			break
		}

		writeConn.WriteTo(buf[:n], collectorAddr) //nolint:errcheck
		stats.PacketsSent++
		stats.BytesSent += uint64(n)
		stats.RecordsSent += uint64(len(batch))
		fe.seqNo++
		if sendTemplate {
			fe.lastTempl = now
			stats.LastTemplateMs = now.UnixMilli()
			sendTemplate = false
		}

		if len(expired) == 0 {
			break
		}
	}
	return stats
}

// SetFlowSourcePerDevice toggles per-device UDP source IP binding. When true,
// each device opens its own UDP socket inside the opensim namespace bound to
// the device's IP, so collectors see per-device exporter IPs rather than the
// container host IP. Must be called before InitFlowExport.
func (sm *SimulatorManager) SetFlowSourcePerDevice(enabled bool) {
	sm.flowSourcePerDevice = enabled
}

// openFlowConnForDevice opens a per-device UDP socket bound to the device's
// IP (ephemeral source port) and assigns it to device.flowExporter.conn.
// Silently falls through to the shared socket when:
//   - per-device mode is disabled,
//   - namespace isolation is off (device.netNamespace == nil),
//   - or the bind fails (typically because the opensim ns has no route to
//     the collector — see issue #36).
//
// Best-effort: a failed per-device bind logs once and the exporter keeps
// working via the shared socket.
func (sm *SimulatorManager) openFlowConnForDevice(device *DeviceSimulator) {
	if !sm.flowSourcePerDevice || device.flowExporter == nil {
		return
	}
	if device.netNamespace == nil {
		return
	}
	addr := &net.UDPAddr{IP: device.IP, Port: 0}
	conn, err := device.netNamespace.ListenUDPInNamespace(addr)
	if err != nil {
		log.Printf("flow export: device %s per-device bind failed, falling back to shared socket: %v", device.IP, err)
		return
	}
	conn.SetWriteBuffer(65536)
	device.flowExporter.conn.Store(conn)
}

// domainIDtoIP converts a uint32 ObservationDomainID back to a net.IP.
func domainIDtoIP(id uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, id)
	return ip
}

// InitFlowExport opens a shared UDP socket, selects an encoder, and starts the
// shared ticker goroutine. Call once after NewSimulatorManagerWithOptions.
//
// collectorAddr is "host:port" (e.g. "192.168.1.100:2055").
// protocol selects the wire format: "netflow9" (default), "ipfix", or
// "netflow5". Aliases "nf9", "ipfix10", and "nf5" are also accepted.
//
// Call SetFlowSourcePerDevice beforehand to enable per-device source IP binding.
func (sm *SimulatorManager) InitFlowExport(collectorAddr, protocol string, activeTimeout, inactiveTimeout, templateInterval, tickInterval time.Duration) error {
	if sm.flowActive.Load() {
		return fmt.Errorf("flow export: already active; call Shutdown() before re-initializing")
	}

	addr, err := net.ResolveUDPAddr("udp4", collectorAddr)
	if err != nil {
		return fmt.Errorf("flow export: invalid collector address %q: %w", collectorAddr, err)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return fmt.Errorf("flow export: failed to open UDP socket: %w", err)
	}

	var enc FlowEncoder
	var canonicalProtocol string
	switch strings.ToLower(protocol) {
	case "netflow9", "nf9", "":
		enc = NetFlow9Encoder{}
		canonicalProtocol = "netflow9"
	case "ipfix", "ipfix10":
		enc = IPFIXEncoder{}
		canonicalProtocol = "ipfix"
	case "netflow5", "nf5":
		enc = &NetFlow5Encoder{}
		canonicalProtocol = "netflow5"
	default:
		conn.Close()
		return fmt.Errorf("flow export: unknown protocol %q (supported: netflow9, ipfix, netflow5)", protocol)
	}

	sm.flowConn = conn
	sm.flowCollectorAddr = addr
	sm.flowCollectorStr = collectorAddr
	sm.flowProtocol = canonicalProtocol
	sm.flowEncoder = enc
	sm.flowActiveTimeout = activeTimeout
	sm.flowInactiveTimeout = inactiveTimeout
	sm.flowTemplateInterval = templateInterval
	sm.flowTickInterval = tickInterval
	sm.flowBufPool.New = func() interface{} {
		buf := make([]byte, 1500)
		return buf
	}
	sm.flowStopCh = make(chan struct{})
	sm.flowStopOnce = sync.Once{}
	sm.flowActive.Store(true)

	log.Printf("Flow export: %s → %s (protocol: %s, tick: %s, active-timeout: %s)",
		conn.LocalAddr(), collectorAddr, protocol, tickInterval, activeTimeout)

	sm.startFlowTicker()
	return nil
}

// startFlowTicker launches a single background goroutine that calls Tick on
// every active device's FlowExporter at flowTickInterval. The goroutine exits
// when flowStopCh is closed.
func (sm *SimulatorManager) startFlowTicker() {
	sm.flowWg.Add(1)
	go func() {
		defer sm.flowWg.Done()
		ticker := time.NewTicker(sm.flowTickInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				sm.tickAllFlowExporters(now)
			case <-sm.flowStopCh:
				return
			}
		}
	}()
}

// tickAllFlowExporters calls Tick on every device that has a FlowExporter.
// It takes a read lock to snapshot the device list, then releases it before
// calling Tick to avoid holding the lock during I/O.
func (sm *SimulatorManager) tickAllFlowExporters(now time.Time) {
	sm.mu.RLock()
	exporters := make([]*FlowExporter, 0, len(sm.devices))
	for _, d := range sm.devices {
		if d.flowExporter != nil {
			exporters = append(exporters, d.flowExporter)
		}
	}
	// Snapshot shared transport fields under the lock to avoid racing with Shutdown.
	conn := sm.flowConn
	collectorAddr := sm.flowCollectorAddr
	encoder := sm.flowEncoder
	sm.mu.RUnlock()

	if conn == nil {
		return // flow export was shut down between tick and snapshot
	}

	var totalPackets, totalBytes, totalRecords uint64
	var lastTemplMs int64
	for _, fe := range exporters {
		s := fe.Tick(now, encoder, conn, collectorAddr, &sm.flowBufPool)
		totalPackets += s.PacketsSent
		totalBytes += s.BytesSent
		totalRecords += s.RecordsSent
		if s.LastTemplateMs > lastTemplMs {
			lastTemplMs = s.LastTemplateMs
		}
	}
	if totalPackets > 0 {
		sm.flowStatPackets.Add(totalPackets)
		sm.flowStatBytes.Add(totalBytes)
		sm.flowStatRecords.Add(totalRecords)
	}
	if lastTemplMs > 0 {
		sm.flowStatLastTmpl.Store(lastTemplMs)
	}
}

// GetFlowStatus returns a snapshot of the current flow export state and
// cumulative counters. Returns {Enabled: false} when flow export is off.
func (sm *SimulatorManager) GetFlowStatus() FlowStatus {
	if !sm.flowActive.Load() {
		return FlowStatus{Enabled: false}
	}

	sm.mu.RLock()
	devicesExporting := 0
	for _, d := range sm.devices {
		if d.flowExporter != nil {
			devicesExporting++
		}
	}
	sm.mu.RUnlock()

	var lastTemplate string
	if ms := sm.flowStatLastTmpl.Load(); ms > 0 {
		lastTemplate = time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
	}

	return FlowStatus{
		Enabled:            true,
		Protocol:           sm.flowProtocol,
		Collector:          sm.flowCollectorStr,
		TotalFlowsExported: sm.flowStatRecords.Load(),
		TotalPacketsSent:   sm.flowStatPackets.Load(),
		TotalBytesSent:     sm.flowStatBytes.Load(),
		DevicesExporting:   devicesExporting,
		LastTemplateSend:   lastTemplate,
	}
}
