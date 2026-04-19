# SNMP trap / INFORM reference

l8opensim emits **SNMPv2c** notifications only. The PDU encoder in
`go/simulator/trap_v2c.go` handles TRAPs and INFORMs; SNMPv1 traps and SNMPv3
notifications are deferred and tracked as follow-up work. This page covers
the wire format, the JSON catalog schema, the HTTP endpoints, and the status
JSON shape. For enabling the feature, CLI flags, and troubleshooting see
[SNMP trap / INFORM export (operator guide)](../ops/snmp-traps.md) and
[CLI flags → SNMP trap / INFORM export](cli-flags.md#snmp-trap-inform-export-flags).

## Scope and security posture

Traps are always emitted as **SNMPv2c** regardless of whether the
simulator's polling side is configured with `-snmpv3-engine-id` — the
two paths are independent. An operator running v3-authenticated polls
against the simulator still sees v2c community-authenticated traps on
port 162.

The SNMPv2c community string (`-trap-community`, default `public`) rides
in the clear on every trap and inform. This is a property of SNMPv2c
itself, not a simulator choice, and matches how the simulator's polling
side treats v2c. Do not select a production-like community secret — the
simulator is for testing collector plumbing, not for ingesting
confidential data.

## Architecture

- **Central scheduler goroutine** (`trap_scheduler.go`) owns a min-heap of
  `(nextFire, deviceIP)` entries. Single goroutine regardless of device
  count — no 30k `time.Ticker`s.
- **Per-device `TrapExporter`** (`trap_exporter.go`) owns the device's UDP
  socket, request-id counter, pending-inform map, and stats.
- **Shared `TrapEncoder` interface** (`trap_v2c.go`) — narrow surface
  (`EncodeTrap`, `EncodeInform`, `ParseAck`) so SNMPv1 and SNMPv3 encoders
  can layer in later without changing the scheduler or exporter.
- **Embedded catalog** loaded via `go:embed` from
  `resources/_common/traps.json` at startup — no filesystem dependency
  for the out-of-box experience. `-trap-catalog <path>` replaces the
  embedded default with a user-supplied JSON file (complete override,
  not merge).
- **Global rate limiter** (`golang.org/x/time/rate`) gates both fresh
  fires and INFORM retransmissions so a collector outage cannot amplify
  wire traffic past the operator-configured ceiling.

## Wire format

Every SNMPv2c notification is a BER-encoded SEQUENCE containing three
top-level fields:

| Field | Type | Value |
|-------|------|-------|
| `version` | INTEGER | `1` (SNMPv2c) |
| `community` | OCTET STRING | From `-trap-community` (default `public`) |
| `data` | PDU | TRAP or InformRequest |

The PDU envelope is one of three ASN.1 tags:

| Tag | Name | Direction | Used for |
|-----|------|-----------|----------|
| `0xA7` | SNMPv2-Trap-PDU | simulator → collector | `-trap-mode trap` |
| `0xA6` | InformRequest-PDU | simulator → collector | `-trap-mode inform` |
| `0xA2` | GetResponse-PDU | collector → simulator | INFORM acknowledgement |

Inside each PDU: `request-id` (INTEGER), `error-status` (INTEGER, always 0
on emission), `error-index` (INTEGER, always 0), and a variable-bindings
SEQUENCE.

### Auto-prepended varbinds

RFC 3416 §4.2.6 mandates the first two varbinds of every SNMPv2
notification. The encoder always prepends them automatically — catalog
authors supply only body varbinds, and the catalog loader rejects entries
that list either reserved OID:

| Position | OID | Type | Source |
|----------|-----|------|--------|
| 1 | `1.3.6.1.2.1.1.3.0` (`sysUpTime.0`) | TimeTicks (`0x43`) | Device uptime in 1/100-second ticks |
| 2 | `1.3.6.1.6.3.1.1.4.1.0` (`snmpTrapOID.0`) | OID (`0x06`) | Catalog entry's `snmpTrapOID` |

Everything after position 2 is the catalog entry's body varbinds with
templates resolved to concrete values.

### INFORM acknowledgement

The collector replies to an INFORM with a GetResponse-PDU (`0xA2`) whose
`request-id` matches the INFORM's. The simulator demultiplexes acks via
each device's per-device UDP socket — no global request-id table. Acks
without a matching pending entry (duplicates, stale responses) are
silently ignored.

## Catalog JSON schema

The embedded universal catalog at
`go/simulator/resources/_common/traps.json` is the authoritative example
of the schema:

```json
{
  "traps": [
    {
      "name": "linkDown",
      "snmpTrapOID": "1.3.6.1.6.3.1.1.5.3",
      "weight": 40,
      "varbinds": [
        { "oid": "1.3.6.1.2.1.2.2.1.1.{{.IfIndex}}", "type": "integer", "value": "{{.IfIndex}}" },
        { "oid": "1.3.6.1.2.1.2.2.1.7.{{.IfIndex}}", "type": "integer", "value": "2" },
        { "oid": "1.3.6.1.2.1.2.2.1.8.{{.IfIndex}}", "type": "integer", "value": "2" }
      ]
    }
  ]
}
```

Top-level object:

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `traps` | array | yes | List of catalog entries. Must contain at least one. |

Per-entry object:

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `name` | string | yes | Unique within the catalog. Used by the HTTP fire-on-demand endpoint and for log attribution. |
| `snmpTrapOID` | string | yes | Dotted-decimal OID. Becomes the value of the auto-prepended `snmpTrapOID.0` varbind. |
| `weight` | integer | no (default `1`) | Relative weight for weighted-random selection by the scheduler. Zero means omit the entry from scheduled firing (still reachable via the HTTP endpoint). |
| `varbinds` | array | yes (may be empty) | Body varbinds following the two auto-prepended ones. |

Per-varbind object:

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `oid` | string | yes | Dotted-decimal OID. Templates allowed. Must not be `1.3.6.1.2.1.1.3.0` (`sysUpTime.0`) or `1.3.6.1.6.3.1.1.4.1.0` (`snmpTrapOID.0`). |
| `type` | string | yes | One of `integer`, `octet-string`, `oid`, `counter32`, `gauge32`, `timeticks`, `counter64`, `ipaddress`. |
| `value` | string | yes | Literal value, type-parsed against the `type` field. Templates allowed. |

### Universal catalog (embedded default)

Ships five entries, all from `SNMPv2-MIB`:

| Name | `snmpTrapOID` | Weight | Body varbinds |
|------|---------------|--------|---------------|
| `linkDown` | `1.3.6.1.6.3.1.1.5.3` | 40 | `ifIndex`, `ifAdminStatus` = 2, `ifOperStatus` = 2 |
| `linkUp` | `1.3.6.1.6.3.1.1.5.4` | 40 | `ifIndex`, `ifAdminStatus` = 1, `ifOperStatus` = 1 |
| `authenticationFailure` | `1.3.6.1.6.3.1.1.5.5` | 10 | _(none)_ |
| `coldStart` | `1.3.6.1.6.3.1.1.5.1` | 5 | _(none)_ |
| `warmStart` | `1.3.6.1.6.3.1.1.5.2` | 5 | _(none)_ |

Weights bias scheduled firing toward link-state notifications (the most
common interesting traps for monitoring-pipeline validation) while still
exercising the other three types.

### Template vocabulary

Both `oid` and `value` fields are evaluated as Go `text/template` strings
per fire. The current vocabulary (see `go/simulator/trap_catalog.go` for
the ground truth) is restricted to four fields:

| Field | Evaluation |
|-------|-----------|
| `{{.IfIndex}}` | Random ifIndex drawn from the device's simulated interface set at fire time |
| `{{.Uptime}}` | Device uptime in 1/100-second ticks |
| `{{.Now}}` | Unix epoch seconds |
| `{{.DeviceIP}}` | Dotted-quad IPv4 of the device |

References to any other field are rejected at catalog load — the
simulator refuses to start rather than silently emitting a trap with an
empty OID component.

## HTTP endpoints

### Fire a trap on demand

`POST /api/v1/devices/{ip}/trap` — schedules one trap for the named
device immediately, bypassing the Poisson scheduler. Body:

```json
{
  "name": "linkDown",
  "varbindOverrides": {
    "IfIndex": "3"
  }
}
```

`name` is required and must match a catalog entry. `varbindOverrides` is
optional — supplied keys pin the corresponding template field for this
fire only.

Responses:

| Status | Body | When |
|--------|------|------|
| `202 Accepted` | `{"requestId": <uint32>}` | Success; the trap has been enqueued. For INFORM mode the `requestId` is the INFORM PDU's `request-id` — correlate with `/api/v1/traps/status` to watch its lifecycle. |
| `400 Bad Request` | `{"success": false, "message": "..."}` | Malformed JSON body, missing/empty `name` field, or unknown catalog entry name. |
| `404 Not Found` | `{"success": false, "message": "..."}` | Unknown device IP. |
| `500 Internal Server Error` | `{"success": false, "message": "..."}` | `Fire` failed for a non-lookup reason — template resolve error or write failure. Logs on the simulator side carry the detail. |
| `503 Service Unavailable` | `{"success": false, "message": "..."}` | Trap export is disabled (`-trap-collector` not set). |

The endpoint is fire-and-forget — it does **not** block waiting for an
INFORM ack.

### Trap export status

`GET /api/v1/traps/status` — current snapshot of the trap subsystem.

Unlike `/api/v1/flows/status`, this endpoint does **not** wrap its body
in the `{success, message, data}` envelope — `TrapStatus` is serialised
directly at the top level. When enabled in INFORM mode (the most complete
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

When enabled in TRAP mode, the four `informs_*` fields are omitted (no
INFORM state to report). When disabled the response is:

```json
{"enabled": false, "sent": 0, "devices_exporting": 0}
```

(`sent` and `devices_exporting` are not tagged `omitempty` so they are
always present; their values are zero when the feature is off.)

`rate_limiter_tokens_available` is present only when `-trap-global-cap`
is set; it's a best-effort instantaneous snapshot, not synchronised with
concurrent rate-limiter waits.

The `sent` counter increments on **every wire emission including INFORM
retransmissions**, so `sent` can exceed the sum of the four INFORM state
counters under retry churn. The counter invariant below applies to
*originated* informs, not to the `sent` tally.

### Counter invariant

For INFORM mode, the four disjoint terminal states of every originated
INFORM satisfy:

```
informs_pending + informs_acked + informs_failed + informs_dropped == informs_originated
```

`informs_originated` isn't exposed in the status JSON — it's an internal
counter verified by `TestInformInvariant_AtExporterLevel` in
`trap_api_test.go`. If the four exposed counters don't add up across two
successive polls (after allowing for newly-originated informs between
reads), something is miscounted or a retransmit path is skipping a
state transition.

## CLI flags

The nine `-trap-*` flags are documented with their types, defaults, and
purposes at
[CLI flags → SNMP trap / INFORM export](cli-flags.md#snmp-trap-inform-export-flags).

## Related

- [SNMP trap / INFORM export (operator guide)](../ops/snmp-traps.md) — how to enable, INFORM constraints, `snmptrapd` smoke test
- [SNMP reference](snmp.md) — polling-side SNMP (v2c/v3 GETs, GETNEXTs, OID lookup, HC counters)
- [Web API](web-api.md) — control-plane REST surface
- Epic [#52](https://github.com/labmonkeys-space/l8opensim/issues/52) and PR [#65](https://github.com/labmonkeys-space/l8opensim/pull/65) for the original design and implementation context
