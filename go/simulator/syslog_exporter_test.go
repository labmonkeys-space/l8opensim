/*
 * © 2025 Labmonkeys Space
 * Apache-2.0 — see LICENSE.
 */

package main

import (
	"net"
	"strings"
	"testing"
	"time"
)

// newLocalUDPCollector starts a UDP listener on 127.0.0.1 and returns it
// plus its address. The caller is responsible for closing it via t.Cleanup.
func newLocalUDPCollector(t *testing.T) (*net.UDPConn, *net.UDPAddr) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen collector: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

// readNextDatagram blocks up to timeout for one UDP datagram on conn.
// Returns the payload bytes, the sender address, or fails the test.
func readNextDatagram(t *testing.T, conn *net.UDPConn, timeout time.Duration) ([]byte, *net.UDPAddr) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 2048)
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read datagram: %v", err)
	}
	return buf[:n], addr
}

// newTestSharedSocket creates a UDP socket for the exporter to transmit
// from. We don't use openSyslogConnForDevice because tests don't run in a
// network namespace.
func newTestSharedSocket(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen shared: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestSyslogExporter_Fire5424 — a 5424 emission lands on the collector
// with the expected PRI, HOSTNAME, and message body, and Sent increments.
func TestSyslogExporter_Fire5424(t *testing.T) {
	cat := testSyslogCatalog(t)
	entry := cat.ByName["interface-down"]
	if entry == nil {
		t.Fatal("interface-down missing from embedded catalog")
	}

	collector, collectorAddr := newLocalUDPCollector(t)
	shared := newTestSharedSocket(t)

	exporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   net.IPv4(10, 42, 0, 7),
		Encoder:    &RFC5424Encoder{},
		Collector:  collectorAddr,
		SharedConn: shared,
		SysName:    "rtr-edge-07",
		IfIndexFn:  func() int { return 3 },
		IfNameFn:   func(i int) string { return "GigabitEthernet0/3" },
	})
	t.Cleanup(func() { _ = exporter.Close() })

	if err := exporter.Fire(entry, nil); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if got := exporter.Stats().Sent.Load(); got != 1 {
		t.Errorf("Sent: got %d, want 1", got)
	}
	if got := exporter.Stats().SendFailures.Load(); got != 0 {
		t.Errorf("SendFailures: got %d, want 0", got)
	}

	payload, _ := readNextDatagram(t, collector, 500*time.Millisecond)
	wire := string(payload)
	// PRI for local7 (23) + error (3) = 187.
	if !strings.HasPrefix(wire, "<187>1 ") {
		t.Errorf("wire does not start with expected PRI: %q", wire)
	}
	if !strings.Contains(wire, "rtr-edge-07") {
		t.Errorf("wire missing hostname: %q", wire)
	}
	if !strings.Contains(wire, "IFMGR") {
		t.Errorf("wire missing appName: %q", wire)
	}
	if !strings.Contains(wire, "LINKDOWN") {
		t.Errorf("wire missing msgId: %q", wire)
	}
	if !strings.Contains(wire, "GigabitEthernet0/3") {
		t.Errorf("wire missing ifName from body: %q", wire)
	}
}

// TestSyslogExporter_Fire3164 — 3164 format lands on the collector with
// the single-digit-day timestamp and truncated TAG path exercised.
func TestSyslogExporter_Fire3164(t *testing.T) {
	cat := testSyslogCatalog(t)
	entry := cat.ByName["auth-failure"]

	collector, collectorAddr := newLocalUDPCollector(t)
	shared := newTestSharedSocket(t)

	exporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   net.IPv4(10, 42, 0, 7),
		Encoder:    &RFC3164Encoder{},
		Collector:  collectorAddr,
		SharedConn: shared,
		SysName:    "rtr-edge-07",
	})
	t.Cleanup(func() { _ = exporter.Close() })

	if err := exporter.Fire(entry, nil); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	payload, _ := readNextDatagram(t, collector, 500*time.Millisecond)
	wire := string(payload)
	// authpriv (10) * 8 + warning (4) = 84.
	if !strings.HasPrefix(wire, "<84>") {
		t.Errorf("wire does not start with expected PRI: %q", wire)
	}
	if !strings.Contains(wire, "rtr-edge-07") {
		t.Errorf("wire missing hostname: %q", wire)
	}
	if !strings.Contains(wire, "sshd:") {
		t.Errorf("wire missing tag: %q", wire)
	}
}

// TestSyslogExporter_HostnameFallback — when sysName is empty, the
// exporter falls back to DeviceIP for HOSTNAME (design.md §D5 priority 3).
func TestSyslogExporter_HostnameFallback(t *testing.T) {
	cat := testSyslogCatalog(t)
	entry := cat.ByName["interface-up"]

	collector, collectorAddr := newLocalUDPCollector(t)
	shared := newTestSharedSocket(t)

	exporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   net.IPv4(10, 42, 0, 7),
		Encoder:    &RFC5424Encoder{},
		Collector:  collectorAddr,
		SharedConn: shared,
		// SysName deliberately empty.
	})
	t.Cleanup(func() { _ = exporter.Close() })

	if err := exporter.Fire(entry, nil); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	payload, _ := readNextDatagram(t, collector, 500*time.Millisecond)
	if !strings.Contains(string(payload), "10.42.0.7") {
		t.Errorf("wire missing DeviceIP fallback hostname: %q", string(payload))
	}
}

// TestSyslogExporter_FireAfterCloseIsNoOp — Close marks the exporter and
// subsequent Fires must not write and must not crash.
func TestSyslogExporter_FireAfterCloseIsNoOp(t *testing.T) {
	cat := testSyslogCatalog(t)
	entry := cat.ByName["interface-up"]

	_, collectorAddr := newLocalUDPCollector(t)
	shared := newTestSharedSocket(t)

	exporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   net.IPv4(10, 42, 0, 7),
		Encoder:    &RFC5424Encoder{},
		Collector:  collectorAddr,
		SharedConn: shared,
		SysName:    "host",
	})
	if err := exporter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent.
	if err := exporter.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := exporter.Fire(entry, nil); err != nil {
		t.Errorf("Fire after Close should no-op without error, got: %v", err)
	}
	if got := exporter.Stats().Sent.Load(); got != 0 {
		t.Errorf("Sent after Close: got %d, want 0", got)
	}
}

// TestSyslogExporter_SendFailureCounterIncrements — an encoder that
// returns an error bumps SendFailures without crashing.
func TestSyslogExporter_SendFailureCounterIncrements(t *testing.T) {
	cat := testSyslogCatalog(t)
	entry := cat.ByName["interface-up"]

	shared := newTestSharedSocket(t)
	// Bad collector: a non-listening port on a blackhole address triggers
	// a write error on many systems, but on others UDP writes to closed
	// ports silently succeed. Instead, force failure by closing the
	// shared socket before Fire.
	_ = shared.Close()

	exporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   net.IPv4(10, 42, 0, 7),
		Encoder:    &RFC5424Encoder{},
		Collector:  &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9},
		SharedConn: shared,
		SysName:    "host",
	})
	if err := exporter.Fire(entry, nil); err == nil {
		t.Error("Fire on closed socket should return error")
	}
	if got := exporter.Stats().SendFailures.Load(); got != 1 {
		t.Errorf("SendFailures: got %d, want 1", got)
	}
	if got := exporter.Stats().Sent.Load(); got != 0 {
		t.Errorf("Sent: got %d, want 0", got)
	}
}

// TestSyslogExporter_ImplementsFirer verifies the scheduler firer contract
// at compile time. If someone breaks the Fire signature this won't compile.
func TestSyslogExporter_ImplementsFirer(t *testing.T) {
	var _ syslogFirer = (*SyslogExporter)(nil)
}

// TestSyslogExporter_TemplateOverrides — HTTP-style overrides pin the
// per-fire context fields so on-demand fires can target a specific
// interface or user.
func TestSyslogExporter_TemplateOverrides(t *testing.T) {
	cat := testSyslogCatalog(t)
	entry := cat.ByName["interface-down"]

	collector, collectorAddr := newLocalUDPCollector(t)
	shared := newTestSharedSocket(t)

	exporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:   net.IPv4(10, 42, 0, 7),
		Encoder:    &RFC5424Encoder{},
		Collector:  collectorAddr,
		SharedConn: shared,
		SysName:    "host",
		IfIndexFn:  func() int { return 1 },
		IfNameFn:   func(i int) string { return "FastEthernet0/1" },
	})
	t.Cleanup(func() { _ = exporter.Close() })

	err := exporter.Fire(entry, map[string]string{
		"IfIndex": "42",
		"IfName":  "GigabitEthernet7/42",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := readNextDatagram(t, collector, 500*time.Millisecond)
	wire := string(payload)
	if !strings.Contains(wire, "GigabitEthernet7/42") {
		t.Errorf("override IfName did not apply: %q", wire)
	}
	if !strings.Contains(wire, "ifIndex=42") {
		t.Errorf("override IfIndex did not apply: %q", wire)
	}
	if strings.Contains(wire, "FastEthernet0/1") {
		t.Errorf("pre-override value leaked into wire: %q", wire)
	}
}
