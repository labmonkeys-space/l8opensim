## Context

**Current state.** The simulator has a mature SNMP request/response stack:

| Surface | Current shape |
|---|---|
| SNMP server | `snmp_server.go` opens a UDP listener per device on port 161; `snmp.go` dispatches GET/GETNEXT/GETBULK/SET; `snmp_handlers.go` does lock-free `sync.Map` OID lookup. |
| ASN.1/BER codec | `snmp_encoding.go` — integer, octet-string, OID, null, sequence encoders and length-prefix helpers. Battle-tested under the query path. |
| SNMPv3 crypto | `snmpv3.go` + `snmpv3_crypto.go` — MD5/SHA1 auth, DES/AES128 privacy. Not used by this change (v2c only). |
| Per-device UDP source IP | Flow export already solves this: `-flow-source-per-device` opens a per-device UDP socket in the `opensim` netns bound to the device IP. `setupVethPair` installs `FORWARD -i veth-sim-host -j ACCEPT` so egress works on Docker-present hosts. `rp_filter` caveat documented for flow-collector side. |
| Flow export architecture | Shared ticker goroutine drives per-device `FlowExporter`s, each owning an optional per-device `*net.UDPConn` plus a `FlowCache`. `FlowEncoder` interface abstracts NetFlow9 / IPFIX / NetFlow5 / sFlow. |
| Resources | 379 JSON files under `resources/<device-type>/` merged at startup. No `_common/` subtree exists today. |
| HTTP API | `web.go` + `api.go` serve `/api/v1/devices/*` and `/api/v1/flows/status`. |

**Problem shape.** Trap emission is push-initiated and a fundamentally different workload from poll-response:

1. **Initiator**, not responder. Requires a scheduler, not a request handler.
2. **No existing back-channel for INFORM acks.** The SNMP server listens on 161; the collector's ack comes back to the source port of the outgoing socket on UDP/162 and needs its own read loop.
3. **Scale.** 30,000 devices × any nontrivial per-device rate = scheduler design matters. Naïve `time.Ticker` per device = 30k goroutines + 30k timers in the runtime heap.
4. **Catalog-driven content.** Unlike poll responses (where the OID tree fully determines the bytes on the wire), trap varbinds include a trap-specific OID (`snmpTrapOID.0`), a trap-type-specific set of varbinds, and templated fields (device-specific `IfIndex`, runtime `Uptime`). A small template evaluator is needed.

**Constraints.**
- No regression in the SNMP poll path. `snmp.go`/`snmp_handlers.go` are not edited.
- Reuse the flow-export per-device source-IP plumbing — don't duplicate it. This constrains the UDP socket lifecycle to match the shape `FlowExporter` already uses.
- Catalog must work out-of-box (no external file required) so `go test ./...` and the Docker image don't need a bind-mount. Embedded default + optional override is the shape.
- OpenNMS trapd is the primary validation target. The v2c wire format has ambiguity only at the margins (varbind ordering, request-id width); follow net-snmp's `snmptrap` behaviour for compatibility.

**Stakeholders.**
- Primary: Ronny Trommer (maintainer), for OpenNMS trapd scale testing.
- Consumers: OpenNMS trapd operators validating ingestion-pipeline capacity, event-de-duplication, source-IP-to-node mapping.
- Indirect: future SNMPv1 / SNMPv3 trap changes (should not have to redesign the scheduler / catalog / interface).

## Goals / Non-Goals

**Goals:**
- Emit valid SNMPv2c TRAP (PDU 0xA7) and INFORM (PDU 0xA6) datagrams from simulated devices with source IP = device IP.
- Sustain 30k-device scale without 30k goroutines or 30k heap timers.
- Catalog is JSON, per-trap-name, with a small template vocabulary; universal catalog embedded so the feature works without external files.
- Per-device mean firing rate (`-trap-interval`) is Poisson-distributed to avoid synchronized thundering herds; a global token-bucket (`-trap-global-cap`) provides a hard ceiling.
- INFORM acks are demultiplexed per device via the per-device UDP socket (no request-id table across devices). Pending state is bounded; retries consume global-cap tokens.
- On-demand API (`POST /api/v1/devices/{ip}/trap`) fires a named catalog trap for one device immediately (test-harness use).
- Reuse `snmp_encoding.go` for all BER primitives; no duplicated codec.
- Unit tests round-trip emitted PDUs through an in-process BER decoder and assert the wire layout matches a trapped snmpdump.

**Non-Goals:**
- SNMPv1 traps. Separate PDU shape (`Trap-PDU` 0xA4, enterprise OID, generic-trap enum, specific-trap int, timestamp). Deferred.
- SNMPv3 traps. Engine-id discovery + USM adds substantial per-target state; deferred.
- Per-device-type catalogs. The universal 5-trap catalog ships; device-specific trap lists (e.g. `bgpBackwardTransition` on routers only) can layer on later via the same catalog interface.
- State-coupled traps. No API ↔ catalog feedback loop in phase 1.
- L8 overlay integration. Traps exit directly from the simulator's per-device socket path; `go/l8/` is untouched.
- Trap reception / trapd simulation. We emit only.

## Decisions

### D1. Single central min-heap scheduler (not per-device tickers)

**Decision:** One scheduler goroutine owns a min-heap of `(nextFireTime, deviceID)` entries. On each iteration it pops the earliest due entry, consumes one token from the global limiter (blocking if the cap is exceeded), fires the trap for that device via the device's `TrapExporter`, then requeues the device with a Poisson-distributed next-fire offset.

**Shape (illustrative, not normative):**

```go
type trapHeapEntry struct {
    nextFire time.Time
    deviceIP net.IP
    index    int
}

type TrapScheduler struct {
    heap    trapHeap            // container/heap
    mu      sync.Mutex
    wake    chan struct{}       // signals heap push
    limiter *rate.Limiter       // global tps cap
    now     func() time.Time    // injectable for tests
    rnd     *rand.Rand          // Poisson draw
    devices *sync.Map           // deviceIP → *TrapExporter
}
```

**Rationale:** 30,000 `time.Ticker`s = 30,000 goroutines + timers in the runtime's 4-heap. A single scheduler goroutine with an explicit min-heap is:
- Cheaper (1 goroutine, explicit heap ops are O(log N) and obvious under profiling).
- Naturally rate-capped — the global limiter is consulted *before* the fire, so cap consumption is centralized.
- Easier to test — the whole firing order is deterministic given a fixed RNG seed and `now` injection.

**Alternatives considered:**
- **Per-device `time.Ticker`.** Rejected: goroutine count and timer-heap churn. Also makes the global cap awkward (every goroutine consulting a shared limiter around a timer).
- **Sharded schedulers (N workers, M devices each).** Rejected as premature — 30k entries in one min-heap is ~15-level deep and trivial for Go. Sharding would help only if the heap itself became a bottleneck, which profiling would reveal later.
- **Wheel timer (hashed timer wheels).** Rejected: higher complexity for no measurable gain at this scale. Worth revisiting at 300k devices.

### D2. Poisson inter-arrival, not fixed interval

**Decision:** Per-device next-fire offset is drawn from an exponential distribution with mean = `-trap-interval`. This gives Poisson arrivals per device and, because devices start with random phases, Poisson arrivals system-wide up to the cap.

**Rationale:** A deterministic `N ticks/minute` schedule produces thundering herds (every 60s all 30k devices fire at once), which exercises the collector's ingestion *burst* path but not its sustained path, and often drops packets at the UDP socket before they reach trapd. Poisson is memoryless and matches what real misbehaving network fleets look like — clustered but not synchronized.

**Alternatives considered:**
- **Deterministic interval + per-device random phase offset at startup.** Smooths the initial burst but still produces exact periodic peaks after startup drift correlates. Rejected.
- **Make distribution a CLI flag (`-trap-distribution poisson|uniform|constant`).** Rejected for phase 1 — one knob, one behaviour. Add if users ask.

### D3. Catalog = embedded default + optional file override

**Decision:** Package the universal 5-trap catalog as a Go string constant loaded via `embed.FS` from `resources/_common/traps.json`. `-trap-catalog <path>` causes the loader to read the given file instead (complete override, not merge). Catalog parsing happens once at startup; the parsed catalog is stored in `SimulatorManager` and shared by all devices.

**Universal catalog contents:**

| Name | snmpTrapOID | Body varbinds |
|---|---|---|
| `coldStart` | `1.3.6.1.6.3.1.1.5.1` | none |
| `warmStart` | `1.3.6.1.6.3.1.1.5.2` | none |
| `linkDown` | `1.3.6.1.6.3.1.1.5.3` | `ifIndex.N`, `ifAdminStatus.N`=2, `ifOperStatus.N`=2 |
| `linkUp` | `1.3.6.1.6.3.1.1.5.4` | `ifIndex.N`, `ifAdminStatus.N`=1, `ifOperStatus.N`=1 |
| `authenticationFailure` | `1.3.6.1.6.3.1.1.5.5` | none |

**Rationale:** Embedded default means `go test` and `docker run` work with no setup. File override covers the "we need custom enterprise traps" use case without shipping a DSL. Complete-override (not merge) keeps the semantics simple — operators who want a custom catalog copy the embedded JSON and edit it, which is what they'd do anyway.

**Alternatives considered:**
- **Directory-based catalog** (`resources/_common/traps/*.json`, one file per trap). Rejected — adds complexity for no operator benefit at the universal-catalog scale. Revisit if per-device-type catalogs are added.
- **Merge embedded + file** (file adds to default). Rejected — operators who want to remove a default trap can't, so they end up in "override entirely" mode anyway.

### D4. Template vocabulary: four fields, text/template

**Decision:** Use Go's `text/template` with four named fields:

| Field | Resolution |
|---|---|
| `{{.IfIndex}}` | Random integer from device's simulated interface-index set, resolved once per fire |
| `{{.Uptime}}` | Device uptime in 1/100s ticks (same units as `sysUpTime.0`) |
| `{{.Now}}` | `time.Now().Unix()` at fire time |
| `{{.DeviceIP}}` | Device's primary IPv4 as dotted-quad string |

Template resolution happens in `trap_catalog.go` at fire time, producing a `[]Varbind` with concrete values before the PDU encoder runs.

**Rationale:** `text/template` is stdlib, familiar, and its escape rules are acceptable for SNMP scalar values (which are integers, OIDs, or short strings). Four fields cover every case I can think of for the universal catalog; the `-trap-catalog` file override can reference only these four (validator at load time).

**Alternatives considered:**
- **No templates, literal values only.** Rejected — `linkDown` without an `ifIndex` per-fire is useless for trapd load testing (all traps look identical, defeats event-de-duplication testing).
- **Full Go template expression support (functions, pipelines).** Rejected — opens an attack surface for the operator-supplied catalog; the four fields are a known-bounded surface.
- **Custom `${IFINDEX}` placeholder syntax.** Rejected — yet another micro-language. `text/template` is cheap.

### D5. INFORM demux via per-device socket (request per-device-source-IP)

**Decision:** When `-trap-mode inform` is selected, `-trap-source-per-device=true` is required. Startup fails with a clear error otherwise. The per-device UDP socket that already exists for source-IP binding becomes the demux mechanism: each device's socket handles only its own outgoing informs and incoming acks. A per-device reader goroutine (started alongside the exporter, torn down with it) reads GetResponse-PDU acks, matches by request-id against the device's pending-inform map, and either marks the pending entry acked or schedules a retry.

**Rationale:** Without per-device sockets, a single shared socket on port 162-source receives acks for *all* devices, and we need a request-id → device lookup table. At 30k devices × pending informs per device, collision-free request-ids need a 64-bit counter and a central concurrent map, and we also need a single reader goroutine fighting with all the writer goroutines. The per-device socket makes each device's state strictly local: request-ids only need to be unique per device, and each reader goroutine has a trivial loop.

30k reader goroutines is fine — the SNMP server already runs one per device (`snmp_server.go`), so we already know Go handles this shape at this scale. The reader goroutine cost is bounded by the ack rate, which is capped by the fire rate, which is capped by `-trap-global-cap`.

**Alternatives considered:**
- **Shared socket + request-id demux.** Rejected per the above. Also: doesn't preserve source IP.
- **Make INFORM work without per-device sockets by forging src in the datagram.** Not possible on a standard UDP socket; would require raw sockets with further kernel privileges. Rejected.

### D6. Bounded pending-inform map, drop-oldest on overflow

**Decision:** Each `TrapExporter` keeps a `map[uint32]*pendingInform` keyed by request-id, bounded to 100 entries (named constant). On overflow, the oldest entry is dropped and counted as `informsDropped`. The `GET /api/v1/traps/status` endpoint exposes this counter.

**Rationale:** If the collector dies, informs pile up at each device. Without a bound, memory grows until OOM. 100 is large enough that normal retry latency (retries × timeout = 2 × 5s = 10s) × per-device fire rate (default 30s) doesn't hit the bound, but small enough that 30k devices × 100 × sizeof(pendingInform) is bounded total memory.

**Drop-oldest** rather than drop-newest because an old pending inform is more likely to be genuinely lost (the collector hasn't responded in 10+ seconds).

**Alternatives considered:**
- **Block the scheduler when device's pending map is full.** Rejected — head-of-line blocks other devices. The cap should be at the global limiter, not per-device.
- **Dynamic cap based on observed inform round-trip time.** Rejected — premature optimization. 100 is a round number that works.

### D7. Retries consume global-cap tokens

**Decision:** When an INFORM times out and is retried, the retry call-site reserves one token from the global limiter before transmitting, just as a fresh fire would.

**Rationale:** If the collector is unreachable (the common failure causing retries), treating retries as "free" causes the simulator to burn socket and CPU on retry-storm transmission beyond the configured cap. Consuming tokens puts an upper bound on total wire traffic regardless of failure mode. The cost — retries compete with fresh fires for tokens — is fine: if the collector is struggling, sending *more* new informs at the expense of completing existing ones is not the behaviour we want.

**Alternatives considered:**
- **Retries bypass the cap.** Rejected (retry-storm amplification).
- **Retries consume a separate retry-budget.** Rejected — another knob, and the failure modes align with one shared budget.

### D8. Reuse `snmp_encoding.go`, no parallel ASN.1 codec

**Decision:** `trap_v2c.go` imports the BER primitives from `snmp_encoding.go`. The v2c TRAP PDU structure (SEQUENCE → version / community / PDU) is encoded by composing existing primitives; only the new PDU type tags (`0xA7` SNMPv2-Trap, `0xA6` InformRequest, `0xA2` GetResponse for ack parsing) are added.

**Rationale:** The existing codec handles variable-length integer encoding, OID sub-identifier compression, and counter/gauge wire tags — all of which traps need. Duplicating this in a trap-specific codec risks divergence (e.g. an integer-encoding bug in one place but not the other).

**Ack parsing** reuses `snmp.go`'s PDU decoder: a GetResponse-PDU carries the same structure as a GetRequest-PDU, and the existing decoder already handles all SNMP PDU types. We only need to check that the decoded PDU type is GetResponse, not add a new decoder.

**Alternatives considered:**
- **Standalone codec in `trap_v2c.go`.** Rejected — bug surface doubled for zero benefit.
- **Pull `gosnmp` dependency.** Rejected — the simulator's deps list is deliberately small, and the SNMP path has been in-house since day one.

### D9. `TrapEncoder` interface scoped to phase 1

**Decision:** Introduce a minimal `TrapEncoder` interface with just enough surface for v2c:

```go
type TrapEncoder interface {
    EncodeTrap(community string, reqID uint32, trapOID []byte, uptimeHundredths uint32, varbinds []Varbind, buf []byte) (int, error)
    EncodeInform(community string, reqID uint32, trapOID []byte, uptimeHundredths uint32, varbinds []Varbind, buf []byte) (int, error)
    ParseAck(pkt []byte) (reqID uint32, ok bool, err error)
}
```

`SNMPv2cEncoder` is the only implementation for now. v1 / v3 encoders can add themselves later; the interface stays narrow.

**Rationale:** Don't over-design. v1 traps have a different PDU shape (no community-auth equivalence? no, same community; but different varbind layout), and v3 needs engine-id / USM context — those changes will likely want to revise the interface. Keeping it narrow now lets us revise freely when the second implementation arrives. The scheduler and per-device exporter are encoder-agnostic via this interface.

**Alternatives considered:**
- **No interface — inline v2c encoding in `trap_exporter.go`.** Rejected — breaks the `FlowEncoder` precedent and makes future v1/v3 work harder.
- **Over-spec the interface for v1/v3 now.** Rejected — YAGNI, and we don't actually know what v3 wants yet.

### D10. Required varbinds (`sysUpTime.0`, `snmpTrapOID.0`) prepended by encoder

**Decision:** RFC 3416 §4.2.6 mandates the first two varbinds of an SNMPv2-Trap-PDU are `sysUpTime.0` (TimeTicks) and `snmpTrapOID.0` (OID). The catalog author supplies only the trap identity (OID) and body varbinds; `SNMPv2cEncoder.EncodeTrap` prepends the two required varbinds automatically from the arguments. The catalog JSON does NOT list `sysUpTime.0` or `snmpTrapOID.0` — attempting to do so is flagged as an error at catalog load time.

**Rationale:** The two required varbinds are not an authoring concern; they're a wire-format concern. Making the author supply them invites bugs (wrong order, wrong type, forgotten entirely) and clutters the catalog JSON.

**Alternatives considered:**
- **Document that authors must supply them.** Rejected — error-prone.
- **Silently ignore author-supplied copies.** Rejected — masks a bug in the catalog.

### D11. Rate-limit dependency: `golang.org/x/time/rate`

**Decision:** Depend on `golang.org/x/time/rate` for the global token bucket. It's already a transitive dep in many Go projects; likely already in `go.sum` via the gopacket/gosnmp ecosystem. If not, we add it to `go.mod` directly — it's a sub-repository of the Go project with compatibility guarantees.

**Rationale:** A token bucket is ~40 lines to write and ~1 line to import. `x/time/rate` is the canonical choice, has fuzzer-hardened implementation, and is audit-small. The alternative inline implementation would have to match `x/time/rate`'s behaviour anyway (Reserve / Allow / Wait semantics, burst handling).

**Alternatives considered:**
- **Write it inline.** Rejected — maintenance cost without upside.
- **Use `time.Tick` with a fixed-size channel.** Rejected — doesn't handle bursts well and has subtle drift semantics.

### D12. HTTP API: idempotence and async semantics

**Decision:**
- `POST /api/v1/devices/{ip}/trap` is **fire-and-forget** for `mode=trap` and returns `202 Accepted` with `{"requestId": N}`. For `mode=inform`, it's also 202 — the body's requestId refers to the INFORM PDU's request-id, which the operator can correlate with `/api/v1/traps/status` pending-inform state. The endpoint does **not** block on ack.
- `GET /api/v1/traps/status` is idempotent, returns counters (sent, informsPending, informsAcked, informsFailed, informsDropped, rateLimiterTokensAvailable).
- Concurrent POSTs for the same device serialize at the device's `TrapExporter` mutex; no global lock.

**Rationale:** Blocking on ack in a web handler would tie up HTTP worker goroutines for the inform timeout (5s default). 202 + correlation via `/api/v1/traps/status` matches REST conventions for async operations.

**Alternatives considered:**
- **`POST` with `?wait=true` for blocking ack.** Rejected for phase 1 — an anti-pattern that invites timeouts in load tests. Revisit if asked.
- **WebSocket / SSE stream of trap-status events.** Rejected — wildly over-engineered for the use case.

## Risks / Trade-offs

| Risk | Mitigation |
|---|---|
| Global scheduler goroutine is a single point of failure (panic there = no traps) | Standard Go practice: panic recovery in the scheduler loop + `log.Printf`. The scheduler is also a small, well-tested piece; the heap and limiter ops are straightforward. |
| Single scheduler becomes a throughput bottleneck at >100k devices | Sharded scheduler (D1 alternative) is a pure-code change under the `TrapScheduler` interface if profiling shows it's needed. No catalog / encoder / API change required. |
| `text/template` evaluation cost at scale (30k devices × 30s interval × template parse) | Parse each trap's template **once** at catalog load (`text/template.Must(template.New(...).Parse(...))`); evaluation with a pre-parsed `*Template` is ~microseconds. Benchmark this in `trap_catalog_test.go`. |
| INFORM reader goroutines leak on device removal | `TrapExporter.Close()` closes the device socket; reader goroutine exits its `ReadFrom` loop with `net.ErrClosed` and returns. Test this in `trap_exporter_test.go`. |
| Source IP on the wire doesn't match device IP if per-device bind fails (e.g. rp_filter collides, netns issue) | Same mitigation as flow export: log a warning at exporter init. For INFORM mode, startup error rather than warning — INFORM without per-device binding is misbehaviour we should refuse. |
| UDP socket buffer overflow at the collector drops traps silently | Not something the simulator can detect. Documented in CLAUDE.md: operators should size `net.core.rmem_max` at the collector for expected tps. `-trap-global-cap` lets the operator dial back. |
| Global cap isn't actually a hard ceiling under GC pauses (Wait() sleeps through a long STW) | Accept — `x/time/rate.Limiter.Wait` is as tight a bound as the runtime allows. Documented. |
| Catalog parse errors at startup abort the simulator | By design — no traps is better than bad traps. Error message points at the offending JSON path. |
| 30k reader goroutines under INFORM mode at high per-device rate may strain scheduler | Same shape as 30k SNMP server goroutines (per-device listener), which the simulator already runs fine. Regression target: 30k devices × 1 trap/s × 10% inform rate = 3k acks/s aggregate. |
| rp_filter drops UDP/162 from 10.42.0.0/16 at the collector host | Same caveat already documented for flow export in CLAUDE.md. Extend the caveat to traps (same fix: `net.ipv4.conf.*.rp_filter=0` or `2`). |
| Trap PDU encoder bug produces malformed BER that crashes `trapd` | Round-trip tests in `trap_v2c_test.go` decode emitted PDUs with `snmp.go`'s in-process decoder (GetResponse-PDU is structurally identical to Trap-PDU except for tag) and assert field-by-field equality against inputs. |

## Migration Plan

No user-visible migration — this is additive.

**Rollout:**
1. Land all new files and flag wiring in one PR. Feature is off unless `-trap-collector` is set.
2. Validate against OpenNMS trapd in a local docker-compose: 100 devices × 1 trap/s × TRAP mode, verify trapd logs show events per device.
3. Re-run at INFORM mode, verify inform-ack round-trip completes and `/api/v1/traps/status` shows `informsPending` drains.
4. Scale test: 30k devices × `-trap-interval 30s` × `-trap-global-cap 1000`, verify sustained 1000 tps over 10 minutes with no memory growth.
5. Update CLAUDE.md + README.md in the same PR.

**Rollback:** Revert the PR. No state or on-disk migration. Operators running `-trap-collector` fall back to a "flag not recognized" error and must remove the flags.

## Open Questions

1. **Catalog weights / random selection.** Phase 1 fires traps round-robin or fully random from the catalog? Proposal is weighted random (each catalog entry has a `weight` field, default 1); the universal catalog ships with `linkDown`/`linkUp`=40 each, `coldStart`/`warmStart`=5 each, `authenticationFailure`=10. Decision: ship with weighted random, weights in catalog JSON. Revisit if operators want round-robin.
2. **Should `/api/v1/traps/status` be per-device (`/api/v1/devices/{ip}/traps/status`) as well as global?** Phase 1: global only. Revisit if CI assertions need per-device counters.
3. **Universal catalog naming.** `resources/_common/traps.json` introduces a new `_common` subtree. Any objections to the underscore prefix? (The rest of `resources/` is device-type-keyed without prefix.) Alternative: `resources/traps.json` at the top level, no subdirectory. Decision during implementation.
4. **Port 162 default.** `-trap-collector host:162` — should the port be implicit when omitted (`-trap-collector host` defaults to `host:162`)? Convenient but surprising for operators expecting the flow-export semantics where port is required. Decision: require explicit port, match flow-export behaviour.
5. **`snmpTrapEnterprise.0` varbind.** Some collectors expect this third standard varbind for v2c traps converted from v1 (via RFC 3584). Phase 1 omits it. Add later if OpenNMS trapd complains.
