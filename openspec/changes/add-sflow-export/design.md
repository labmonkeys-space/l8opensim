## Context

**Current state.** The flow-export subsystem in `go/simulator/` supports two protocols via a single abstraction:

| Surface | Current shape |
|---|---|
| Encoder contract | `FlowEncoder` interface in `flow_exporter.go` — `EncodePacket(domainID, seqNo, uptimeMs, records, includeTemplate, buf) (int, error)` plus `PacketSizes() (baseOverhead, templateSize, recordSize)`. Both numbers in `PacketSizes()` assume a **fixed** record size. |
| Protocol switch | `InitFlowExport` at `flow_exporter.go:271` — hard-coded `switch` on `netflow9` / `ipfix`. |
| Pagination | `FlowExporter.Tick` at `flow_exporter.go:160–200` — computes batch capacity as `(len(buf) - overhead) / recSize`. Assumes every record is the same size. |
| Record source | `FlowCache.GenerateFlows()` in `flow_cache.go` synthesizes IPv4 5-tuple records with byte/packet counters and active/inactive timeout semantics. |
| Counter source | `IfCounterCycler` in `if_counters.go` — SNMP-only. Not wired into flow export. No per-device CPU / memory counter sources exist. |
| Per-device source IP | `-flow-source-per-device` opens a UDP socket in the `opensim` netns bound to the device's IP; each per-device `FlowExporter` owns one `*net.UDPConn`. |
| CLI | `-flow-protocol netflow9|ipfix` in `simulator.go`. |
| Status API | `GET /api/v1/flows/status` reports `"protocol"` in canonical form. |

**sFlow v5 structural misfit.** sFlow v5 ([sflow_version_5.txt](https://sflow.org/sflow_version_5.txt)) differs from NetFlow/IPFIX in three ways that hit the existing abstractions:

1. **Variable-length records.** Each `FLOW_SAMPLE` carries one or more typed "flow records" (raw-packet-header, extended-switch, extended-router, etc.), each with its own XDR-encoded length. `PacketSizes()` can't return a single scalar `recordSize`.
2. **No templates.** sFlow records are self-describing (typed with format+length), so the `includeTemplate` / `templateInterval` machinery has no analogue.
3. **Two sample kinds.** `FLOW_SAMPLE` (packet-sampled data) and `COUNTERS_SAMPLE` (periodic counter dumps). The current export pipeline only knows about flows, not counter polls.

**Constraints.**
- Keep NetFlow v9 and IPFIX behaviour exactly unchanged. No regression risk tolerated.
- `flow_exporter.go` is also touched by issue #43 (NetFlow v5). Per the task-sequencing note in `tasks.md`, NetFlow v5 lands first; this change rebases on top.
- Go stdlib has no XDR encoder. Adding a dependency needs justification.
- The simulator does not forward real traffic. Any sFlow "sampling rate" is therefore synthetic — collectors that extrapolate volume from sample rate need a documented, predictable value.

**Stakeholders.**
- Primary: Ronny Trommer (maintainer).
- Consumers: OpenNMS Telemetryd sFlow adapter (primary test target), plus users validating HP/Aruba, Arista, or Juniper MX integrations.
- Indirect: Issue #43 (NetFlow v5) author — our encoder-contract changes should not make their change harder.

## Goals / Non-Goals

**Goals:**
- `-flow-protocol sflow` (alias `sflow5`) selects sFlow v5 output end-to-end.
- Phase 1 emits valid sFlow v5 datagrams with `FLOW_SAMPLE` records that any compliant collector decodes without error.
- Phase 2 emits `COUNTERS_SAMPLE` records for interface + CPU + memory counters on the same tick, with counter sources reusable by future protocols and by existing `if_counters.go` consumers.
- The encoder contract evolves to tolerate variable-length records without forcing NetFlow/IPFIX implementations to change observable behaviour.
- Per-device source IP binding continues to work: each datagram carries the device IPv4 as the sFlow agent address, and is sent from the per-device socket when available.
- Synthetic sampling rate is explicit in output and in docs — no collector should silently multiply meaningless numbers.
- Unit tests decode emitted datagrams with an in-process XDR oracle, mirroring `netflow9_test.go` and `ipfix_test.go`.

**Non-Goals:**
- Real packet capture / forwarding (the simulator has none to sample).
- sFlow v4 (legacy; non-XDR).
- IPv6 flow samples. IPv4 5-tuple only, matching what `FlowRecord` already models.
- New counter categories beyond interface + CPU + memory in Phase 2 (no ASIC, per-core, or storage-backend counters this round).
- Extending the web UI or `/api/v1/flows/status` beyond reporting `"protocol": "sflow"`.
- Porting `if_counters.go` SNMP consumers to the new counter-source interface beyond what Phase 2 requires (they keep working via a thin adapter).

## Decisions

### D1. Extend `FlowEncoder` rather than add a sibling interface

**Decision:** Extend the existing `FlowEncoder` interface with one additional method that supports variable-length pagination, rather than introducing a parallel `SampleEncoder` interface.

**Shape (illustrative, not normative):**
```go
type FlowEncoder interface {
    EncodePacket(...) (int, error)
    PacketSizes() (baseOverhead, templateSize, recordSize int)
    // New: optional hint for variable-length protocols. Encoders that
    // return a non-zero value here opt into variable-length pagination.
    // NetFlow9/IPFIX return 0 and keep fixed-size semantics.
    MaxRecordSize() int
}
```
`FlowExporter.Tick` inspects `MaxRecordSize()`; if non-zero, it paginates by calling back into the encoder for a per-record size estimate (or simply bounds the batch by a worst-case record size and lets the encoder return `ErrBatchTooBig` to trigger a re-paginate).

**Rationale:** A parallel interface duplicates the pagination loop and the per-device exporter lifecycle for marginal cleanliness gain. The existing two encoders don't grow new responsibilities — they just answer "0" to the new method. The maintenance cost is one extra tiny method on two types.

**Alternatives considered:**
- **`SampleEncoder` sibling interface.** Rejected: requires branching in `SimulatorManager.InitFlowExport`, `FlowExporter`, `startFlowTicker`, and tests on encoder kind. Net complexity higher than the extension path.
- **Swap `PacketSizes()` to return variable-size info for everyone.** Rejected: ripples into NetFlow/IPFIX tests and implementations for zero behavioural gain. The extension above is additive and non-breaking.

### D2. Inline XDR primitives, no new dependency

**Decision:** Implement the XDR primitives we need inline in `sflow.go` rather than adding `github.com/davecgh/go-xdr` to `go.mod`.

**Primitives needed:**
- `uint32` (4 bytes big-endian) — covers length fields, enum tags, counter samples, sequence numbers.
- `opaque<>` (length-prefixed byte slice with 4-byte alignment padding) — covers sampled packet headers and variable fields.
- Fixed-length arrays — covers agent address (4 bytes for IPv4), MAC fields, etc.

Everything fits in ~40 lines of Go using `encoding/binary.BigEndian.PutUint32` and a `padTo4()` helper.

**Rationale:** The dependency is tiny, rarely updated, and MIT-licensed, so vendoring it is defensible. But the cost of a new dep in a simulator that already has a minimal deps list outweighs the 40 lines of code we save. Inline keeps audit surface small.

**Alternative:** `github.com/davecgh/go-xdr`. Fine if inline ends up fighting us on complex records, but Phase 1's `flow_sample_expanded` layout is flat enough that inline primitives suffice.

### D3. Synthetic sampling rate derived from `FlowProfile.ConcurrentFlows`

**Decision:** Every `FLOW_SAMPLE` emits a fixed sampling rate of `10 × ConcurrentFlows`. Documented in the README as synthetic.

**Rationale:** The `ConcurrentFlows` value drives how many flows `FlowCache` keeps live; `10 ×` gives a plausible "1 in N packets sampled" number for the collector's volume-extrapolation math that still yields meaningful-looking rates without implying a specific real-world link speed.

The alternative — sampling rate = 1 (every packet sampled) — produces correct-looking XDR but breaks collectors that multiply by sample rate to estimate link utilization; their graphs would be flatlined at the synthetic record rate. A non-1 synthetic value is honest-ish: the number is wrong but the shape matches real devices.

**Alternative:** Make the sampling rate a CLI flag (`-flow-sflow-sampling-rate`). Rejected for Phase 1 — one more knob for a behaviour we don't want collectors to trust anyway. If users object, add the flag in a follow-up.

### D4. Agent identity = device IPv4 (per-device datagram)

**Decision:** Each sFlow datagram carries the emitting device's IPv4 as its `agent_address`. The `sub_agent_id` is 0. When `-flow-source-per-device` is enabled (the default), the UDP source IP matches the agent_address by virtue of the per-device socket bind.

**Rationale:** sFlow's "one agent per datagram" rule maps cleanly onto the existing per-device `FlowExporter` model — each exporter already produces its own datagrams from its own socket. Keeping agent_address = source IP = device IP means the collector's agent-to-node mapping works identically across NetFlow9, IPFIX, and sFlow.

**Alternative:** Emit a single "aggregate agent" per simulator with all device samples folded under one agent identity. Rejected: defeats the purpose of the per-device-source-IP feature and misrepresents the simulated topology to the collector.

### D5. `FlowCache` records are reinterpreted, not rewritten

**Decision:** The existing `FlowRecord` / `FlowCache.GenerateFlows()` output is consumed unchanged by the sFlow encoder. The encoder synthesizes a "sampled packet header" by constructing a minimal IPv4+UDP header from the 5-tuple and wrapping it as the `sampled_header` portion of a `FLOW_SAMPLE`.

**Rationale:** Keeps the flow-generation pipeline protocol-agnostic. The cost is that the synthesized packet header is obviously synthetic (no real Ethernet framing, no payload), which matches the documented "synthetic samples" caveat.

**Alternative:** Teach `FlowCache` to generate sFlow-native packet snapshots. Rejected: couples the cache to protocol specifics and breaks the clean "records in, bytes out" separation we have today.

### D6. Counter samples use a new `CounterSource` interface (Phase 2)

**Decision:** Phase 2 introduces:
```go
type CounterRecord struct {
    Format   uint32 // sFlow counter-record format tag
    SourceID uint32 // ifIndex for per-interface records, 0 for device-wide
    Body     []byte
}

type CounterSource interface {
    // Snapshot returns counter records for the device at time t.
    Snapshot(t time.Time) []CounterRecord
}
```
- `InterfaceCounterSource` — extracted from `IfCounterCycler`. `if_counters.go` keeps its SNMP wiring but delegates the counter math to this source. Tags each record with `SourceID = ifIndex`.
- `CPUCounterSource` — new, per-device. Produces a single `processor_information` record (format 1001) carrying CPU utilization plus total/free memory. Tagged with `SourceID = 0`. Memory is folded into this standard record rather than emitted under a non-standard format ID.

`FlowExporter.Tick` in sFlow mode calls `Snapshot` on all registered sources once per tick. `SFlowEncoder.EncodeCounterDatagram` groups the returned records by `SourceID` and emits one `counters_sample` per group (per-interface records keyed by ifIndex, device-wide records under source_id 0). This matches how collectors such as OpenNMS Telemetryd key `if_counters` by ds_index.

**Rationale:** A single interface keeps the sFlow encoder from knowing about counter internals, and sets up `if_counters.go` for the separately-tracked ifHC cycling gap (see memory `project_ifhc_counter_gap.md`). The gap fix can land on top of this abstraction cleanly. Per-SourceID grouping is required for correct collector attribution — a single `counters_sample` with `source_id = 0` and N `if_counters` records is valid XDR but misattributes per-interface metrics.

**Alternative:** Hard-code the three counter types in `sflow.go`. Rejected: blocks the ifHC fix and forces future counter additions into a protocol-specific file. Keeping a dedicated `MemoryCounterSource` was also rejected — `processor_information` already carries the fields, and a simulator-local format ID would be silently dropped by strict collectors.

### D7. Protocol string canonicalization: `sflow`

**Decision:** Accept `sflow` and `sflow5` on the CLI; canonicalize to `sflow` in `SimulatorManager.flowProtocol` and `/api/v1/flows/status`. Error message in the default switch branch lists all three protocols.

**Rationale:** Matches the existing `netflow9` / `nf9` alias pattern in `flow_exporter.go:272`. Users searching docs for "sflow v5" find the right flag either way.

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| Variable-length pagination regression impacts NetFlow/IPFIX | `MaxRecordSize() int` returns 0 for fixed-size encoders; `Tick` branches on zero and preserves the existing code path byte-for-byte. Unit tests assert NetFlow/IPFIX emission is identical before and after. |
| Synthetic sampling rate confuses operators into trusting extrapolated numbers | README and `CLAUDE.md` explicitly label sFlow output synthetic. `/api/v1/flows/status` could expose a `"synthetic": true` flag for sFlow (nice-to-have; in tasks). |
| Counter sample omission trips collector warnings | Phase 1 ships flow-samples-only. Release notes call out Phase 2 is required for full collector happiness. Ship both phases together if schedule allows. |
| XDR encoding bugs hide behind "collector accepts the packet" | Unit tests use an in-process XDR decoder oracle that re-parses our output and asserts field-by-field equality with the input `FlowRecord`. Mirrors `netflow9_test.go` structure. |
| Per-device socket source-IP mismatch with agent_address (e.g. per-device binding failed, socket fell back to shared) | The exporter logs a warning on per-device bind failure today; we extend that log to mention "sFlow agent identity may not match source IP" in sFlow mode. Collectors that cross-check agent vs. packet source will see coherent mismatch logs rather than silent misattribution. |
| `flow_exporter.go` merge conflict with issue #43 (NetFlow v5) | NetFlow v5 lands first (documented in `tasks.md`). Our protocol switch + interface extension rebases on top of #43's switch changes. Task 1.1 checks issue #43 merge status before starting. |
| sFlow datagrams exceed 1500-byte MTU when records carry long raw headers | We cap per-datagram record count on encode; if a single record exceeds MTU, we drop it with a `log.Printf` (best-effort, same policy as NetFlow/IPFIX for over-capacity buffers). Phase 1 sampled headers are short (IPv4+UDP = 28 bytes) so this is a theoretical concern for Phase 1. |

## Migration Plan

No user-visible migration — this is an additive change.

**Rollout:**
1. Land Phase 1 (flow samples) behind `-flow-protocol sflow`. Validate with OpenNMS Telemetryd + at least one external collector (Inmon sFlowTrend or equivalent).
2. Land Phase 2 (counter samples) as a follow-up commit or PR in the same change set. Regression test: NetFlow9/IPFIX tick output unchanged.
3. Update `CLAUDE.md` and `README.md` in the same PR as Phase 1.

**Rollback:** Revert the PR. No state migration required. Users running `-flow-protocol sflow` fall back to an error message and must switch to `netflow9` or `ipfix`.

## Open Questions

1. **CLI flag for sampling rate?** D3 defers this. If collector feedback after Phase 1 demands per-simulation sampling rate control, add `-flow-sflow-sampling-rate` in a follow-up. Not blocking.
2. **Scope of Phase 2 counter types.** CPU and memory are new counter surfaces. Should they also be exposed via SNMP for consistency (`hrProcessorTable`, `hrStorageTable`)? Out of scope for this change but worth tracking.
3. **`CounterSource` for sFlow-only or all exporters?** Phase 2 wires it only into sFlow's `COUNTERS_SAMPLE` path. NetFlow v9 Options Templates could carry similar data, but that's a separate feature.
4. **Agent sub_agent_id ever non-zero?** Always 0 in Phase 1. If we ever simulate multi-linecard chassis, sub_agent_id could distinguish linecards. Not relevant for the current device types.
5. **Should `/api/v1/flows/status` expose a `"synthetic": true` flag for sFlow?** Noted in risks; decide during Phase 1 review.
