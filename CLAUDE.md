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

# UDP syslog export flags (RFC 5424 / RFC 3164)
-syslog-collector <host:port>     # Enable UDP syslog export to this collector (default port 514)
-syslog-format <fmt>              # 5424 (default, structured) | 3164 (legacy BSD)
-syslog-interval <duration>       # Per-device mean firing interval, Poisson-distributed (default: 10s)
-syslog-global-cap <rate>         # Simulator-wide rate ceiling (0 = unlimited)
-syslog-catalog <path>            # Override embedded universal 6-entry catalog
-syslog-source-per-device         # Source IP = device IP (default: true; bind failure is non-fatal, falls back to shared socket)

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
- Override with `-trap-catalog <path>` (complete replacement, not merge). When set, per-type overlays are NOT loaded — the single file becomes the catalog for every device.
- Universal catalog ships 5 entries: `coldStart`, `warmStart`, `linkDown`, `linkUp`, `authenticationFailure` (RFC 3418). Weights: linkDown=40, linkUp=40, authenticationFailure=10, coldStart=5, warmStart=5.
- **Per-type overlays:** `resources/<type>/traps.json` overlays the universal for devices of that type (e.g., `resources/cisco_ios/traps.json` affects all cisco_ios devices). Resolved lazily per fire via `SimulatorManager.CatalogFor(ip)` → `trapCatalogsByType[slug]` → `_universal`. Devices whose type has no per-type file fall through to the universal. `GET /api/v1/traps/status` exposes a `catalogs_by_type` object showing per-type entry counts and sources (embedded / file / override).
- **Shipped vendor catalogs** (PRs 4 & 5 of epic #103):
  - `cisco_ios/traps.json` — 7 Cisco-MIB notifications: `ciscoConfigManEvent`, `ciscoEnvMonSupplyStatusChangeNotif`, `ciscoEnvMonTemperatureNotification`, `cefcModuleStatusChange`, `cefcFanTrayStatusChangeNotif`, `ciscoEntSensorThresholdNotification`, `ciscoFlashDeviceChangeTrap`. Merged with universal 5 → cisco_ios devices fire from 12 entries. All use `snmpTrapEnterprise` for v1↔v2c proxy compatibility.
  - `juniper_mx240/traps.json` — 7 JUNIPER-MIB notifications: `jnxYellowAlarmOn` / `jnxYellowAlarmOff` / `jnxRedAlarmOn` (alarms from JUNIPER-ALARM-MIB), `jnxFruInsertion` / `jnxFruRemoval` / `jnxFruPowerOff` / `jnxFruFailed` (FRU events from JUNIPER-MIB). Merged with universal 5 → juniper_mx240 devices fire from 12 entries. `snmpTrapEnterprise` = `1.3.6.1.4.1.2636` (Juniper Networks) on all entries.
  - Other cisco_* slugs (`cisco_catalyst_9500`, `cisco_crs_x`, `cisco_nexus_9500`, `asr9k`) and `juniper_mx960` fall back to universal in this epic. Family-catalog concept is a follow-up refactor.
- **Overlay semantic:** per-type files default to `"extends": true` — entries unique to the per-type file are added, same-name entries override the universal, unused universal entries carry through. Set `"extends": false` at the top of the per-type JSON for a pure replacement (no universal content for that type). Weights are recomputed post-merge.
- **Unified template vocabulary (9 fields, trap + syslog share the same surface):** `{{.IfIndex}}`, `{{.IfName}}` (synthesised `GigabitEthernet0/<N>` in PR2; live `ifDescr.<N>` lookup in PR3), `{{.Uptime}}`, `{{.Now}}`, `{{.DeviceIP}}`, `{{.SysName}}`, `{{.Model}}` (human-readable label from slug → `deviceTypeLabels`), `{{.Serial}}` (`SN<hex>` synthesised from device IP, deterministic), `{{.ChassisID}}` (`02:42:xx:xx:xx:xx` MAC-style synthesised from device IP). Unknown fields are rejected at catalog load — Class 2 random-per-fire fields (`PeerIP`, `User`, `SourceIP`, `RuleName`, …) are explicitly deferred and rejected.
- Class 1 fields (SysName, Model, Serial, ChassisID) are resolved at exporter construction and captured on the exporter; IfName is resolved per fire via a callback. `FieldResolver` interface in `field_resolver.go` is the seam for testability and for the PR3 swap to live `ifDescr` lookup.
- The two mandatory SNMPv2-Trap varbinds (`sysUpTime.0`, `snmpTrapOID.0`) are prepended automatically by the encoder — catalog authors supply only body varbinds; entries that list either reserved OID (or `snmpTrapEnterprise.0`) as a body varbind are rejected.
- Optional top-level `snmpTrapEnterprise` field (string, dotted OID) per entry. When set, the encoder emits an additional `snmpTrapEnterprise.0` varbind (OID `1.3.6.1.6.3.1.1.4.3.0`) after the two mandatory ones — useful for v1↔v2c cross-compatibility on collectors that expect the enterprise OID per SNMPv2-MIB §10. Catalog-loader rejects reserved OIDs (`sysUpTime.0`, `snmpTrapOID.0`, `snmpTrapEnterprise.0`) as enterprise values. Omit the field to keep the backward-compatible 2-varbind prefix.

**Trap operational notes:**
- INFORM mode (`-trap-mode inform`) requires `-trap-source-per-device=true` (the default) so the per-device UDP socket can demux acks without a global request-id table. Startup fails with a clear error if the operator explicitly sets the flag false.
- Pending-inform map is bounded at 100 per device with oldest-drop overflow policy (exposed as `informsDropped` in `GET /api/v1/traps/status`).
- Retransmissions consume global-cap tokens (design decision to prevent retry-storm amplification when the collector is unreachable).
- Collector-side `rp_filter` may need relaxing (`net.ipv4.conf.*.rp_filter=0` or `2`) to accept UDP/162 with 10.42.0.0/16 source IPs — same caveat already documented for flow export.
- Per-device UDP source binding reuses the same `setupVethPair` + `FORWARD -i veth-sim-host -j ACCEPT` iptables rule that flow export already relies on. No new netns / iptables surface.

**Trap HTTP endpoints:**
- `GET /api/v1/traps/status` — JSON with `enabled`, `mode`, `sent`, INFORM counters (`informs_pending`, `informs_acked`, `informs_failed`, `informs_dropped` when mode=inform), `rate_limiter_tokens_available` (when `-trap-global-cap` is set), `devices_exporting`.
- `POST /api/v1/devices/{ip}/trap` — body `{"name":"linkDown","varbindOverrides":{"IfIndex":"3"}}` → `202 Accepted` + `{"requestId": N}`. `400` for unknown catalog entry (response body includes `catalog` — the device's resolved catalog label — and `availableEntries` list so operators can self-service), `404` for unknown device, `503` when trap export is disabled. Fire-and-forget: returns without waiting on INFORM ack.
- `GET /api/v1/traps/status` additionally reports `catalogs_by_type` when enabled: `{ "cisco_ios": {entries: 11, source: "file:resources/cisco_ios/traps.json"}, "_universal": {entries: 5, source: "embedded"} }`.

**UDP syslog export:** `syslog_manager.go` (SimulatorManager integration, SyslogConfig, `StartSyslogExport` / `StopSyslogExport`, `SyslogStatus`) + `syslog_catalog.go` (JSON catalog with embedded universal 6-entry set; weighted-random pick; `text/template`-based body / structured-data resolution with all templates pre-compiled at load) + `syslog_wire.go` (`SyslogEncoder` interface with `RFC5424Encoder` and `RFC3164Encoder` — PRI calc, ISO 8601 / `Mmm DD HH:MM:SS` timestamps, SD-PARAM escape per §6.3.3, HOSTNAME / APP-NAME / MSGID / TAG sanitisation, MaxMessageSize enforcement) + `syslog_scheduler.go` (single central min-heap scheduler with Poisson inter-arrival + `golang.org/x/time/rate` global cap; derived context so `Stop()` is bounded-time under cap) + `syslog_exporter.go` (per-device `SyslogExporter` with atomic per-device UDP socket and shared-socket fallback).

**Syslog catalog:**
- Default catalog is compiled into the binary from `resources/_common/syslog.json` via `embed.FS` — feature works out of the box.
- Override with `-syslog-catalog <path>` (complete replacement, not merge). When set, per-type overlays are NOT loaded.
- Universal catalog ships 6 entries: `interface-up` / `interface-down` (local7.notice/error, IFMGR), `auth-success` / `auth-failure` (authpriv.info/warning, sshd), `config-change` / `system-restart` (local7.notice/warning, SYSMGR). Weights sum to 135.
- **Per-type overlays:** mirror trap-side behaviour. `resources/<type>/syslog.json` overlays the universal for devices of that type. Resolved via `SimulatorManager.SyslogCatalogFor(ip)`. Defaults to `"extends": true` (merge, same-name override); set `"extends": false` for pure replacement. `GET /api/v1/syslog/status` reports `catalogs_by_type` for observability. `POST /api/v1/devices/{ip}/syslog` resolves entry names against the device's catalog; a 400 response includes `catalog` and `availableEntries` for self-service.
- **Shipped vendor catalogs** (PRs 4 & 5 of epic #103):
  - `cisco_ios/syslog.json` — 8 Cisco-format messages: `%LINK-3-UPDOWN` (up/down pair), `%LINEPROTO-5-UPDOWN` (up/down pair), `%SYS-5-CONFIG_I`, `%SNMP-5-COLDSTART`, `%SYS-5-RESTART`, `%ENVMON-5-TEMP_OK`. Merged with universal 6 → cisco_ios devices fire from 14 entries. Message bodies match IOS's `%FACILITY-SEVERITY-MNEMONIC:` form verbatim so OpenNMS UEI matchers tuned for Cisco strings fire correctly.
  - `juniper_mx240/syslog.json` — 7 Junos-format messages using daemon tags (`snmpd`, `mib2d`, `chassisd`, `mgd`, `license-check`) and Junos MSGID structure: `SNMP_TRAP_LINK_UP` / `SNMP_TRAP_LINK_DOWN`, `MIB2D_IFD_IFL_ENCAPS_MISMATCH`, `CHASSISD_FRU_TEMP_CRITICAL`, `CHASSISD_EEPROM_READ_FAIL`, `LICENSE_EXPIRED_KEY_DELETED`, `UI_COMMIT_COMPLETED`. Merged with universal 6 → juniper_mx240 devices fire from 13 entries. Message bodies match Junos's canonical `MSGID: body` form verbatim.
  - Linux / Palo Alto / Arista deferred — their realistic content requires Class 2 random-per-fire fields.
- **Unified template vocabulary (9 fields, same set as trap):** `{{.DeviceIP}}`, `{{.SysName}}`, `{{.IfIndex}}`, `{{.IfName}}`, `{{.Now}}`, `{{.Uptime}}`, `{{.Model}}`, `{{.Serial}}`, `{{.ChassisID}}`. Unknown fields are rejected at catalog load — Class 2 random fields (`PeerIP`, `User`, `SourceIP`, `RuleName`, …) remain deferred. See trap section above for resolution semantics — trap and syslog share the same `FieldResolver` seam and Class 1 values are captured at exporter construction.
- SD-NAME keys are validated against RFC 5424 §6.3.3 at load; each templated value is pre-compiled to a `*template.Template` so the fire hot path is allocation-light (measured 894 ns/op).
- Entry `appName` is required (RFC 3164 TAG has no NILVALUE). Facility and severity accept canonical names (`local7`, `error`) or integers in range (`0..23` / `0..7`). MTU-safety dry-render rejects entries whose worst-case rendered output exceeds 1400 bytes.

**Syslog catalog JSON schema** (one entry; the file is `{"entries":[…]}`):

```json
{
  "name":     "interface-down",       // required; unique within catalog
  "weight":   40,                     // weighted-random Pick; 0/omitted → 1
  "facility": "local7",               // name (kern/user/.../local0..local7) or integer 0..23
  "severity": "error",                // name (emerg/alert/crit/err|error/warning|warn/notice/info/debug) or integer 0..7
  "appName":  "IFMGR",                // required (3164 TAG has no NILVALUE); sanitised to ASCII token
  "msgId":    "LINKDOWN",             // 5424 MSGID; empty → NILVALUE; dropped in 3164
  "hostname": "{{.SysName}}",         // optional override; empty → sysName→DeviceIP fallback
  "structuredData": {                 // 5424 STRUCTURED-DATA; empty map → NILVALUE; dropped in 3164
    "ifIndex": "{{.IfIndex}}",        // keys must match RFC 5424 §6.3.3 SD-NAME grammar
    "ifName":  "{{.IfName}}"
  },
  "template": "Interface {{.IfName}} (ifIndex={{.IfIndex}}) changed state to down"
}
```

**HOSTNAME derivation priority** (resolved at fire time, per design §D5):
1. If the catalog entry defines a non-empty `hostname` template, render it (with the six-field vocabulary) and use the result.
2. Otherwise, use the device's stored `sysName.0` value (captured at device construction).
3. Otherwise, use the device's IPv4 as dotted-quad.

In every branch the result is run through `sanitiseHostname`: spaces become hyphens (spec mandate), other framing / control chars become `_`.

**PRI calculation and vocabulary** (per RFC 5424 §6.2.1, shared by 5424 and 3164):

- `PRI = facility * 8 + severity`, emitted as `<N>` with no leading zeros (range 0..191).
- Catalog entries accept either the canonical name or the integer:

  | Facility   | Int | Facility   | Int | Facility   | Int |
  |------------|-----|------------|-----|------------|-----|
  | `kern`     | 0   | `cron`     | 9   | `local0`   | 16  |
  | `user`     | 1   | `authpriv` | 10  | `local1`   | 17  |
  | `mail`     | 2   | `ftp`      | 11  | `local2`   | 18  |
  | `daemon`   | 3   | `ntp`      | 12  | `local3`   | 19  |
  | `auth`     | 4   | `audit`    | 13  | `local4`   | 20  |
  | `syslog`   | 5   | `alert`    | 14  | `local5`   | 21  |
  | `lpr`      | 6   | `clock`    | 15  | `local6`   | 22  |
  | `news`     | 7   |            |     | `local7`   | 23  |
  | `uucp`     | 8   |            |     |            |     |

  | Severity  | Int | Aliases       |
  |-----------|-----|---------------|
  | `emerg`   | 0   |               |
  | `alert`   | 1   |               |
  | `crit`    | 2   |               |
  | `err`     | 3   | `error`       |
  | `warning` | 4   | `warn`        |
  | `notice`  | 5   |               |
  | `info`    | 6   |               |
  | `debug`   | 7   |               |

  Out-of-range integers or unknown names are rejected at catalog load.

**Syslog operational notes:**
- Only one format on the wire at a time — `-syslog-format 5424` (default) or `3164`. Mixed formats on one socket break auto-detecting parsers downstream; operators select one per deployment.
- Per-device UDP source binding reuses the same `setupVethPair` + `FORWARD -i veth-sim-host -j ACCEPT` rule shared by flow / trap. No new netns / iptables surface.
- Per-device bind failure is **non-fatal** for syslog (unlike INFORM): exporter logs a warning and falls back to the shared socket. When the primary per-device write fails but the shared fallback succeeds, the primary failure is logged and stats count the send as successful.
- The collector-side `rp_filter` caveat is the same as flow / trap — accept UDP from device IPs with `net.ipv4.conf.*.rp_filter=0` or `2`.
- On-demand HTTP fires **bypass the global rate limiter** (test-harness use case; scheduler-driven traffic still honours `-syslog-global-cap`).

**Syslog HTTP endpoints:**
- `GET /api/v1/syslog/status` — JSON with `enabled`, `format` (`5424` or `3164`), `collector`, `sent`, `send_failures`, `rate_limiter_tokens_available` (when `-syslog-global-cap` is set), `devices_exporting`.
- `POST /api/v1/devices/{ip}/syslog` — body `{"name":"interface-down","templateOverrides":{"IfIndex":"3","IfName":"Gi0/3"}}` → `202 Accepted` + `{}`. `400` for unknown catalog entry or malformed JSON, `404` for unknown device, `503` when syslog export is disabled.

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
