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
	"strconv"
	"time"
)

// CounterRecord is a single protocol-agnostic counter payload ready to be
// serialised into an sFlow counters_sample flow_record. Format is the sFlow v5
// counter-record format tag (enterprise 0), Body is the XDR-encoded counter
// body without the 8-byte record header.
//
// SourceID is the sFlow data source identifier that groups records into
// counters_sample records on the wire:
//   - For per-interface records (if_counters) it is the ifIndex
//     (ds_class=0, ds_index=ifIndex encoded as 0<<24 | ifIndex).
//   - For device-wide records (processor_information, memory etc.) it is 0.
//
// Protocol-specific encoding of this record into the target wire format is
// owned by the encoder (see sflow.go EncodeCounterDatagram).
type CounterRecord struct {
	Format   uint32
	SourceID uint32
	Body     []byte
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
// ifHCInOctets / ifHCOutOctets sourced from the cycler's sine-wave math. Each
// record is tagged with SourceID = ifIndex so EncodeCounterDatagram emits one
// counters_sample per interface (collectors such as OpenNMS Telemetryd key
// if_counters by ds_index).
//
// The remaining if_counters fields (ifInUcastPkts etc.) are synthesized from
// the octet counters using a coarse 500-byte average packet size — adequate
// for collector smoke tests, not for graphing accuracy.
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
		inStr := s.cycler.GetHCOctets(hcInOIDPrefix + strconv.Itoa(ifIndex))
		outStr := s.cycler.GetHCOctets(hcOutOIDPrefix + strconv.Itoa(ifIndex))
		inOctets := parseUintOrZero(inStr)
		outOctets := parseUintOrZero(outStr)
		// Synthesize packet counters from octets / 500B — coarse but
		// monotonic since octets are monotonic.
		inPkts := uint32(inOctets / 500)
		outPkts := uint32(outOctets / 500)

		body := encodeIfCountersBody(uint32(ifIndex), s.cycler.ifSpeedBps[slot], inOctets, outOctets, inPkts, outPkts)
		out = append(out, CounterRecord{
			Format:   sflowCtrFmtGeneric,
			SourceID: uint32(ifIndex),
			Body:     body,
		})
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
// synthesized constants so the shape looks plausible to sFlow collectors.
// The standard processor_information counter already carries total_memory
// and free_memory fields, so no separate memory counter record is needed.
type CPUCounterSource struct{}

// NewCPUCounterSource returns a per-device CPU counter source. The receiver
// carries no device state today because values are synthetic constants; the
// signature keeps room for MetricsCycler-driven sine-wave wiring in a
// follow-up without changing call sites.
func NewCPUCounterSource(_ *DeviceSimulator) *CPUCounterSource {
	return &CPUCounterSource{}
}

// Snapshot returns a single processor_information CounterRecord. The record
// carries device-wide counters, so SourceID is 0 (ds_class=0, ds_index=0) —
// collectors group these under a single counters_sample per device.
//
// Layout (sflow_version_5.txt §5.4 "processor information"):
//
//	u32 cpu_5s           (load 0..100)
//	u32 cpu_1m           (load 0..100)
//	u32 cpu_5m           (load 0..100)
//	u64 total_memory     (bytes)
//	u64 free_memory      (bytes)
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
	return []CounterRecord{{
		Format:   sflowCtrFmtProcessor,
		SourceID: 0,
		Body:     body,
	}}
}
