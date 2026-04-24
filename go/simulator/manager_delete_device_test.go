package main

import (
	"errors"
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

// Regression: if the netlink delete fails, DeleteDevice must still remove
// the device from its maps. The device's listeners are already Stop'd and
// the FD is closed; leaving it in sm.devices creates a permanently-wedged
// ghost that reports as alive but can't be interacted with. Pre-fix,
// DeleteDevice returned early on the netlink error, half-deleting the
// device. The surfaced error is still propagated to the caller.
func TestDeleteDeviceCleansMapsEvenOnTunDeleteError(t *testing.T) {
	originalDelete := deleteDeviceTunInterfaces
	t.Cleanup(func() { deleteDeviceTunInterfaces = originalDelete })

	netlinkErr := errors.New("netlink: device busy")
	deleteDeviceTunInterfaces = func(sm *SimulatorManager, interfaceNames []string) error {
		return netlinkErr
	}

	deviceIP := net.IPv4(127, 0, 0, 2)
	device := &DeviceSimulator{
		ID:       "device-busy",
		IP:       deviceIP,
		tunIface: &TunInterface{Name: "sim456"},
		running:  true,
	}
	sm := &SimulatorManager{
		devices:         map[string]*DeviceSimulator{device.ID: device},
		deviceIPs:       map[string]struct{}{deviceIP.String(): {}},
		deviceTypesByIP: map[string]string{deviceIP.String(): "cisco_ios"},
	}

	manager = nil
	err := sm.DeleteDevice(device.ID)
	if err == nil {
		t.Fatal("DeleteDevice() error = nil, want netlink error")
	}
	if !errors.Is(err, netlinkErr) {
		t.Fatalf("DeleteDevice() error = %v, want to wrap %v", err, netlinkErr)
	}

	if _, exists := sm.devices[device.ID]; exists {
		t.Fatal("device was not removed from devices map despite netlink failure")
	}
	if _, exists := sm.deviceIPs[deviceIP.String()]; exists {
		t.Fatal("device IP was not removed from deviceIPs despite netlink failure")
	}
	if _, exists := sm.deviceTypesByIP[deviceIP.String()]; exists {
		t.Fatal("device type was not removed from deviceTypesByIP despite netlink failure")
	}
}
