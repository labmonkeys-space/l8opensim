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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// parseAuthProtocol converts string to authentication protocol constant
func parseAuthProtocol(proto string) int {
	switch strings.ToLower(proto) {
	case "md5":
		return SNMPV3_AUTH_MD5
	case "sha1", "sha":
		return SNMPV3_AUTH_SHA1
	case "none", "":
		return SNMPV3_AUTH_NONE
	default:
		log.Printf("Unknown auth protocol '%s', using MD5", proto)
		return SNMPV3_AUTH_MD5
	}
}

// parsePrivProtocol converts string to privacy protocol constant
func parsePrivProtocol(proto string) int {
	switch strings.ToLower(proto) {
	case "des":
		return SNMPV3_PRIV_DES
	case "aes128", "aes":
		return SNMPV3_PRIV_AES128
	case "none", "":
		return SNMPV3_PRIV_NONE
	default:
		log.Printf("Unknown privacy protocol '%s', using none", proto)
		return SNMPV3_PRIV_NONE
	}
}

// getFirstDeviceKey returns the first device key from the map
func getFirstDeviceKey(devices map[string]*DeviceSimulator) string {
	for key := range devices {
		return key
	}
	return ""
}

// setupSignalHandler sets up graceful shutdown on SIGINT/SIGTERM
func setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("\nReceived signal %v, shutting down gracefully...", sig)

		// Cleanup manager (deletes devices, cleans up namespace)
		if manager != nil {
			if err := manager.Shutdown(); err != nil {
				log.Printf("Error during shutdown: %v", err)
			}
		}

		log.Println("Shutdown complete")
		os.Exit(0)
	}()
}

func main() {
	// Define command-line flags
	var (
		autoStartIP     = flag.String("auto-start-ip", "", "Auto-create devices starting from this IP address (e.g., 192.168.100.1)")
		autoCount       = flag.Int("auto-count", 0, "Number of devices to auto-create (requires -auto-start-ip)")
		autoNetmask     = flag.String("auto-netmask", "24", "Netmask for auto-created devices (default: 24)")
		snmpv3EngineID  = flag.String("snmpv3-engine-id", "", "Enable SNMPv3 with specified engine ID (e.g., 800000090300AABBCCDD)")
		snmpv3AuthProto = flag.String("snmpv3-auth", "md5", "SNMPv3 authentication protocol: none, md5, sha1 (default: md5)")
		snmpv3PrivProto = flag.String("snmpv3-priv", "none", "SNMPv3 privacy protocol: none, des, aes128 (default: none)")
		port            = flag.String("port", "8080", "Server port (default: 8080)")
		snmpPort        = flag.Int("snmp-port", DEFAULT_SNMP_PORT, "UDP port for SNMP listener on each device (default: 161)")
		noNamespace     = flag.Bool("no-namespace", false, "Disable network namespace isolation (use root namespace)")
		showHelp        = flag.Bool("help", false, "Show this help message")
		ifScenario      = flag.Int("if-scenario", 2, "Interface state scenario: 1=all-shutdown, 2=all-normal (default), 3=all-failure, 4=pct-failure")
		ifFailurePct    = flag.Int("if-failure-pct", 10, "Percentage of interfaces with oper-down (used with -if-scenario 4, 0–100)")

		// Flow export flags
		flowCollector        = flag.String("flow-collector", "", "NetFlow/IPFIX collector address (host:port, e.g. 192.168.1.100:2055); disables flow export when empty")
		flowProtocol         = flag.String("flow-protocol", "netflow9", "Flow export protocol: netflow9 (default), ipfix, netflow5. Under netflow5, -flow-template-interval is accepted but has no effect (v5 has no template mechanism)")
		flowActiveSecs       = flag.Int("flow-active-timeout", 30, "Active flow timeout in seconds (default: 30)")
		flowInactiveSecs     = flag.Int("flow-inactive-timeout", 15, "Inactive flow timeout in seconds (default: 15)")
		flowTemplateIntervalSecs = flag.Int("flow-template-interval", 60, "Template retransmission interval in seconds (default: 60)")
		flowTickSecs         = flag.Int("flow-tick-interval", 5, "Flow ticker interval in seconds (default: 5)")
		flowSourcePerDevice  = flag.Bool("flow-source-per-device", true, "Bind a per-device UDP socket inside the opensim namespace so flow packets use the device's IP as the source address (default: true). Requires the opensim ns to have a route to the collector; set to false to use a single shared socket from the host namespace")
	)

	flag.Parse()

	// Apply interface state scenario
	ifStateConfig = &IfStateConfig{
		Scenario:   *ifScenario,
		FailurePct: *ifFailurePct,
	}

	// Show help if requested
	if *showHelp {
		fmt.Println("Layer 8 Data Center Simulator with TUN/TAP support")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Printf("  %s [options]\n", os.Args[0])
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Network Namespace Isolation:")
		fmt.Println("  By default, devices are created in a dedicated network namespace ('opensim')")
		fmt.Println("  to prevent systemd-networkd from consuming excessive CPU/memory with many devices.")
		fmt.Println("  External machines can still access devices via static routes to this host.")
		fmt.Println("  Use -no-namespace to disable this (not recommended for 1000+ devices).")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Printf("  %s                                                    # Start server only\n", os.Args[0])
		fmt.Printf("  %s -auto-start-ip 192.168.100.1 -auto-count 5       # Auto-create 5 devices\n", os.Args[0])
		fmt.Printf("  %s -auto-start-ip 10.10.10.1 -auto-count 3 -port 9090  # Custom API port\n", os.Args[0])
		fmt.Printf("  %s -auto-start-ip 10.10.10.1 -auto-count 3 -snmp-port 1161  # Non-privileged SNMP port\n", os.Args[0])
		fmt.Printf("  %s -auto-start-ip 192.168.100.1 -auto-count 30000      # 30K devices (uses namespace)\n", os.Args[0])
		fmt.Printf("  %s -auto-start-ip 192.168.100.1 -auto-count 100 -no-namespace  # Disable namespace\n", os.Args[0])
		fmt.Printf("  %s -auto-start-ip 192.168.100.1 -auto-count 2 \\      # SNMPv3 with MD5 auth\n", os.Args[0])
		fmt.Printf("    -snmpv3-engine-id 800000090300AABBCCDD -snmpv3-auth md5\n")
		fmt.Println()
		return
	}

	log.Println("Layer 8 Data Center Simulator with TUN/TAP support starting...")

	// Check if running as root
	if os.Geteuid() != 0 {
		log.Println("WARNING: Not running as root. TUN/TAP interface creation will fail.")
		log.Println("Please run with: sudo ./simulator")
	}

	// Initialize manager with namespace support (unless disabled)
	useNamespace := !*noNamespace
	manager = NewSimulatorManagerWithOptions(useNamespace)

	// Setup signal handler for graceful shutdown
	setupSignalHandler()

	// Load world cities from CSV file
	if err := loadWorldCities(); err != nil {
		log.Printf("Warning: failed to load world cities: %v", err)
	}

	// Load default resources - look for asr9k first, then fallback to cisco_ios
	err := manager.LoadResources("resources/asr9k.json")
	if err != nil {
		log.Printf("Failed to load ASR9K resources: %v", err)
		log.Println("Trying to load default Cisco IOS resources...")
		err = manager.LoadResources("resources/cisco_ios.json")
		if err != nil {
			log.Fatalf("Failed to load any resources: %v", err)
		}
	}

	// Enable flow export if a collector address was provided.
	if *flowCollector != "" {
		manager.SetFlowSourcePerDevice(*flowSourcePerDevice)
		err := manager.InitFlowExport(
			*flowCollector,
			*flowProtocol,
			time.Duration(*flowActiveSecs)*time.Second,
			time.Duration(*flowInactiveSecs)*time.Second,
			time.Duration(*flowTemplateIntervalSecs)*time.Second,
			time.Duration(*flowTickSecs)*time.Second,
		)
		if err != nil {
			log.Fatalf("Failed to initialize flow export: %v", err)
		}
	}

	// Validate auto-creation parameters
	if *autoStartIP != "" && *autoCount <= 0 {
		log.Println("WARNING: -auto-start-ip provided but -auto-count is 0 or negative. No devices will be auto-created.")
	} else if *autoStartIP == "" && *autoCount > 0 {
		log.Println("WARNING: -auto-count provided but -auto-start-ip is empty. No devices will be auto-created.")
	}

	// Setup REST API first
	router := setupRoutes()

	// Start API server in background
	apiPort := ":" + *port
	log.Printf("Layer 8 Data Center Simulator server starting on port %s", apiPort)
	log.Println()
	log.Println("Web UI:")
	log.Printf("  http://localhost%s/", apiPort)
	log.Printf("  http://localhost%s/ui", apiPort)
	log.Println()

	// Start web server in background
	go func() {
		log.Fatal(http.ListenAndServe(apiPort, router))
	}()

	// Give web server a moment to start
	time.Sleep(100 * time.Millisecond)
	log.Printf("Web UI is now available at http://localhost%s/ui", apiPort)
	log.Println()

	// Auto-create devices in background if requested
	if *autoStartIP != "" && *autoCount > 0 {
		go func() {
			log.Printf("Starting background device creation: %d devices from %s/%s", *autoCount, *autoStartIP, *autoNetmask)

			// Create SNMPv3 configuration if engine ID is provided
			var v3Config *SNMPv3Config
			if *snmpv3EngineID != "" {
				authProto := parseAuthProtocol(*snmpv3AuthProto)
				privProto := parsePrivProtocol(*snmpv3PrivProto)

				v3Config = &SNMPv3Config{
					Enabled:      true,
					EngineID:     *snmpv3EngineID,
					Username:     USERNAME, // Use same as SSH
					Password:     PASSWORD, // Use same as SSH
					AuthProtocol: authProto,
					PrivProtocol: privProto,
					PrivPassword: PASSWORD, // Use same password for privacy
				}
				log.Printf("SNMPv3 enabled with engine ID: %s, auth: %s, priv: %s",
					*snmpv3EngineID, *snmpv3AuthProto, *snmpv3PrivProto)
			}

			err := manager.CreateDevices(*autoStartIP, *autoCount, *autoNetmask, "", v3Config, false, "", *snmpPort)
			if err != nil {
				log.Printf("Failed to auto-create devices: %v", err)
			} else {
				log.Printf("Successfully auto-created %d devices", *autoCount)
			}
		}()
	}

	// Print API documentation
	log.Println("API Endpoints:")
	log.Println("  POST   /api/v1/devices           - Create devices")
	log.Println("  GET    /api/v1/devices           - List devices")
	log.Println("  GET    /api/v1/devices/export    - Export devices to CSV")
	log.Println("  GET    /api/v1/devices/routes    - Download route configuration script")
	log.Println("  DELETE /api/v1/devices/{id}      - Delete device")
	log.Println("  DELETE /api/v1/devices           - Delete all devices")
	log.Println("  GET    /health                   - Health check")
	log.Println()
	log.Println("Example curl commands:")
	log.Printf(`  curl -X POST http://localhost%s/api/v1/devices -H "Content-Type: application/json" -d '{"start_ip":"192.168.100.1","device_count":3,"netmask":"24"}'`, apiPort)
	log.Println()
	log.Printf(`  curl http://localhost%s/api/v1/devices`, apiPort)
	log.Println()
	log.Printf(`  curl http://localhost%s/api/v1/devices/export -o devices.csv`, apiPort)
	log.Println()
	log.Printf(`  curl http://localhost%s/api/v1/devices/routes -o add_routes.sh`, apiPort)
	log.Println()
	log.Println()
	log.Println("SNMPv3 + SSH Examples:")
	log.Println("  Create devices with SNMPv3 support:")
	log.Printf("    sudo ./sim -auto-start-ip 192.168.100.1 -auto-count 2 \\")
	log.Println()
	log.Printf("      -snmpv3-engine-id 800000090300AABBCCDD -snmpv3-auth md5")
	log.Println()
	log.Println()
	log.Printf("  Or via REST API with SNMPv3:")
	log.Printf(`    curl -X POST http://localhost%s/api/v1/devices \`, apiPort)
	log.Println()
	log.Printf(`      -H "Content-Type: application/json" \`)
	log.Println()
	log.Printf(`      -d '{"start_ip":"192.168.100.1","device_count":1,"netmask":"24",`)
	log.Println()
	log.Printf(`           "snmpv3":{"enabled":true,"engine_id":"800000090300AABBCCDD",`)
	log.Println()
	log.Printf(`           "username":"simadmin","password":"simadmin","auth_protocol":1,"priv_protocol":0}}'`)
	log.Println()
	log.Println()
	log.Println("Connection Examples:")
	log.Println("  SSH (same credentials for all devices):")
	log.Println("    ssh simadmin@<device-ip>")
	log.Println("    Password: simadmin")
	log.Println()
	log.Println("  SNMP v2c (traditional):")
	log.Println("    snmpwalk -v2c -c public <device-ip> 1.3.6.1.2.1.1.1.0")
	log.Println()
	log.Println("  SNMPv3 (when enabled):")
	log.Println("    # MD5 auth, no privacy:")
	log.Println("    snmpwalk -v3 -u simadmin -A simadmin -a MD5 -l authNoPriv <device-ip> 1.3.6.1.2.1.1.1.0")
	log.Println()
	log.Println("    # MD5 auth + DES privacy:")
	log.Println("    snmpwalk -v3 -u simadmin -A simadmin -X simadmin -a MD5 -x DES -l authPriv <device-ip> 1.3.6.1.2.1.1.1.0")
	log.Println()
	log.Println("Additional Tips:")
	log.Println("  - Open the Web UI in your browser for easy management")
	log.Println("  - All devices use same credentials: simadmin/simadmin")
	log.Println("  - SNMPv2c community: public")
	log.Println("  - Check TUN interfaces: ip addr show | grep sim")
	log.Println("  - Test script available: ./test_snmpv3.sh")

	// Keep the main thread alive
	select {}
}
