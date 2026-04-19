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
| `/api/v1/traps/status` | GET | SNMP trap export status and INFORM counters. |
| `/api/v1/devices/{ip}/trap` | POST | Fire a named catalog trap on a specific device. |
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
  "devices_exporting": 100
}
```

In TRAP mode the four `informs_*` fields are omitted. When trap export is
disabled the response is:

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
| `name` | string | yes | Catalog entry name (e.g. `linkDown`, `linkUp`, `coldStart`). |
| `varbindOverrides` | object | no | Map of template-field → string-value overrides. Only fields from the catalog template vocabulary are accepted (`IfIndex`, `Uptime`, `Now`, `DeviceIP`). |

Response:

| Status | Body | When |
|--------|------|------|
| `202 Accepted` | `{"requestId": <uint32>}` | Trap has been enqueued. In INFORM mode the `requestId` is the INFORM PDU's `request-id`. |
| `400 Bad Request` | error JSON | Malformed JSON body, missing/empty `name`, or unknown catalog entry. |
| `404 Not Found` | error JSON | Unknown device IP. |
| `500 Internal Server Error` | error JSON | Template resolve or write failure (`Fire` returned 0). |
| `503 Service Unavailable` | error JSON | Trap export is disabled. |

The endpoint does not block waiting for an INFORM ack — use
`/api/v1/traps/status` to observe INFORM lifecycle counters.

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
