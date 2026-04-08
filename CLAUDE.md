# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
cd go/simulator
go mod tidy
go build -o simulator .

# Run (requires root for TUN/network namespace)
sudo ./simulator [flags]

# Key flags
-auto-start-ip <IP>     # Auto-create devices starting at this IP
-auto-count <N>         # Number of devices to auto-create
-port <port>            # HTTP API port (default: 8080)
-snmpv3-engine-id <id>  # Enable SNMPv3 (omit for v2c only)
-snmpv3-auth <proto>    # none | md5 | sha1
-snmpv3-priv <proto>    # none | des | aes128
-no-namespace           # Disable network namespace isolation

# Tests
cd go
go test ./...

# Run a single test
go test ./tests/ -run TestDevices

# Docker build (L8 integration)
cd go/l8
docker build --no-cache --platform=linux/amd64 -t saichler/opensim-web:latest .
```

## Architecture

**l8opensim** is a Go-based network device simulator capable of running 30,000+ concurrent simulated devices, each responding to SNMP (v2c/v3), SSH, and HTTPS REST protocols. It uses Linux TUN interfaces and network namespaces to give each device its own IP address.

### Package layout

| Path | Purpose |
|------|---------|
| `go/simulator/` | Core simulator — all device simulation logic |
| `go/l8/` | Layer 8 vnet overlay + HTTPS web proxy (port 9095) |
| `go/proxy/` | Reverse proxy from L8 frontend to simulator backend |
| `go/tests/` | Integration tests |
| `resources/` | 341 JSON files (28 device types) with SNMP/SSH/REST response data |

### Core simulator components (`go/simulator/`)

**Device lifecycle:** `simulator.go` (CLI/entry) → `manager.go` (SimulatorManager, shared keys/certs) → `device.go` (per-device startup, protocol server lifecycle)

**SNMP stack:** `snmp_server.go` → `snmp.go` (request handling) → `snmp_handlers.go` (OID lookup via sync.Map) → `snmp_response.go` (response building) → `snmp_encoding.go` (ASN.1 BER/DER). SNMPv3 is handled separately in `snmpv3.go` + `snmpv3_crypto.go` (MD5/SHA1 auth, DES/AES128 privacy).

**Metrics engine:** `metrics_cycler.go` drives 100-point pre-generated sine-wave patterns per device. `gpu_metrics.go` handles per-GPU metrics (utilization, VRAM, temperature, power, clocks). `device_profiles.go` defines per-category baselines.

**Network infrastructure:** `tun.go` creates TUN interfaces, `netns.go` manages the `opensim` network namespace, `prealloc.go` does parallel pre-allocation of TUN interfaces (configurable worker count 100–200) for fast scaling.

**Web API:** `web.go` (route setup) + `api.go` (handlers) + `web_routes*.go` (Linux route script generation). Serves device CRUD, CSV export, system stats.

**Resource loading:** `resources.go` loads and caches the 341 JSON files at startup. Each device type directory has split JSON files for SNMP, SSH, and REST responses that are merged at load time.

### Key design decisions

- **sync.Map for OID lookups** — lock-free O(1) access during concurrent SNMP queries
- **Pre-computed next-OID mappings** — efficient SNMP GETNEXT/WALK without scanning
- **Buffer pool** — reduces GC pressure on SNMP request handling
- **Shared SSH/TLS keys** across all devices — avoids per-device key generation overhead
- **Network namespace isolation** (`opensim` namespace) — prevents systemd-networkd interference

### Device types

28 device types across 8 categories: Core Routers, Edge Routers, Data Center Switches, Campus Switches, Firewalls, Servers, GPU Servers (NVIDIA DGX-A100/H100/HGX-H200), Storage Systems (AWS S3, Pure Storage, NetApp ONTAP, Dell EMC Unity).

Each device type has resource files under `resources/<device-type>/` containing JSON for SNMP OID responses, SSH command responses, and REST API responses.

## Commit convention

Follow Conventional Commits: `<type>[scope]: <description>`
Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`, `build`, `revert`

## Pull requests

This repo is a fork. Always create PRs against **origin** (`labmonkeys-space/l8opensim`), never against upstream (`saichler/l8opensim`):

```bash
gh pr create --repo labmonkeys-space/l8opensim --base main ...
```
