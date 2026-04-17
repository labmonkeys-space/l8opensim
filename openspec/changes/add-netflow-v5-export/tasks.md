## 1. Encoder scaffold

- [x] 1.1 Create `go/simulator/netflow5.go` with the standard Layer 8 Ecosystem license header (match `netflow9.go` / `ipfix.go`), `package main`, and imports for `encoding/binary`, `fmt`, `log`, `net`, `sync/atomic`, `time`.
- [x] 1.2 Declare `type NetFlow5Encoder struct` with two unexported `atomic.Bool` fields for one-shot warnings: one for IPv6 skip, one for ASN clamp.
- [x] 1.3 Implement `PacketSizes() (baseOverhead, templateSize, recordSize int)` returning `(24, 0, 48)`.
- [x] 1.4 Wire-format constants — declare `const netFlow5Version = 5`, `const netFlow5MaxRecords = 30`, `const netFlow5HeaderLen = 24`, `const netFlow5RecordLen = 48` at file scope.

## 2. Encoder implementation

- [x] 2.1 Implement `EncodePacket(domainID, seqNo, uptimeMs uint32, records []FlowRecord, includeTemplate bool, buf []byte) (int, error)`. Ignore `includeTemplate`.
- [x] 2.2 Pre-filter `records`: build a local slice of records whose `SrcIP.To4() != nil && DstIP.To4() != nil`. Log a one-shot warning (via the `atomic.Bool`) for any skip, including `domainID`.
- [x] 2.3 Clamp the filtered slice to `netFlow5MaxRecords` (30) if longer.
- [x] 2.4 Verify `len(buf) >= 24 + len(filtered)*48`; return an error if not (match the error style of `netflow9.go`).
- [x] 2.5 Write the 24-byte header into `buf[0:24]`: version=5, count=`len(filtered)`, sys_uptime=`uptimeMs`, unix_secs + unix_nsecs from `time.Now()`, flow_sequence=`seqNo`, engine_type=0, engine_id=0, sampling_interval=0. All multi-byte fields big-endian.
- [x] 2.6 Write each filtered record into the following 48 bytes in canonical order — srcaddr, dstaddr, nexthop (IPv6 nexthop coerced to `0.0.0.0`), input ifIndex, output ifIndex, dPkts, dOctets, first (StartMs), last (EndMs), srcport, dstport, pad1=0, tcp_flags, prot, tos, src_as, dst_as, src_mask, dst_mask, pad2=0.
- [x] 2.7 Implement ASN clamping: if `SrcAS > 0xFFFF` or `DstAS > 0xFFFF`, write `0xFFFF` on the wire and emit a one-shot warning log per encoder lifetime.
- [x] 2.8 Return `24 + len(filtered)*48, nil` on success.

## 3. Protocol registration and CLI

- [x] 3.1 In `go/simulator/flow_exporter.go` `InitFlowExport`, add a new case: `case "netflow5", "nf5": enc = NetFlow5Encoder{}; canonicalProtocol = "netflow5"`.
- [x] 3.2 Update the `default` case error message to read `(supported: netflow9, ipfix, netflow5)`.
- [x] 3.3 Update the comment at `flow_exporter.go:251` that currently says `"protocol is 'netflow9' (the only supported value for Phase 2)"` to list all three supported values.
- [x] 3.4 In `go/simulator/simulator.go` (around line 106-112), update the `-flow-protocol` flag help text to list `netflow9`, `ipfix`, `netflow5`, and to note that under `netflow5` the `-flow-template-interval` flag is accepted but has no effect.

## 4. Tests

- [x] 4.1 Create `go/simulator/netflow5_test.go` with license header and `package main`.
- [x] 4.2 Implement an inline `decodeNetFlow5(data []byte) (header netFlow5Header, records []netFlow5WireRecord, err error)` oracle that parses the 24-byte header and each 48-byte record back into structs. Mirror the style of `decodeNetFlow9` / `decodeIPFIX`.
- [x] 4.3 `TestNetFlow5PacketSizes` — assert `PacketSizes() == (24, 0, 48)`.
- [x] 4.4 `TestNetFlow5Header` — encode one record, decode, assert every header field (version, count=1, sys_uptime, unix_secs within tolerance, flow_sequence, engine_type, engine_id, sampling_interval=0).
- [x] 4.5 `TestNetFlow5RecordRoundtrip` — build a `FlowRecord` with a distinct non-zero value for every one of the 18 fields; encode; decode; assert field-by-field equality.
- [x] 4.6 `TestNetFlow5MultiPacketPagination` — submit 45 records across two simulated calls (30 + 15) and assert counts and sequence numbers.
- [x] 4.7 `TestNetFlow5ThirtyRecordCap` — submit 31 records in one call; assert output contains exactly 30 records and returned byte count equals `24 + 30*48 = 1464`.
- [x] 4.8 `TestNetFlow5IPv4OnlyFiltering` — submit a slice containing both IPv6-bearing and IPv4-bearing records; assert only IPv4 records are encoded and the header `count` reflects that; capture log output and assert exactly one warning was emitted.
- [x] 4.9 `TestNetFlow5NextHopCoercion` — submit a record with IPv4 src/dst but IPv6 NextHop; assert the record is encoded and on-wire nexthop is `0.0.0.0`.
- [x] 4.10 `TestNetFlow5TemplateFlagIgnored` — call `EncodePacket(..., includeTemplate=true, ...)` with one record; assert output length is exactly `24 + 48 = 72` bytes (no template prepended).
- [x] 4.11 `TestNetFlow5ASNClamp` — FlowRecord.SrcAS/DstAS are uint16 in the current schema, so this test exercises `clampASN` directly: an in-range value passes through without logging, a first 32-bit value clamps to `0xFFFF` with one warning log, and a second 32-bit value clamps without an additional log. Documented as the placeholder path for a future uint32 widening.
- [x] 4.12 Run `cd go && go test ./simulator/ -run TestNetFlow5 -v` and confirm all tests pass.

## 5. Documentation

- [x] 5.1 Update `CLAUDE.md` flow export section:
  - Add `netflow5` to the list of valid `-flow-protocol` values in the "Key flags" block.
  - Add a paragraph in the Architecture section noting: v5 is IPv4-only, capped at 30 records per packet, has no template mechanism (so `-flow-template-interval` is a silent no-op under v5), uses SysUptime-relative timestamps (same as v9), and clamps 32-bit ASNs to AS_TRANS (`0xFFFF`).
  - Update the flow export design note under "Flow export:" to include `netflow5.go` (NetFlow5Encoder, 24-byte header, 48-byte record, IPv4-only, 30-record cap).

## 6. Integration and verification

- [x] 6.1 Build the simulator: `cd go/simulator && go mod tidy && go build -o simulator .`. Verify no new import errors.
- [ ] 6.2 Manual smoke test: `sudo ./simulator -flow-collector=127.0.0.1:2055 -flow-protocol=netflow5 -auto-start-ip=10.42.1.1 -auto-count=5`. Confirm startup log reads `protocol: netflow5` and no fatal errors. (Requires sudo + Linux host; deferred to maintainer.)
- [ ] 6.3 Capture packets with `tcpdump -w /tmp/nf5.pcap -i any udp port 2055` for 30 seconds; verify packets are UDP with payload starting `00 05` (version 5) in big-endian. (Requires running simulator; deferred.)
- [ ] 6.4 Verify `curl -s http://localhost:8080/api/v1/flows/status | jq .protocol` returns `"netflow5"`. (Requires running simulator; deferred.)
- [ ] 6.5 (Recommended, not a gate) Run a real collector: `nfcapd -p 2055 -l /tmp/nf5 -T all` in parallel with the simulator; after a minute, `nfdump -r /tmp/nf5/<file>` SHOULD list records with the expected 5-tuples and non-zero packet/byte counts.
- [ ] 6.6 (Recommended, not a gate) Ingest into OpenNMS Telemetryd configured for NetFlow v5 and confirm flows land in the flow index.

## 7. Change closeout

- [x] 7.1 Run `openspec validate add-netflow-v5-export --strict` and resolve any findings.
- [ ] 7.2 Open PR against `labmonkeys-space/l8opensim:main` (per CLAUDE.md PR convention). Title: `feat(flow): add NetFlow v5 export support`. Reference issue #43 in the body. (Deferred per instructions — user will commit/PR.)
- [ ] 7.3 After merge, archive the change via `openspec archive add-netflow-v5-export`.

## 8. PR #46 review follow-ups

- [x] 8.1 Fix AS_TRANS value — replace `0xFFFF` with `23456` (`0x5BA0`, RFC 6793 §2) in `go/simulator/netflow5.go` (add a named `netFlow5ASTrans` constant), the ASN-clamping scenario in `specs/flow-export/spec.md`, and decision D6 in `design.md`. Update `TestNetFlow5ASNClamp` to assert `23456`.
- [x] 8.2 Fix `flow_sequence` semantics for v5 — add `SeqIncrement(packetRecordCount int) int` to the `FlowEncoder` interface (returning `1` for NF9/IPFIX and `packetRecordCount` for NF5). Change `FlowExporter.Tick` to call `encoder.SeqIncrement(len(batch))` instead of `fe.seqNo++`. Document as decision D9.
- [x] 8.3 Add `TestNetFlow5Tick_FlowSequenceCumulative` in `go/simulator/netflow5_test.go` — drives the real `Tick → EncodePacket` path with 80 pre-expired records and asserts each emitted packet's `flow_sequence` equals the cumulative record count of preceding packets. Add `TestNetFlow5SeqIncrement` locking in the per-encoder return values.
- [x] 8.4 Document fleet-wide warn-once semantics — update the `NetFlow5Encoder` struct doc, the `EncodePacket` / `clampASN` doc comments, and both warning log messages to state explicitly that the one-shot is per simulator lifetime (not per device). Drop `domainIDtoIP(domainID)` from the logs. Update D4 in `design.md` and the IPv4-filtering scenarios in `spec.md`. Update `TestNetFlow5IPv4OnlyFiltering` to assert the warning does NOT include a device identity.
- [x] 8.5 Add a struct-level comment on `NetFlow5Encoder` explaining the pointer-receiver deviation from NF9/IPFIX (atomic.Bool fields require it; value literal would silently break atomic state).
- [x] 8.6 Add `TestNetFlow5GoldenBytes` — construct one fully-specified `FlowRecord`, encode it, and assert `bytes.Equal(got, expected)` against a hand-constructed 72-byte payload (timestamp fields masked). Independent oracle that catches bugs a mirrored decoder would round-trip.
