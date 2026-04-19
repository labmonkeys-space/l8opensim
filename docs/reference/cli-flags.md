# CLI flags

The `simulator` binary is driven entirely by command-line flags. This page is
the authoritative catalog — new flags land here first.

Run the simulator with:

```bash
sudo ./simulator [options]
```

Root is required because the simulator creates TUN interfaces and manages the
`opensim` network namespace. See [Network namespace](../ops/network-namespace.md)
for the namespace details and [Quick start](../getting-started/quick-start.md)
for a minimal invocation.

## Core flags

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `-auto-start-ip` | string | — | Auto-create devices starting from this IP (e.g. `192.168.100.1`). |
| `-auto-count` | int | 0 | Number of devices to auto-create. Requires `-auto-start-ip`. |
| `-auto-netmask` | string | `24` | Netmask for auto-created devices. |
| `-port` | string | `8080` | HTTP API server port. |
| `-snmp-port` | int | `161` | UDP port for the SNMP listener on each device. Use `1161` to avoid requiring `CAP_NET_BIND_SERVICE`. |
| `-no-namespace` | bool | `false` | Disable network namespace isolation (run in the root namespace). |
| `-help` | — | — | Show the help message and exit. |

## SNMPv3 flags

Omit the engine-id flag to run in v2c-only mode.

| Flag | Values | Default | Purpose |
|------|--------|---------|---------|
| `-snmpv3-engine-id` | string | — | Enable SNMPv3 with the specified engine ID (e.g. `0x80001234`). |
| `-snmpv3-auth` | `none` \| `md5` \| `sha1` | `md5` | SNMPv3 auth protocol. |
| `-snmpv3-priv` | `none` \| `des` \| `aes128` | `none` | SNMPv3 privacy protocol. |

See [SNMP reference](snmp.md) for the auth/priv compatibility matrix.

## Interface-state scenarios

The `-if-scenario` flag controls the SNMP admin/oper status reported for all
simulated interfaces, so you can reproduce common network conditions without
editing resource files.

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `-if-scenario` | int | `2` | Interface state scenario: 1=all-shutdown, 2=all-normal, 3=all-failure, 4=pct-failure. |
| `-if-failure-pct` | int | `10` | Percentage of interfaces with oper-down (used with `-if-scenario 4`, 0–100). |

| Scenario | Name | `ifAdminStatus` | `ifOperStatus` | Use case |
|----------|------|-----------------|----------------|----------|
| 1 | all-shutdown | down (2) | down (2) | Planned maintenance, device decommission |
| 2 | all-normal *(default)* | up (1) | up (1) | Normal steady-state operations |
| 3 | all-failure | up (1) | down (2) | Link failures, SFP issues, cable pull |
| 4 | pct-failure | up (1) | down for n% | Partial outage, staged rollout testing |

Scenario 4 uses a deterministic rule (`ifIndex % 100 < n`) so test runs are
reproducible across restarts.

## Flow export flags

See [Flow export (operator guide)](../ops/flow-export.md) for prerequisites and
collector setup, and [Flow export reference](flow-export.md) for protocol
details.

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `-flow-collector` | string | — | Enable flow export to this UDP collector (e.g. `192.168.1.10:2055`). |
| `-flow-protocol` | `netflow9` \| `ipfix` \| `netflow5` \| `sflow` | `netflow9` | Flow export protocol (alias: `sflow5`). |
| `-flow-tick-interval` | int (seconds) | `5` | Flow ticker interval. |
| `-flow-active-timeout` | int (seconds) | `30` | Active flow timeout. |
| `-flow-inactive-timeout` | int (seconds) | `15` | Inactive flow timeout. |
| `-flow-template-interval` | int (seconds) | `60` | Template retransmission interval (NetFlow v9 / IPFIX only). |
| `-flow-source-per-device` | bool | `true` | Use each device's IP as the UDP source address. |

## SNMP trap / INFORM export flags

See [SNMP trap / INFORM export (operator guide)](../ops/snmp-traps.md) for
prerequisites and `snmptrapd` smoke-test, and
[SNMP trap reference](snmp-traps.md) for wire format and catalog JSON.

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `-trap-collector` | string | — | Enable trap export to this UDP collector (e.g. `192.168.1.10:162`). Empty disables the feature. |
| `-trap-mode` | `trap` \| `inform` | `trap` | Notification mode. TRAP is fire-and-forget; INFORM is acknowledged and retried. |
| `-trap-interval` | duration | `30s` | Per-device mean firing interval (Poisson-distributed, not periodic). |
| `-trap-global-cap` | int (tps) | `0` | Simulator-wide rate ceiling across fires + INFORM retries. `0` is unlimited. |
| `-trap-catalog` | string | — | Path to a JSON catalog; empty uses the embedded universal 5-trap catalog. |
| `-trap-community` | string | `public` | SNMPv2c community string. |
| `-trap-source-per-device` | bool | `true` | Use each device's IP as the UDP source address. **Required** when `-trap-mode inform`. |
| `-trap-inform-timeout` | duration | `5s` | Per-retry timeout in INFORM mode. |
| `-trap-inform-retries` | int | `2` | Maximum retransmissions per INFORM before it's declared failed. |

## Examples

```bash
# Start server only (all interfaces up/up by default)
sudo ./simulator

# Auto-create 5 devices starting from 192.168.100.1
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 5

# Custom API port and subnet
sudo ./simulator -auto-start-ip 10.10.10.1 -auto-count 100 -port 9090

# Non-privileged SNMP port (no CAP_NET_BIND_SERVICE needed)
sudo ./simulator -auto-start-ip 10.10.10.1 -auto-count 10 -snmp-port 1161

# SNMPv3 with MD5 auth and AES128 privacy
sudo ./simulator -snmpv3-engine-id 0x80001234 -snmpv3-auth md5 -snmpv3-priv aes128

# Disable network namespace isolation
sudo ./simulator -no-namespace -auto-start-ip 192.168.100.1 -auto-count 10

# Maintenance window — all interfaces admin-shutdown
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 10 -if-scenario 1

# Link failure — all interfaces admin-up but oper-down
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 10 -if-scenario 3

# Partial outage — 30% of interfaces oper-down
sudo ./simulator -auto-start-ip 192.168.100.1 -auto-count 10 \
    -if-scenario 4 -if-failure-pct 30
```
