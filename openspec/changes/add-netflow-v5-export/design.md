## Context

**Current state.** The flow export subsystem in `go/simulator/` is already factored for multi-protocol output. Key surfaces:

| Surface | Current value |
|---|---|
| Encoder interface | `FlowEncoder` in `go/simulator/flow_exporter.go:33` — `EncodePacket(domainID, seqNo, uptimeMs, records, includeTemplate, buf) (int, error)` and `PacketSizes() (baseOverhead, templateSize, recordSize)` |
| Existing encoders | `NetFlow9Encoder` in `netflow9.go` → `PacketSizes() = (24, 80, 45)`; `IPFIXEncoder` in `ipfix.go` → `PacketSizes() = (20, 80, 53)` |
| Protocol selection | `switch strings.ToLower(protocol)` in `InitFlowExport` at `flow_exporter.go:271`, supporting `netflow9` / `nf9` / `""` and `ipfix` / `ipfix10` |
| CLI | `-flow-protocol` flag defined in `simulator.go:106-112` |
| Shared runtime | One UDP socket + one ticker goroutine on `SimulatorManager`; per-device `FlowExporter` owns its `FlowCache` |
| Canonical record | `FlowRecord` in `flow_cache.go:39` — already holds every field v5 needs (srcaddr, dstaddr, nexthop, input/output ifIndex, dPkts, dOctets, first/last SysUptime ms, src/dst port, tcp_flags, protocol, tos, src/dst AS, src/dst mask) |

**Constraints** (v5-specific, from Cisco's implementation; no RFC).
- Header is 24 bytes fixed. Fields: `version=5 (u16)`, `count (u16)`, `sys_uptime (u32, ms)`, `unix_secs (u32)`, `unix_nsecs (u32)`, `flow_sequence (u32)`, `engine_type (u8)`, `engine_id (u8)`, `sampling_interval (u16)`.
- Record is 48 bytes fixed. Eighteen fields in a strict order — srcaddr, dstaddr, nexthop (all `u32` IPv4), input, output (`u16` ifIndex), dPkts, dOctets (`u32`), first, last (`u32` SysUptime ms), srcport, dstport (`u16`), `pad1 (u8)`, tcp_flags, prot, tos (`u8`), src_as, dst_as (`u16`), src_mask, dst_mask (`u8`), `pad2 (u16)`.
- Max 30 records per datagram (Cisco's limit; a larger count is what older collectors reject outright).
- IPv4-only — no IPv6 NetFlow v5 variant exists.
- No template mechanism — record layout is fixed at the protocol level; `-flow-template-interval` has no meaning under v5.
- Timestamps are SysUptime-relative (same semantics as v9); `unix_secs`/`unix_nsecs` in the header carry the wall-clock reference.
- ASN fields are 16-bit; cannot represent 32-bit ASNs.
- SysUptime is a 32-bit millisecond counter and wraps at ~49.7 days. Same as v9 — not v5-specific, but worth noting.

**Stakeholders.**
- Primary: Ronny Trommer (maintainer).
- Consumers: Operators testing v5-only collectors (nfdump/nfcapd, OpenNMS Telemetryd v5 adapter, legacy vendor appliances) against l8opensim.

## Goals / Non-Goals

**Goals:**
- Add `netflow5` as a first-class value for `-flow-protocol`, peer to `netflow9` and `ipfix`.
- Implement a `NetFlow5Encoder` that satisfies the existing `FlowEncoder` interface with zero changes to that interface or to `FlowExporter` / `FlowCache` / device lifecycle.
- Enforce v5-specific constraints in the encoder itself (IPv4-only filtering, 30-record cap, 16-bit ASN clamping) rather than leaking them into shared plumbing.
- Provide deterministic, hermetic unit tests with an inline decoder oracle, matching the rigor of `netflow9_test.go` / `ipfix_test.go`.

**Non-Goals:**
- IPv6 flow export under v5 (impossible by protocol).
- Sampled NetFlow (`sampling_interval` is emitted but pinned at `0`; sampling is not simulated).
- NetFlow v1 / v7 / v8 aggregation formats.
- Any change to `FlowEncoder`, `FlowExporter`, `FlowCache`, the web API, or device lifecycle.
- Real-collector interoperability testing (nfdump round-trip, OpenNMS ingest). Recommended as follow-up validation, not a gate on this change.
- Changing default values for any existing flow flag.

## Decisions

### D1. New encoder lives in `go/simulator/netflow5.go`

**Decision:** Implement `NetFlow5Encoder` as a value-typed struct in a new file `netflow5.go`, mirroring the file layout of `netflow9.go` and `ipfix.go`.

**Rationale:** Matching the existing file-per-protocol convention keeps diffs obvious and review cheap. A value-typed struct (like `NetFlow9Encoder{}` / `IPFIXEncoder{}`) means the encoder is stateless on the Go side — state (sequence number, uptime, domain ID) comes in via `EncodePacket` arguments, so a single instance is safe to share across all devices.

**Alternative considered:** Embed v5 logic in `netflow9.go` as a v5 flag. Rejected — v5 and v9 share zero wire-format code (different header, different record, no TLV templates). Splitting into its own file is clearer and has no runtime cost.

### D2. `PacketSizes()` returns `(24, 0, 48)`

**Decision:** `baseOverhead = 24` (header), `templateSize = 0` (no templates exist in v5), `recordSize = 48`.

**Rationale:** `templateSize = 0` is the natural signal that the encoder has no template to retransmit. The caller's capacity math becomes `(MTU − 24) / 48 = capacity`, which for a 1500-byte buffer yields 30.75 — clamped to the protocol-mandated 30.

**Interaction with `includeTemplate`:** `EncodePacket` ignores the `includeTemplate` argument for v5. The flag costs nothing to accept and keeps the interface uniform.

### D3. Enforce the 30-record cap explicitly in `EncodePacket`

**Decision:** Before encoding, if `len(records) > 30` the encoder truncates to the first 30 and returns those bytes. The remaining records are left for the next packet cycle, which is exactly how the ticker already handles partial batches for v9/IPFIX.

**Rationale:** Making the cap explicit rather than relying on the caller's capacity math (`(MTU − 24) / 48 = 30`) defends against future tuning of MTU, buffer-pool size, or overhead constants. A collector parsing `count > 30` may reject the datagram entirely.

**Alternative considered:** Return an error if `len(records) > 30`. Rejected — the ticker does not currently treat "too many records" as a caller error; it batches based on `PacketSizes()`. Truncating silently is the least-surprising behavior and leaves the loop invariant unchanged.

### D4. IPv4-only filtering happens inside `EncodePacket`

**Decision:** Before writing any record, skip any `FlowRecord` whose `SrcIP.To4() == nil` or `DstIP.To4() == nil`. `NextHop` that is non-IPv4 is coerced to `0.0.0.0`. A per-encoder `atomic.Bool` emits a one-shot warning log the first time a skip occurs, including the device domain ID.

**Rationale:** Synthetic flows today are IPv4-only (`syntheticFlow()` generates IPv4), but future work may add IPv6 profiles. Putting the filter in the encoder rather than upstream keeps the concern local to the protocol that can't represent IPv6. One-shot logging is loud enough to surface the misconfiguration once without spamming steady-state logs.

**Alternative considered:** Reject IPv6-bearing cache entries at the device-profile level. Rejected — that would couple profile validation to the currently-selected protocol, which may change at runtime on operator command.

### D5. `-flow-template-interval` is a silent no-op under v5

**Decision:** When `-flow-protocol=netflow5`, the `-flow-template-interval` value is accepted but has no effect. No warning at startup. Documented in `CLAUDE.md`.

**Rationale:** Rejecting the flag would force operators to swap flag sets every time they switch protocols, which is more friction than value. A startup warning would cry wolf — every v5 invocation would emit it, and it's load-bearing information only on the first encounter. Documentation catches the intent once.

**Alternative considered:** Emit a one-line info log at startup when the flag is set to a non-default value under v5. Reasonable but not necessary; defer until an operator reports confusion.

### D6. 16-bit ASN clamping with one-shot log

**Decision:** When a `FlowRecord` carries `SrcAS > 0xFFFF` or `DstAS > 0xFFFF`, the encoder writes `0xFFFF` into the 16-bit wire field and logs one warning per encoder lifetime identifying the device.

**Rationale:** Today `FlowRecord.SrcAS` / `DstAS` are already `uint16`, so this is a defense-in-depth measure against a future schema widening. Clamping to `0xFFFF` is conventional (RFC 6793 §2 "AS_TRANS"). One-shot logging keeps the simulator usable if a profile ever generates 32-bit ASNs.

### D7. Protocol aliases `netflow5` and `nf5`

**Decision:** `InitFlowExport` accepts both `netflow5` and `nf5` (case-insensitive), mirroring the `netflow9`/`nf9` pair. Canonical form stored on `SimulatorManager.flowProtocol` is `netflow5`.

**Rationale:** The aliasing pattern already exists; applying it uniformly is cheaper than explaining an exception.

### D8. Test oracle is an inline v5 decoder, not a fixture

**Decision:** `netflow5_test.go` includes a compact byte-exact decoder that reads the 24-byte header and 48-byte records back into struct form, then asserts equality against the expected values. No binary fixtures.

**Rationale:** Matches `netflow9_test.go` / `ipfix_test.go` (`decodeNetFlow9(...)` / `decodeIPFIX(...)`). Keeps the test hermetic — no fixture drift, no vendor-tool dependency in CI, no hidden encoding assumptions. Real-collector interop is an out-of-band validation step.

**Test matrix:**
1. Header structure — every field in its documented position with expected values.
2. Record structure — all 18 fields round-trip.
3. Multi-packet pagination — 45 records in, two packets out (30 + 15).
4. Explicit 30-record cap — 31 records in, first 30 encoded, 31st unchanged on next call.
5. IPv4-only filtering — mixed v4/v6 input, only v4 encoded, warning-log path exercised.
6. `includeTemplate = true` is a no-op (no template bytes emitted, `count` unchanged).
7. ASN clamping — `SrcAS = 0x100000` produces `0xFFFF` on wire.

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| Silent IPv6 skip hides misconfiguration from operators | One-shot warning log at first skip per encoder, including device domain ID. Documented in `CLAUDE.md`. |
| Unit tests pass but real collectors reject the datagram | Recommend (not gate) round-trip validation with `nfcapd`/`nfdump` and OpenNMS Telemetryd v5 before closing issue #43. Structural tests catch most wire-format errors; interop catches the rest. |
| Silent acceptance of `-flow-template-interval` under v5 confuses operators | Document in `CLAUDE.md` flow-export section. If questions arise, promote to a one-line startup info log. |
| 30-record cap enforced at two layers (capacity math + explicit check) is redundant | Accept the redundancy — cheaper than a future regression where someone tunes MTU / overhead and silently emits `count = 31` datagrams. |
| Future schema widening to 32-bit ASNs would truncate silently under v5 | Clamp-to-`0xFFFF` with one-shot warning log. Tracked in test matrix (case 7). |
| SysUptime 49.7-day wrap emits monotonically decreasing timestamps | Not v5-specific (v9 has the same bug). Out of scope for this change. |
| Synthetic flows from a single device can burst past 30 records per tick, inflating tick latency | The ticker already batches across multiple packets per device; no change in total tick cost, just more packets per tick. |

## Migration Plan

No runtime migration required — this change is purely additive. Operators explicitly opt in via `-flow-protocol=netflow5`. Default behavior (`netflow9`) is unchanged.

**Per-step verification:**

1. **Unit tests pass** (`cd go && go test ./simulator/ -run TestNetFlow5`).
2. **Integration smoke** (manual): `sudo ./simulator -flow-collector=127.0.0.1:2055 -flow-protocol=netflow5 -auto-start-ip=10.42.1.1 -auto-count=5`. Confirm `nfcapd -p 2055 -l /tmp/nf5` captures files; `nfdump -r /tmp/nf5/<file>` decodes records with expected 5-tuples.
3. **Status endpoint unchanged**: `curl localhost:8080/api/v1/flows/status` reflects `"protocol": "netflow5"`.

**Rollback:** Revert the single PR. No state to clean up; no external systems hold protocol-version assumptions.

## Open Questions

1. **Should the `flow_exporter.go` documentation strings (e.g. `InitFlowExport`'s "only supported value for Phase 2" comment) be updated as part of this change, or left for a follow-up doc sweep?** Low cost either way. Recommend updating in this PR to keep the code self-consistent.
2. **Should the `flow-status` JSON include a `"capabilities"` hint so UIs can display protocol-specific notes (e.g. "template interval ignored")?** Out of scope — no UI currently consumes this. Defer.
3. **Real-collector validation owner?** Not a gate on merging, but someone should confirm nfdump + OpenNMS Telemetryd v5 before closing issue #43. Maintainer's call.
