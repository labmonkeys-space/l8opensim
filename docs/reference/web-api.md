# Web API

The simulator exposes a REST control-plane on port 8080 (override with
[`-port`](cli-flags.md#core-flags)) for device CRUD, CSV / route-script
export, system stats, and flow-export status. The same port also serves the
management web UI at `/`.

## Endpoint catalog

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/devices` | POST | Create devices (bulk, round-robin, category-based). |
| `/api/v1/devices` | GET | List all devices. |
| `/api/v1/devices/{id}` | DELETE | Delete a specific device. |
| `/api/v1/devices` | DELETE | Delete all devices. |
| `/api/v1/devices/export` | GET | Export device list to CSV. |
| `/api/v1/devices/routes` | GET | Generate a routing script (Debian/Ubuntu). |
| `/api/v1/resources` | GET | List available device resource types. |
| `/api/v1/status` | GET | Manager status. |
| `/api/v1/system-stats` | GET | System stats (file descriptors, memory). |
| `/api/v1/flows/status` | GET | Flow export status and cumulative counters. |
| `/api/v1/traps/status` | GET | SNMP trap export status, INFORM counters, and per-type catalog map. |
| `/api/v1/devices/{ip}/trap` | POST | Fire a named catalog trap on a specific device. |
| `/api/v1/syslog/status` | GET | UDP syslog export status, counters, and per-type catalog map. |
| `/api/v1/devices/{ip}/syslog` | POST | Fire a named catalog syslog message on a specific device. |
| `/health` | GET | Health check endpoint. |

## Create devices

Bulk creation supports round-robin across all device types, category-based
filtering, per-request SNMP port selection, and an optional SNMPv3 block.

```bash
# Round-robin across all device types
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 10,
    "netmask": "24",
    "round_robin": true
  }'

# Non-privileged SNMP port (avoids CAP_NET_BIND_SERVICE)
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 5,
    "netmask": "24",
    "snmp_port": 1161
  }'

# Filter by category
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 3,
    "netmask": "24",
    "round_robin": true,
    "category": "GPU Servers"
  }'

# SNMPv3
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

A specific resource file can be requested directly (useful for storage
devices):

```bash
# Create a Pure Storage FlashArray device
curl -X POST http://localhost:8080/api/v1/devices \
  -H "Content-Type: application/json" \
  -d '{
    "start_ip": "192.168.100.1",
    "device_count": 1,
    "netmask": "24",
    "resource_file": "pure_storage_flasharray.json"
  }'
```

## List devices

```bash
curl http://localhost:8080/api/v1/devices
```

## Export to CSV

```bash
curl http://localhost:8080/api/v1/devices/export -o devices.csv
```

## Generate a route script

```bash
curl http://localhost:8080/api/v1/devices/routes -o add_routes.sh
```

The generated script adds Linux kernel routes for every device IP — handy
when running the simulator inside a VM and testing from the host.

## Delete devices

```bash
# Single device
curl -X DELETE http://localhost:8080/api/v1/devices/{device-id}

# All devices
curl -X DELETE http://localhost:8080/api/v1/devices
```

## Flow export status

```bash
curl http://localhost:8080/api/v1/flows/status
```

When flow export is enabled:

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

See [Flow export (operator guide)](../ops/flow-export.md) and
[Flow export reference](flow-export.md) for protocol-specific details.

## Trap export status

```bash
curl http://localhost:8080/api/v1/traps/status
```

Unlike the flow-status endpoint, this response is **not** wrapped in the
`{success, message, data}` envelope — the handler serialises `TrapStatus`
directly. When trap export is enabled in INFORM mode (the most complete
response shape):

```json
{
  "enabled": true,
  "mode": "inform",
  "collector": "192.168.1.10:162",
  "community": "public",
  "sent": 182430,
  "informs_pending": 17,
  "informs_acked": 182380,
  "informs_failed": 33,
  "informs_dropped": 0,
  "rate_limiter_tokens_available": 94,
  "devices_exporting": 100,
  "catalogs_by_type": {
    "_universal":    {"entries": 5,  "source": "embedded"},
    "cisco_ios":     {"entries": 12, "source": "file:resources/cisco_ios/traps.json"},
    "juniper_mx240": {"entries": 12, "source": "file:resources/juniper_mx240/traps.json"}
  }
}
```

`catalogs_by_type` keys are device-type slugs (plus the reserved
`_universal` entry for the fallback catalog). `source` is
`"embedded"`, `"file:<path>"`, or `"override:<path>"` when
`-trap-catalog` was supplied. In TRAP mode the four `informs_*`
fields are omitted. When trap export is disabled the response is:

```json
{"enabled": false, "sent": 0, "devices_exporting": 0}
```

`rate_limiter_tokens_available` is only present when `-trap-global-cap` is
set. The `sent` counter increments on **every wire emission including
INFORM retransmissions**, so it can exceed `informs_acked + informs_failed
+ informs_dropped + informs_pending` under retry churn.

See [SNMP trap / INFORM export (operator guide)](../ops/snmp-traps.md) and
[SNMP trap reference](snmp-traps.md) for the full feature details.

## Fire a trap on demand

```bash
curl -X POST http://localhost:8080/api/v1/devices/192.168.100.1/trap \
  -H "Content-Type: application/json" \
  -d '{"name":"linkDown","varbindOverrides":{"IfIndex":"3"}}'
```

Request body:

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `name` | string | yes | Catalog entry name (e.g. `linkDown`, `ciscoConfigManEvent`). Must match an entry in the **device's resolved catalog** (per-type overlay if present, universal otherwise) — not the universal catalog globally. |
| `varbindOverrides` | object | no | Map of template-field → string-value overrides. Only fields from the nine-field unified vocabulary are accepted (`IfIndex`, `IfName`, `Uptime`, `Now`, `DeviceIP`, `SysName`, `Model`, `Serial`, `ChassisID`). |

Response:

| Status | Body | When |
|--------|------|------|
| `202 Accepted` | `{"requestId": <uint32>}` | Trap has been enqueued. In INFORM mode the `requestId` is the INFORM PDU's `request-id`. |
| `400 Bad Request` | `{"error": "...", "catalog": "<slug>", "availableEntries": [...]}` | Unknown catalog entry for this device. The enriched body reports which catalog the device resolved to (`cisco_ios`, `_universal`, etc.) and lists its entries alphabetically so a scripted caller can fix its call without a separate discovery endpoint. For malformed JSON or missing `name`, the legacy envelope form `{"success": false, "message": "..."}` applies. |
| `404 Not Found` | error JSON | Unknown device IP. |
| `500 Internal Server Error` | error JSON | Template resolve error, catalog resolution returned nil despite feature active (pathological manager state), or write failure. |
| `503 Service Unavailable` | error JSON | Trap export is disabled. |

The endpoint does not block waiting for an INFORM ack — use
`/api/v1/traps/status` to observe INFORM lifecycle counters.

## Syslog export status

```bash
curl http://localhost:8080/api/v1/syslog/status
```

When syslog export is enabled:

```json
{
  "enabled": true,
  "format": "5424",
  "collector": "192.168.1.10:514",
  "sent": 18240,
  "send_failures": 12,
  "rate_limiter_tokens_available": 380,
  "devices_exporting": 100,
  "catalogs_by_type": {
    "_universal":    {"entries": 6,  "source": "embedded"},
    "cisco_ios":     {"entries": 14, "source": "file:resources/cisco_ios/syslog.json"},
    "juniper_mx240": {"entries": 13, "source": "file:resources/juniper_mx240/syslog.json"}
  }
}
```

`format` is `"5424"` or `"3164"`. `catalogs_by_type` follows the same
shape as the trap endpoint. `rate_limiter_tokens_available` is present
only when `-syslog-global-cap` is set. When disabled the response is
`{"enabled": false}`.

See [UDP syslog export (operator guide)](../ops/syslog-export.md) and
[UDP syslog reference](syslog-export.md) for the full feature details.

## Fire a syslog message on demand

```bash
curl -X POST http://localhost:8080/api/v1/devices/192.168.100.1/syslog \
  -H "Content-Type: application/json" \
  -d '{"name":"interface-down","templateOverrides":{"IfIndex":"3"}}'
```

Request body:

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `name` | string | yes | Catalog entry name. Same device's-catalog resolution rule as the trap endpoint. |
| `templateOverrides` | object | no | Nine-field unified vocabulary (same set as `varbindOverrides` on the trap side). |

Response:

| Status | Body | When |
|--------|------|------|
| `202 Accepted` | `{}` | Message emitted. On-demand fires **do not** consume global rate-cap tokens. |
| `400 Bad Request` | `{"error": "...", "catalog": "<slug>", "availableEntries": [...]}` | Unknown catalog entry for this device. Same enriched-error shape as the trap endpoint. |
| `404 Not Found` | error JSON | Unknown device IP. |
| `500 Internal Server Error` | error JSON | Pathological catalog-resolution-nil state. |
| `503 Service Unavailable` | error JSON | Syslog export is disabled. |

## Device interaction

The control-plane only manages devices — once a device is up, you interact
with it via its own IP on port 22 (SSH), 161 (SNMP), and, for storage
devices, 8443 (HTTPS).

```bash
# SSH (VT100 terminal emulation)
ssh simadmin@192.168.100.1     # password: simadmin

# SNMP v2c
snmpget  -v2c -c public 192.168.100.1 1.3.6.1.2.1.1.1.0
snmpwalk -v2c -c public 192.168.100.1 1.3.6.1.2.1.2.2.1

# SNMP v3 (when enabled)
snmpget -v3 -l authPriv -u admin -a MD5 -A authpass123 -x AES -X privpass123 \
  -e 0x80001234 192.168.100.1 1.3.6.1.2.1.1.1.0
```

See [SNMP reference](snmp.md) for the OID coverage, including the dynamic HC
interface counters on `ifXTable`.

### Storage HTTPS endpoints

Storage devices expose vendor-shaped REST APIs on port 8443 with shared TLS
certificates generated at simulator startup.

```bash
# Pure Storage FlashArray
curl -k https://192.168.100.1:8443/api/2.14/volumes
curl -k https://192.168.100.1:8443/api/2.14/arrays
curl -k https://192.168.100.1:8443/api/2.14/arrays/space

# NetApp ONTAP
curl -k https://192.168.100.1:8443/api/cluster
curl -k https://192.168.100.1:8443/api/storage/volumes
curl -k https://192.168.100.1:8443/api/storage/aggregates

# AWS S3
curl http://192.168.100.1:8443/            # list buckets
curl http://192.168.100.1:8443/my-bucket   # bucket contents
```
