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
-snmp-port <port>       # UDP port for SNMP listener on each device (default: 161)
-snmpv3-engine-id <id>  # Enable SNMPv3 (omit for v2c only)
-snmpv3-auth <proto>    # none | md5 | sha1
-snmpv3-priv <proto>    # none | des | aes128
-no-namespace           # Disable network namespace isolation

# Flow export flags (NetFlow v5 / v9 / IPFIX / sFlow v5)
-flow-collector <host:port>       # Enable flow export to this UDP collector
-flow-protocol <proto>            # netflow9 (default) | ipfix | netflow5 | sflow (alias: sflow5)
-flow-tick <duration>             # How often to emit flows (default: 10s)
-flow-active-timeout <duration>   # Active flow expiry timeout (default: 5m)
-flow-inactive-timeout <duration> # Inactive flow expiry timeout (default: 1m)
-flow-template-interval <dur>     # Re-send template every N ticks (default: 10m; ignored under netflow5/sflow)
-flow-source-per-device           # Bind per-device UDP socket so src IP = device IP (default: true)

# SNMP trap / INFORM export flags (SNMPv2c only)
-trap-collector <host:port>       # Enable trap export to this UDP collector (default port 162)
-trap-mode <proto>                # trap (default, fire-and-forget) | inform (acknowledged)
-trap-interval <duration>         # Per-device mean firing interval, Poisson-distributed (default: 30s)
-trap-global-cap <tps>            # Simulator-wide tps ceiling (0 = unlimited)
-trap-catalog <path>              # Override embedded universal 5-trap catalog
-trap-community <string>          # SNMPv2c community (default: public)
-trap-source-per-device           # Source IP = device IP (default: true; REQUIRED in inform mode)
-trap-inform-timeout <duration>   # Per-retry timeout in inform mode (default: 5s)
-trap-inform-retries <int>        # Max retransmissions per inform (default: 2)

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
| `go/simulator/resources/` | 379 JSON files (28 device types) with SNMP/SSH/REST response data |

### Core simulator components (`go/simulator/`)

**Device lifecycle:** `simulator.go` (CLI/entry) → `manager.go` (SimulatorManager, shared keys/certs) → `device.go` (per-device startup, protocol server lifecycle)

**SNMP stack:** `snmp_server.go` → `snmp.go` (request handling) → `snmp_handlers.go` (OID lookup via sync.Map) → `snmp_response.go` (response building) → `snmp_encoding.go` (ASN.1 BER/DER). SNMPv3 is handled separately in `snmpv3.go` + `snmpv3_crypto.go` (MD5/SHA1 auth, DES/AES128 privacy).

**Metrics engine:** `metrics_cycler.go` drives 100-point pre-generated sine-wave patterns per device. `gpu_metrics.go` handles per-GPU metrics (utilization, VRAM, temperature, power, clocks). `device_profiles.go` defines per-category baselines.

**Network infrastructure:** `tun.go` creates TUN interfaces, `netns.go` manages the `opensim` network namespace, `prealloc.go` does parallel pre-allocation of TUN interfaces (configurable worker count 100–200) for fast scaling.

**Web API:** `web.go` (route setup) + `api.go` (handlers) + `web_routes*.go` (Linux route script generation). Serves device CRUD, CSV export, system stats, flow export status (`GET /api/v1/flows/status`), trap export status (`GET /api/v1/traps/status`), and on-demand trap firing (`POST /api/v1/devices/{ip}/trap`).

**Flow export:** `flow_exporter.go` (FlowExporter, FlowEncoder interface, SimulatorManager integration) + `netflow9.go` (NetFlow9Encoder, RFC 3954) + `ipfix.go` (IPFIXEncoder, RFC 7011) + `netflow5.go` (NetFlow5Encoder, Cisco v5: 24B header, 48B/record, IPv4-only, 30-record datagram cap, no templates) + `sflow.go` (SFlowEncoder, sFlow v5 per sflow_version_5.txt: 28B XDR datagram header, variable-length flow_sample records carrying sampled_header=IPv4+UDP/TCP synthesized from the FlowRecord 5-tuple, no template mechanism). One shared UDP socket and ticker goroutine; per-device FlowExporter owns a FlowCache. Protocols:

| Protocol   | Header | Record size    | Template? | Timestamps         | IPv6 records | Notes |
|------------|--------|----------------|-----------|--------------------|--------------|-------|
| `netflow5` | 24B    | 48B fixed      | none      | SysUptime-relative | filtered     | 30-record datagram cap; 32-bit ASNs clamp to `23456` (AS_TRANS, RFC 6793 §2); `-flow-template-interval` is a silent no-op |
| `netflow9` | 20B    | 45B fixed      | yes       | SysUptime-relative | filtered     | Single 18-field template, ID 256 |
| `ipfix`    | 16B    | 53B fixed      | yes       | absolute epoch ms  | filtered     | Template Set ID 2, IE-based fields |
| `sflow`    | 28B    | variable (~100B typical) | none (self-describing) | uptime + flow_sample sampling_rate | filtered (IPv4 agent only) | Synthetic sampling_rate = `10 × FlowProfile.ConcurrentFlows` (see `SyntheticSamplingRateMultiplier`); emits flow_sample (type 1) + Phase-2 counters_sample (type 2) per tick. **sFlow output is synthetic — the simulator does not observe real packet streams.** Agent identity = device IPv4; `-flow-source-per-device` makes the UDP source IP match `agent_address`. |

The `FlowEncoder` interface has a `MaxRecordSize() int` extension point: fixed-size encoders return 0 (NetFlow5/9, IPFIX), variable-length encoders (sFlow) return a worst-case per-record byte bound that `FlowExporter.Tick` uses for MTU-safe pagination.

**SNMP trap export:** `trap_manager.go` (SimulatorManager integration, TrapConfig, `StartTrapExport` / `StopTrapExport`, HTTP handlers' helpers, `TrapStatus`) + `trap_catalog.go` (JSON catalog loader with embedded universal set + weighted-random pick + `text/template`-based varbind resolution) + `trap_v2c.go` (SNMPv2c TRAP [0xA7] and InformRequest [0xA6] PDU encoder, GetResponse [0xA2] ack parser — reuses `snmp_encoding.go` ASN.1 primitives) + `trap_scheduler.go` (single central min-heap scheduler goroutine with Poisson inter-arrival + `golang.org/x/time/rate` global cap) + `trap_exporter.go` (per-device `TrapExporter` with atomic per-device UDP socket, bounded pending-inform map with oldest-drop, reader/retry goroutines in INFORM mode).

**Trap catalog:**
- Default catalog is compiled into the binary from `resources/_common/traps.json` via `embed.FS` — no filesystem dependency for the out-of-box experience.
- Override with `-trap-catalog <path>` (complete replacement, not merge).
- Universal catalog ships 5 entries: `coldStart`, `warmStart`, `linkDown`, `linkUp`, `authenticationFailure` (RFC 3418). Weights: linkDown=40, linkUp=40, authenticationFailure=10, coldStart=5, warmStart=5.
- Template vocabulary is restricted to `{{.IfIndex}}`, `{{.Uptime}}`, `{{.Now}}`, `{{.DeviceIP}}`. Unknown fields are rejected at catalog load.
- The two mandatory SNMPv2-Trap varbinds (`sysUpTime.0`, `snmpTrapOID.0`) are prepended automatically by the encoder — catalog authors supply only body varbinds; entries that list either reserved OID explicitly are rejected.

**Trap operational notes:**
- INFORM mode (`-trap-mode inform`) requires `-trap-source-per-device=true` (the default) so the per-device UDP socket can demux acks without a global request-id table. Startup fails with a clear error if the operator explicitly sets the flag false.
- Pending-inform map is bounded at 100 per device with oldest-drop overflow policy (exposed as `informsDropped` in `GET /api/v1/traps/status`).
- Retransmissions consume global-cap tokens (design decision to prevent retry-storm amplification when the collector is unreachable).
- Collector-side `rp_filter` may need relaxing (`net.ipv4.conf.*.rp_filter=0` or `2`) to accept UDP/162 with 10.42.0.0/16 source IPs — same caveat already documented for flow export.
- Per-device UDP source binding reuses the same `setupVethPair` + `FORWARD -i veth-sim-host -j ACCEPT` iptables rule that flow export already relies on. No new netns / iptables surface.

**Trap HTTP endpoints:**
- `GET /api/v1/traps/status` — JSON with `enabled`, `mode`, `sent`, INFORM counters (`informs_pending`, `informs_acked`, `informs_failed`, `informs_dropped` when mode=inform), `rate_limiter_tokens_available` (when `-trap-global-cap` is set), `devices_exporting`.
- `POST /api/v1/devices/{ip}/trap` — body `{"name":"linkDown","varbindOverrides":{"IfIndex":"3"}}` → `202 Accepted` + `{"requestId": N}`. `400` for unknown catalog entry, `404` for unknown device, `503` when trap export is disabled. Fire-and-forget: returns without waiting on INFORM ack.

**Resource loading:** `resources.go` loads and caches the 379 JSON files at startup. Each device type directory has split JSON files for SNMP, SSH, and REST responses that are merged at load time.

### Key design decisions

- **sync.Map for OID lookups** — lock-free O(1) access during concurrent SNMP queries
- **Pre-computed next-OID mappings** — efficient SNMP GETNEXT/WALK without scanning
- **Buffer pool** — reduces GC pressure on SNMP request handling
- **Shared SSH/TLS keys** across all devices — avoids per-device key generation overhead
- **Network namespace isolation** (`opensim` namespace) — prevents systemd-networkd interference
- **Per-device flow egress** — `setupVethPair` installs a `FORWARD -i veth-sim-host -j ACCEPT` iptables rule so that per-device flow exporters can send UDP out of the ns through the host's routing table (Docker-present hosts default FORWARD to drop). Rule is removed in `NetNamespace.Close`. The simulator image includes `iptables` for this reason. On the downstream flow collector, `rp_filter` may need to be relaxed (`net.ipv4.conf.*.rp_filter=0` or `2`) to accept packets with 10.42.0.0/16 source IPs.

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
