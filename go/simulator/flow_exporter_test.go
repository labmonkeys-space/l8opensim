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
	"net"
	"sync"
	"testing"
	"time"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

// testUDPListener opens an ephemeral loopback UDP socket, returning the
// listener and a channel that delivers raw packet bytes as they arrive.
// The goroutine exits when the listener is closed.
func testUDPListener(t *testing.T) (*net.UDPConn, <-chan []byte) {
	t.Helper()
	ln, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	ch := make(chan []byte, 64)
	go func() {
		defer close(ch)
		buf := make([]byte, 2048)
		for {
			n, _, err := ln.ReadFromUDP(buf)
			if err != nil {
				return // listener closed
			}
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			ch <- pkt
		}
	}()
	return ln, ch
}

// testSender opens an ephemeral UDP socket for sending.
func testSender(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatalf("ListenUDP (sender): %v", err)
	}
	return conn
}

// testDevice builds a minimal DeviceSimulator with the given IPv4 address.
func testDevice(ipStr string) *DeviceSimulator {
	return &DeviceSimulator{IP: net.ParseIP(ipStr).To4()}
}

// testPool returns a sync.Pool supplying 1500-byte buffers.
func testPool() *sync.Pool {
	return &sync.Pool{New: func() interface{} { return make([]byte, 1500) }}
}

// receivePacket reads one packet from ch with a short deadline.
// Returns nil if nothing arrives within 200 ms.
func receivePacket(ch <-chan []byte) []byte {
	select {
	case pkt := <-ch:
		return pkt
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestNewFlowExporter_DomainID(t *testing.T) {
	device := testDevice("10.0.0.1")
	fe := NewFlowExporter(device, flowProfileEdgeRouter, 30*time.Second, 15*time.Second, 60*time.Second)

	// domainID must be the device IPv4 encoded as big-endian uint32.
	if fe.domainID != 0x0A000001 {
		t.Errorf("domainID = %08x, want 0a000001", fe.domainID)
	}
	if fe.cache == nil {
		t.Error("cache is nil")
	}
	if fe.rng == nil {
		t.Error("rng is nil")
	}
}

func TestNewFlowExporter_DifferentDevicesDifferentDomainIDs(t *testing.T) {
	feA := NewFlowExporter(testDevice("10.0.0.1"), flowProfileEdgeRouter, time.Second, time.Second, time.Minute)
	feB := NewFlowExporter(testDevice("10.0.0.2"), flowProfileEdgeRouter, time.Second, time.Second, time.Minute)

	if feA.domainID == feB.domainID {
		t.Errorf("expected different domainIDs for different devices, both got %08x", feA.domainID)
	}
}

func TestDomainIDtoIP_RoundTrip(t *testing.T) {
	cases := []string{"10.0.0.1", "192.168.1.100", "172.16.0.1"}
	for _, c := range cases {
		original := net.ParseIP(c).To4()
		device := &DeviceSimulator{IP: original}
		fe := NewFlowExporter(device, flowProfileEdgeRouter, time.Second, time.Second, time.Minute)
		recovered := domainIDtoIP(fe.domainID)
		if !recovered.Equal(original) {
			t.Errorf("%s: domainIDtoIP(%08x) = %v, want %v", c, fe.domainID, recovered, original)
		}
	}
}

// TestFlowExporter_Tick_TemplateOnFirstCall verifies that Tick sends a
// template-only NF9 packet on the first call (seqNo == 0) even when no flows
// have expired yet. This satisfies the RFC 3954 requirement to send the
// template before any data records.
func TestFlowExporter_Tick_TemplateOnFirstCall(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	// Large timeouts so nothing expires during the test.
	fe := NewFlowExporter(testDevice("10.1.2.3"), flowProfileEdgeRouter,
		10*time.Minute, 5*time.Minute, 10*time.Minute)

	fe.Tick(time.Now(), NetFlow9Encoder{}, conn, collectorAddr, testPool())

	pkt := receivePacket(ch)
	if pkt == nil {
		t.Fatal("no UDP packet received on first Tick (expected template)")
	}

	// Decode and verify it is a valid NF9 packet containing a template.
	decoded := decodeNF9Packet(t, pkt)
	if decoded.Header.Version != 9 {
		t.Errorf("version = %d, want 9", decoded.Header.Version)
	}
	if len(decoded.Templates) != 1 {
		t.Errorf("template count = %d, want 1", len(decoded.Templates))
	}
	// seqNo should have advanced.
	if fe.seqNo != 1 {
		t.Errorf("seqNo after first Tick = %d, want 1", fe.seqNo)
	}
}

// TestFlowExporter_Tick_NoSendWithoutExpiredFlows verifies that Tick skips
// packet encoding when no flows have expired and the template interval has
// not elapsed (i.e., seqNo > 0 and lastTempl is fresh).
func TestFlowExporter_Tick_NoSendWhenIdle(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	fe := NewFlowExporter(testDevice("10.1.2.4"), flowProfileEdgeRouter,
		10*time.Minute, 5*time.Minute, 10*time.Minute)

	now := time.Now()

	// First Tick sends template (seqNo=0).
	fe.Tick(now, NetFlow9Encoder{}, conn, collectorAddr, testPool())
	receivePacket(ch) // drain

	// Second Tick immediately after — no flows expired, template fresh.
	fe.Tick(now, NetFlow9Encoder{}, conn, collectorAddr, testPool())

	pkt := receivePacket(ch)
	if pkt != nil {
		t.Error("expected no packet on idle Tick, but one was received")
	}
}

// TestFlowExporter_Tick_SendsFlowsWhenExpired pre-populates the cache with
// flows at a past createdAt so they are immediately expired, then calls Tick
// and verifies that flow records are received by the collector.
func TestFlowExporter_Tick_SendsFlowsWhenExpired(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	// Very short timeouts so manually-inserted flows are expired immediately.
	fe := NewFlowExporter(testDevice("10.1.2.5"), flowProfileEdgeRouter,
		1*time.Millisecond, 1*time.Millisecond, 10*time.Minute)

	// Insert 3 flows directly into the cache, timestamped far in the past.
	past := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 3; i++ {
		fe.cache.Add(FlowRecord{
			SrcIP:   net.ParseIP("10.0.0.1").To4(),
			DstIP:   net.ParseIP("10.0.0.2").To4(),
			NextHop: net.IPv4(0, 0, 0, 0).To4(),
			SrcPort: uint16(1000 + i),
			DstPort: 443,
			Protocol: 6,
			Bytes:   1024,
			Packets: 10,
		}, past)
	}

	fe.Tick(time.Now(), NetFlow9Encoder{}, conn, collectorAddr, testPool())

	pkt := receivePacket(ch)
	if pkt == nil {
		t.Fatal("no packet received after Tick with expired flows")
	}
	decoded := decodeNF9Packet(t, pkt)
	if len(decoded.Records) != 3 {
		t.Errorf("expected 3 flow records, got %d", len(decoded.Records))
	}
}

// TestFlowExporter_Tick_TemplateRetransmit verifies that the template is
// re-sent after templateInterval has elapsed since the last transmission.
func TestFlowExporter_Tick_TemplateRetransmit(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	fe := NewFlowExporter(testDevice("10.1.2.6"), flowProfileEdgeRouter,
		10*time.Minute, 5*time.Minute, 60*time.Second)

	now := time.Now()

	// First Tick: sends template.
	fe.Tick(now, NetFlow9Encoder{}, conn, collectorAddr, testPool())
	pkt1 := receivePacket(ch)
	if pkt1 == nil {
		t.Fatal("expected template on first Tick")
	}

	// Second Tick: template interval has not elapsed — no send.
	fe.Tick(now, NetFlow9Encoder{}, conn, collectorAddr, testPool())
	if pkt := receivePacket(ch); pkt != nil {
		t.Error("unexpected packet before template interval elapsed")
	}

	// Simulate template interval elapsed by advancing lastTempl back.
	fe.lastTempl = now.Add(-61 * time.Second)

	// Third Tick: template should be retransmitted.
	fe.Tick(now, NetFlow9Encoder{}, conn, collectorAddr, testPool())
	pkt3 := receivePacket(ch)
	if pkt3 == nil {
		t.Fatal("expected template retransmission after interval elapsed")
	}
	decoded := decodeNF9Packet(t, pkt3)
	if len(decoded.Templates) != 1 {
		t.Errorf("expected 1 template on retransmission, got %d", len(decoded.Templates))
	}
}

// TestFlowExporter_Tick_IPFIXTemplateOnFirstCall verifies that the first Tick
// with an IPFIXEncoder sends an IPFIX message containing a Template Set.
func TestFlowExporter_Tick_IPFIXTemplateOnFirstCall(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	fe := NewFlowExporter(testDevice("10.2.3.4"), flowProfileEdgeRouter,
		10*time.Minute, 5*time.Minute, 10*time.Minute)

	fe.Tick(time.Now(), IPFIXEncoder{}, conn, collectorAddr, testPool())

	pkt := receivePacket(ch)
	if pkt == nil {
		t.Fatal("no UDP packet received on first IPFIX Tick (expected template)")
	}

	decoded := decodeIPFIXPacket(t, pkt)
	if decoded.Header.Version != 10 {
		t.Errorf("IPFIX version = %d, want 10", decoded.Header.Version)
	}
	if len(decoded.Templates) != 1 {
		t.Errorf("template count = %d, want 1", len(decoded.Templates))
	}
	if fe.seqNo != 1 {
		t.Errorf("seqNo after first Tick = %d, want 1", fe.seqNo)
	}
}

// TestFlowExporter_Tick_IPFIXPagination verifies that 80 expired records are
// fully delivered across multiple IPFIX datagrams with no silent record loss.
// This specifically exercises the pagination logic with ipfixRecordSize=53
// (which is larger than nf9RecordSize=45 and was previously causing 5 records
// to be silently discarded per datagram).
func TestFlowExporter_Tick_IPFIXPagination(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	profile := &FlowProfile{
		TCPWeight: 1.0, UDPWeight: 0, ICMPWeight: 0,
		DstPorts:        []PortWeight{{443, 1.0}},
		SrcPortMin:      1024, SrcPortMax: 65535,
		BytesMin:        100, BytesMax: 200,
		PktsMin:         1, PktsMax: 2,
		DurationMinMs:   100, DurationMaxMs: 200,
		ConcurrentFlows: 100,
		MaxFlows:        256,
	}

	fe := NewFlowExporter(testDevice("10.2.3.5"), profile,
		1*time.Millisecond, 1*time.Millisecond, 10*time.Minute)

	// Insert 80 distinct flows all with past timestamps so they expire immediately.
	past := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 80; i++ {
		fe.cache.Add(FlowRecord{
			SrcIP:    net.ParseIP("10.0.0.1").To4(),
			DstIP:    net.ParseIP("10.0.0.2").To4(),
			NextHop:  net.IPv4(0, 0, 0, 0).To4(),
			SrcPort:  uint16(1024 + i),
			DstPort:  443,
			Protocol: 6,
			Bytes:    100,
			Packets:  1,
		}, past)
	}

	if got := fe.cache.Len(); got != 80 {
		t.Fatalf("expected 80 cache entries, got %d", got)
	}

	fe.Tick(time.Now(), IPFIXEncoder{}, conn, collectorAddr, testPool())

	// Collect all received packets.
	var packets [][]byte
	for {
		pkt := receivePacket(ch)
		if pkt == nil {
			break
		}
		packets = append(packets, pkt)
	}

	if len(packets) < 2 {
		t.Errorf("expected ≥2 IPFIX packets for 80 records, got %d", len(packets))
	}

	// Count total records across all packets — must equal 80 with no loss.
	total := 0
	for _, pkt := range packets {
		decoded := decodeIPFIXPacket(t, pkt)
		if decoded.Header.Version != 10 {
			t.Errorf("packet version = %d, want 10 (IPFIX)", decoded.Header.Version)
		}
		total += len(decoded.Records)
	}
	if total != 80 {
		t.Errorf("total IPFIX records across all packets = %d, want 80 (no silent loss)", total)
	}
}

// TestFlowExporter_Tick_Pagination verifies that when more records expire than
// fit in a single 1500-byte UDP datagram, multiple packets are sent.
func TestFlowExporter_Tick_Pagination(t *testing.T) {
	ln, ch := testUDPListener(t)
	defer ln.Close()

	conn := testSender(t)
	defer conn.Close()

	collectorAddr := ln.LocalAddr().(*net.UDPAddr)

	// Use a large MaxFlows to allow many concurrent entries.
	profile := &FlowProfile{
		TCPWeight: 1.0, UDPWeight: 0, ICMPWeight: 0,
		DstPorts:        []PortWeight{{443, 1.0}},
		SrcPortMin:      1024, SrcPortMax: 65535,
		BytesMin:        100, BytesMax: 200,
		PktsMin:         1, PktsMax: 2,
		DurationMinMs:   100, DurationMaxMs: 200,
		ConcurrentFlows: 100,
		MaxFlows:        256,
	}

	fe := NewFlowExporter(testDevice("10.1.2.7"), profile,
		1*time.Millisecond, 1*time.Millisecond, 10*time.Minute)

	// Insert 80 distinct flows all with past timestamps so they expire immediately.
	past := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 80; i++ {
		fe.cache.Add(FlowRecord{
			SrcIP:    net.ParseIP("10.0.0.1").To4(),
			DstIP:    net.ParseIP("10.0.0.2").To4(),
			NextHop:  net.IPv4(0, 0, 0, 0).To4(),
			SrcPort:  uint16(1024 + i),
			DstPort:  443,
			Protocol: 6,
			Bytes:    100,
			Packets:  1,
		}, past)
	}

	if got := fe.cache.Len(); got != 80 {
		t.Fatalf("expected 80 cache entries, got %d", got)
	}

	fe.Tick(time.Now(), NetFlow9Encoder{}, conn, collectorAddr, testPool())

	// Collect all received packets.
	var packets [][]byte
	for {
		pkt := receivePacket(ch)
		if pkt == nil {
			break
		}
		packets = append(packets, pkt)
	}

	if len(packets) < 2 {
		t.Errorf("expected ≥2 packets for 80 records, got %d", len(packets))
	}

	// Count total records across all packets.
	total := 0
	for _, pkt := range packets {
		decoded := decodeNF9Packet(t, pkt)
		total += len(decoded.Records)
	}
	if total != 80 {
		t.Errorf("total records across all packets = %d, want 80", total)
	}
}
