# Flow export reference

l8opensim emits synthetic flow telemetry in four protocols: **NetFlow v5**
(Cisco), **NetFlow v9** (RFC 3954), **IPFIX** (RFC 7011), and **sFlow v5**
(`sflow_version_5.txt`). This page covers the protocol-level details. For
deployment, collector setup, and `rp_filter` tuning see
[Flow export (operator guide)](../ops/flow-export.md); for the CLI flags see
[CLI flags → Flow export](cli-flags.md#flow-export-flags).

## Architecture

- One **shared UDP socket** per host (or per-device sockets when
  `-flow-source-per-device=true`, the default), driven by a single ticker
  goroutine in `flow_exporter.go`.
- Each simulated device owns a `FlowCache` populated with
  role-appropriate synthetic flows (edge router, DC switch, firewall, …).
- `FlowEncoder` is a protocol-agnostic interface; `netflow5.go`,
  `netflow9.go`, `ipfix.go`, and `sflow.go` implement it.
- Batch pagination is protocol-aware (different header sizes, different
  record limits per UDP datagram).
- Template refresh is handled in v9 / IPFIX via the
  [`-flow-template-interval`](cli-flags.md#flow-export-flags) flag.

## Protocol details

| Protocol    | Version field | Template ID              | Record size                      | Timestamps                                    |
|-------------|---------------|--------------------------|----------------------------------|-----------------------------------------------|
| NetFlow v5  | `5`           | n/a (no template)        | 48 B / record (30 max per PDU)   | `SysUptime`-relative ms (First / Last)        |
| NetFlow v9  | `9`           | FlowSet ID 0             | 45 B / record                    | `SysUptime`-relative ms (FIRST / LAST_SWITCHED) |
| IPFIX       | `10`          | Set ID 2                 | 53 B / record                    | Absolute epoch ms (IE 152 / 153)              |
| sFlow v5    | `5` (XDR)     | n/a (self-describing)    | ~100 B / record typical (variable) | uptime (ms) + `sampling_rate` per sample    |

NetFlow v5, v9, and IPFIX all use the same 18-field template (bytes, packets,
protocol, ToS, TCP flags, src/dst ports, src/dst IPv4, src/dst mask,
ingress/egress interface, next-hop, src/dst AS, timestamps). NetFlow v5 bakes
this into a fixed 48-byte on-wire record and has no template mechanism at all,
so `-flow-template-interval` is a silent no-op under both v5 and sFlow.

## sFlow caveat

sFlow is a packet-sampling protocol built for real devices that observe real
traffic. l8opensim has no packet stream to sample — sFlow output is
synthesised from the same `FlowCache` records the other protocols consume,
re-wrapped as `FLOW_SAMPLE` records with a fixed, synthetic `sampling_rate`
of `10 × FlowProfile.ConcurrentFlows`. Collectors that multiply sample rate
by captured packet count to estimate link utilisation will produce
plausibly-shaped numbers that do not reflect any real traffic. Use sFlow
mode for collector-plumbing validation, not for link-volume benchmarks.

sFlow v5 emits one `FLOW_SAMPLE` per `FlowRecord` with a `sampled_header`
flow-record carrying a synthesised IPv4 + UDP/TCP header derived from the
5-tuple. On every tick it also emits `COUNTERS_SAMPLE` records (Phase 2)
for each interface's `if_counters`, a processor sample, and a memory sample.

## Per-device source IP

By default (`-flow-source-per-device=true`), each device binds its own UDP
socket inside the `opensim` namespace so the collector observes flow packets
with the **device's IP as the source address**, not the simulator host's.
This makes per-device attribution work out of the box on collectors that key
on the exporter source IP (OpenNMS, Elastiflow, nfcapd, …).

Set the flag to `false` to fall back to a single shared socket bound in the
host namespace.

See [Flow export (operator guide)](../ops/flow-export.md#prerequisites-for-per-device-source-ip)
for the prerequisites (iptables `FORWARD` rule, route to the collector from
the namespace, collector-side `rp_filter` tuning).

## Status API

```bash
curl http://localhost:8080/api/v1/flows/status
```

See [Web API → Flow export status](web-api.md#flow-export-status) for an
example response.
