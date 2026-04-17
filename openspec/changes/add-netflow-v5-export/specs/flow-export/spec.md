## ADDED Requirements

### Requirement: Pluggable flow encoder interface

The simulator SHALL expose a protocol-agnostic `FlowEncoder` interface that isolates wire-format concerns from the shared export runtime (UDP socket, ticker goroutine, per-device `FlowCache`). Adding a new flow-export protocol SHALL require only a new `FlowEncoder` implementation and a new case in the protocol-selection switch; it SHALL NOT require changes to `FlowExporter`, `FlowCache`, device lifecycle, or the web API.

#### Scenario: Encoder interface shape is stable

- **WHEN** `go/simulator/flow_exporter.go` is inspected
- **THEN** `FlowEncoder` SHALL declare `EncodePacket(domainID uint32, seqNo uint32, uptimeMs uint32, records []FlowRecord, includeTemplate bool, buf []byte) (int, error)`
- **AND** `FlowEncoder` SHALL declare `PacketSizes() (baseOverhead int, templateSize int, recordSize int)`
- **AND** every supported protocol SHALL satisfy `FlowEncoder` without additional methods

#### Scenario: Adding a protocol is localized

- **WHEN** a new protocol is added
- **THEN** the only production-code touchpoints SHALL be a new `*.go` file under `go/simulator/` implementing `FlowEncoder`, a new `case` clause in `InitFlowExport`'s protocol switch, and CLI help text
- **AND** no changes SHALL be required to `FlowExporter`, `FlowCache`, `DeviceSimulator`, or any HTTP handler

### Requirement: Flow-export protocol selection

The simulator SHALL select the wire protocol based on the `-flow-protocol` CLI flag. Supported values SHALL be `netflow9` (default, alias `nf9`, alias empty string), `ipfix` (alias `ipfix10`), and `netflow5` (alias `nf5`). Matching SHALL be case-insensitive. Any other value SHALL cause `InitFlowExport` to return an error naming all supported values.

#### Scenario: Default protocol is NetFlow v9

- **WHEN** the simulator is started with `-flow-collector` set but without `-flow-protocol`
- **THEN** the active encoder SHALL be `NetFlow9Encoder`
- **AND** the canonical protocol recorded on `SimulatorManager` SHALL be `netflow9`

#### Scenario: IPFIX is selectable

- **WHEN** the simulator is started with `-flow-protocol=ipfix` or `-flow-protocol=ipfix10`
- **THEN** the active encoder SHALL be `IPFIXEncoder`
- **AND** the canonical protocol recorded on `SimulatorManager` SHALL be `ipfix`

#### Scenario: NetFlow v5 is selectable

- **WHEN** the simulator is started with `-flow-protocol=netflow5` or `-flow-protocol=nf5` (case-insensitive)
- **THEN** the active encoder SHALL be `NetFlow5Encoder`
- **AND** the canonical protocol recorded on `SimulatorManager` SHALL be `netflow5`

#### Scenario: Unknown protocol is rejected

- **WHEN** the simulator is started with `-flow-protocol=sflow`
- **THEN** `InitFlowExport` SHALL return an error
- **AND** the error message SHALL list `netflow9`, `ipfix`, and `netflow5` as supported values
- **AND** no UDP socket SHALL remain open

### Requirement: NetFlow v9 encoder wire-format parameters

`NetFlow9Encoder` SHALL implement NetFlow v9 (RFC 3954) with a 20-byte message header, an 80-byte template flowset, and 45-byte data records. Templates SHALL be re-sent every `-flow-template-interval` cycles. Timestamps SHALL be SysUptime-relative (milliseconds since device start).

#### Scenario: PacketSizes reports v9 constants

- **WHEN** `NetFlow9Encoder{}.PacketSizes()` is called
- **THEN** it SHALL return `baseOverhead = 20`, `templateSize = 80`, `recordSize = 45`

#### Scenario: Template is emitted on the configured interval

- **WHEN** the template interval has elapsed since the last template emission for a given device
- **THEN** the next `EncodePacket` call for that device with `includeTemplate = true` SHALL prepend the template flowset to the datagram

### Requirement: IPFIX encoder wire-format parameters

`IPFIXEncoder` SHALL implement IPFIX (RFC 7011) with a 16-byte message header, an 80-byte template set, and 53-byte data records. Timestamps SHALL be absolute epoch milliseconds rather than SysUptime-relative.

#### Scenario: PacketSizes reports IPFIX constants

- **WHEN** `IPFIXEncoder{}.PacketSizes()` is called
- **THEN** it SHALL return `baseOverhead = 20`, `templateSize = 80`, `recordSize = 53`

#### Scenario: IPFIX uses absolute timestamps

- **WHEN** an IPFIX data record is encoded for a flow that started at device-uptime 10s and ended at device-uptime 15s
- **THEN** the record's `flowStartMilliseconds` and `flowEndMilliseconds` fields SHALL contain absolute Unix epoch milliseconds
- **AND** they SHALL NOT contain SysUptime-relative offsets

### Requirement: NetFlow v5 encoder wire-format parameters

`NetFlow5Encoder` SHALL implement Cisco NetFlow v5 with a 24-byte header and a 48-byte record. `PacketSizes()` SHALL return `(baseOverhead = 24, templateSize = 0, recordSize = 48)`. Timestamps SHALL be SysUptime-relative (milliseconds since device start), carried in the record's `first`/`last` fields, with the wall-clock reference carried in the header's `unix_secs`/`unix_nsecs` fields.

#### Scenario: PacketSizes reports v5 constants

- **WHEN** `NetFlow5Encoder{}.PacketSizes()` is called
- **THEN** it SHALL return `baseOverhead = 24`, `templateSize = 0`, `recordSize = 48`

#### Scenario: Header carries version 5 and expected fields

- **WHEN** `NetFlow5Encoder.EncodePacket(domainID, seqNo, uptimeMs, records, _, buf)` is called with at least one IPv4 record
- **THEN** bytes 0..1 of the output SHALL be big-endian `0x0005`
- **AND** bytes 2..3 SHALL be the count of records written (at most 30)
- **AND** bytes 4..7 SHALL be `uptimeMs`
- **AND** bytes 8..11 SHALL be the current Unix seconds
- **AND** bytes 12..15 SHALL be the current Unix nanoseconds-within-second
- **AND** bytes 16..19 SHALL be `seqNo`
- **AND** byte 20 SHALL be the engine type and byte 21 the engine id
- **AND** bytes 22..23 SHALL be `0x0000` for `sampling_interval`

#### Scenario: Record fields are written in canonical order

- **WHEN** a single `FlowRecord` is encoded
- **THEN** the 48 bytes immediately following the header SHALL contain, in order and big-endian: srcaddr (u32), dstaddr (u32), nexthop (u32), input ifIndex (u16), output ifIndex (u16), dPkts (u32), dOctets (u32), first-SysUptime-ms (u32), last-SysUptime-ms (u32), srcport (u16), dstport (u16), pad1 (u8 = 0), tcp_flags (u8), prot (u8), tos (u8), src_as (u16), dst_as (u16), src_mask (u8), dst_mask (u8), pad2 (u16 = 0)

#### Scenario: EncodePacket ignores includeTemplate under v5

- **WHEN** `NetFlow5Encoder.EncodePacket(...)` is called with `includeTemplate = true`
- **THEN** no template bytes SHALL be prepended to the datagram
- **AND** the header `count` field SHALL equal the number of data records written
- **AND** the returned byte count SHALL equal `24 + (count * 48)`

### Requirement: NetFlow v5 IPv4-only filtering

`NetFlow5Encoder` SHALL skip any `FlowRecord` whose `SrcIP` or `DstIP` is not an IPv4 address. Non-IPv4 `NextHop` SHALL be coerced to `0.0.0.0` rather than skipping the record. The encoder SHALL emit exactly one warning log per encoder lifetime on the first skip, naming the device by `domainID`.

#### Scenario: IPv6-bearing record is skipped

- **WHEN** a `FlowRecord` with `SrcIP = 2001:db8::1` and `DstIP = 10.0.0.1` is submitted to `EncodePacket`
- **THEN** that record SHALL NOT appear in the output datagram
- **AND** the header `count` SHALL reflect only the IPv4-bearing records that were encoded

#### Scenario: Non-IPv4 NextHop is coerced

- **WHEN** a `FlowRecord` has IPv4 `SrcIP` and `DstIP` but an IPv6 `NextHop`
- **THEN** the record SHALL be encoded
- **AND** the on-wire `nexthop` field SHALL be `0.0.0.0`

#### Scenario: First skip emits a one-shot warning

- **WHEN** a `NetFlow5Encoder` skips an IPv6 record for the first time on a given device
- **THEN** exactly one warning log SHALL be emitted identifying the device by its `domainID`
- **AND** subsequent skips on the same encoder SHALL NOT emit additional warnings

### Requirement: NetFlow v5 30-record cap per datagram

`NetFlow5Encoder.EncodePacket` SHALL write at most 30 records per datagram, regardless of input slice length. Records beyond the 30th SHALL NOT be encoded in that call; callers rely on the ticker's existing batching to emit them in subsequent datagrams.

#### Scenario: Input of 31 records produces a 30-record packet

- **WHEN** `EncodePacket` is called with 31 IPv4 records
- **THEN** the output SHALL contain exactly 30 records
- **AND** the header `count` field SHALL be `30`
- **AND** the returned byte count SHALL be `24 + (30 * 48) = 1464`

#### Scenario: Input of 45 records paginates across two ticks

- **WHEN** the ticker batches 45 records for a device over the course of one cycle
- **THEN** the first datagram SHALL carry the first 30 records
- **AND** the second datagram SHALL carry the remaining 15 records
- **AND** both datagrams SHALL share the same flow sequence counter incrementing by the record count of each packet

### Requirement: NetFlow v5 template-interval flag is a no-op

When the active protocol is `netflow5`, the `-flow-template-interval` CLI flag SHALL be accepted without error and SHALL have no effect on wire output. The simulator SHALL NOT emit a warning at startup for this case.

#### Scenario: Template-interval flag is accepted under v5

- **WHEN** the simulator is started with `-flow-protocol=netflow5 -flow-template-interval=30s`
- **THEN** `InitFlowExport` SHALL succeed
- **AND** no warning SHALL be logged about the flag being ignored
- **AND** no template bytes SHALL appear in any emitted datagram

### Requirement: NetFlow v5 ASN clamping

`NetFlow5Encoder` SHALL write ASN values greater than `0xFFFF` as the 16-bit value `0xFFFF` on the wire. It SHALL emit exactly one warning log per encoder lifetime on the first such clamp, identifying the device by `domainID`.

#### Scenario: 32-bit ASN is clamped to AS_TRANS

- **WHEN** a `FlowRecord` with `SrcAS = 0x00100000` is encoded
- **THEN** the on-wire `src_as` field SHALL be `0xFFFF`
- **AND** exactly one warning log SHALL be emitted for the first clamp on that encoder

### Requirement: Flow export status endpoint reports active protocol

`GET /api/v1/flows/status` SHALL report the canonical protocol name currently active on the simulator. Supported values SHALL be `netflow9`, `ipfix`, or `netflow5`. The schema of the response SHALL NOT change when a new protocol is added.

#### Scenario: Status reflects v5 selection

- **WHEN** the simulator is running with `-flow-protocol=netflow5` and `GET /api/v1/flows/status` is called
- **THEN** the JSON response SHALL contain `"protocol": "netflow5"`
- **AND** the response schema (field names and types) SHALL match the schema used for `netflow9` and `ipfix`
