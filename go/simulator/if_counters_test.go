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
	"strconv"
	"sync"
	"testing"
	"time"
)

// buildTestResources constructs a minimal DeviceResources with HC counter and
// speed OIDs for the given interfaces. speeds are in bps.
func buildTestResources(t *testing.T, speeds []uint64) *DeviceResources {
	t.Helper()
	res := &DeviceResources{
		oidIndex: &sync.Map{},
	}
	for i, spd := range speeds {
		ifIndex := i + 1
		// ifHighSpeed in Mbps
		res.oidIndex.Store(
			fmt.Sprintf(".1.3.6.1.2.1.31.1.1.1.15.%d", ifIndex),
			strconv.FormatUint(spd/1_000_000, 10),
		)
		// HC in/out placeholders (any value — InitIfCounters only reads speed)
		res.oidIndex.Store(fmt.Sprintf(".1.3.6.1.2.1.31.1.1.1.6.%d", ifIndex), "0")
		res.oidIndex.Store(fmt.Sprintf(".1.3.6.1.2.1.31.1.1.1.10.%d", ifIndex), "0")
	}
	return res
}

func TestIfCounterCycler_Monotonic(t *testing.T) {
	const gbps = 1_000_000_000
	res := buildTestResources(t, []uint64{gbps, gbps})

	c := &MetricsCycler{}
	c.InitIfCounters(res, 42)

	if c.ifCounters == nil {
		t.Fatal("InitIfCounters did not create ifCounters")
	}

	// Poll 5 times with small sleeps and verify strict monotonic increase.
	prev1, prev2 := uint64(0), uint64(0)
	for poll := 0; poll < 5; poll++ {
		time.Sleep(5 * time.Millisecond)

		v1str := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.1")
		v2str := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.2")
		v1, err1 := strconv.ParseUint(v1str, 10, 64)
		v2, err2 := strconv.ParseUint(v2str, 10, 64)
		if err1 != nil || err2 != nil {
			t.Fatalf("poll %d: non-numeric HC value (%q, %q)", poll, v1str, v2str)
		}
		if poll > 0 && v1 <= prev1 {
			t.Errorf("poll %d: ifHCInOctets.1 not increasing: %d -> %d", poll, prev1, v1)
		}
		if poll > 0 && v2 <= prev2 {
			t.Errorf("poll %d: ifHCInOctets.2 not increasing: %d -> %d", poll, prev2, v2)
		}
		prev1, prev2 = v1, v2
	}
}

func TestIfCounterCycler_RateInRange(t *testing.T) {
	// Use a 10 Gbps interface and verify the byte-rate is within [60%, 100%] of capacity.
	const gbps10 = 10_000_000_000
	res := buildTestResources(t, []uint64{gbps10})

	c := &MetricsCycler{}
	c.InitIfCounters(res, 7)

	if c.ifCounters == nil {
		t.Fatal("InitIfCounters did not create ifCounters")
	}

	// Sample over ~50 ms; compute average byte-rate.
	start := time.Now()
	v0str := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.1")
	v0, _ := strconv.ParseUint(v0str, 10, 64)

	time.Sleep(50 * time.Millisecond)

	v1str := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.1")
	v1, _ := strconv.ParseUint(v1str, 10, 64)
	elapsed := time.Since(start).Seconds()

	rate := float64(v1-v0) / elapsed // bytes/sec
	speedBytesPerSec := float64(gbps10) / 8.0

	minRate := speedBytesPerSec * 0.55 // allow 5% margin below 60%
	maxRate := speedBytesPerSec * 1.05 // allow 5% margin above 100%

	if rate < minRate || rate > maxRate {
		t.Errorf("byte-rate %.0f B/s out of expected range [%.0f, %.0f] B/s (%.1f%%..%.1f%% of capacity)",
			rate, minRate, maxRate, rate/speedBytesPerSec*100, rate/speedBytesPerSec*100)
	}
}

func TestIfCounterCycler_UnknownOID(t *testing.T) {
	res := buildTestResources(t, []uint64{1_000_000_000})
	c := &MetricsCycler{}
	c.InitIfCounters(res, 1)

	// Wrong column — should return empty string
	if v := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.7.1"); v != "" {
		t.Errorf("expected empty for non-HC OID, got %q", v)
	}
	// Out-of-range interface index
	if v := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.99"); v != "" {
		t.Errorf("expected empty for out-of-range ifIndex, got %q", v)
	}
}

func TestIfCounterCycler_InOutDiffer(t *testing.T) {
	// In- and out-octets use independent phase offsets; they should differ after
	// a brief interval (unless the seed is pathological — extremely unlikely).
	res := buildTestResources(t, []uint64{1_000_000_000})
	c := &MetricsCycler{}
	c.InitIfCounters(res, 123456)

	time.Sleep(10 * time.Millisecond)
	inStr := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.1")
	outStr := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.10.1")

	in, _ := strconv.ParseUint(inStr, 10, 64)
	out, _ := strconv.ParseUint(outStr, 10, 64)

	// Different bases and phases almost certainly produce different values.
	if in == out {
		t.Errorf("ifHCInOctets and ifHCOutOctets are identical (%d) — phases may not be independent", in)
	}
}

func TestIfCounterCycler_NoHCOIDs(t *testing.T) {
	// A device with no HC OIDs in oidIndex must leave ifCounters nil.
	res := &DeviceResources{oidIndex: &sync.Map{}}
	res.oidIndex.Store(".1.3.6.1.2.1.1.1.0", "Linux 5.15")

	c := &MetricsCycler{}
	c.InitIfCounters(res, 1)

	if c.ifCounters != nil {
		t.Error("expected ifCounters to be nil for device with no HC OIDs")
	}
}

func TestIfCounterCycler_BaseIsPositive(t *testing.T) {
	// At t≈0, counter must equal base (large positive number, not 0).
	res := buildTestResources(t, []uint64{1_000_000_000})
	c := &MetricsCycler{}
	c.InitIfCounters(res, 99)

	// Read immediately (t ≈ 0)
	vStr := c.ifCounters.GetHCOctets(".1.3.6.1.2.1.31.1.1.1.6.1")
	v, _ := strconv.ParseUint(vStr, 10, 64)

	// ~24h of 80% traffic at 1 Gbps should be well above zero
	minExpected := uint64(1e10) // 10 GB — modest lower bound
	if v < minExpected {
		t.Errorf("initial counter value %d is unexpectedly small (< %d); base seeding may have failed", v, minExpected)
	}

	_ = math.Pi // keep math import used
}
