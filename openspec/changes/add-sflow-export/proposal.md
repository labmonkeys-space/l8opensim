## Why

l8opensim currently exports synthetic flow data as NetFlow v9 and IPFIX, but a significant class of real-world collectors and devices speak only sFlow — notably HP/Aruba and Arista switches, Juniper MX platforms, and the OpenNMS Telemetryd sFlow adapter. Without sFlow v5 support, users validating these collectors against l8opensim must stand up a second traffic generator or switch tooling, which defeats the purpose of a single pluggable simulator.

sFlow v5 ([sflow_version_5.txt](https://sflow.org/sflow_version_5.txt)) is also a structurally different beast from NetFlow/IPFIX: it is a sampling-and-polling protocol with XDR-encoded variable-length sample records, not a cache-with-timeouts flow protocol. Adding it forces the flow-export subsystem's abstractions to grow beyond "fixed-size records with templates" — which benefits any future sampling-style protocol too.

## What Changes

- Add a new flow-export protocol `sflow` (alias `sflow5`) selectable via the existing `-flow-protocol` CLI flag alongside `netflow9` and `ipfix`.
- Introduce an sFlow v5 datagram encoder that emits `FLOW_SAMPLE` records (packet-header sampled-packet layout, XDR-encoded). Phase 1 ships flow samples only; counter samples are Phase 2.
- Extend the `FlowEncoder` contract (or add a sibling encoder interface) so that variable-length records are a first-class concern. `PacketSizes()` currently assumes a fixed record size; the pagination logic in `FlowExporter.Tick` must tolerate per-record sizing.
- Reinterpret the existing synthetic flows produced by `FlowCache.GenerateFlows()` as sampled packets with a documented, fixed synthetic sampling rate derived from `FlowProfile.ConcurrentFlows`. Collectors that extrapolate volume from sample rate get a predictable (if synthetic) number.
- Add a second phase that factors interface counters out of `IfCounterCycler` behind a reusable counter-source interface, adds per-device CPU and memory counter sources, and emits sFlow `COUNTERS_SAMPLE` records on the same tick as flow samples. Phase 2 is in scope for this change but gated behind Phase 1.
- Make the `-flow-source-per-device` semantics explicit for sFlow: each datagram carries one agent identity (the device's IPv4), so per-device source binding remains the default.
- Update `CLAUDE.md` flow-export reference and README to list `sflow` as a supported protocol and call out that sFlow samples are synthetic (no real packet stream is forwarded).

No breaking changes. NetFlow v9 and IPFIX behaviour is preserved.

## Capabilities

### New Capabilities
- `flow-export-sflow`: sFlow v5 datagram export capability — XDR encoding, flow samples (Phase 1) and counter samples (Phase 2), agent identity rules, synthetic sampling-rate semantics, and CLI/selector integration.

### Modified Capabilities
<!-- None — the existing flow-export subsystem has no prior OpenSpec spec, so the encoder-contract extension lands entirely within this new capability's spec plus design notes. -->

## Impact

**In-tree files touched**
- `go/simulator/sflow.go` — new encoder (Phase 1 flow samples, Phase 2 counter samples)
- `go/simulator/flow_exporter.go` — protocol switch (same file touched by issue #43 NetFlow v5, see `tasks.md` sequencing note) and `FlowEncoder` interface extension or sibling interface
- `go/simulator/flow_cache.go` — no structural change; existing synthetic flows are reinterpreted as sampled packets in the sFlow code path
- `go/simulator/if_counters.go` — Phase 2 refactor: extract a reusable counter-source interface
- `go/simulator/simulator.go` — CLI flag help text for `-flow-protocol`
- New: per-device CPU / memory counter-source sites (Phase 2; location TBD in design.md)
- New: `go/simulator/sflow_test.go` — unit tests with an in-process XDR decoder oracle
- `CLAUDE.md` — flow-export section
- `README.md` — protocol list + synthetic-samples caveat

**Dependencies**
- XDR encoding: either vendor a minimal XDR primitive set inline (preferred — only `uint32`, `opaque<>`, fixed-length arrays needed) or add `github.com/davecgh/go-xdr` to `go.mod`. Decision recorded in `design.md`.

**Downstream consumers**
- Users currently invoking `-flow-protocol netflow9` or `-flow-protocol ipfix` see no change.
- Users of `GET /api/v1/flows/status` see `"protocol": "sflow"` when sFlow is selected.

**Out of scope**
- Real packet-forwarding sampling — the simulator never sees real traffic, so all samples are synthesized from `FlowCache`.
- sFlow v4 (legacy, non-XDR). Only v5 is supported.
- Sampling of arbitrary protocols beyond what `FlowRecord` already models (IPv4 5-tuple). IPv6 flow samples remain follow-up work.
- Expanded counter types beyond interface + CPU + memory in Phase 2 (e.g., processor-per-core breakdowns, ASIC counters) — reserved for future changes.
