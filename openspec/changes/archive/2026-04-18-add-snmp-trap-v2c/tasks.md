## 1. Pre-flight

- [x] 1.1 Confirm `golang.org/x/time/rate` is present in `go.sum` after `cd go && go mod tidy`. If missing, add to `go/simulator/go.mod` and re-run `go mod tidy`. **Resolved: added via `go get`; now `golang.org/x/time v0.15.0` in `go/go.mod`.**
- [x] 1.2 Confirm the existing per-device UDP socket helper (`openFlowConnForDevice` in `flow_exporter.go`) is suitable for reuse in trap export, or identify the smallest refactor needed to share it (e.g. extract to a device-scoped helper in `device.go` or `manager.go`). Record the decision in a code comment on the helper. **Resolved: copy the pattern (atomic.Pointer[net.UDPConn], ListenUDPInNamespace) in trap_exporter.go rather than share. Keeps subsystems decoupled and matches how sflow/netflow each own their sockets.**
- [x] 1.3 Resolve Open Question #3 from design.md: decide embedded catalog path — `resources/_common/traps.json` vs `resources/traps.json`. Pick the path, document it in CLAUDE.md reference section. **Resolved: `resources/_common/traps.json`.**
- [x] 1.4 Resolve Open Question #1 from design.md: confirm weighted-random catalog selection with per-entry `weight` field (default 1). This affects the catalog JSON schema in task 2.1. **Resolved: weighted-random, default 1. Universal weights: linkDown=40, linkUp=40, authenticationFailure=10, coldStart=5, warmStart=5.**

## 2. Catalog subsystem

- [x] 2.1 Create `go/simulator/trap_catalog.go` with the catalog JSON schema types (`Catalog`, `CatalogEntry`, `VarbindTemplate`) matching design.md §D3 and §D4.
- [x] 2.2 Implement `LoadEmbeddedCatalog()` using `embed.FS` pointing at the path chosen in task 1.3. Add the `//go:embed` directive.
- [x] 2.3 Implement `LoadCatalogFromFile(path string) (*Catalog, error)` for the `-trap-catalog` override path.
- [x] 2.4 Implement catalog validation: reject entries with `sysUpTime.0` or `snmpTrapOID.0` in body varbinds (design.md §D10); reject templates referencing fields outside `IfIndex`/`Uptime`/`Now`/`DeviceIP` (spec: "Unknown template field rejected").
- [x] 2.5 Pre-parse `text/template` objects per entry varbind OID and value at load time; store as `*template.Template` on the parsed entry (design.md Risks — "template evaluation cost at scale").
- [x] 2.6 Implement the five universal entries in a new file `go/simulator/resources/_common/traps.json` (or whichever path task 1.3 chose): `coldStart`, `warmStart`, `linkDown`, `linkUp`, `authenticationFailure` per design.md §D3 table, with weights per resolved Open Question #1.
- [x] 2.7 Implement `(*Catalog) Pick(rnd *rand.Rand) *CatalogEntry` — weighted-random selection.
- [x] 2.8 Implement `(*CatalogEntry) Resolve(ctx TemplateCtx, overrides map[string]string) ([]Varbind, error)` — evaluate templates per fire, honoring `varbindOverrides` from the HTTP endpoint.
- [x] 2.9 Create `go/simulator/trap_catalog_test.go`: (a) universal-catalog parse success; (b) invalid-JSON error; (c) reserved-OID-in-body rejection; (d) unknown-template-field rejection; (e) `Pick` weight distribution over N draws within tolerance; (f) `Resolve` with and without overrides.

## 3. SNMPv2c PDU encoder

- [x] 3.1 Create `go/simulator/trap_v2c.go` with a `SNMPv2cEncoder` struct implementing the `TrapEncoder` interface defined in design.md §D9.
- [x] 3.2 Implement `EncodeTrap(community, reqID, trapOID, uptimeHundredths, varbinds, buf)`: outer SEQUENCE with version=1, community, SNMPv2-Trap-PDU (tag 0xA7) containing request-id / error-status=0 / error-index=0 / variable-bindings. Reuse `snmp_encoding.go` primitives (ASN.1 integer / octet-string / OID / sequence encoders).
- [x] 3.3 Prepend `sysUpTime.0` (OID `1.3.6.1.2.1.1.3.0`, TimeTicks tag `0x43`) and `snmpTrapOID.0` (OID `1.3.6.1.6.3.1.1.4.1.0`, OID value = argument `trapOID`) to the variable-bindings sequence automatically per design.md §D10.
- [x] 3.4 Implement `EncodeInform(...)` identically to `EncodeTrap` but with InformRequest-PDU tag `0xA6`.
- [x] 3.5 Implement `ParseAck(pkt []byte) (reqID uint32, ok bool, err error)`: decode outer SEQUENCE, verify version/community, verify PDU tag is `0xA2` (GetResponse-PDU), return `request-id` and `error-status == 0`. Reuse the decoder surface from `snmp.go` (see design.md §D8).
- [x] 3.6 Create `go/simulator/trap_v2c_test.go`: (a) round-trip TRAP — encode a TRAP, decode with `snmp.go`'s existing PDU decoder, assert every field matches; (b) round-trip INFORM — same for INFORM with tag `0xA6`; (c) `ParseAck` happy path; (d) `ParseAck` rejects malformed / non-GetResponse input; (e) request-id uniqueness — 10k encodes with a counter-based generator produce 10k distinct request-ids.
- [x] 3.7 Add a byte-pinned regression test (MD5 of structural bytes for a canonical TRAP encode) so future encoder changes trip the hash and must be reviewed — mirrors the `TestByteIdentity_NetFlow9` pattern from the flow-export work.

## 4. Rate limiter and scheduler

- [x] 4.1 Create `go/simulator/trap_scheduler.go` with `TrapScheduler` struct and `trapHeap` type (`container/heap` implementation). Fields per design.md §D1.
- [x] 4.2 Implement `(*TrapScheduler) Run(ctx)`: loop popping earliest due entry, consuming one token from `*rate.Limiter` (blocking Wait), invoking the per-device `TrapExporter.Fire(catalog.Pick(...))`, requeueing with exponential-distributed next-fire offset (design.md §D2).
- [x] 4.3 Implement `(*TrapScheduler) Register(deviceIP, exporter)` and `Deregister(deviceIP)` for device lifecycle wiring; protect the heap with a mutex.
- [x] 4.4 Implement injectable `now func() time.Time` and `rnd *rand.Rand` (seeded from crypto/rand at construction, exposed for tests).
- [x] 4.5 Handle panic recovery in the scheduler loop (design.md Risks) — `defer recover()` + `log.Printf`, continue loop.
- [x] 4.6 Create `go/simulator/trap_scheduler_test.go`: (a) `Run` fires devices in nextFire order given a deterministic seed; (b) global cap is honored — with cap=10, 1000 registered devices each at interval=0 produce ≤10 fires per second ± burst; (c) `Register`/`Deregister` thread-safety smoke test (100 goroutines × mixed ops); (d) exponential inter-arrival KS test per spec scenario.

## 5. Per-device TrapExporter

- [x] 5.1 Create `go/simulator/trap_exporter.go` with a `TrapExporter` struct: device IP, `*net.UDPConn` (may be nil if shared socket), `community string`, `requestIDCounter uint32`, `pendingInforms map[uint32]*pendingInform` (bounded 100), `encoder TrapEncoder`, `stats *TrapStats`.
- [x] 5.2 Implement `(*TrapExporter) Fire(entry *CatalogEntry, overrides map[string]string)` — build template context, `Resolve` varbinds, allocate request-id, call `encoder.EncodeTrap` or `EncodeInform` based on mode, `WriteToUDP` to collector. On INFORM, insert pending-inform record and start/ensure the reader goroutine.
- [x] 5.3 Implement bounded-pending-inform overflow: on insert, if len == 100, remove oldest (track via a linked-list or a monotonically-increasing timestamp + linear scan at overflow — design decision; linked-list is cleaner). Increment `informsDropped`.
- [x] 5.4 Implement per-device reader goroutine (INFORM mode): loop `ReadFromUDP` on the per-device socket, hand bytes to `encoder.ParseAck`, on match remove pending record and increment `informsAcked`. Exit cleanly on `net.ErrClosed`.
- [x] 5.5 Implement retry logic: a per-device goroutine (or a shared retry scheduler) wakes on inform-timeout boundaries, for each pending inform past its deadline either retries (consuming a limiter token) or gives up and increments `informsFailed`.
- [x] 5.6 Implement `(*TrapExporter) Close()` — close socket, wait for reader and retry goroutines to exit.
- [x] 5.7 Create `go/simulator/trap_exporter_test.go`: (a) `Fire` in TRAP mode emits one datagram and increments `sent`; (b) `Fire` in INFORM mode emits and registers pending; matching ack via a mock-collector UDP responder removes pending and increments `informsAcked`; (c) no-ack + timeout exhaustion increments `informsFailed`; (d) 101 pending informs triggers `informsDropped`; (e) `Close` terminates reader without leaks (check `runtime.NumGoroutine` before/after).

## 6. SimulatorManager integration

- [x] 6.1 Add `TrapConfig` struct to `go/simulator/manager.go` capturing parsed CLI flags. **(Placed in new file `trap_manager.go` alongside related methods.)**
- [x] 6.2 Wire `SimulatorManager` to own: `*TrapCatalog`, `*TrapScheduler`, `*rate.Limiter` (global cap), and shared-fallback `*net.UDPConn` if `-trap-source-per-device=false`.
- [x] 6.3 Add `SimulatorManager.StartTrapExport(cfg TrapConfig) error` — validates config (INFORM + per-device-bind-false = error per spec), loads catalog, starts scheduler goroutine.
- [x] 6.4 Enforce INFORM + `-trap-source-per-device=false` error in config validation (spec: "Explicit conflict fails startup").
- [x] 6.5 Add `SimulatorManager.StopTrapExport()` — signals scheduler context cancel, waits on all per-device exporters to `Close()`. Also wired into `Shutdown()`.

## 7. Device lifecycle wiring

- [x] 7.1 In `go/simulator/device.go`, add per-device `TrapExporter` instantiation during device startup when `SimulatorManager.TrapConfig.Enabled` is true. Both bulk-create (around line 267) and single-create (around line 498) sites wired via `sm.startDeviceTrapExporter(device)`.
- [x] 7.2 Open the per-device UDP socket via the helper identified in task 1.2 (reuse or extraction of `openFlowConnForDevice`). In TRAP mode, log a warning on bind failure and fall back to shared socket per spec. In INFORM mode, return an error per spec. **(Implemented `openTrapConnForDevice` in trap_exporter.go — pattern copied not shared per pre-flight 1.2.)**
- [x] 7.3 Register the new `TrapExporter` with the `TrapScheduler` via `Register(deviceIP, exporter)`.
- [x] 7.4 On device teardown, call `Deregister` on the scheduler and `Close` on the exporter. Wired into both `Stop()` and `stopListenersOnly()`.

## 8. CLI flag wiring

- [x] 8.1 In `go/simulator/simulator.go`, declare the nine `-trap-*` flags per the CLI surface table in spec.md.
- [x] 8.2 Parse flag values into a `TrapConfig` struct; invoke `SimulatorManager.StartTrapExport` after device pre-allocation but before opening the HTTP listener.
- [x] 8.3 Update `--help` output — verify all nine flags appear with descriptions and defaults (spec scenario: "--help lists trap flags").
- [x] 8.4 Reject invalid values: `-trap-mode notAMode`, negative `-trap-interval`, negative `-trap-inform-retries`, negative `-trap-global-cap` (spec scenarios). Validation happens in `ParseTrapMode` and `StartTrapExport` with unit tests in `trap_api_test.go`.

## 9. HTTP API

- [x] 9.1 In `go/simulator/web.go`, register routes `POST /api/v1/devices/{ip}/trap` and `GET /api/v1/traps/status`.
- [x] 9.2 In `go/simulator/api.go`, implement `POST /api/v1/devices/{ip}/trap` handler: parse JSON body `{name, varbindOverrides}`; look up device by IP (404 if missing); look up catalog entry by name (400 if missing); call `TrapExporter.Fire`; return 202 with `{"requestId": N}`. **(Handler lives in web.go alongside the existing flow-status handler, matching the repo pattern.)**
- [x] 9.3 Implement `GET /api/v1/traps/status` handler returning the fields defined in spec: `enabled`, `mode`, `sent`, INFORM counters, `rateLimiterTokensAvailable`.
- [x] 9.4 Add HTTP API tests in a new `go/simulator/trap_api_test.go` or extend existing `api_test.go` (whichever matches repo convention): (a) POST happy path returns 202 + requestId; (b) unknown catalog name returns 400; (c) unknown device IP returns 404; (d) GET /status fields match mode; (e) counter invariant `informsPending + informsAcked + informsFailed + informsDropped == totalInformsEmitted` holds. **(Tests exercise the manager surface directly; full-router httptest requires real device lifecycle which needs root + netns.)**

## 10. Documentation

- [x] 10.1 Update `CLAUDE.md`: add an "SNMP Trap export" section with the nine `-trap-*` flags table, catalog JSON schema (point at `_common/traps.json` or chosen path), auto-prepend behavior for `sysUpTime.0`/`snmpTrapOID.0`, INFORM constraints (per-device binding required, pending bounded at 100), and the `rp_filter` caveat on the collector host.
- [x] 10.2 Update `README.md` feature list: add SNMPv2c trap/inform export as a supported capability with a pointer to the CLAUDE.md section.
- [x] 10.3 Spot-check the CLAUDE.md table against the actual flag help text — any discrepancy is a documentation bug. All 9 `-trap-*` flags present in `--help` output.

## 11. Validation

- [x] 11.1 Run `cd go && go mod tidy && go build ./...`; assert no unexpected additions to `go.mod` beyond `golang.org/x/time/rate` (if it was added). **Only new dep: `golang.org/x/time v0.15.0`.**
- [x] 11.2 Run `cd go && go test ./...`; assert all new trap tests pass and all existing tests still pass. All packages pass.
- [x] 11.3 Run `cd go && go test -race ./simulator/...`; assert no data races — the scheduler heap, pending-inform map, and stats counters are concurrency-hot. Simulator test suite passes under `-race` in ~13s.
- [ ] 11.4 Manual smoke test with `snmptrapd -f -Of -Lo` as a collector: 100 simulated devices, `-trap-mode trap -trap-interval 1s`, verify `snmptrapd` receives traps with correct source IPs and decodes varbinds without error. **Deferred: requires root + network namespace + a running `snmptrapd` instance; not reachable from `go test` in CI. Byte-identity regression test + unit-level mock-collector tests cover the wire format.**
- [ ] 11.5 Manual smoke test with OpenNMS trapd: same config, verify events appear in OpenNMS with one node-association per simulated device. **Deferred to maintainer post-merge — requires a running OpenNMS instance.**
- [ ] 11.6 Manual smoke test INFORM mode: 10 devices × `-trap-mode inform`, kill the collector mid-run, verify `informsPending`/`informsFailed`/`informsDropped` counters move correctly in `GET /api/v1/traps/status`. **Deferred to maintainer post-merge — requires root + netns. Counter invariants verified at unit level by `TestInformInvariant_AtExporterLevel`.**
- [ ] 11.7 Scale test: 30k devices × `-trap-interval 30s` × `-trap-global-cap 1000` over 10 minutes; assert sustained ~1000 tps at the collector, `runtime.NumGoroutine` stable, RSS bounded. **Deferred to maintainer post-merge — requires root + namespace + 30k TUN interfaces; infeasible in CI.**
- [x] 11.8 Run `openspec validate add-snmp-trap-v2c --strict` and fix any findings. Passes strict.

## 12. Post-merge

- [x] 12.1 Update project memory (`MEMORY.md`) if appropriate — add an entry documenting SNMP trap/inform support and the INFORM-requires-per-device-source constraint, so future operator questions can be routed to the CLAUDE.md section.
- [ ] 12.2 Archive the OpenSpec change per the experimental workflow (`openspec archive add-snmp-trap-v2c`) once it is merged and deployed. **Deferred — blocks on merge.**
- [ ] 12.3 Open follow-up issues or OpenSpec proposals for the explicitly-deferred scope: SNMPv1 traps, SNMPv3 traps, per-device-type catalogs, state-coupled traps, `snmpTrapEnterprise.0` varbind (design.md Open Question #5). **Deferred — post-merge hygiene.**
