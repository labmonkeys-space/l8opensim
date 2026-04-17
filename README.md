# OpenSim - Layer 8 Data Center Simulator

![OpenSim Logo](opensim.png)
![OpenSim Logo1](opensim1.png)

A powerful, scalable network and infrastructure simulator that provides realistic SNMP v2c/v3, SSH, and HTTPS REST API interfaces for testing network management applications, monitoring systems, and automation tools. OpenSim can simulate thousands of network devices, GPU servers, storage systems, and Linux servers with dedicated IP addresses using TUN interfaces and Linux network namespaces.

## Features

- **Multi-Protocol Support**: SNMP v2c/v3 (MD5/SHA1 auth, DES/AES128 privacy), SSH with VT100 terminal emulation, and HTTPS REST API simulation
- **Scalable Architecture**: Support for 30,000+ concurrent simulated devices with parallel TUN pre-allocation
- **28 Device Types**: Routers, switches, firewalls, servers, GPU servers (NVIDIA DGX/HGX), storage systems, and Linux servers across 8 categories
- **GPU Server Simulation**: NVIDIA DGX-A100, DGX-H100, and HGX-H200 with per-GPU metrics (utilization, VRAM, temperature, power, fan speed, clock speeds) via NVIDIA DCGM OIDs
- **Dynamic Metrics**: Realistic CPU, memory, temperature, and GPU metrics with 100-point sine-wave cycling patterns and correlated metric generation
- **Dynamic HC Interface Counters**: `ifHCInOctets`/`ifHCOutOctets` (ifXTable) are monotonically increasing Counter64 values computed analytically on-demand — no polling loop or goroutine. Byte-rate follows a sine wave between 60 % and 100 % of interface speed on a 1-hour period; counters are pre-seeded with ~24 h of traffic for realism from the first poll
- **Network Namespace Isolation**: Devices run in a dedicated `opensim` network namespace for realistic isolation
- **TUN Interface Integration**: Each device gets its own IP address via TUN interfaces with parallel pre-allocation for fast creation
- **HTTPS Storage APIs**: Secure REST API endpoints for storage device simulation with shared TLS certificates
- **Web Management UI**: Web interface for device management with real-time monitoring and system stats
- **RESTful API**: Complete REST API for programmatic control with round-robin and category-based device creation
- **High Performance**: Pre-generated metrics, lock-free sync.Map for O(1) OID lookups, pre-computed next-OID mappings, buffer pool optimization, shared SSH/TLS keys
- **Device Export**: Export device configurations to CSV and routing scripts (Debian/Ubuntu support)
- **Routing Protocol Support**: OSPF, BGP, and VRF simulation via SSH commands
- **Storage System Simulation**: AWS S3, Pure Storage, NetApp ONTAP, Dell EMC Unity with HTTPS REST APIs
- **Linux Server Simulation**: Comprehensive Ubuntu server with 36+ SSH commands
- **CDP & LLDP Support**: Cisco Discovery Protocol and LLDP for network topology discovery
- **World Cities**: Device sysLocation populated from 98 world cities datasets for realistic geographic distribution
- **Flow Export**: Per-device NetFlow v5 (Cisco), NetFlow v9 (RFC 3954), IPFIX (RFC 7011), and sFlow v5 (sflow_version_5.txt) export to any UDP collector; protocol-aware batch pagination, template refresh (v9/IPFIX), and a `/api/v1/flows/status` endpoint with cumulative counters. sFlow output is synthesised from `FlowCache` records — the simulator does not observe real packet streams
- **Layer 8 Integration**: Optional L8 vnet overlay with HTTPS web proxy for distributed deployment

## Quick Start

### Prerequisites

- Linux system with root access (required for TUN interface and network namespace creation)
- Go 1.26+ installed
- Basic networking tools (`ip`, `iptables`)

### Installation

1. **Clone the repository:**
   ```bash
   git clone https://github.com/saichler/opensim.git
   cd opensim
   ```

2. **Install dependencies:**
   ```bash
   cd go
   go mod tidy
   ```

3. **Build the simulator:**
   ```bash
   cd simulator
   go build -o simulator .
   ```

4. **Run with root privileges:**
   ```bash
   sudo ./simulator
   ```

### Auto-Setup for Ubuntu

For Ubuntu systems, use the automated setup script:

```bash
sudo ./ubuntu_setup.sh
```

This script installs all dependencies, configures system limits, and sets up TUN/TAP support.

## Usage

### Command Line Options

```bash
sudo ./simulator [options]

Options:
  -auto-start-ip string       Auto-create devices starting from this IP (e.g., 192.168.100.1)
  -auto-count int             Number of devices to auto-create (requires -auto-start-ip)
  -auto-netmask string        Netmask for auto-created devices (default: "24")
  -port string                HTTP API server port (default: "8080")
  -snmp-port int              UDP port for the SNMP listener on each device (default: 161)
  -snmpv3-engine-id string    Enable SNMPv3 with specified engine ID
  -snmpv3-auth string         SNMPv3 auth protocol: none, md5, sha1 (default: "md5")
  -snmpv3-priv string         SNMPv3 privacy protocol: none, des, aes128 (default: "none")
  -no-namespace               Disable network namespace isolation (use root namespace)
  -if-scenario int            Interface state scenario: 1=all-shutdown, 2=all-normal (default), 3=all-failure, 4=pct-failure
  -if-failure-pct int         Percentage of interfaces with oper-down (used with -if-scenario 4, 0–100, default: 10)
  -flow-collector string      Enable flow export to this UDP collector (e.g., 192.168.1.10:2055)
  -flow-protocol string       Flow export protocol: netflow9 (default) | ipfix | netflow5 | sflow (alias: sflow5)
  -flow-tick-interval int     Flow ticker interval in seconds (default: 5)
  -flow-active-timeout int    Active flow timeout in seconds (default: 30)
  -flow-inactive-timeout int  Inactive flow timeout in seconds (default: 15)
  -flow-template-interval int Template retransmission interval in seconds (default: 60)
  -flow-source-per-device     Use each device's IP as the UDP source address (default: true)
  -help                       Show help message
```

### Examples

```bash
# Start server only (all interfaces up/up by default)
sudo ./simulator

# Auto-create 5 devices starting from 192.168.100.1
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 5

# Custom API port and subnet
sudo ./simulator -auto-start-ip 10.10.10.1 -auto-count 100 -port 9090

# Use a non-privileged SNMP port (avoids requiring CAP_NET_BIND_SERVICE)
sudo ./simulator -auto-start-ip 10.10.10.1 -auto-count 10 -snmp-port 1161

# Enable SNMPv3 with MD5 authentication and AES128 privacy
sudo ./simulator -snmpv3-engine-id 0x80001234 -snmpv3-auth md5 -snmpv3-priv aes128

# Disable network namespace isolation
sudo ./simulator -no-namespace -auto-start-ip 192.168.100.1 -auto-count 10

# Simulate a maintenance window — all interfaces admin-shutdown (scenario 1)
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 10 -if-scenario 1

# Simulate a link failure — all interfaces admin-up but oper-down (scenario 3)
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 10 -if-scenario 3

# Simulate a partial outage — 30% of interfaces oper-down (scenario 4)
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 10 \
    -if-scenario 4 -if-failure-pct 30
```

### Interface State Scenarios

The `-if-scenario` flag controls the SNMP admin/oper status reported for all simulated interfaces, allowing you to reproduce common network conditions without editing resource files.

| Scenario | Name | ifAdminStatus | ifOperStatus | Use case |
|----------|------|--------------|--------------|----------|
| 1 | all-shutdown | down (2) | down (2) | Planned maintenance, device decommission |
| 2 | all-normal *(default)* | up (1) | up (1) | Normal steady-state operations |
| 3 | all-failure | up (1) | down (2) | Link failures, SFP issues, cable pull |
| 4 | pct-failure | up (1) | down for n% | Partial outage, staged rollout testing |

Scenario 4 uses a deterministic rule (`ifIndex % 100 < n`) so test runs are reproducible across restarts.

```bash
# Verify interface states with snmpwalk
# All oper-down with scenario 3:
snmpwalk -v2c -c public 192.168.100.1 1.3.6.1.2.1.2.2.1.8   # ifOperStatus

# Spot-check admin status (should all be "1" in scenarios 2/3/4):
snmpwalk -v2c -c public 192.168.100.1 1.3.6.1.2.1.2.2.1.7   # ifAdminStatus
```

## Flow Export (NetFlow v5 / v9 / IPFIX / sFlow v5)

OpenSim can emit synthetic flow telemetry to any NetFlow v5 (Cisco), NetFlow v9 (RFC 3954), IPFIX (RFC 7011), or sFlow v5 (sflow_version_5.txt) collector. Each simulated device generates realistic flows that reflect its role (edge router, data-center switch, firewall, etc.).

**sFlow caveat:** sFlow is a packet-sampling protocol built for real devices that observe real traffic. OpenSim has no packet stream to sample — sFlow output is synthesised from the same `FlowCache` records the other protocols consume, re-wrapped as `FLOW_SAMPLE` records with a fixed, synthetic `sampling_rate` of `10 × FlowProfile.ConcurrentFlows`. Collectors that multiply sample rate by captured packet count to estimate link utilisation will produce plausibly-shaped numbers that do not reflect any real traffic. Use sFlow mode for collector-plumbing validation, not for link-volume benchmarks.

By default (`-flow-source-per-device=true`), each device binds its own UDP socket inside the `opensim` namespace so the collector observes flow packets with the **device's IP as the source address**, not the simulator host's. This makes per-device attribution work out of the box on collectors that key on the exporter source IP (e.g. OpenNMS, Elastiflow, nfcapd). Set the flag to `false` to fall back to a single shared socket bound in the host namespace.

### Starting flow export

```bash
# Export NetFlow v9 to a local collector on port 2055
sudo ./simulator -auto-start-ip 10.0.0.1 -auto-count 100 \
  -flow-collector 192.168.1.10:2055

# Use IPFIX instead
sudo ./simulator -auto-start-ip 10.0.0.1 -auto-count 100 \
  -flow-collector 192.168.1.10:4739 -flow-protocol ipfix

# Use sFlow v5 (default UDP port 6343)
sudo ./simulator -auto-start-ip 10.0.0.1 -auto-count 100 \
  -flow-collector 192.168.1.10:6343 -flow-protocol sflow

# Faster ticks for high-fidelity testing (integer seconds)
sudo ./simulator -auto-start-ip 10.0.0.1 -auto-count 10 \
  -flow-collector 127.0.0.1:9999 -flow-tick-interval 1

# Disable per-device source IP (export from host IP instead)
sudo ./simulator -auto-start-ip 10.0.0.1 -auto-count 100 \
  -flow-collector 192.168.1.10:2055 -flow-source-per-device=false
```

### Prerequisites for per-device source IP

When `-flow-source-per-device` is enabled (default), flow packets originate from inside the `opensim` namespace and must traverse the `veth-sim-host` ↔ `veth-sim-ns` pair to reach the collector. A few things have to be in place:

- **`iptables` must be installed on the simulator host.** At startup, OpenSim inserts `iptables -I FORWARD 1 -i veth-sim-host -j ACCEPT` so that hosts with a default-DROP `FORWARD` policy (common when Docker is installed) let per-device egress through. The rule is removed on clean shutdown. Without `iptables` the warning is logged and flows will be silently dropped on such hosts.
- **Route to the collector from the namespace.** The namespace has a default route via `veth-sim-host` (`10.254.0.1`), so any collector reachable from the host via its normal routing table is reachable from the namespace. If you've customised host routing, verify with `ip netns exec opensim ip route get <collector-ip>`.
- **Collector-side `rp_filter`.** Reverse-path filtering on the collector machine may drop flow packets whose source IP (e.g. `10.0.0.x`) isn't reachable back through the receiving interface. Relax it per-interface if needed:
  ```bash
  sudo sysctl -w net.ipv4.conf.all.rp_filter=2
  sudo sysctl -w net.ipv4.conf.<iface>.rp_filter=2
  ```
  (`2` = loose mode; `0` disables filtering entirely.) The simulator side auto-configures its own `rp_filter` and `forwarding` sysctls — no user action needed there.

### Troubleshooting

If the collector doesn't see flows:

1. `curl http://localhost:8080/api/v1/flows/status` — confirm `enabled: true`, `devices_exporting > 0`, and `total_packets_sent` increasing.
2. `sudo tcpdump -ni any udp port <collector-port>` on the simulator host — packets should be visible with device IPs as sources.
3. `sudo iptables -L FORWARD -v -n` — verify the `ACCEPT … veth-sim-host` rule is present (packet counter should be non-zero).
4. Same `tcpdump` on the collector host — if packets arrive but the collector doesn't count them, check `rp_filter` (above) and any firewall rules.
5. As a diagnostic, restart with `-flow-source-per-device=false` to rule out namespace/forwarding issues; flows will then use the host IP as the source.

### Protocol details

| Protocol   | Version field | Template ID       | Record size             | Timestamps                           |
|------------|---------------|-------------------|-------------------------|--------------------------------------|
| NetFlow v5 | `5`           | n/a (no template) | 48 B/record (30 max)    | SysUptime-relative ms (First/Last)   |
| NetFlow v9 | `9`           | FlowSet ID 0      | 45 B/record             | SysUptime-relative ms (FIRST/LAST_SWITCHED) |
| IPFIX      | `10`          | Set ID 2          | 53 B/record             | Absolute epoch ms (IE 152/153)       |
| sFlow v5   | `5` (XDR)     | n/a (self-describing) | ~100 B/record typical (variable) | uptime (ms) + `sampling_rate` per sample |

NetFlow v5/v9 and IPFIX all use the same 18-field template (bytes, packets, protocol, ToS, TCP flags, src/dst ports, src/dst IPv4, src/dst mask, ingress/egress interface, next-hop, src/dst AS, timestamps) — v5 bakes this into a fixed 48-byte on-wire record and has no template mechanism at all, so `-flow-template-interval` is a silent no-op under both v5 and sFlow.

sFlow v5 emits one `FLOW_SAMPLE` per `FlowRecord` with a `sampled_header` flow-record carrying a synthesised IPv4+UDP/TCP header derived from the 5-tuple. On every tick it also emits `COUNTERS_SAMPLE` records (Phase 2): one per interface carrying that interface's `if_counters` record (with `source_id = ifIndex` so collectors such as OpenNMS Telemetryd can key by ds_index), plus one device-wide sample carrying a `processor_information` record (format 1001) whose standard `total_memory` / `free_memory` fields convey the device's memory totals. `sampling_rate` is fixed at `10 × FlowProfile.ConcurrentFlows` — see the caveat above.

### Flow status API

```bash
curl http://localhost:8080/api/v1/flows/status
```

```json
{
  "success": true,
  "message": "Success",
  "data": {
    "enabled": true,
    "protocol": "ipfix",
    "collector": "192.168.1.10:4739",
    "total_flows_exported": 1824300,
    "total_packets_sent": 91215,
    "total_bytes_sent": 136823040,
    "devices_exporting": 100,
    "last_template_send": "2026-04-16T10:35:00Z"
  }
}
```

When flow export is disabled the response is `{"enabled": false}`.

## Web Interface

Access the web UI at `http://localhost:8080/` for:

- Create and manage simulated devices with category filtering
- Choose specific device types or round-robin across all 28 types
- View device status, system stats (memory, CPU, load average)
- Export device lists to CSV
- Generate routing scripts
- Filter devices by ID, IP, interface, type, ports, or status

## REST API Reference

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/devices` | POST | Create devices (bulk, round-robin, category-based) |
| `/api/v1/devices` | GET | List all devices |
| `/api/v1/devices/{id}` | DELETE | Delete a specific device |
| `/api/v1/devices` | DELETE | Delete all devices |
| `/api/v1/devices/export` | GET | Export device list to CSV |
| `/api/v1/devices/routes` | GET | Generate routing script |
| `/api/v1/resources` | GET | List available device resource types |
| `/api/v1/status` | GET | Manager status |
| `/api/v1/system-stats` | GET | System stats (file descriptors, memory) |
| `/api/v1/flows/status` | GET | Flow export status and cumulative counters |
| `/health` | GET | Health check endpoint |

### Create Devices
```bash
# Create 10 devices with default round-robin
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 10,
    "netmask": "24",
    "round_robin": true
  }'

# Create devices on a non-privileged SNMP port
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 5,
    "netmask": "24",
    "snmp_port": 1161
  }'

# Create devices filtered by category
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 3,
    "netmask": "24",
    "round_robin": true,
    "category": "GPU Servers"
  }'

# Create devices with SNMPv3
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 5,
    "netmask": "24",
    "snmpv3": {
      "enabled": true,
      "engine_id": "0x80001234",
      "username": "admin",
      "password": "authpass123",
      "auth_protocol": "md5",
      "priv_protocol": "aes128"
    }
  }'
```

### List Devices
```bash
curl http://localhost:8080/api/v1/devices
```

### Export Devices to CSV
```bash
curl http://localhost:8080/api/v1/devices/export -o devices.csv
```

### Download Route Script
```bash
curl http://localhost:8080/api/v1/devices/routes -o add_routes.sh
```

### Delete Device
```bash
curl -X DELETE http://localhost:8080/api/v1/devices/{device-id}
```

### Delete All Devices
```bash
curl -X DELETE http://localhost:8080/api/v1/devices
```

## Device Interaction

### SSH Access
```bash
# Connect to any simulated device (VT100 terminal emulation)
ssh simadmin@192.168.100.1
# Password: simadmin

# Example commands:
show version
show interfaces
show ip route
ping 8.8.8.8
```

### SNMP Queries
```bash
# SNMPv2c query
snmpget -v2c -c public 192.168.100.1 1.3.6.1.2.1.1.1.0

# Walk interface table
snmpwalk -v2c -c public 192.168.100.1 1.3.6.1.2.1.2.2.1

# Query on a custom SNMP port (e.g. 1161)
snmpwalk -v2c -c public -p 1161 192.168.100.1 1.3.6.1.2.1.1

# SNMPv3 query (when enabled)
snmpget -v3 -l authPriv -u admin -a MD5 -A authpass123 -x AES -X privpass123 \
  -e 0x80001234 192.168.100.1 1.3.6.1.2.1.1.1.0
```

#### Dynamic HC Interface Traffic Counters

`ifHCInOctets` (`.1.3.6.1.2.1.31.1.1.1.6`) and `ifHCOutOctets` (`.1.3.6.1.2.1.31.1.1.1.10`) are generated dynamically — the byte-rate oscillates between 60 % and 100 % of the interface's reported speed on a 1-hour sine wave. Each interface has a random phase offset so interfaces do not peak simultaneously. Counter values are pre-seeded with ~24 h of traffic so they appear realistic from the very first poll.

```bash
# Walk ifXTable to see all HC counters (updates every poll)
snmpwalk -v2c -c public 192.168.100.1 1.3.6.1.2.1.31.1.1

# Fetch HC in/out for interface 1 directly
snmpget -v2c -c public 192.168.100.1 \
  1.3.6.1.2.1.31.1.1.1.6.1 \
  1.3.6.1.2.1.31.1.1.1.10.1

# Continuous traffic rate monitoring (poll every 10 s)
watch -n 10 "snmpget -v2c -c public 192.168.100.1 \
  1.3.6.1.2.1.31.1.1.1.6.1 1.3.6.1.2.1.31.1.1.1.10.1"
```

### Routing Protocol Commands
```bash
# On supported router devices
ssh simadmin@192.168.100.1

show ip ospf neighbor         # OSPF neighbors
show ip bgp summary          # BGP peering summary
show ip vrf                  # VRF instances
```

### Linux Server Commands
```bash
# Connect to a Linux server device
ssh simadmin@192.168.100.1

# Available commands include:
uname -a              # System information
cat /etc/os-release   # OS details
lscpu                 # CPU information
free -h               # Memory usage
df -h                 # Disk space
ip addr show          # Network interfaces
ps aux                # Running processes
docker ps             # Container status
systemctl list-units  # Running services
```

### CDP & LLDP Discovery
```bash
# On Cisco devices, view network neighbors
ssh simadmin@192.168.100.1

show cdp neighbors           # Brief neighbor list
show cdp neighbors detail    # Detailed neighbor info
show lldp neighbors          # LLDP neighbor discovery
```

## Storage System Simulation

OpenSim supports enterprise storage system simulation with HTTPS REST API endpoints on port 8443 using shared TLS certificates.

### Supported Storage Systems

| System | Protocols | Key Features |
|--------|-----------|--------------|
| AWS S3 | SNMP, SSH, REST | Bucket operations, object management, versioning |
| Pure Storage FlashArray | SNMP, SSH, REST | Volumes, hosts, pods, data reduction metrics |
| NetApp ONTAP | SNMP, SSH, REST | Aggregates, volumes, NFS/CIFS/iSCSI protocols |
| Dell EMC Unity | SNMP, SSH, REST | Pools, LUNs, filesystems, health monitoring |

### Storage API Examples

**Pure Storage FlashArray:**
```bash
# List volumes
curl -k https://192.168.100.1:8443/api/2.14/volumes

# Get array information
curl -k https://192.168.100.1:8443/api/2.14/arrays

# Space analytics
curl -k https://192.168.100.1:8443/api/2.14/arrays/space
```

**NetApp ONTAP:**
```bash
# Cluster info
curl -k https://192.168.100.1:8443/api/cluster

# List volumes
curl -k https://192.168.100.1:8443/api/storage/volumes

# Aggregates
curl -k https://192.168.100.1:8443/api/storage/aggregates
```

**AWS S3:**
```bash
# List buckets
curl http://192.168.100.1:8443/

# Bucket contents
curl http://192.168.100.1:8443/my-bucket
```

### Creating Storage Devices
```bash
# Create a Pure Storage device
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 1,
    "netmask": "24",
    "resource_file": "pure_storage_flasharray.json"
  }'

# Create a NetApp device
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.2",
    "device_count": 1,
    "netmask": "24",
    "resource_file": "netapp_ontap.json"
  }'
```

## GPU Server Simulation

OpenSim provides first-class GPU server simulation with NVIDIA DCGM (Data Center GPU Manager) OID support for AI/HPC infrastructure monitoring.

### Supported GPU Servers

| Device | GPUs | VRAM/GPU | Description |
|--------|------|----------|-------------|
| NVIDIA DGX-A100 | 8 | 80 GB | A100 GPU training system |
| NVIDIA DGX-H100 | 8 | 80 GB | H100 GPU training system |
| NVIDIA HGX-H200 | 8 | 141 GB | H200 GPU inference system |

### GPU Metrics

Each GPU server simulates per-GPU metrics with correlated sine-wave patterns:

- **GPU Utilization** (%) - workload activity level
- **VRAM Usage** (MB) - memory consumption (follows utilization with lag)
- **GPU Temperature** (C) - correlated with power draw
- **Power Draw** (Watts) - TDP-based cycling
- **Fan Speed** (%) - responds to temperature (0% for liquid-cooled systems)
- **SM Clock** (MHz) - varies with P-state
- **Memory Clock** (MHz) - varies with P-state

Metrics are exposed via NVIDIA DCGM SNMP OIDs and cycle through 100 pre-generated data points for realistic time-series behavior.

## Device Types

**28 device types across 8 categories:**

### Core Routers
| Device | Ports | Description |
|--------|-------|-------------|
| Cisco ASR9K | 48 | High-end service provider router |
| Cisco CRS-X | 144 | Carrier-class router |
| Huawei NE8000 | 96 | Carrier-class router |
| Nokia 7750 SR-12 | 72 | IP/MPLS service router |
| Juniper MX960 | 96 | Service provider edge router |

### Edge Routers
| Device | Ports | Description |
|--------|-------|-------------|
| Juniper MX240 | 24 | Compact modular router |
| NEC IX3315 | 48 | Enterprise router |
| Cisco IOS | 4 | Standard IOS router |

### Data Center Switches
| Device | Ports | Description |
|--------|-------|-------------|
| Cisco Nexus 9500 | 48 | Data center spine switch |
| Arista 7280R3 | 32 | High-performance switch |

### Campus Switches
| Device | Ports | Description |
|--------|-------|-------------|
| Cisco Catalyst 9500 | 48 | Enterprise core switch |
| Extreme VSP4450 | 48 | Campus switch |
| D-Link DGS-3630 | 52 | L3 managed switch |

### Firewalls
| Device | Ports | Description |
|--------|-------|-------------|
| Palo Alto PA-3220 | 12 | Next-gen firewall |
| Fortinet FortiGate-600E | 20 | Enterprise firewall |
| SonicWall NSa 6700 | 16 | Next-gen firewall |
| Check Point 15600 | 24 | Security gateway |

### Servers
| Device | Ports | Description |
|--------|-------|-------------|
| Dell PowerEdge R750 | 4 | Server BMC/iDRAC |
| HPE ProLiant DL380 | 4 | Server iLO interface |
| IBM Power S922 | 4 | Power Systems server |
| Linux Server | - | Ubuntu 24.04 LTS (SNMP, SSH) |

### GPU Servers
| Device | GPUs | VRAM/GPU | Description |
|--------|------|----------|-------------|
| NVIDIA DGX-A100 | 8 | 80 GB | A100 GPU training system |
| NVIDIA DGX-H100 | 8 | 80 GB | H100 GPU training system |
| NVIDIA HGX-H200 | 8 | 141 GB | H200 GPU inference system |

### Storage Systems
| Device | Type | Protocols |
|--------|------|-----------|
| AWS S3 Storage | Object storage | SNMP, SSH, HTTPS REST |
| Pure Storage FlashArray | All-flash array | SNMP, SSH, HTTPS REST |
| NetApp ONTAP | Unified storage | SNMP, SSH, HTTPS REST |
| Dell EMC Unity | Unified storage | SNMP, SSH, HTTPS REST |

### Enhanced Features
- **Entity MIB Alignment**: All network devices have properly aligned ifTable and Entity MIB data
- **Complete physical inventory**: Chassis, line cards, power supplies, fans, temperature sensors
- **entAliasMappingTable**: Proper mapping between physical ports and logical interfaces
- **Dynamic metrics**: Realistic CPU, memory, and temperature cycling with 100-point sine-wave patterns
- **Dynamic HC interface counters**: `ifHCInOctets`/`ifHCOutOctets` computed on-demand (O(1)) as monotonically increasing Counter64 — rate oscillates between 60–100 % of `ifHighSpeed`/`ifSpeed` on a 1-hour sine cycle; per-interface phase offsets prevent simultaneous peaks; visible on both GET and GETNEXT/GETBULK
- **GPU metrics via NVIDIA DCGM OIDs**: Per-GPU utilization, VRAM, temperature, power, fan speed, clock speeds
- **SNMPv3 support**: Engine ID, MD5/SHA1 authentication, DES/AES128 privacy
- **Device profiles**: Per-category CPU/memory/temperature baselines with configurable spike ranges
- Interface statistics and operational status
- System information and hardware details
- Vendor-specific OID implementations
- CDP & LLDP support for network topology discovery
- OSPF, BGP, and VRF routing protocol simulation via SSH

### Resource Configuration

Each device type has its own directory under `go/simulator/resources/` with JSON files split for maintainability. The loader automatically merges all JSON files in a device directory. There are currently 341 JSON resource files across 28 device types.

OIDs in resource files may be written with or without a leading dot — the loader normalises them to the net-snmp convention (`.1.3.6.1…`) at startup.

```json
{
  "snmp": [
    {
      "oid": ".1.3.6.1.2.1.1.1.0",
      "response": "Cisco IOS Software, Router Version 15.1"
    }
  ],
  "ssh": [
    {
      "command": "show version",
      "response": "Cisco IOS Software, Router Version 15.1\\nDevice Simulator v1.0"
    }
  ],
  "api": [
    {
      "method": "GET",
      "path": "/api/v1/system",
      "status": 200,
      "response": "{\"name\": \"device-01\", \"status\": \"healthy\"}"
    }
  ]
}
```

*Note: The `api` section is optional and used primarily for storage device simulation.*

## Project Structure

```
opensim/
├── go/                              # Go source code
│   ├── simulator/                   # Main simulator package (29 Go files)
│   │   ├── simulator.go             # Entry point, CLI flags, signal handling
│   │   ├── manager.go               # Device management, shared TLS/SSH keys
│   │   ├── device.go                # Device lifecycle and creation
│   │   ├── types.go                 # Data structures (TunInterface, DeviceSimulator, etc.)
│   │   ├── constants.go             # ASN.1 constants, SNMP tags
│   │   ├── names.go                 # Device name generation (prefixes, suffixes)
│   │   ├── cities.go                # World city data loading for sysLocation
│   │   ├── snmp.go                  # SNMP v2c/v3 request handling
│   │   ├── snmp_server.go           # SNMP server with buffer pool optimization
│   │   ├── snmp_handlers.go         # OID lookup (sync.Map), precomputed next-OID mappings
│   │   ├── snmp_response.go         # SNMPv3 response building
│   │   ├── snmp_encoding.go         # ASN.1 BER/DER encoding/decoding
│   │   ├── snmpv3.go                # SNMPv3 message parsing
│   │   ├── snmpv3_crypto.go         # SNMPv3 auth/priv encryption (MD5, SHA1, DES, AES128)
│   │   ├── ssh.go                   # SSH server with VT100 terminal emulation
│   │   ├── api.go                   # REST API handlers for storage devices
│   │   ├── device_profiles.go       # Per-category device metric profiles
│   │   ├── gpu_metrics.go           # Per-GPU metric generation with correlated cycling
│   │   ├── metrics_cycler.go        # 100-point pre-generated CPU/memory/temp cycling
│   │   ├── metrics_oids.go          # SNMP handlers for dynamic metrics OIDs
│   │   ├── system_stats.go          # System stats (process memory/CPU)
│   │   ├── netns.go                 # Network namespace management (opensim namespace)
│   │   ├── tun.go                   # TUN interface creation/management
│   │   ├── prealloc.go              # Parallel TUN interface pre-allocation for fast scaling
│   │   ├── resources.go             # Resource file loading and merging
│   │   ├── web.go                   # HTTP handlers (device CRUD, stats, exports)
│   │   ├── web_routes.go            # Route script generation (generic)
│   │   ├── web_routes_linux.go      # Linux-specific route config (Debian/Ubuntu)
│   │   ├── web_routes_utils.go      # Route script utilities
│   │   ├── web/                     # Web UI static files
│   │   │   ├── index.html           # Main UI page
│   │   │   ├── styles.css           # UI stylesheet
│   │   │   ├── app_ui.js            # UI JavaScript
│   │   │   ├── app_api.js           # API JavaScript
│   │   │   └── logo.png             # Branding
│   │   ├── resources/               # Device resource definitions (28 directories, 341 JSON files)
│   │   │   ├── asr9k/               # Cisco ASR9K (48 ports)
│   │   │   ├── nvidia_dgx_a100/     # NVIDIA DGX-A100 (8 GPUs)
│   │   │   ├── nvidia_dgx_h100/     # NVIDIA DGX-H100 (8 GPUs)
│   │   │   ├── nvidia_hgx_h200/     # NVIDIA HGX-H200 (8 GPUs)
│   │   │   ├── pure_storage_flasharray/
│   │   │   ├── linux_server/
│   │   │   └── ...                  # 28 device type directories total
│   │   └── worldcities/             # 98 CSV files with world city data
│   ├── l8/                          # Layer 8 integration service
│   │   ├── main.go                  # vnet overlay + HTTPS web proxy (port 9095)
│   │   ├── build.sh                 # Docker build script
│   │   ├── Dockerfile               # Multi-stage Docker build
│   │   ├── opensim.yaml             # K8s StatefulSet manifest
│   │   └── web/                     # Landing page (login, register)
│   ├── proxy/                       # HTTP proxy to simulator backend
│   │   └── Proxy.go                 # Reverse proxy implementation
│   ├── tests/                       # Device and polling tests
│   │   ├── TestDevices_test.go      # Integration tests
│   │   ├── Devices.go               # Test device utilities
│   │   └── Polling.go               # Polling test helpers
│   ├── go.mod                       # Go module (Go 1.26.1)
│   └── go.sum                       # Go module checksums
├── diagnose_system.sh               # System diagnostics script
├── ubuntu_setup.sh                  # Ubuntu automated setup
├── increase_file_limits.sh          # File descriptor limit configuration
├── opensim.png                      # Project logo
├── LICENSE                          # Apache License 2.0
└── README.md                        # This file
```

### Resource Directory Structure

Each device type has its own directory with JSON files split into manageable chunks:

```
resources/
├── asr9k/
│   ├── asr9k_snmp_1.json      # System MIB, ifNumber
│   ├── asr9k_snmp_2.json      # ifTable entries
│   ├── asr9k_snmp_3_1.json    # ifXTable entries (part 1)
│   ├── asr9k_snmp_3_2.json    # ifXTable entries (part 2)
│   ├── asr9k_snmp_4.json      # Entity MIB
│   └── asr9k_snmp_5.json      # entAliasMappingTable
└── ...
```

## Layer 8 Integration

OpenSim includes an optional Layer 8 overlay service (`go/l8/`) for distributed deployment:

- **vnet overlay**: Connects to the L8 virtual network mesh for service discovery
- **HTTPS web proxy**: Serves the simulator UI via the L8 web infrastructure with authentication
- **Kubernetes-ready**: Includes Dockerfile and K8s StatefulSet manifest (`opensim.yaml`)
- **Proxy**: Forwards API requests from the L8 web frontend to the simulator backend

## Performance & Scaling

The simulator is optimized for high-scale deployments:

- **Tested**: Up to 30,000+ concurrent devices
- **Memory**: ~50MB base + ~1KB per device
- **CPU**: Minimal usage during steady state
- **Network**: Network namespace isolation prevents systemd-networkd overhead
- **Optimization**: Pre-generated 100-point metric arrays, lock-free sync.Map for O(1) OID lookups, pre-computed next-OID mappings, buffer pool for SNMP reads, shared SSH/TLS keys, parallel TUN pre-allocation

### Scaling Tips

- Use `./increase_file_limits.sh` to raise file descriptor limits before large deployments
- Keep network namespaces enabled (default) to avoid systemd-networkd overhead
- Run `./diagnose_system.sh` to verify system readiness
- Use `./ubuntu_setup.sh` for automated Ubuntu system configuration

## Troubleshooting

### Common Issues

1. **Permission Denied**: Ensure running with `sudo` for TUN interface creation
2. **Port Conflicts**: Use `-port` flag to specify an alternative HTTP API port
3. **SNMP Privileged Port**: Port 161 requires root or `CAP_NET_BIND_SERVICE`; use `-snmp-port 1161` to bind a non-privileged port instead
4. **TUN Module Missing**: Run `sudo modprobe tun`
5. **High Resource Usage**: Increase file limits with `./increase_file_limits.sh` and use network namespaces (enabled by default)
6. **SNMP Integer Encoding**: Fixed panic issues with negative integer values in ASN.1 encoding

### Debug Commands

```bash
# Check TUN interfaces
ip addr show | grep sim

# Verify device processes (adjust port if using -snmp-port)
ss -tulpn | grep -E "(161|1161|22)"

# Monitor system resources
htop

# Run system diagnostics
sudo ./diagnose_system.sh
```

### Log Files

- Application logs: stdout/stderr
- System logs: `journalctl -u <service-name>`
- Web access logs: Built into the application

## Development

### Building from Source

```bash
cd go/simulator
go mod tidy
go build -o simulator .
```

### Docker Build

```bash
cd go/l8
docker build --no-cache --platform=linux/amd64 -t saichler/opensim-web:latest .
```

### Kubernetes Deployment

```bash
kubectl apply -f go/l8/opensim.yaml
```

The K8s manifest deploys a StatefulSet in the `opensim` namespace with `hostNetwork: true` and a `/data` hostPath volume.

### Running Tests

```bash
cd go
go test ./...
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test thoroughly
5. Submit a pull request

## Use Cases

- **Network Monitoring Testing**: Test SNMP v2c/v3 polling applications with dynamic metrics
- **GPU Infrastructure Monitoring**: Validate GPU monitoring tools against NVIDIA DCGM OIDs with realistic per-GPU metric cycling
- **AI/HPC Infrastructure Testing**: Simulate DGX/HGX GPU clusters for monitoring tool development
- **Automation Development**: Develop SSH-based network automation with VT100 terminal support
- **Load Testing**: Simulate large network topologies with 30,000+ devices
- **Training**: Network management skill development
- **CI/CD Testing**: Automated testing of network applications
- **Storage Management Testing**: Validate storage monitoring and provisioning tools via HTTPS APIs
- **Infrastructure Monitoring**: Test Linux server and GPU server monitoring and metrics collection
- **Topology Discovery**: Validate CDP/LLDP-based network mapping tools

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.

## Support

For issues, questions, or contributions:

- Create an issue on GitHub
- Check existing documentation
- Review troubleshooting guides

---

**OpenSim** - Simulate networks, test at scale, develop with confidence.
