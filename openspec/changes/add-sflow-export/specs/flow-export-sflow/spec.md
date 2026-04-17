## ADDED Requirements

### Requirement: sFlow v5 protocol selection

The simulator SHALL accept `sflow` and `sflow5` as values for the `-flow-protocol` CLI flag and canonicalize both to `sflow` internally. The help text for `-flow-protocol` SHALL list `sflow` alongside `netflow9` and `ipfix`.

#### Scenario: CLI accepts sflow as protocol

- **WHEN** the simulator is started with `-flow-protocol sflow -flow-collector <host:port>`
- **THEN** flow export initialization SHALL succeed
- **AND** `GET /api/v1/flows/status` SHALL return `"protocol": "sflow"`

#### Scenario: CLI accepts sflow5 as protocol alias

- **WHEN** the simulator is started with `-flow-protocol sflow5 -flow-collector <host:port>`
- **THEN** flow export initialization SHALL succeed
- **AND** `GET /api/v1/flows/status` SHALL return `"protocol": "sflow"` (canonicalized form)

#### Scenario: Unknown protocol is rejected with updated error message

- **WHEN** the simulator is started with `-flow-protocol foo`
- **THEN** initialization SHALL fail with an error listing the supported protocols
- **AND** the error message SHALL include `netflow9`, `ipfix`, and `sflow`

### Requirement: sFlow v5 datagram structure

When the selected protocol is `sflow`, the simulator SHALL emit UDP datagrams conforming to the sFlow v5 datagram format defined in sflow_version_5.txt. Each datagram SHALL contain a version field of 5, an `agent_address_type` of 1 (IPv4), an `agent_address` equal to the emitting device's IPv4, a `sub_agent_id` of 0, a monotonically increasing `sequence_number` per (agent, sub-agent) pair, an `uptime` in milliseconds since device start, and at least one sample record.

#### Scenario: Datagram version field equals 5

- **WHEN** an sFlow datagram is emitted
- **THEN** the first 4 bytes SHALL encode the XDR uint32 value 5

#### Scenario: Agent address matches emitting device

- **WHEN** device `D` with IPv4 `A.B.C.D` emits an sFlow datagram
- **THEN** the datagram's `agent_address_type` SHALL be 1
- **AND** the datagram's `agent_address` SHALL equal `A.B.C.D` in network byte order

#### Scenario: Sequence number increments per device

- **WHEN** device `D` emits two consecutive sFlow datagrams
- **THEN** the second datagram's `sequence_number` SHALL be greater than the first's by exactly 1

#### Scenario: Sub-agent id is zero

- **WHEN** any sFlow datagram is emitted
- **THEN** its `sub_agent_id` field SHALL be 0

### Requirement: sFlow v5 flow samples (Phase 1)

When the selected protocol is `sflow`, the simulator SHALL convert each synthetic flow produced by `FlowCache.GenerateFlows` into an sFlow `FLOW_SAMPLE` (sample_type tag `flow_sample` or `flow_sample_expanded`) containing a `sampled_header` flow-record with a synthesized IPv4+UDP header derived from the 5-tuple, byte counters, and packet counters of the originating `FlowRecord`.

#### Scenario: FLOW_SAMPLE tag is present

- **WHEN** an sFlow datagram carrying flow data is decoded
- **THEN** at least one sample record SHALL have the `flow_sample` or `flow_sample_expanded` tag

#### Scenario: Sampled header carries the 5-tuple

- **WHEN** a `FLOW_SAMPLE` is decoded and its `sampled_header` payload is parsed as an IPv4 packet
- **THEN** the IPv4 source and destination addresses SHALL equal the originating `FlowRecord.SrcAddr` and `DstAddr`
- **AND** for UDP/TCP protocols the transport-layer source and destination ports SHALL equal `FlowRecord.SrcPort` and `DstPort`
- **AND** the IPv4 protocol field SHALL equal `FlowRecord.Protocol`

#### Scenario: Synthetic sampling rate is consistent and documented

- **WHEN** any `FLOW_SAMPLE` is emitted
- **THEN** its `sampling_rate` field SHALL equal `10 * FlowProfile.ConcurrentFlows` for the emitting device's profile
- **AND** the simulator's documentation (README and `CLAUDE.md`) SHALL describe this rate as synthetic and not reflective of real traffic volume

### Requirement: sFlow v5 counter samples (Phase 2)

When the selected protocol is `sflow` and Phase 2 counter-sample support is enabled, the simulator SHALL emit `COUNTERS_SAMPLE` (sample_type tag `counters_sample` or `counters_sample_expanded`) records alongside flow samples on each flow-export tick. Each counter sample SHALL include at minimum: one `if_counters` record per simulated interface, one `processor_information` record per device carrying CPU utilization, and one `memory_information` record per device carrying memory totals and usage.

#### Scenario: Counter sample emitted per tick per device

- **WHEN** the flow-export ticker fires with protocol `sflow` and Phase 2 enabled
- **THEN** each active device SHALL emit at least one `COUNTERS_SAMPLE` record in that tick's datagram batch

#### Scenario: Interface counters included

- **WHEN** a `COUNTERS_SAMPLE` is decoded for device `D`
- **THEN** it SHALL contain one `if_counters` record for each simulated interface on `D`
- **AND** each `if_counters` record SHALL carry non-zero `ifInOctets`, `ifOutOctets`, `ifInUcastPkts`, and `ifOutUcastPkts` fields derived from `IfCounterCycler` output

#### Scenario: CPU and memory counters included

- **WHEN** a `COUNTERS_SAMPLE` is decoded for device `D`
- **THEN** it SHALL contain exactly one `processor_information` record carrying at minimum `cpu_percentage` and `total_memory`
- **AND** it SHALL contain exactly one `memory_information` record carrying `total` and `free` memory byte counts

### Requirement: CounterSource abstraction

Phase 2 counter sample emission SHALL be driven by a `CounterSource` interface rather than by direct coupling between `sflow.go` and `if_counters.go`. Existing `IfCounterCycler` state SHALL be exposed through an `InterfaceCounterSource` adapter implementing this interface without changing observable SNMP behaviour.

#### Scenario: InterfaceCounterSource reuses IfCounterCycler state

- **WHEN** a device's `InterfaceCounterSource.Snapshot` is called at time `t`
- **THEN** the returned `CounterRecord` values for interface counters SHALL equal the values that `IfCounterCycler` would produce at time `t` for the same device
- **AND** the existing SNMP exposure of the same counters via `snmp_handlers.go` SHALL remain byte-identical before and after this refactor

#### Scenario: CPU and memory counter sources are per-device

- **WHEN** the simulator initializes a device in sFlow mode with Phase 2 enabled
- **THEN** the device SHALL have exactly one `CPUCounterSource` and exactly one `MemoryCounterSource` registered

### Requirement: FlowEncoder interface tolerates variable-length records

The `FlowEncoder` interface SHALL be extended with a method that allows protocol-specific encoders to opt into variable-length record pagination. The extension SHALL NOT change the observable output of `NetFlow9Encoder` or `IPFIXEncoder` — both continue to paginate by the fixed `recordSize` returned from `PacketSizes()`.

#### Scenario: NetFlow9Encoder output is unchanged

- **WHEN** a `FlowExporter` ticks with encoder = `NetFlow9Encoder` before and after the interface extension
- **THEN** the emitted datagrams SHALL be byte-identical for identical input records, input buffer, and timing

#### Scenario: IPFIXEncoder output is unchanged

- **WHEN** a `FlowExporter` ticks with encoder = `IPFIXEncoder` before and after the interface extension
- **THEN** the emitted datagrams SHALL be byte-identical for identical input records, input buffer, and timing

#### Scenario: sFlow encoder signals variable-length pagination

- **WHEN** the sFlow encoder is registered as the active encoder
- **THEN** its variable-length opt-in (the new interface method) SHALL return a non-zero value indicating its maximum per-record byte size
- **AND** `FlowExporter.Tick` SHALL paginate so that no emitted datagram exceeds the buffer capacity of 1500 bytes

### Requirement: Per-device source IP binding compatibility

When `-flow-source-per-device` is enabled (the default) and the selected protocol is `sflow`, each sFlow datagram SHALL be sent from the per-device UDP socket bound to the device's IPv4. When per-device binding fails or is disabled, the shared simulator socket SHALL be used and a warning SHALL be logged noting that the sFlow `agent_address` may not match the UDP source IP observed by the collector.

#### Scenario: Per-device socket carries agent address

- **WHEN** `-flow-source-per-device` is true and device `D`'s per-device socket is successfully bound
- **THEN** sFlow datagrams for `D` SHALL be sent from that socket
- **AND** the packet's IPv4 source address as observed on the wire SHALL equal the sFlow datagram's `agent_address`

#### Scenario: Warning on per-device bind failure in sFlow mode

- **WHEN** per-device bind fails for device `D` and the selected protocol is `sflow`
- **THEN** the simulator SHALL log a warning that includes both the device IP and a notice that `agent_address` may not match the UDP source IP
- **AND** the exporter SHALL fall back to the shared simulator socket

### Requirement: XDR encoding without new external dependency

sFlow v5 datagrams SHALL be encoded using XDR primitives implemented within `go/simulator/sflow.go`. No new Go module dependency SHALL be added to `go.mod` solely to support XDR encoding.

#### Scenario: No new module added for XDR

- **WHEN** `go.mod` is inspected after this change is applied
- **THEN** it SHALL NOT contain `github.com/davecgh/go-xdr` or any substitute XDR library added for this change

### Requirement: Unit test coverage with XDR decoder oracle

The `go/simulator` package SHALL include unit tests for the sFlow encoder that round-trip emitted datagrams through an in-process XDR decoder and assert field-by-field equality with the input records. The coverage SHALL parallel the structure of the existing `netflow9_test.go` and `ipfix_test.go`.

#### Scenario: Emitted datagram round-trips through decoder

- **WHEN** an `sflow_test.go` test case encodes a set of `FlowRecord` inputs and decodes the resulting bytes
- **THEN** the decoded `agent_address`, `sequence_number`, `uptime`, and each sample record's tuple fields SHALL match the corresponding input values exactly

#### Scenario: Tests run under `go test ./...`

- **WHEN** `cd go && go test ./...` is run
- **THEN** the sFlow tests SHALL execute and pass
- **AND** existing NetFlow9 and IPFIX tests SHALL continue to pass

### Requirement: Documentation labels sFlow samples as synthetic

The project's top-level `README.md` and `CLAUDE.md` SHALL describe the sFlow output as synthetic and explicitly state that the simulator does not forward or sample real packets, so extrapolated link-volume metrics at the collector will not reflect real-world traffic.

#### Scenario: README discloses synthetic sampling

- **WHEN** `README.md` is read
- **THEN** it SHALL list `sflow` (or `sflow5`) as a supported flow-export protocol
- **AND** it SHALL include a sentence clarifying that sFlow samples are synthesized from `FlowCache` output and do not represent real packet capture

#### Scenario: CLAUDE.md flow-export reference updated

- **WHEN** `CLAUDE.md` is read
- **THEN** its flow-export section SHALL include `sflow` in the protocol list for `-flow-protocol`
- **AND** the section SHALL link to or cite sflow_version_5.txt as the reference specification
