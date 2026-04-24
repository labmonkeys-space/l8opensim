package main

import (
	"net"
	"reflect"
	"testing"
)

func TestDeleteDeviceDeletesTunInterface(t *testing.T) {
	originalDelete := deleteDeviceTunInterfaces
	t.Cleanup(func() { deleteDeviceTunInterfaces = originalDelete })

	var deleted []string
	deleteDeviceTunInterfaces = func(sm *SimulatorManager, interfaceNames []string) error {
		deleted = append([]string(nil), interfaceNames...)
		return nil
	}

	deviceIP := net.IPv4(127, 0, 0, 1)
	device := &DeviceSimulator{
		ID:       "device-1",
		IP:       deviceIP,
		tunIface: &TunInterface{Name: "sim123"},
		running:  true,
	}
	sm := &SimulatorManager{
		devices:         map[string]*DeviceSimulator{device.ID: device},
		deviceIPs:       map[string]struct{}{deviceIP.String(): {}},
		deviceTypesByIP: map[string]string{deviceIP.String(): "cisco_ios"},
	}

	manager = nil
	if err := sm.DeleteDevice(device.ID); err != nil {
		t.Fatalf("DeleteDevice() error = %v", err)
	}

	if !reflect.DeepEqual(deleted, []string{"sim123"}) {
		t.Fatalf("deleted interfaces = %v, want [sim123]", deleted)
	}
	if _, exists := sm.devices[device.ID]; exists {
		t.Fatal("device was not removed from devices map")
	}
	if _, exists := sm.deviceIPs[deviceIP.String()]; exists {
		t.Fatal("device IP was not removed from deviceIPs")
	}
	if _, exists := sm.deviceTypesByIP[deviceIP.String()]; exists {
		t.Fatal("device type was not removed from deviceTypesByIP")
	}
}
