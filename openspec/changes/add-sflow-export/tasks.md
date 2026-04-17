## 1. Pre-flight checks

- [x] 1.1 Confirm issue #43 (NetFlow v5) merge status. This change and #43 both modify the protocol switch in `go/simulator/flow_exporter.go` around `InitFlowExport` (line ~271); they will be applied sequentially with **NetFlow v5 first**. If #43 is still open, rebase this change on top of it before starting task group 3.
- [x] 1.2 Verify `-flow-protocol` CLI test fixtures cover the error path so the updated error message ("supported: netflow9, ipfix, sflow") has an assertion point.
- [x] 1.3 Confirm with maintainer that the `10 × ConcurrentFlows` synthetic sampling rate (design.md §D3) is acceptable. If not, add `-flow-sflow-sampling-rate` flag work to the task list before starting.
- [x] 1.4 Decide whether Phase 2 (counter samples) ships in the same PR as Phase 1 or as a follow-up. Default: same PR. If split, move task groups 5 and 6 out of this change.

## 2. FlowEncoder interface extension

- [x] 2.1 Add `MaxRecordSize() int` to the `FlowEncoder` interface in `go/simulator/flow_exporter.go`. Document that a return value of 0 preserves fixed-size pagination; non-zero opts into variable-length pagination.
- [x] 2.2 Implement `MaxRecordSize() int { return 0 }` on `NetFlow9Encoder` (`go/simulator/netflow9.go`) and `IPFIXEncoder` (`go/simulator/ipfix.go`). No other changes to these encoders.
- [x] 2.3 Update `FlowExporter.Tick` pagination loop to branch on `encoder.MaxRecordSize()`: when 0, keep existing fixed-record-size math; when non-zero, paginate by worst-case record size with the new encoder's per-record estimator.
- [x] 2.4 Add a regression test asserting NetFlow9 and IPFIX datagram bytes are identical before and after the interface change for a canonical set of `FlowRecord` inputs. This is the non-regression gate for task 2.3.

## 3. sFlow v5 encoder — Phase 1 (flow samples)

- [x] 3.1 Create `go/simulator/sflow.go` with an `SFlowEncoder` struct implementing the extended `FlowEncoder` interface. Include inline XDR primitives: `putUint32`, `putOpaque` (with 4-byte padding), `putIP4`. No new `go.mod` dependencies.
- [x] 3.2 Implement sFlow v5 datagram header encoding: version=5, agent_address_type=1, agent_address=deviceIP, sub_agent_id=0, sequence_number, uptime.
- [x] 3.3 Implement `FLOW_SAMPLE` sample record encoding (prefer `flow_sample_expanded` for cleaner 5-tuple handling; document choice in code comments).
- [x] 3.4 Implement `sampled_header` flow-record content: synthesize a minimal IPv4+UDP (or IPv4+TCP) header from `FlowRecord.SrcAddr`, `DstAddr`, `Protocol`, `SrcPort`, `DstPort`, with byte and packet counters from the record.
- [x] 3.5 Wire synthetic sampling rate = `10 * FlowProfile.ConcurrentFlows` per design.md §D3. Expose as a named constant for traceability.
- [x] 3.6 Register `"sflow"` and `"sflow5"` in the `InitFlowExport` switch in `go/simulator/flow_exporter.go`. Update the default-case error message to list all three protocols.
- [x] 3.7 Update the `-flow-protocol` CLI flag help text in `go/simulator/simulator.go` to include `sflow` as a supported value.
- [x] 3.8 Confirm `GET /api/v1/flows/status` canonicalizes both `sflow` and `sflow5` to `"sflow"` in its JSON response. Add an API-level test if coverage is missing.

## 4. sFlow Phase 1 tests

- [x] 4.1 Create `go/simulator/sflow_test.go` with an in-process XDR decoder oracle. The oracle decodes datagram header fields, iterates sample records, and exposes the `sampled_header` payload as a parseable IPv4 packet.
- [x] 4.2 Round-trip test: encode N synthetic `FlowRecord` entries, decode them, assert agent_address / sequence_number / uptime / sample-record 5-tuple match input byte-for-byte.
- [x] 4.3 MTU test: emit a batch sized to cross the 1500-byte boundary, assert `FlowExporter.Tick` emits multiple datagrams and no single datagram exceeds the buffer capacity.
- [x] 4.4 Sequence number test: two consecutive ticks from the same device produce datagrams with sequence numbers differing by exactly 1.
- [x] 4.5 Parallel structure check: `netflow9_test.go`, `ipfix_test.go`, and `sflow_test.go` share the same test-naming conventions and helper patterns where sensible.

## 5. Counter source abstraction — Phase 2

- [x] 5.1 Define `CounterSource` and `CounterRecord` types in a new file (suggested: `go/simulator/counter_source.go`). Keep the interface minimal — `Snapshot(t time.Time) []CounterRecord`.
- [x] 5.2 Extract interface counter state from `IfCounterCycler` in `go/simulator/if_counters.go` into an `InterfaceCounterSource` adapter implementing `CounterSource`. `IfCounterCycler` keeps its public SNMP surface; internally it delegates math to the new source.
- [x] 5.3 Regression test: SNMP responses for `ifHCInOctets`, `ifHCOutOctets`, `ifInUcastPkts`, `ifOutUcastPkts`, `ifInErrors`, `ifInDiscards`, broadcast/multicast counters are byte-identical against a fixed device state before and after the refactor.
- [x] 5.4 Add `CPUCounterSource` — per-device CPU utilization. Idle/user/system/wait percentages × 100 (the sFlow convention). Driven from a new simple cycler or tied to `MetricsCycler`.
- [x] 5.5 Add `MemoryCounterSource` — per-device memory totals and usage (total, used, free, cached in bytes).
- [x] 5.6 Wire `InterfaceCounterSource`, `CPUCounterSource`, and `MemoryCounterSource` registration into the device lifecycle in `go/simulator/device.go`.

## 6. sFlow v5 Phase 2 — counter samples

- [x] 6.1 Extend `SFlowEncoder` to emit `COUNTERS_SAMPLE` records (prefer `counters_sample_expanded`). Encode `if_counters`, `processor_information`, and `memory_information` counter-record types.
- [x] 6.2 Update `FlowExporter.Tick` (sFlow code path) so each tick collects `Snapshot` from all registered `CounterSource`s for the device and includes them as counter samples alongside flow samples in the emitted datagram batch.
- [x] 6.3 Add counter-sample test cases to `sflow_test.go`: round-trip assertion that decoded `if_counters` equals `InterfaceCounterSource.Snapshot` output at the same time, and similarly for CPU and memory samples.
- [x] 6.4 Verify counter sample emission respects the 1500-byte MTU: devices with many interfaces SHALL split counter records across multiple datagrams rather than produce an oversize packet.

## 7. Per-device source IP and agent identity

- [x] 7.1 Confirm that the existing per-device socket path in `openFlowConnForDevice` (in `go/simulator/flow_exporter.go`) is reused unchanged by sFlow; the sFlow encoder only needs the device IP it already knows from `fe.domainID`.
- [x] 7.2 Update the per-device bind-failure warning in `openFlowConnForDevice` to mention that in sFlow mode the collector may observe a mismatch between `agent_address` and the UDP packet source IP.
- [ ] 7.3 Add an integration-style test that asserts, in per-device mode, the UDP source IP on the wire equals the sFlow `agent_address` inside the datagram. **(Deferred: requires running as root with the opensim network namespace and TUN interfaces; not reachable from `go test` in CI. Addressed in the pre-commit manual smoke tests — task 9.3 / 9.4.)**

## 8. Documentation

- [x] 8.1 Update `CLAUDE.md` flow-export section: add `sflow` (alias `sflow5`) to the `-flow-protocol` flag list. Cite sflow_version_5.txt as the reference spec.
- [x] 8.2 Update `README.md`: list `sflow` as a supported protocol. Add an explicit sentence that sFlow output is synthesized from `FlowCache` and does not reflect real packet capture or link utilization.
- [x] 8.3 Note the synthetic sampling rate (`10 × ConcurrentFlows`) in the same README paragraph so operators know what they're looking at.

## 9. Validation

- [x] 9.1 Run `cd go && go mod tidy && go build ./...` and verify no new `go.mod` entries beyond what existed before the change.
- [x] 9.2 Run `cd go && go test ./...`; assert all NetFlow9, IPFIX, and new sFlow tests pass.
- [ ] 9.3 Manual smoke test against the OpenNMS Telemetryd sFlow adapter: confirm flows are ingested without decode errors in the Telemetryd log. **(Deferred to maintainer post-merge — requires a running OpenNMS Telemetryd instance.)**
- [ ] 9.4 Manual smoke test against a second external collector (e.g. Inmon sFlowTrend or `sflowtool`) to confirm the emitted datagrams are protocol-compliant beyond a single collector's parser. **(Deferred to maintainer post-merge — requires external collector tooling.)**
- [x] 9.5 Run `openspec validate add-sflow-export --strict` and fix any findings.

## 10. Post-merge

- [ ] 10.1 Update project memory (`MEMORY.md`) if appropriate — add an entry documenting sFlow support and the synthetic-sampling caveat so future operator questions can be routed to the README note.
- [ ] 10.2 Archive the OpenSpec change per the experimental workflow (`openspec archive add-sflow-export`) once it is merged and deployed.
