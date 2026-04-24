package main

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestDeviceStopCleansPartiallyStartedResources(t *testing.T) {
	manager = nil

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})

	flowExporter := &FlowExporter{}
	syslogExporter := NewSyslogExporter(SyslogExporterOptions{
		DeviceIP:     net.IPv4(127, 0, 0, 1),
		Collector:    &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 514},
		CollectorStr: "127.0.0.1:514",
	})
	trapExporter := NewTrapExporter(TrapExporterOptions{
		DeviceIP:      net.IPv4(127, 0, 0, 1),
		Mode:          TrapModeInform,
		Collector:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 162},
		CollectorStr:  "127.0.0.1:162",
		InformTimeout: 10 * time.Millisecond,
	})
	trapExporter.StartBackgroundLoops(context.Background())

	device := &DeviceSimulator{
		IP:             net.IPv4(127, 0, 0, 1),
		tunIface:       &TunInterface{Name: "sim999", fd: int(reader.Fd())},
		flowExporter:   flowExporter,
		trapExporter:   trapExporter,
		syslogExporter: syslogExporter,
	}

	if err := device.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if device.flowExporter != nil {
		t.Fatal("flow exporter was not cleared")
	}
	if device.trapExporter != nil {
		t.Fatal("trap exporter was not cleared")
	}
	if device.syslogExporter != nil {
		t.Fatal("syslog exporter was not cleared")
	}
	if device.tunIface != nil {
		t.Fatal("tun interface was not cleared")
	}
}
