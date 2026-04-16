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
	"time"
)

// FlowEncoder is the protocol-agnostic interface satisfied by both NetFlow v9
// and IPFIX encoders. uptimeMs is the device uptime in milliseconds at export
// time; IPFIX encoders may ignore it (IPFIX uses absolute timestamps).
type FlowEncoder interface {
	EncodePacket(domainID uint32, seqNo uint32, uptimeMs uint32,
		records []FlowRecord, includeTemplate bool, buf []byte) (int, error)
}

// FlowExporter is owned by one DeviceSimulator. It ties the FlowCache and
// encoder together and is driven by the shared SimulatorManager ticker goroutine.
// It has no goroutines of its own — see SimulatorManager.startFlowTicker.
type FlowExporter struct {
	cache            *FlowCache
	profile          *FlowProfile
	rng              *rand.Rand
	seqNo            uint32
	domainID         uint32        // device IPv4 as uint32 (RFC 7011 §3.1)
	startTime        time.Time     // reference point for SysUptime
	lastTempl        time.Time     // last template transmission time
	templateInterval time.Duration
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

// Tick is called by the shared SimulatorManager ticker goroutine on every
// flowTickInterval. It replenishes the flow cache to ConcurrentFlows, expires
// aged records, and emits one or more UDP datagrams to collectorAddr.
//
// bufPool must supply []byte slices of at least 1500 bytes.
// Write errors are ignored (best-effort delivery; collector may be down).
func (fe *FlowExporter) Tick(now time.Time, encoder FlowEncoder, conn *net.UDPConn, collectorAddr *net.UDPAddr, bufPool *sync.Pool) {
	uptimeMs := uint32(now.Sub(fe.startTime).Milliseconds())
	deviceIP := domainIDtoIP(fe.domainID)

	// Replenish cache to the configured ConcurrentFlows level.
	fe.cache.GenerateFlows(fe.profile, deviceIP, fe.rng, now, uptimeMs)

	// Collect all records that crossed an active or inactive timeout boundary.
	expired := fe.cache.Expire(now)

	sendTemplate := fe.seqNo == 0 || now.Sub(fe.lastTempl) >= fe.templateInterval
	if len(expired) == 0 && !sendTemplate {
		return
	}

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	// Paginate: send as many records as fit in each 1500-byte UDP datagram.
	// With a 20-byte packet header, 80-byte template, and 45 bytes per record,
	// one 1500-byte datagram carries up to 30 records. Most ticks produce fewer.
	for {
		overhead := nf9HeaderSize + 4 // NF9 packet header + data FlowSet header
		if sendTemplate {
			overhead += nf9TemplFlowSetSize
		}
		var batch []FlowRecord
		if len(buf) >= overhead+nf9RecordSize {
			cap := (len(buf) - overhead) / nf9RecordSize
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

		conn.WriteTo(buf[:n], collectorAddr) //nolint:errcheck
		fe.seqNo++
		if sendTemplate {
			fe.lastTempl = now
			sendTemplate = false
		}

		if len(expired) == 0 {
			break
		}
	}
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
// protocol is "netflow9" (the only supported value for Phase 2).
func (sm *SimulatorManager) InitFlowExport(collectorAddr, protocol string, activeTimeout, inactiveTimeout, templateInterval, tickInterval time.Duration) error {
	addr, err := net.ResolveUDPAddr("udp4", collectorAddr)
	if err != nil {
		return fmt.Errorf("flow export: invalid collector address %q: %w", collectorAddr, err)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return fmt.Errorf("flow export: failed to open UDP socket: %w", err)
	}

	var enc FlowEncoder
	switch strings.ToLower(protocol) {
	case "netflow9", "nf9", "":
		enc = NetFlow9Encoder{}
	default:
		conn.Close()
		return fmt.Errorf("flow export: unknown protocol %q (supported: netflow9)", protocol)
	}

	sm.flowConn = conn
	sm.flowCollectorAddr = addr
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
	sm.flowActive = true

	log.Printf("Flow export: %s → %s (protocol: %s, tick: %s, active-timeout: %s)",
		conn.LocalAddr(), collectorAddr, protocol, tickInterval, activeTimeout)

	sm.startFlowTicker()
	return nil
}

// startFlowTicker launches a single background goroutine that calls Tick on
// every active device's FlowExporter at flowTickInterval. The goroutine exits
// when flowStopCh is closed.
func (sm *SimulatorManager) startFlowTicker() {
	go func() {
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
	sm.mu.RUnlock()

	for _, fe := range exporters {
		fe.Tick(now, sm.flowEncoder, sm.flowConn, sm.flowCollectorAddr, &sm.flowBufPool)
	}
}
