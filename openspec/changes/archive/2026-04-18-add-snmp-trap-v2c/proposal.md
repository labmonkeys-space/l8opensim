## Why

The simulator today responds to SNMP polls but never initiates a notification — so it cannot exercise a monitoring system's trap-reception path. OpenNMS `trapd` in particular has its own ingestion queue, ringbuffer sizing, event-de-duplication, and source-address-to-node mapping that only show their behaviour (and breaking points) under real trap load. To validate those code paths we need a source that can emit valid SNMPv2c traps from 30,000+ distinct IPv4 "device" identities at a controllable, sustained rate. No production network makes it easy to reproduce that shape of load; a simulator that already owns 30k per-device UDP source identities via the existing TUN / netns / veth plumbing is the natural place to add it.

INFORMs are in scope from day one because the acknowledged path is where trapd's retry/ack handling and source-IP round-tripping actually get exercised — shipping TRAP-only would leave the most interesting failure modes untested.

## What Changes

- Add a new `snmp-trap` capability to `go/simulator/` that emits SNMPv2c TRAP (PDU 0xA7) and INFORM (PDU 0xA6) datagrams to a configured collector.
- Introduce a per-device `TrapExporter` (analogous to `FlowExporter`) driven by a **single** central min-heap scheduler goroutine rather than 30k per-device tickers. Firing is a Poisson process per device with a configurable mean interval; a global token-bucket limiter caps the simulator-wide tps regardless of per-device rate.
- Reuse the existing per-device UDP source-IP plumbing (the same `setupVethPair` FORWARD rule and per-device socket path that `-flow-source-per-device` uses) so each trap's UDP source IP equals the emitting device's IPv4. A new `-trap-source-per-device` flag defaults to true and is **required** when `-trap-mode inform` is selected.
- Ship an embedded universal trap catalog covering the five standard SNMPv2-MIB notifications (`coldStart`, `warmStart`, `linkDown`, `linkUp`, `authenticationFailure`). Operators override via `-trap-catalog <path>` pointing to a JSON catalog file. Varbinds support a small set of templated fields (`{{.IfIndex}}`, `{{.Uptime}}`, `{{.Now}}`, `{{.DeviceIP}}`).
- Add an on-demand HTTP endpoint `POST /api/v1/devices/{ip}/trap` to fire a named catalog trap immediately for one device (for CI/test-harness use), plus `GET /api/v1/traps/status` reporting sent-count, pending-informs, and failed-informs.
- Support both `-trap-mode trap` (fire-and-forget) and `-trap-mode inform` (with per-device pending map, configurable retry count / timeout, retries that also consume the global cap to prevent retry-storm amplification, and a bounded pending-inform map that drops oldest on overflow).
- Reuse the existing ASN.1/BER primitives in `snmp_encoding.go` for PDU encoding and `snmp.go`'s decoder for parsing INFORM acks — no new SNMP wire code.
- New CLI flags (all prefixed `-trap-*` for consistency with `-flow-*`):
  `-trap-collector`, `-trap-mode`, `-trap-interval`, `-trap-global-cap`, `-trap-catalog`, `-trap-community`, `-trap-source-per-device`, `-trap-inform-timeout`, `-trap-inform-retries`.

No breaking changes. The simulator's existing SNMP server, flow export, and all other capabilities are untouched.

## Capabilities

### New Capabilities
- `snmp-trap`: SNMPv2c trap/inform emission from simulated devices. Covers the wire format (TRAP and INFORM PDUs), the JSON catalog schema and varbind templating, the per-device Poisson scheduler and global rate cap, the on-demand HTTP endpoint, INFORM ack/retry semantics, per-device source-IP binding, and `/api/v1/traps/status`.

### Modified Capabilities
<!-- None. The existing SNMP server code paths (`snmp.go`, `snmp_handlers.go`, `snmpv3.go`) are not modified — traps reuse `snmp_encoding.go` primitives but live in new files. `flow-export` and `flow-export-sflow` are untouched. -->

## Impact

**In-tree files touched**

New files in `go/simulator/`:
- `trap_exporter.go` — per-device `TrapExporter`, `TrapEncoder` interface, manager integration (analogous to `flow_exporter.go`).
- `trap_v2c.go` — SNMPv2c TRAP and INFORM PDU encoder; reuses `snmp_encoding.go` ASN.1/BER primitives.
- `trap_catalog.go` — catalog JSON load/parse, varbind template resolution, embedded universal catalog.
- `trap_scheduler.go` — central min-heap scheduler goroutine, Poisson inter-arrival, global token-bucket limiter.
- `trap_exporter_test.go`, `trap_v2c_test.go`, `trap_catalog_test.go`, `trap_scheduler_test.go` — unit tests with an in-process BER decoder oracle, catalog parsing cases, and scheduler rate/cap assertions.

Modified files in `go/simulator/`:
- `simulator.go` — nine new `-trap-*` CLI flags and help text.
- `manager.go` — `SimulatorManager` owns the scheduler, global limiter, catalog, and shared/per-device UDP socket lifecycle.
- `device.go` — per-device `TrapExporter` startup and shutdown tied to device lifecycle.
- `api.go` + `web.go` — new routes `POST /api/v1/devices/{ip}/trap` and `GET /api/v1/traps/status`.

New embedded resource:
- `go/simulator/resources/_common/traps.json` — universal 5-trap catalog, loaded via `embed.FS` so the feature works without external files.

Documentation:
- `CLAUDE.md` — new "SNMP Trap export" section listing the `-trap-*` flags, catalog JSON schema, INFORM constraints, and the rp_filter / FORWARD-rule caveats that already apply to flow export.
- `README.md` — feature bullet and reference to the new CLAUDE.md section.

**Dependencies**

No new Go module dependencies. Rate limiting uses `golang.org/x/time/rate` which is already an indirect dependency (or will be vendored inline in ~20 lines if not). Decision recorded in `design.md`.

**Downstream consumers**

- Users not passing `-trap-collector` see no behavioural change.
- Users passing `-trap-collector host:162 -trap-mode trap` get Poisson-scheduled v2c traps at `-trap-interval` cadence with source IP = device IP.
- Users passing `-trap-mode inform` MUST keep `-trap-source-per-device=true` (default) — startup fails otherwise.
- Collector-side operators must accept UDP/162 with source IPs in the simulator's device range (same rp_filter caveat as flow export; documented in CLAUDE.md).

**Out of scope**

- SNMPv1 traps (separate PDU structure, `Trap-PDU` type 0xA4). Deferred to a follow-up change.
- SNMPv3 traps (engine-id discovery, USM auth/priv). The `snmpv3.go` crypto is already present but v3 trap flows add engine-boot/time negotiation and per-target SecurityName state; out of scope here.
- Per-device-type trap catalogs (e.g. `bgpBackwardTransition` for routers only). Universal 5-trap catalog only in phase 1.
- State-coupled traps (e.g. toggling an interface's `ifAdminStatus` via API automatically emits a `linkDown`). Synthetic only — catalog-driven, no feedback loop from device state.
- Metric-threshold traps (e.g. `cpu > 90% → high-CPU trap`).
- Rendering traps through the L8 web proxy (`go/l8/`). The HTTP API is on the simulator directly, not the L8 overlay.
