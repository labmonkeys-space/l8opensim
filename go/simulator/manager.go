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
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	mathrand "math/rand"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SimulatorManager implementation
func NewSimulatorManager() *SimulatorManager {
	return NewSimulatorManagerWithOptions(true) // Default: use namespace isolation
}

// NewSimulatorManagerWithOptions creates a manager with configurable namespace isolation
func NewSimulatorManagerWithOptions(useNamespace bool) *SimulatorManager {
	// Initialize random seed once at startup
	mathrand.Seed(time.Now().UnixNano())

	sm := &SimulatorManager{
		devices:          make(map[string]*DeviceSimulator),
		deviceIPs:        make(map[string]struct{}),
		deviceTypesByIP:  make(map[string]string),
		nextTunIndex:     0,
		resourcesCache:   make(map[string]*DeviceResources),
		tunInterfacePool: make(map[string]*TunInterface),
		useNamespace:     useNamespace,
	}
	// Initialize atomic values
	sm.isPreAllocating.Store(false)
	sm.preAllocProgress.Store(0)
	sm.isCreatingDevices.Store(false)
	sm.deviceCreateProgress.Store(0)
	sm.deviceCreateTotal.Store(0)

	// Initialize network namespace for device isolation
	if useNamespace {
		ns, err := CreateNetNamespace()
		if err != nil {
			log.Printf("WARNING: Failed to create network namespace: %v", err)
			log.Printf("Falling back to root namespace (systemd-networkd may consume resources)")
			sm.useNamespace = false
		} else {
			sm.netNamespace = ns
			log.Printf("Network namespace '%s' active - devices isolated from systemd-networkd", NETNS_NAME)
		}
	}

	// Pre-generate shared SSH host key for all devices
	sm.generateSharedSSHKey()

	// Pre-generate shared TLS certificate for all API servers
	sm.generateSharedTLSCert()

	return sm
}

func (sm *SimulatorManager) getNextTunName() string {
	name := fmt.Sprintf("%s%d", TUN_DEVICE_PREFIX, sm.nextTunIndex)
	sm.nextTunIndex++
	return name
}

// generateSharedSSHKey generates a single RSA key to be shared by all devices
func (sm *SimulatorManager) generateSharedSSHKey() {
	log.Println("Generating shared SSH host key for all devices...")
	startTime := time.Now()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Printf("WARNING: Failed to generate shared SSH key: %v", err)
		return
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		log.Printf("WARNING: Failed to parse shared SSH key: %v", err)
		return
	}

	sm.sharedSSHSigner = signer
	elapsed := time.Since(startTime)
	log.Printf("Shared SSH host key generated in %v", elapsed)
}

// generateSharedTLSCert generates a single TLS certificate to be shared by all API servers.
// This avoids expensive per-device 4096-bit RSA key generation (~10-20s each).
func (sm *SimulatorManager) generateSharedTLSCert() {
	log.Println("Generating shared TLS certificate for all API servers...")
	startTime := time.Now()

	// Generate a 2048-bit CA key (sufficient for simulation, ~10x faster than 4096-bit)
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Printf("WARNING: Failed to generate shared TLS CA key: %v", err)
		return
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2025),
		Subject: pkix.Name{
			CommonName:   "opensim-ca",
			Organization: []string{"OpenSim"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		log.Printf("WARNING: Failed to create shared TLS CA: %v", err)
		return
	}

	ca, err := x509.ParseCertificate(caBytes)
	if err != nil {
		log.Printf("WARNING: Failed to parse shared TLS CA: %v", err)
		return
	}

	// Generate a server certificate signed by the CA
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Printf("WARNING: Failed to generate shared TLS server key: %v", err)
		return
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "opensim-device",
			Organization: []string{"OpenSim"},
		},
		// Use wildcard-style: accept any IP by including 0.0.0.0
		IPAddresses: []net.IP{net.IPv4zero, net.IPv6zero},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(10, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	serverCertBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		log.Printf("WARNING: Failed to create shared TLS cert: %v", err)
		return
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		log.Printf("WARNING: Failed to load shared TLS cert: %v", err)
		return
	}

	sm.sharedTLSCert = &tlsCert
	elapsed := time.Since(startTime)
	log.Printf("Shared TLS certificate generated in %v", elapsed)
}

func (sm *SimulatorManager) ListDevices() []DeviceInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var devices []DeviceInfo
	for _, device := range sm.devices {
		info := DeviceInfo{
			ID:         device.ID,
			IP:         device.IP.String(),
			SNMPPort:   device.SNMPPort,
			SSHPort:    device.SSHPort,
			Running:    device.running,
			DeviceType: getDeviceTypeFromResourceFile(device.resourceFile),
		}
		if device.tunIface != nil {
			info.Interface = device.tunIface.Name
		}
		devices = append(devices, info)
	}

	return devices
}

func (sm *SimulatorManager) GetStatus() ManagerStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	totalDevices := len(sm.devices)
	runningDevices := 0
	for _, device := range sm.devices {
		if device.running {
			runningDevices++
		}
	}

	return ManagerStatus{
		IsPreAllocating:      sm.isPreAllocating.Load().(bool),
		PreAllocProgress:     sm.preAllocProgress.Load().(int),
		PreAllocTotal:        sm.tunPoolSize,
		IsCreatingDevices:    sm.isCreatingDevices.Load().(bool),
		DeviceCreateProgress: sm.deviceCreateProgress.Load().(int),
		DeviceCreateTotal:    sm.deviceCreateTotal.Load().(int),
		TotalDevices:         totalDevices,
		RunningDevices:       runningDevices,
	}
}

func (sm *SimulatorManager) DeleteDevice(deviceID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	device, exists := sm.devices[deviceID]
	if !exists {
		return fmt.Errorf("device %s not found", deviceID)
	}

	// Stop and cleanup device
	if err := device.Stop(); err != nil {
		// log.Printf("Error stopping device %s: %v", deviceID, err)
	}

	// Always close TUN FD on deletion, even for pre-allocated interfaces
	if device.tunIface != nil && device.tunIface.PreAllocated {
		device.tunIface.destroy()
		// Remove from pre-allocated pool
		sm.tunPoolMutex.Lock()
		delete(sm.tunInterfacePool, device.IP.String())
		sm.tunPoolMutex.Unlock()
	}

	delete(sm.devices, deviceID)
	delete(sm.deviceIPs, device.IP.String())
	delete(sm.deviceTypesByIP, device.IP.String())
	return nil
}

func (sm *SimulatorManager) DeleteAllDevices() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var errors []string
	var tunInterfaces []string

	// Collect all TUN interface names for bulk deletion
	for deviceID, device := range sm.devices {
		if err := device.Stop(); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", deviceID, err))
		}
		if device.tunIface != nil {
			// Always close FD on deletion regardless of PreAllocated status
			if device.tunIface.PreAllocated {
				device.tunIface.destroy()
			}
			tunInterfaces = append(tunInterfaces, device.tunIface.Name)
		}
	}

	// Bulk delete TUN interfaces for better performance
	if len(tunInterfaces) > 0 {
		if sm.useNamespace && sm.netNamespace != nil {
			// Delete interfaces in namespace
			if err := sm.bulkDeleteTunInterfacesInNamespace(tunInterfaces); err != nil {
				errors = append(errors, fmt.Sprintf("bulk TUN deletion in namespace: %v", err))
			}
		} else {
			if err := sm.bulkDeleteTunInterfaces(tunInterfaces); err != nil {
				errors = append(errors, fmt.Sprintf("bulk TUN deletion: %v", err))
			}
		}
	}

	// Clear the devices map, IP set, and pre-allocated pool
	sm.devices = make(map[string]*DeviceSimulator)
	sm.deviceIPs = make(map[string]struct{})
	sm.deviceTypesByIP = make(map[string]string)
	sm.tunPoolMutex.Lock()
	sm.tunInterfacePool = make(map[string]*TunInterface)
	sm.tunPoolMutex.Unlock()

	if len(errors) > 0 {
		return fmt.Errorf("errors deleting devices: %s", strings.Join(errors, ", "))
	}
	return nil
}

// Shutdown cleans up all resources including the network namespace
func (sm *SimulatorManager) Shutdown() error {
	log.Println("Shutting down simulator manager...")
	startTime := time.Now()

	// Stop the flow ticker goroutine and close the shared UDP socket.
	// flowStopOnce ensures close(flowStopCh) is safe on repeated Shutdown() calls.
	// flowWg.Wait() ensures the ticker goroutine has exited before we close flowConn,
	// eliminating the data race between WriteTo and conn.Close()/nil.
	if sm.flowActive.Load() {
		sm.flowStopOnce.Do(func() { close(sm.flowStopCh) })
		sm.flowWg.Wait()
		sm.flowActive.Store(false)
	}
	if sm.flowConn != nil {
		sm.flowConn.Close()
		sm.flowConn = nil
	}

	// Stop the trap subsystem (scheduler goroutine + per-device exporters +
	// shared fallback socket). Safe to call when trap export was never started.
	sm.StopTrapExport()

	// Stop the syslog subsystem (same shape as trap). Safe to call when
	// syslog export was never started.
	sm.StopSyslogExport()

	if sm.useNamespace && sm.netNamespace != nil {
		// Fast path: when using a namespace, deleting it instantly destroys all
		// TUN interfaces inside it. No need to delete them one by one.
		// Just close file descriptors and stop listeners in-process, then nuke the namespace.
		sm.shutdownFast()
	} else {
		// Slow path: no namespace, must delete interfaces individually
		if err := sm.DeleteAllDevices(); err != nil {
			log.Printf("Warning: errors deleting devices during shutdown: %v", err)
		}
		sm.CleanupPreAllocatedInterfaces()
	}

	// Cleanup network namespace (deletes all interfaces inside it)
	if sm.netNamespace != nil {
		if err := sm.netNamespace.Close(); err != nil {
			log.Printf("Warning: failed to close network namespace: %v", err)
		}
		sm.netNamespace = nil
	}

	elapsed := time.Since(startTime)
	log.Printf("Simulator manager shutdown complete in %v", elapsed)
	return nil
}

// shutdownFast stops all device listeners and closes FDs without deleting TUN interfaces.
// The caller is responsible for deleting the namespace, which destroys all interfaces at once.
func (sm *SimulatorManager) shutdownFast() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	deviceCount := len(sm.devices)
	log.Printf("Fast shutdown: closing %d devices (namespace deletion will clean up TUN interfaces)...", deviceCount)

	// Stop all device services (closes UDP/TCP listeners and TUN FDs)
	for _, device := range sm.devices {
		device.stopListenersOnly()
	}

	// Clear maps
	sm.devices = make(map[string]*DeviceSimulator)
	sm.tunPoolMutex.Lock()
	// Close pre-allocated TUN FDs
	for _, tunIface := range sm.tunInterfacePool {
		if tunIface != nil {
			tunIface.destroy()
		}
	}
	sm.tunInterfacePool = make(map[string]*TunInterface)
	sm.tunPoolMutex.Unlock()

	log.Printf("Fast shutdown: all %d device listeners closed", deviceCount)
}

// SetupRoutesForDevices adds host routes to make devices accessible from external machines
func (sm *SimulatorManager) SetupRoutesForDevices(startIP string, count int, netmask string) error {
	if !sm.useNamespace || sm.netNamespace == nil {
		// No namespace, routes not needed (interfaces are in root namespace)
		return nil
	}

	return sm.netNamespace.AddRouteForDevices(startIP, count, netmask)
}

// SetupRoutesFromDevices adds host routes based on actual device IPs rather than
// calculating from startIP + count, ensuring no subnets are missed.
func (sm *SimulatorManager) SetupRoutesFromDevices(netmask string) error {
	if !sm.useNamespace || sm.netNamespace == nil {
		return nil
	}

	sm.mu.RLock()
	// Collect unique /24 subnets from all devices
	subnets := make(map[string]bool)
	for _, device := range sm.devices {
		ip := device.IP.To4()
		if ip == nil {
			continue
		}
		subnet := fmt.Sprintf("%d.%d.%d.0/%s", ip[0], ip[1], ip[2], netmask)
		subnets[subnet] = true
	}
	sm.mu.RUnlock()

	for subnet := range subnets {
		if err := sm.netNamespace.addHostRoute(subnet); err != nil {
			log.Printf("Warning: failed to add route for %s: %v", subnet, err)
		}
	}

	return nil
}

// ensureAllSubnetRoutes adds host routes for every /24 subnet between startIP and currentIP.
func (sm *SimulatorManager) ensureAllSubnetRoutes(startIP net.IP, netmask string) {
	if sm.netNamespace == nil {
		return
	}

	start := startIP.To4()
	if start == nil {
		return
	}

	sm.mu.RLock()
	end := sm.currentIP.To4()
	if end == nil {
		sm.mu.RUnlock()
		return
	}
	endCopy := make(net.IP, 4)
	copy(endCopy, end)
	sm.mu.RUnlock()

	for o3 := int(start[2]); o3 <= int(endCopy[2]); o3++ {
		cidr := fmt.Sprintf("%d.%d.%d.0/%s", start[0], start[1], o3, netmask)
		sm.netNamespace.addHostRoute(cidr)
	}
	log.Printf("ensureAllSubnetRoutes: added routes for %d.%d.%d.0 - %d.%d.%d.0",
		start[0], start[1], start[2], endCopy[0], endCopy[1], endCopy[2])
}

// IsUsingNamespace returns whether namespace isolation is active
func (sm *SimulatorManager) IsUsingNamespace() bool {
	return sm.useNamespace && sm.netNamespace != nil
}

// GetNamespaceName returns the namespace name if active
func (sm *SimulatorManager) GetNamespaceName() string {
	if sm.netNamespace != nil {
		return sm.netNamespace.Name
	}
	return ""
}
