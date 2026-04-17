## Why

NetFlow v5 remains widely deployed in legacy collectors and many teaching / lab setups target it specifically. l8opensim currently exports flows only as NetFlow v9 (RFC 3954) and IPFIX (RFC 7011), which leaves a gap when operators want to exercise v5-only collectors (nfdump/nfcapd, OpenNMS Telemetryd v5 adapter, older vendor appliances). Because the flow export subsystem is already protocol-agnostic via the `FlowEncoder` interface in `go/simulator/flow_exporter.go`, adding v5 is a drop-in addition that unlocks a meaningful compatibility dimension at low cost.

## What Changes

- Add a third flow-export protocol: Cisco NetFlow v5 (24-byte header, 48-byte record, IPv4-only, SysUptime-relative timestamps, max 30 records per datagram).
- Introduce a new `NetFlow5Encoder` implementing the existing `FlowEncoder` interface with `PacketSizes()` returning `(24, 0, 48)`; templates are not used.
- Register `"netflow5"` (alias `"nf5"`) in the `InitFlowExport` protocol switch and update the unsupported-protocol error message accordingly.
- Update the `-flow-protocol` CLI flag help text in `simulator.go` to list `netflow5` as a valid value.
- Define v5-specific encoder behavior: filter IPv4-only records, clamp to 30 records per packet, ignore the `includeTemplate` argument, treat `-flow-template-interval` as a silent no-op, and clamp 32-bit ASNs down to 16 bits (with a one-shot warning log on overflow).
- Add encoder unit tests (`netflow5_test.go`) with an inline decoder oracle covering the header, all 18 fixed record fields, multi-packet pagination, the 30-record cap, and IPv4-only filtering.
- Update `CLAUDE.md` flow-export section to document the new protocol and its limitations.

No interface changes. No changes to `FlowEncoder`, `FlowExporter`, `FlowCache`, device lifecycle, the web API (`GET /api/v1/flows/status` remains unchanged), or TUN / namespace plumbing.

## Capabilities

### New Capabilities
- `flow-export`: Multi-protocol NetFlow / IPFIX export subsystem. Captures the pluggable-encoder contract, protocol selection CLI, per-device FlowCache behavior, and v5/v9/IPFIX protocol-specific constraints. This capability also codifies the pre-existing v9 and IPFIX behavior so v5 can be specified as a peer protocol rather than a bolt-on.

### Modified Capabilities
<!-- None — no prior OpenSpec specs exist in this repo. -->

## Impact

**In-tree files touched**
- New: `go/simulator/netflow5.go` (~150-200 LOC) — `NetFlow5Encoder` implementation.
- New: `go/simulator/netflow5_test.go` — structure, cap, and filter tests with inline decoder oracle.
- `go/simulator/flow_exporter.go` — one new `case` in the `InitFlowExport` protocol switch and updated error message.
- `go/simulator/simulator.go` — help text for `-flow-protocol` flag.
- `CLAUDE.md` — flow export section documents the new protocol, the 30-record cap, IPv4-only scope, and that `-flow-template-interval` is ignored under v5.

**Downstream / operator-facing**
- New valid value for `-flow-protocol`: `netflow5` (and alias `nf5`).
- Operators pointing v5 collectors (nfdump, OpenNMS Telemetryd v5 adapter, legacy vendor tools) at l8opensim can now exercise those collectors without a second exporter.
- Existing `netflow9` and `ipfix` users are unaffected; defaults do not change.

**Out of scope**
- IPv6 flow records for v5 (the protocol cannot represent them; they are filtered out).
- Sampled NetFlow (`sampling_interval` is emitted but always `0`).
- NetFlow v1 / v7 / v8 aggregations.
- Changes to the `FlowEncoder` interface or the `FlowCache` schema.
- Real-collector interoperability validation (nfdump round-trip, OpenNMS ingest) — recommended as a follow-up, not a gate on this change.
