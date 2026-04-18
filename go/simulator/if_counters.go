/*
 * © 2025 Sharon Aicler (saichler@gmail.com)
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
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

const (
	hcInOIDPrefix  = ".1.3.6.1.2.1.31.1.1.1.6."
	hcOutOIDPrefix = ".1.3.6.1.2.1.31.1.1.1.10."
	hcPeriodSec    = 3600.0 // 1-hour sine-wave cycle
)

// IfCounterCycler generates monotonically increasing HC counter values
// (ifHCInOctets / ifHCOutOctets) whose byte-rate follows a sine wave
// between 60% and 100% of interface speed over a 1-hour period.
//
// Formula per interface i at time t seconds since device start:
//
//	rate(t) = ifSpeed_Bps × (0.8 + 0.2·sin(2π·t/T + φᵢ))
//	octets(t) = base_i + ifSpeed_Bps × [0.8·t + (0.2·T/2π)·(cos(φᵢ) − cos(2π·t/T + φᵢ))]
//
// where T = 3600 s and φᵢ is a per-interface random phase offset.
// The rate never falls below 60% of capacity, so the counter is strictly monotonic.
//
// Thread safety: all fields are written once by InitIfCounters before the device's
// SNMP server goroutine is started. Concurrent reads in GetHCOctets are safe because
// goroutine creation provides the required happens-before relationship.
type IfCounterCycler struct {
	startTime      time.Time
	maxIfIndex     int              // upper bound for array indexing
	knownIfIndexes map[int]struct{} // exact set of ifIndex values present in oidIndex
	// ifIndexList caches knownIfIndexes as a slice so IfIndices is
	// allocation-free on the hot path (trap varbind template resolution
	// calls it per fire). Populated once in InitIfCounters; read-only after.
	ifIndexList []int
	ifSpeedBps  []uint64  // per-interface link speed in bps (slot = ifIndex-1)
	baseIn      []uint64  // per-interface starting octet counter (in)
	baseOut     []uint64  // per-interface starting octet counter (out)
	phaseIn     []float64 // per-interface random phase offset in [0, 2π)
	phaseOut    []float64
}

// IfIndices returns the cached slice of known ifIndex values for this device.
// Used by trap templating ({{.IfIndex}}) to pick a random interface per fire.
// Returns nil when the device has no indexed interfaces.
//
// The returned slice is a shared read-only view — callers must NOT mutate it.
// Indexing into it with `rand.Intn(len(slice))` is the intended usage.
func (ic *IfCounterCycler) IfIndices() []int {
	if ic == nil {
		return nil
	}
	return ic.ifIndexList
}

// GetHCOctets returns the current dynamic counter value for an HC OID, or ""
// if the OID is not an HC in/out-octets OID for a known interface index.
func (ic *IfCounterCycler) GetHCOctets(oid string) string {
	var prefix string
	isIn := false

	switch {
	case strings.HasPrefix(oid, hcInOIDPrefix):
		prefix = hcInOIDPrefix
		isIn = true
	case strings.HasPrefix(oid, hcOutOIDPrefix):
		prefix = hcOutOIDPrefix
	default:
		return ""
	}

	ifIndex, err := strconv.Atoi(oid[len(prefix):])
	if err != nil || ifIndex < 1 || ifIndex > ic.maxIfIndex {
		return ""
	}
	// Reject ifIndex values that don't exist in the device's OID table
	// (guards against sparse interface numbering, e.g., ifIndex 1, 3, 5).
	if _, known := ic.knownIfIndexes[ifIndex]; !known {
		return ""
	}
	slot := ifIndex - 1

	t := time.Since(ic.startTime).Seconds()
	speedBytesPerSec := float64(ic.ifSpeedBps[slot]) / 8.0

	var phase float64
	var base uint64
	if isIn {
		phase = ic.phaseIn[slot]
		base = ic.baseIn[slot]
	} else {
		phase = ic.phaseOut[slot]
		base = ic.baseOut[slot]
	}

	// ∫₀ᵗ (0.8 + 0.2·sin(2π·τ/T + φ)) dτ
	//   = 0.8·t + 0.2·(T/2π)·(cos(φ) − cos(2π·t/T + φ))
	T := hcPeriodSec
	deltaOctets := speedBytesPerSec * (0.8*t + 0.2*(T/(2*math.Pi))*(math.Cos(phase)-math.Cos(2*math.Pi*t/T+phase)))

	// Clamp to zero: floating-point imprecision at t≈0 can produce a
	// tiny negative value; casting a negative float64 to uint64 wraps.
	if deltaOctets < 0 {
		deltaOctets = 0
	}
	return fmt.Sprintf("%d", base+uint64(deltaOctets))
}

// InitIfCounters sets up per-interface HC counter cycling for dynamic
// ifHCInOctets / ifHCOutOctets values. Interface speeds are read from
// the device's oidIndex (ifHighSpeed in Mbps preferred; falls back to
// ifSpeed in bps).
//
// Must be called after NewMetricsCycler and before device.Start() so that
// goroutine creation provides the happens-before edge required for thread safety.
func (c *MetricsCycler) InitIfCounters(resources *DeviceResources, seed int64) {
	if resources == nil || resources.oidIndex == nil {
		return
	}

	// Collect the exact set of ifIndex values that have HC in-octets OIDs.
	knownIdxs := make(map[int]struct{})
	resources.oidIndex.Range(func(k, _ interface{}) bool {
		oid, ok := k.(string)
		if !ok {
			return true
		}
		if strings.HasPrefix(oid, hcInOIDPrefix) {
			if idx, err := strconv.Atoi(oid[len(hcInOIDPrefix):]); err == nil && idx > 0 {
				knownIdxs[idx] = struct{}{}
			}
		}
		return true
	})
	if len(knownIdxs) == 0 {
		return // no HC counters for this device type
	}

	maxIdx := 0
	for idx := range knownIdxs {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	// Freeze the ifIndex set as a slice once so IfIndices returns a cached
	// read-only view (hot path: trap template resolution).
	indexList := make([]int, 0, len(knownIdxs))
	for idx := range knownIdxs {
		indexList = append(indexList, idx)
	}

	ic := &IfCounterCycler{
		startTime:      time.Now(),
		maxIfIndex:     maxIdx,
		knownIfIndexes: knownIdxs,
		ifIndexList:    indexList,
		ifSpeedBps:     make([]uint64, maxIdx),
		baseIn:         make([]uint64, maxIdx),
		baseOut:        make([]uint64, maxIdx),
		phaseIn:        make([]float64, maxIdx),
		phaseOut:       make([]float64, maxIdx),
	}

	rng := rand.New(rand.NewSource(seed))

	for idx := range knownIdxs {
		slot := idx - 1

		// Prefer ifHighSpeed (Mbps → bps) over ifSpeed (bps, capped at ~4 Gbps).
		var speedBps uint64 = 1_000_000_000 // default 1 Gbps
		highSpeedOID := fmt.Sprintf(".1.3.6.1.2.1.31.1.1.1.15.%d", idx)
		if v, ok := resources.oidIndex.Load(highSpeedOID); ok {
			if s, ok := v.(string); ok {
				if mbps, err := strconv.ParseUint(s, 10, 64); err == nil && mbps > 0 {
					speedBps = mbps * 1_000_000
				}
			}
		} else {
			ifSpeedOID := fmt.Sprintf(".1.3.6.1.2.1.2.2.1.5.%d", idx)
			if v, ok := resources.oidIndex.Load(ifSpeedOID); ok {
				if s, ok := v.(string); ok {
					if bps, err := strconv.ParseUint(s, 10, 64); err == nil && bps > 0 {
						speedBps = bps
					}
				}
			}
		}
		ic.ifSpeedBps[slot] = speedBps

		// Seed counters with ~24 h of 80%-average traffic so they look realistic
		// from the first poll. Add up to +5% per-interface jitter for variety.
		avg24h := uint64(float64(speedBps) / 8.0 * 0.8 * 86400.0)
		ic.baseIn[slot] = avg24h + uint64(rng.Float64()*float64(avg24h)*0.05)
		ic.baseOut[slot] = avg24h + uint64(rng.Float64()*float64(avg24h)*0.05)

		// Random phase offsets so interfaces don't peak simultaneously.
		ic.phaseIn[slot] = rng.Float64() * 2 * math.Pi
		ic.phaseOut[slot] = rng.Float64() * 2 * math.Pi
	}

	c.ifCounters = ic
}
