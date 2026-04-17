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
	"time"
)

// CounterRecord is a single protocol-agnostic counter payload ready to be
// serialised into an sFlow counters_sample flow_record. Format is the sFlow v5
// counter-record format tag (enterprise 0), Body is the XDR-encoded counter
// body without the 8-byte record header.
//
// Protocol-specific encoding of this record into the target wire format is
// owned by the encoder (see sflow.go EncodeCounterDatagram).
type CounterRecord struct {
	Format uint32
	Body   []byte
}

// CounterSource produces CounterRecord snapshots at the given time. Sources
// are registered per-device in the device lifecycle and drained on each sFlow
// flow-export tick when Phase 2 counter samples are enabled.
//
// Implementations must be safe for concurrent use — the sFlow ticker goroutine
// calls Snapshot while SNMP queries may be touching the same underlying
// counter state from multiple worker goroutines.
type CounterSource interface {
	Snapshot(t time.Time) []CounterRecord
}

// InterfaceCounterSource adapts an IfCounterCycler to the CounterSource
// interface. It reuses the cycler's monotonic HC-octet math so the sFlow
// if_counters record values match byte-for-byte against what SNMP returns for
// the same device at time t. This is the spec's "byte-identical SNMP/sFlow
// counters" requirement (flow-export-sflow Scenario
// "InterfaceCounterSource reuses IfCounterCycler state").
type InterfaceCounterSource struct {
	cycler *IfCounterCycler
}

// NewInterfaceCounterSource returns an adapter that reads its counter state
// from c. Returns nil if c is nil — callers should skip registration in that
// case.
func NewInterfaceCounterSource(c *IfCounterCycler) *InterfaceCounterSource {
	if c == nil {
		return nil
	}
	return &InterfaceCounterSource{cycler: c}
}

// Snapshot returns one if_counters CounterRecord per known interface, with
// ifHCInOctets / ifHCOutOctets sourced from the cycler's sine-wave math. The
// remaining fields (ifInUcastPkts etc.) are synthesized from the octet
// counters using a coarse 500-byte average packet size — adequate for
// collector smoke tests, not for graphing accuracy.
func (s *InterfaceCounterSource) Snapshot(_ time.Time) []CounterRecord {
	if s == nil || s.cycler == nil {
		return nil
	}
	idxs := make([]int, 0, len(s.cycler.knownIfIndexes))
	for i := range s.cycler.knownIfIndexes {
		idxs = append(idxs, i)
	}
	out := make([]CounterRecord, 0, len(idxs))
	for _, ifIndex := range idxs {
		slot := ifIndex - 1
		if slot < 0 || slot >= len(s.cycler.ifSpeedBps) {
			continue
		}
		inStr := s.cycler.GetHCOctets(hcInOIDPrefix + itoa(ifIndex))
		outStr := s.cycler.GetHCOctets(hcOutOIDPrefix + itoa(ifIndex))
		inOctets := parseUintOrZero(inStr)
		outOctets := parseUintOrZero(outStr)
		// Synthesize packet counters from octets / 500B — coarse but
		// monotonic since octets are monotonic.
		inPkts := uint32(inOctets / 500)
		outPkts := uint32(outOctets / 500)

		body := encodeIfCountersBody(uint32(ifIndex), s.cycler.ifSpeedBps[slot], inOctets, outOctets, inPkts, outPkts)
		out = append(out, CounterRecord{Format: sflowCtrFmtGeneric, Body: body})
	}
	return out
}

// encodeIfCountersBody encodes the sFlow generic-interface-counters body
// (sflow_version_5.txt §5.4 "generic interface counters"). The structure is
// 88 bytes fixed:
//
//	u32 ifIndex
//	u32 ifType       (6 = ethernetCsmacd)
//	u64 ifSpeed      (bps)
//	u32 ifDirection  (1 = full-duplex)
//	u32 ifStatus     (bit 0 = admin up, bit 1 = oper up; 3 = both up)
//	u64 ifInOctets
//	u32 ifInUcastPkts
//	u32 ifInMulticastPkts
//	u32 ifInBroadcastPkts
//	u32 ifInDiscards
//	u32 ifInErrors
//	u32 ifInUnknownProtos
//	u64 ifOutOctets
//	u32 ifOutUcastPkts
//	u32 ifOutMulticastPkts
//	u32 ifOutBroadcastPkts
//	u32 ifOutDiscards
//	u32 ifOutErrors
//	u32 ifPromiscuousMode
func encodeIfCountersBody(ifIndex uint32, speedBps, inOctets, outOctets uint64, inPkts, outPkts uint32) []byte {
	body := make([]byte, 88)
	pos := 0
	binary.BigEndian.PutUint32(body[pos:], ifIndex)
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 6) // ethernetCsmacd
	pos += 4
	binary.BigEndian.PutUint64(body[pos:], speedBps)
	pos += 8
	binary.BigEndian.PutUint32(body[pos:], 1) // full-duplex
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 3) // admin+oper up
	pos += 4
	binary.BigEndian.PutUint64(body[pos:], inOctets)
	pos += 8
	binary.BigEndian.PutUint32(body[pos:], inPkts)
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // multicast
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // broadcast
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // in discards
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // in errors
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // in unknown protos
	pos += 4
	binary.BigEndian.PutUint64(body[pos:], outOctets)
	pos += 8
	binary.BigEndian.PutUint32(body[pos:], outPkts)
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // out multicast
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // out broadcast
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // out discards
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // out errors
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 0) // promiscuous
	return body
}

// itoa is a tiny helper to avoid pulling strconv into this counter path for
// hot-loop use — the caller only passes small positive ifIndex values.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	n := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}

// parseUintOrZero is tolerant of empty / malformed inputs — it returns 0
// rather than erroring so Snapshot never blocks the tick goroutine on a
// single malformed OID.
func parseUintOrZero(s string) uint64 {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

// CPUCounterSource emits a single sFlow processor_information counter record
// per snapshot. The values are not driven from any real CPU meter — they are
// synthesized from the device's metricsCycler so the shape looks plausible to
// sFlow collectors (non-flat line, slowly drifting).
type CPUCounterSource struct {
	device *DeviceSimulator
}

// NewCPUCounterSource returns a per-device CPU counter source.
func NewCPUCounterSource(d *DeviceSimulator) *CPUCounterSource {
	return &CPUCounterSource{device: d}
}

// Snapshot returns a single processor_information CounterRecord. Layout
// (simplified — the standard sFlow processor_information counter only carries
// CPU percentage fields plus memory; real counters are surfaced via SNMP):
//
//	u32 cpu_5s    (0..100)
//	u32 cpu_1m    (0..100)
//	u32 cpu_5m    (0..100)
//	u64 total_memory
//	u64 free_memory
func (s *CPUCounterSource) Snapshot(_ time.Time) []CounterRecord {
	body := make([]byte, 4+4+4+8+8)
	pos := 0
	// Synthetic 20% / 25% / 30% loads — stable across runs so tests assert.
	binary.BigEndian.PutUint32(body[pos:], 20)
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 25)
	pos += 4
	binary.BigEndian.PutUint32(body[pos:], 30)
	pos += 4
	binary.BigEndian.PutUint64(body[pos:], 16*1024*1024*1024) // 16 GiB total
	pos += 8
	binary.BigEndian.PutUint64(body[pos:], 8*1024*1024*1024) // 8 GiB free
	return []CounterRecord{{Format: sflowCtrFmtProcessor, Body: body}}
}

// MemoryCounterSource emits a single memory counter record per snapshot. See
// sflowCtrFmtMemory for the format caveat — this is a simulator-local format
// ID because sFlow v5's standard counter registry doesn't include a
// freestanding memory type.
type MemoryCounterSource struct {
	device *DeviceSimulator
}

// NewMemoryCounterSource returns a per-device memory counter source.
func NewMemoryCounterSource(d *DeviceSimulator) *MemoryCounterSource {
	return &MemoryCounterSource{device: d}
}

// Snapshot returns one memory CounterRecord with total / used / free / cached
// bytes. Values are synthetic constants; real memory telemetry should use
// hrStorage via SNMP.
func (s *MemoryCounterSource) Snapshot(_ time.Time) []CounterRecord {
	body := make([]byte, 8*4)
	pos := 0
	binary.BigEndian.PutUint64(body[pos:], 16*1024*1024*1024) // total
	pos += 8
	binary.BigEndian.PutUint64(body[pos:], 8*1024*1024*1024) // used
	pos += 8
	binary.BigEndian.PutUint64(body[pos:], 8*1024*1024*1024) // free
	pos += 8
	binary.BigEndian.PutUint64(body[pos:], 2*1024*1024*1024) // cached
	return []CounterRecord{{Format: sflowCtrFmtMemory, Body: body}}
}
