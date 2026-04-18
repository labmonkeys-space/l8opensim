# snmp-trap Specification

## Purpose
TBD - created by archiving change add-snmp-trap-v2c. Update Purpose after archive.
## Requirements
### Requirement: SNMP trap export feature toggle

The simulator SHALL support an opt-in SNMP trap export feature controlled by the `-trap-collector <host:port>` CLI flag. When the flag is absent the feature SHALL be disabled and no trap scheduler, catalog, or per-device trap exporter SHALL be instantiated. When the flag is present the simulator SHALL parse the `host:port` value, resolve the host to an IPv4 address (or fail startup with a clear error), and initialize the trap subsystem before accepting SNMP polls.

#### Scenario: Flag absent disables feature

- **WHEN** the simulator is started without `-trap-collector`
- **THEN** no trap scheduler goroutine SHALL be running
- **AND** `GET /api/v1/traps/status` SHALL return `{"enabled": false}`
- **AND** no per-device trap UDP socket SHALL be opened

#### Scenario: Flag present enables feature

- **WHEN** the simulator is started with `-trap-collector collector.example:162`
- **THEN** the trap scheduler SHALL be running
- **AND** `GET /api/v1/traps/status` SHALL return `{"enabled": true, ...}` with counter fields
- **AND** per-device trap sockets SHALL be opened subject to `-trap-source-per-device`

#### Scenario: Unresolvable collector host fails startup

- **WHEN** `-trap-collector host-does-not-resolve.invalid:162` is passed
- **THEN** simulator startup SHALL fail with a non-zero exit code
- **AND** the error message SHALL name the unresolved host

### Requirement: SNMPv2c TRAP PDU wire format

When trap export is enabled and `-trap-mode trap` is selected, the simulator SHALL emit SNMPv2c messages whose outer envelope is a SEQUENCE containing: `version` INTEGER = 1 (v2c), `community` OCTET STRING, and an SNMPv2-Trap-PDU. The SNMPv2-Trap-PDU SHALL use ASN.1 tag `0xA7` and contain: `request-id` INTEGER (non-zero, unique per device), `error-status` INTEGER = 0, `error-index` INTEGER = 0, and a `variable-bindings` SEQUENCE whose first element is `sysUpTime.0` (OID `1.3.6.1.2.1.1.3.0`, TimeTicks value in 1/100s since device start), whose second element is `snmpTrapOID.0` (OID `1.3.6.1.6.3.1.1.4.1.0`, OID value = the catalog entry's `snmpTrapOID`), and whose remaining elements are the catalog entry's body varbinds with templates resolved.

#### Scenario: Version field equals 1

- **WHEN** a TRAP datagram is emitted and its outer SEQUENCE is decoded
- **THEN** the `version` INTEGER SHALL equal 1

#### Scenario: PDU tag equals 0xA7

- **WHEN** a TRAP datagram is emitted
- **THEN** the inner PDU SHALL have ASN.1 tag byte `0xA7`

#### Scenario: sysUpTime.0 is first varbind

- **WHEN** a TRAP datagram is emitted and its variable-bindings SEQUENCE is decoded
- **THEN** the first varbind's OID SHALL equal `1.3.6.1.2.1.1.3.0`
- **AND** its value type SHALL be TimeTicks (ASN.1 tag `0x43`)
- **AND** its value SHALL equal the device's uptime in 1/100 second units at the moment of fire

#### Scenario: snmpTrapOID.0 is second varbind

- **WHEN** a TRAP datagram is emitted for a catalog entry `E`
- **THEN** the second varbind's OID SHALL equal `1.3.6.1.6.3.1.1.4.1.0`
- **AND** its value type SHALL be OID (ASN.1 tag `0x06`)
- **AND** its value SHALL equal `E.snmpTrapOID`

#### Scenario: Body varbinds follow required two

- **WHEN** a TRAP for catalog entry `E` with N body varbinds is emitted
- **THEN** the variable-bindings SEQUENCE SHALL contain exactly `2 + N` varbinds
- **AND** varbinds 3..N+2 SHALL match `E.varbinds` with templates resolved to concrete values

### Requirement: SNMPv2c INFORM PDU wire format

When trap export is enabled and `-trap-mode inform` is selected, the simulator SHALL emit InformRequest-PDU messages (ASN.1 tag `0xA6`) with the same outer envelope and varbind structure as TRAP PDUs (version, community, required `sysUpTime.0` and `snmpTrapOID.0` varbinds first, followed by body varbinds). Each INFORM SHALL use a request-id unique within the emitting device's pending-inform window, non-zero, and drawn from a 32-bit space.

#### Scenario: PDU tag equals 0xA6

- **WHEN** an INFORM datagram is emitted
- **THEN** the inner PDU SHALL have ASN.1 tag byte `0xA6`

#### Scenario: Request-id is non-zero

- **WHEN** an INFORM datagram is emitted
- **THEN** the `request-id` INTEGER SHALL be > 0

#### Scenario: Request-id is unique per device pending window

- **WHEN** device `D` has N pending unacked INFORMs
- **AND** a new INFORM is emitted for `D`
- **THEN** the new INFORM's `request-id` SHALL NOT equal any of the N pending request-ids

### Requirement: INFORM requires per-device source binding

When `-trap-mode inform` is selected and `-trap-source-per-device=false` is also set (explicitly by operator), the simulator SHALL fail startup with an error message stating that INFORM mode requires per-device source binding. The default value of `-trap-source-per-device` is `true`, so operators passing `-trap-mode inform` without explicitly disabling per-device binding SHALL see the feature work as expected.

#### Scenario: Explicit conflict fails startup

- **WHEN** the simulator is started with `-trap-mode inform -trap-source-per-device=false`
- **THEN** startup SHALL fail with a non-zero exit code
- **AND** the error message SHALL mention that INFORM requires per-device source binding

#### Scenario: Default configuration accepts INFORM

- **WHEN** the simulator is started with `-trap-collector host:162 -trap-mode inform` (without `-trap-source-per-device`)
- **THEN** startup SHALL succeed
- **AND** `-trap-source-per-device` SHALL default to `true`

### Requirement: INFORM acknowledgement handling

For each emitted INFORM, the simulator SHALL retain a pending-inform record keyed by `request-id`, SHALL await a matching GetResponse-PDU (ASN.1 tag `0xA2`) from the collector on the per-device UDP socket, SHALL mark the record as acknowledged and remove it when the response arrives with matching `request-id` and `error-status = 0`, SHALL retransmit the INFORM up to `-trap-inform-retries` times (default 2) at intervals of `-trap-inform-timeout` (default 5s) if no matching response arrives, and SHALL count retransmissions against the global rate limiter.

#### Scenario: Matching ack marks inform acknowledged

- **WHEN** device `D` emits INFORM with request-id `R`
- **AND** a GetResponse-PDU with request-id `R` and error-status 0 arrives on `D`'s UDP socket
- **THEN** the pending-inform record for `R` SHALL be removed from `D`'s pending map
- **AND** the `informsAcked` counter exposed by `GET /api/v1/traps/status` SHALL increment by 1

#### Scenario: Timeout triggers retransmission

- **WHEN** an INFORM is emitted for device `D` with request-id `R`
- **AND** no response is received within `-trap-inform-timeout`
- **THEN** the simulator SHALL retransmit the INFORM with the same `R`
- **AND** SHALL consume one token from the global rate limiter for the retransmission

#### Scenario: Retry exhaustion marks inform failed

- **WHEN** an INFORM is emitted and no response arrives after `1 + trap-inform-retries` transmissions
- **THEN** the pending-inform record SHALL be removed
- **AND** the `informsFailed` counter SHALL increment by 1

#### Scenario: Pending map bounded at 100 per device

- **WHEN** device `D` has 100 pending-inform records
- **AND** a new INFORM is emitted for `D`
- **THEN** the oldest pending-inform record SHALL be dropped
- **AND** the `informsDropped` counter SHALL increment by 1

### Requirement: Per-device source IP binding

When `-trap-source-per-device` is true (the default), the simulator SHALL open a UDP socket per device, bound to that device's IPv4 address inside the `opensim` network namespace, and SHALL use that socket as both the transmit and receive socket for the device's trap traffic. When `-trap-source-per-device` is false (TRAP mode only), the simulator SHALL use a single shared UDP socket for all devices, and the `agent` source-IP-to-device mapping at the collector SHALL NOT be relied upon.

#### Scenario: Per-device socket carries device source IP

- **WHEN** `-trap-source-per-device=true` and device `D` with IPv4 `A.B.C.D` emits a trap
- **THEN** the UDP datagram's source IP observed on the wire SHALL equal `A.B.C.D`

#### Scenario: Per-device bind failure falls back with warning in TRAP mode

- **WHEN** `-trap-source-per-device=true`, `-trap-mode trap`, and per-device bind fails for device `D`
- **THEN** the simulator SHALL log a warning naming `D`'s IP and the reason
- **AND** the trap SHALL still be emitted via the shared fallback socket
- **AND** the collector's observed source IP MAY differ from `D`'s IP

#### Scenario: Per-device bind failure is fatal in INFORM mode

- **WHEN** `-trap-mode inform` and per-device bind fails for any device during initialization
- **THEN** startup SHALL fail with a non-zero exit code
- **AND** the error message SHALL name the failing device IP

### Requirement: Trap catalog structure and loading

The simulator SHALL ship with an embedded universal trap catalog containing exactly the following five catalog entries: `coldStart` (OID `1.3.6.1.6.3.1.1.5.1`, no body varbinds), `warmStart` (OID `1.3.6.1.6.3.1.1.5.2`, no body varbinds), `linkDown` (OID `1.3.6.1.6.3.1.1.5.3`, body varbinds for `ifIndex`/`ifAdminStatus`/`ifOperStatus` parameterized by `{{.IfIndex}}`), `linkUp` (OID `1.3.6.1.6.3.1.1.5.4`, analogous to `linkDown`), and `authenticationFailure` (OID `1.3.6.1.6.3.1.1.5.5`, no body varbinds). Operators SHALL override the entire catalog (not merge) by passing `-trap-catalog <path>` pointing to a JSON file with the same schema.

#### Scenario: Embedded catalog loaded when no flag passed

- **WHEN** the simulator is started with `-trap-collector host:162` and no `-trap-catalog`
- **THEN** the catalog SHALL contain exactly the five universal entries
- **AND** no filesystem read of `_common/traps.json` SHALL be attempted

#### Scenario: File override replaces embedded catalog

- **WHEN** the simulator is started with `-trap-catalog /path/to/custom.json`
- **AND** the file contains valid JSON matching the schema with three entries named `enterpriseAlarmA`, `enterpriseAlarmB`, `enterpriseAlarmC`
- **THEN** the loaded catalog SHALL contain exactly those three entries
- **AND** the five universal entries SHALL NOT be present

#### Scenario: Invalid catalog JSON aborts startup

- **WHEN** `-trap-catalog /path/to/broken.json` is passed and the file is not valid JSON or violates the schema
- **THEN** startup SHALL fail with a non-zero exit code
- **AND** the error message SHALL include the file path and the offending schema violation

#### Scenario: Catalog entry specifying sysUpTime.0 or snmpTrapOID.0 body varbind is rejected

- **WHEN** a catalog JSON entry's `varbinds` array contains an entry whose OID is `1.3.6.1.2.1.1.3.0` or `1.3.6.1.6.3.1.1.4.1.0`
- **THEN** catalog loading SHALL fail with an error naming the catalog entry
- **AND** the error SHALL state that these two varbinds are automatically prepended by the encoder

### Requirement: Varbind templating

Catalog varbind OID and value strings SHALL support the Go `text/template` vocabulary restricted to the following four field accesses: `{{.IfIndex}}` (random integer drawn per-fire from the device's simulated interface-index set), `{{.Uptime}}` (device uptime in 1/100s ticks), `{{.Now}}` (Unix epoch seconds at fire time), `{{.DeviceIP}}` (device IPv4 as dotted-quad string). Templates SHALL be parsed once per catalog entry at load time; evaluation SHALL happen per fire. Use of any field outside this set SHALL fail catalog loading.

#### Scenario: IfIndex template resolves per fire

- **WHEN** a catalog entry with varbind OID `1.3.6.1.2.1.2.2.1.7.{{.IfIndex}}` is fired for device `D` whose interface indices are {1, 2, 3}
- **THEN** the emitted varbind's OID SHALL be one of `1.3.6.1.2.1.2.2.1.7.1`, `1.3.6.1.2.1.2.2.1.7.2`, or `1.3.6.1.2.1.2.2.1.7.3`

#### Scenario: Unknown template field rejected

- **WHEN** a catalog JSON entry contains `{{.NotAField}}` in any OID or value string
- **THEN** catalog loading SHALL fail with an error naming the catalog entry and the offending field

#### Scenario: Template evaluation is not N² at scale

- **WHEN** 30000 devices × 1 trap/second for 60 seconds fire traps from the embedded catalog
- **THEN** mean per-fire template evaluation time measured under `go test -bench` SHALL be < 50 microseconds

### Requirement: Poisson scheduling and global rate cap

When trap export is enabled and `-trap-interval <duration>` is non-zero (default 30s), each per-device fire time SHALL be drawn from an exponential distribution with mean equal to `-trap-interval`. A global token-bucket rate limiter SHALL gate all fires and retries when `-trap-global-cap <rate>` is set to a non-zero value (default: unlimited). The scheduler SHALL be implemented as a single goroutine owning a min-heap of `(nextFire, deviceIP)` entries.

#### Scenario: Fire intervals follow exponential distribution

- **WHEN** `-trap-interval 1s` and a single device is observed over 10000 fires
- **THEN** the observed inter-arrival distribution SHALL pass a Kolmogorov-Smirnov test for exponential(1) at α=0.05
- **AND** the observed mean SHALL be within 5% of 1.0 seconds

#### Scenario: Global cap enforced across devices

- **WHEN** `-trap-global-cap 100` is set with 30000 devices each at `-trap-interval 1s`
- **THEN** the simulator SHALL emit no more than approximately 100 traps per second averaged over 60 seconds
- **AND** the observed rate over any 1-second window SHALL be ≤ 100 + token-bucket burst

#### Scenario: Scheduler uses single goroutine

- **WHEN** the simulator is running with trap export enabled for 30000 devices
- **THEN** `runtime.NumGoroutine()` attributable to trap scheduling SHALL be O(1) (the scheduler goroutine, not O(devices))
- **AND** per-device INFORM reader goroutines are separately accounted and are expected in INFORM mode only

### Requirement: On-demand HTTP trap endpoint

The simulator SHALL expose `POST /api/v1/devices/{ip}/trap` that immediately schedules a named catalog trap for the device at the given IP. The request body SHALL be JSON with required field `name` (matching a catalog entry name) and optional `varbindOverrides` (map of template-field → string-value, overriding the per-fire template resolution). The response SHALL be `202 Accepted` with body `{"requestId": <uint32>}`. The endpoint SHALL NOT block on INFORM ack.

#### Scenario: Valid request returns 202

- **WHEN** `POST /api/v1/devices/10.42.0.1/trap` is made with body `{"name":"linkDown"}`
- **AND** device `10.42.0.1` exists and the catalog has a `linkDown` entry
- **THEN** the response status SHALL be 202
- **AND** the response body SHALL contain a `requestId` field with a non-zero integer

#### Scenario: Unknown catalog entry returns 400

- **WHEN** `POST /api/v1/devices/10.42.0.1/trap` is made with body `{"name":"notACatalogEntry"}`
- **THEN** the response status SHALL be 400
- **AND** the response body SHALL include an error message naming the unknown catalog entry

#### Scenario: Unknown device returns 404

- **WHEN** `POST /api/v1/devices/10.99.99.99/trap` is made and no such device exists
- **THEN** the response status SHALL be 404

#### Scenario: Varbind override resolves template

- **WHEN** `POST /api/v1/devices/10.42.0.1/trap` is made with body `{"name":"linkDown","varbindOverrides":{"IfIndex":"7"}}`
- **THEN** the emitted trap's `{{.IfIndex}}` template occurrences SHALL all resolve to `7` rather than a random interface index

### Requirement: Trap status HTTP endpoint

The simulator SHALL expose `GET /api/v1/traps/status` returning a JSON object with at least the following fields: `enabled` (bool), `mode` (`"trap"` or `"inform"`, absent when disabled), `sent` (uint64), `informsPending` (uint64, absent in TRAP mode), `informsAcked` (uint64, absent in TRAP mode), `informsFailed` (uint64, absent in TRAP mode), `informsDropped` (uint64, absent in TRAP mode), `rateLimiterTokensAvailable` (uint64, absent when `-trap-global-cap` is unlimited). Values are point-in-time snapshots; reads are lock-free where possible.

#### Scenario: Disabled status

- **WHEN** the feature is disabled and `GET /api/v1/traps/status` is called
- **THEN** the response SHALL be JSON `{"enabled": false}` with no counter fields

#### Scenario: TRAP mode status

- **WHEN** the feature is enabled with `-trap-mode trap` and `GET /api/v1/traps/status` is called
- **THEN** the response SHALL include `enabled: true`, `mode: "trap"`, `sent: <N>`
- **AND** INFORM-specific fields SHALL be absent

#### Scenario: INFORM mode status

- **WHEN** the feature is enabled with `-trap-mode inform` and `GET /api/v1/traps/status` is called
- **THEN** the response SHALL include `enabled`, `mode: "inform"`, `sent`, `informsPending`, `informsAcked`, `informsFailed`, `informsDropped`
- **AND** `informsPending + informsAcked + informsFailed + informsDropped` SHALL equal the total number of INFORMs ever emitted (invariant over the process lifetime)

### Requirement: CLI flag surface

The simulator SHALL accept the following CLI flags, each appearing in `--help` output with a concise description:

| Flag | Type | Default | Purpose |
|---|---|---|---|
| `-trap-collector` | `host:port` | (empty; feature off) | Enables feature, targets the collector |
| `-trap-mode` | `trap` or `inform` | `trap` | Selects PDU type |
| `-trap-interval` | duration | `30s` | Mean per-device firing interval (Poisson) |
| `-trap-global-cap` | integer tps | `0` (unlimited) | Simulator-wide rate ceiling |
| `-trap-catalog` | path | (empty; embedded) | Override embedded catalog |
| `-trap-community` | string | `public` | SNMPv2c community string |
| `-trap-source-per-device` | bool | `true` | Source IP = device IP |
| `-trap-inform-timeout` | duration | `5s` | Per-retry timeout in INFORM mode |
| `-trap-inform-retries` | integer | `2` | Max retransmissions in INFORM mode |

#### Scenario: `--help` lists trap flags

- **WHEN** the simulator is invoked with `--help`
- **THEN** the output SHALL contain all nine `-trap-*` flag names with their descriptions and defaults

#### Scenario: Invalid `-trap-mode` rejected

- **WHEN** `-trap-mode notAMode` is passed
- **THEN** startup SHALL fail with a non-zero exit code
- **AND** the error SHALL list the valid values (`trap`, `inform`)

#### Scenario: Negative `-trap-interval` rejected

- **WHEN** `-trap-interval -1s` is passed
- **THEN** startup SHALL fail with a non-zero exit code

### Requirement: Documentation

`CLAUDE.md` SHALL include an "SNMP Trap export" section listing all nine `-trap-*` flags, the catalog JSON schema, the required `sysUpTime.0` and `snmpTrapOID.0` auto-prepend behavior, the INFORM constraints (per-device binding required), and the `rp_filter` caveat on the collector host (consistent with the existing flow-export note). `README.md` SHALL list SNMPv2c trap/inform export as a feature with a pointer to the `CLAUDE.md` section.

#### Scenario: CLAUDE.md documents all trap flags

- **WHEN** `CLAUDE.md` is read
- **THEN** each of the nine `-trap-*` flags SHALL appear at least once with its description

#### Scenario: CLAUDE.md references rp_filter caveat for traps

- **WHEN** `CLAUDE.md` is read
- **THEN** the trap section SHALL note that collectors receiving UDP/162 from the simulator's device IP range may need `net.ipv4.conf.*.rp_filter` relaxed

#### Scenario: README lists trap feature

- **WHEN** `README.md` is read
- **THEN** the feature list SHALL include SNMPv2c trap and inform export as a supported capability

### Requirement: Unit test coverage with BER decoder oracle

The `go/simulator/` package SHALL include unit tests that round-trip emitted TRAP and INFORM PDUs through an in-process BER decoder (reusing `snmp.go`'s existing PDU decoding surface) and assert field-by-field equality with the inputs. Coverage SHALL include: PDU tag correctness, required varbind ordering, request-id uniqueness under concurrent fires, INFORM ack matching, retry counting, pending-inform bounded-map overflow, catalog parse errors, and template field-set validation.

#### Scenario: Round-trip test asserts field equality

- **WHEN** a TRAP PDU is encoded for catalog entry `linkDown` with `{{.IfIndex}}=3` and decoded by the in-process decoder
- **THEN** the decoded `request-id`, `sysUpTime.0` value, `snmpTrapOID.0` value, and body varbind values SHALL equal the encoder inputs exactly

#### Scenario: Tests run under `go test ./...`

- **WHEN** `cd go && go test ./...` is run
- **THEN** the new trap tests SHALL execute and pass
- **AND** existing SNMP, flow-export, and other test suites SHALL continue to pass

