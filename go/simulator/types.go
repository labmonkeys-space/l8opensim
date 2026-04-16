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
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// TUN interface management structures
type TunInterface struct {
	Name         string
	IP           net.IP
	fd           int
	PreAllocated bool // Track if this interface was pre-allocated
	InNamespace  bool // Track if this interface is in a network namespace
}

type SNMPResource struct {
	OID      string `json:"oid"`
	Response string `json:"response"`
}

type SSHResource struct {
	Command  string `json:"command"`
	Response string `json:"response"`
}

type APIResource struct {
	Method   string      `json:"method"`             // HTTP method: GET, POST, PUT, DELETE, PATCH
	Path     string      `json:"path"`               // API endpoint path
	Request  interface{} `json:"request,omitempty"`  // Optional request body for POST/PUT
	Response interface{} `json:"response"`           // Response body
}

type DeviceResources struct {
	SNMP []SNMPResource `json:"snmp"`
	SSH  []SSHResource  `json:"ssh"`
	API  []APIResource  `json:"api,omitempty"` // Optional API endpoints for storage devices

	// Performance optimization indexes (not serialized)
	oidIndex    *sync.Map  `json:"-"` // Lock-free OID -> Response mapping for O(1) lookups
	sortedOIDs  []string   `json:"-"` // Pre-sorted OID list for GetNext operations
	oidNextMap  *sync.Map  `json:"-"` // Pre-computed next OID mapping for walks
}

// Device simulator represents a single simulated device
type DeviceSimulator struct {
	ID           string
	IP           net.IP
	SNMPPort     int
	SSHPort      int
	APIPort      int            // HTTP API port for storage devices
	tunIface     *TunInterface
	snmpServer   *SNMPServer
	sshServer    *SSHServer
	apiServer    *APIServer     // HTTP API server for storage devices
	resources    *DeviceResources
	resourceFile string // Track which resource file was used
	sysLocation  string // Dynamic sysLocation for this device
	sysName      string // Dynamic sysName for this device
	// Cached frequently accessed values (lock-free)
	cachedSysName     atomic.Value // Stores string
	cachedSysLocation atomic.Value // Stores string
	metricsCycler *MetricsCycler   // Per-device cycling CPU/memory metrics
	flowExporter  *FlowExporter   // NetFlow/IPFIX exporter (nil if flow export disabled)
	netNamespace  *NetNamespace   // Network namespace (nil if using root namespace)
	running      bool
	mu           sync.RWMutex
}

// SNMPv3 USM (User-based Security Model) configuration
type SNMPv3Config struct {
	Enabled      bool   `json:"enabled"`
	EngineID     string `json:"engine_id"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	AuthProtocol int    `json:"auth_protocol"` // 0=none, 1=MD5, 2=SHA1
	PrivProtocol int    `json:"priv_protocol"` // 0=none, 1=DES, 2=AES128
	PrivPassword string `json:"priv_password"` // Can be same as auth password
}

// SNMPv3 message structures
type SNMPv3Message struct {
	Version        int
	GlobalData     SNMPv3GlobalData
	SecurityParams SNMPv3SecurityParams
	ScopedPDU      []byte // Can be encrypted
}

type SNMPv3GlobalData struct {
	MsgID           int
	MsgMaxSize      int
	MsgFlags        byte
	MsgSecurityModel int
}

type SNMPv3SecurityParams struct {
	AuthoritativeEngineID    string
	AuthoritativeEngineBoots int
	AuthoritativeEngineTime  int
	UserName                 string
	AuthParams               []byte
	PrivParams               []byte
}

type SNMPServer struct {
	device       *DeviceSimulator
	listener     *net.UDPConn
	running      bool
	v3Config     *SNMPv3Config
	cachedDESKey []byte // cached result of generateDESKey()
	cachedAESKey []byte // cached result of generateAESKey()
}

type SSHServer struct {
	device   *DeviceSimulator
	listener net.Listener
	config   *ssh.ServerConfig
	running  bool
	signer   ssh.Signer // SSH host key signer
}

type APIServer struct {
	device        *DeviceSimulator
	listener      net.Listener
	running       bool
	sharedTLSCert *tls.Certificate // Shared TLS cert from SimulatorManager
}

// Manager for all simulated devices
type SimulatorManager struct {
	devices         map[string]*DeviceSimulator
	currentIP       net.IP
	nextTunIndex    int
	deviceResources *DeviceResources
	resourcesCache  map[string]*DeviceResources // Cache for loaded resource files
	sharedSSHSigner ssh.Signer                   // Shared SSH host key for all devices
	sharedTLSCert   *tls.Certificate             // Shared TLS certificate for all API servers

	// Network namespace for device isolation (prevents systemd-networkd overhead)
	netNamespace    *NetNamespace // Network namespace for all simulated devices
	useNamespace    bool          // Whether to use network namespace isolation

	// TUN interface pre-allocation settings
	tunPoolSize     int                   // Size of the pre-allocated pool (0 = no pre-allocation)
	maxWorkers      int                   // Maximum parallel workers for interface creation
	tunInterfacePool map[string]*TunInterface // Pool of pre-allocated interfaces indexed by IP
	tunPoolMutex    sync.RWMutex          // Mutex for interface pool access

	// Status tracking for pre-allocation and device creation
	isPreAllocating      atomic.Value      // bool - true when pre-allocation is in progress
	preAllocProgress     atomic.Value      // int - number of interfaces pre-allocated so far
	isCreatingDevices    atomic.Value      // bool - true when device creation is in progress
	deviceCreateProgress atomic.Value      // int - number of devices created so far
	deviceCreateTotal    atomic.Value      // int - total number of devices to create

	// Flow export state (nil/zero when disabled; set by InitFlowExport)
	flowConn             *net.UDPConn
	flowCollectorAddr    *net.UDPAddr
	flowCollectorStr     string        // original "host:port" string for status reporting
	flowProtocol         string        // normalised protocol name ("netflow9" or "ipfix")
	flowEncoder          FlowEncoder
	flowBufPool          sync.Pool     // supplies []byte(1500); set via flowBufPool.New
	flowActive           atomic.Bool   // true after InitFlowExport; safe for concurrent reads
	flowTickInterval     time.Duration
	flowActiveTimeout    time.Duration
	flowInactiveTimeout  time.Duration
	flowTemplateInterval time.Duration
	flowStopCh           chan struct{}  // closed by Shutdown to stop the ticker goroutine
	flowStopOnce         sync.Once     // ensures flowStopCh is closed exactly once
	flowWg               sync.WaitGroup // tracks the ticker goroutine; Wait before closing flowConn

	// Flow export cumulative counters (updated atomically by tickAllFlowExporters).
	flowStatPackets  atomic.Uint64 // total UDP datagrams sent since InitFlowExport
	flowStatBytes    atomic.Uint64 // total encoded payload bytes sent since InitFlowExport
	flowStatRecords  atomic.Uint64 // total flow records exported since InitFlowExport
	flowStatLastTmpl atomic.Int64  // unix milliseconds of the most recent template transmission

	mu              sync.RWMutex
}

// Resource file info for API
type ResourceInfo struct {
	Filename string `json:"filename"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Category string `json:"category"`
}

// API request/response structures
type CreateDevicesRequest struct {
	StartIP      string         `json:"start_ip"`
	DeviceCount  int            `json:"device_count"`
	Netmask      string         `json:"netmask"`
	ResourceFile string         `json:"resource_file,omitempty"` // Optional resource file selection
	RoundRobin   bool           `json:"round_robin,omitempty"`   // Optional: cycle through device types
	Category     string         `json:"category,omitempty"`      // Optional: filter round robin to a category
	SNMPv3       *SNMPv3Config  `json:"snmpv3,omitempty"`
	PreAllocate  bool           `json:"pre_allocate,omitempty"` // Optional: explicitly enable/disable pre-allocation
	MaxWorkers   int            `json:"max_workers,omitempty"`  // Optional: max workers for pre-allocation
	SNMPPort     int            `json:"snmp_port,omitempty"`    // Optional: UDP port for SNMP listener (default: 161)
}

// RoundRobinDeviceTypes defines all 28 device flavors for round robin creation
var RoundRobinDeviceTypes = []string{
	// Network Devices
	"cisco_catalyst_9500.json",
	"juniper_mx240.json",
	"asr9k.json",
	"palo_alto_pa3220.json",
	"fortinet_fortigate_600e.json",
	"juniper_mx960.json",
	"cisco_nexus_9500.json",
	"huawei_ne8000.json",
	"nec_ix3315.json",
	"arista_7280r3.json",
	"check_point_15600.json",
	"cisco_crs_x.json",
	"cisco_ios.json",
	"extreme_vsp4450.json",
	"nokia_7750_sr12.json",
	"sonicwall_nsa6700.json",
	"dlink_dgs3630.json",
	// Servers
	"dell_poweredge_r750.json",
	"hpe_proliant_dl380.json",
	"ibm_power_s922.json",
	"linux_server.json",
	// GPU Servers
	"nvidia_dgx_a100.json",
	"nvidia_dgx_h100.json",
	"nvidia_hgx_h200.json",
	// Storage
	"netapp_ontap.json",
	"pure_storage_flasharray.json",
	"dell_emc_unity.json",
	"aws_s3_storage.json",
}

type DeviceInfo struct {
	ID         string `json:"id"`
	IP         string `json:"ip"`
	Interface  string `json:"interface,omitempty"`
	SNMPPort   int    `json:"snmp_port"`
	SSHPort    int    `json:"ssh_port"`
	Running    bool   `json:"running"`
	DeviceType string `json:"device_type,omitempty"`
}

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ManagerStatus struct {
	IsPreAllocating      bool `json:"is_pre_allocating"`
	PreAllocProgress     int  `json:"pre_alloc_progress"`
	PreAllocTotal        int  `json:"pre_alloc_total"`
	IsCreatingDevices    bool `json:"is_creating_devices"`
	DeviceCreateProgress int  `json:"device_create_progress"`
	DeviceCreateTotal    int  `json:"device_create_total"`
	TotalDevices         int  `json:"total_devices"`
	RunningDevices       int  `json:"running_devices"`
}

// FlowStatus is the JSON body returned by GET /api/v1/flows/status.
type FlowStatus struct {
	Enabled            bool   `json:"enabled"`
	Protocol           string `json:"protocol,omitempty"`
	Collector          string `json:"collector,omitempty"`
	TotalFlowsExported uint64 `json:"total_flows_exported,omitempty"`
	TotalPacketsSent   uint64 `json:"total_packets_sent,omitempty"`
	TotalBytesSent     uint64 `json:"total_bytes_sent,omitempty"`
	DevicesExporting   int    `json:"devices_exporting,omitempty"`
	LastTemplateSend   string `json:"last_template_send,omitempty"`
}