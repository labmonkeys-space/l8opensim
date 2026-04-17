
## Deferred from: code review of technical-netflow-v9-ipfix-l8opensim-research-2026-04-16 PR #33 (2026-04-16)

## Deferred from: code review of Phase 3 ipfix.go (2026-04-16)

- **IPFIX sequence number counts datagrams not records** — RFC 7011 §3.1 requires SeqNo to advance by number of Data Records in the message, not by 1 per datagram. Same as NF9 encoder; simulation-grade tolerable. [`flow_exporter.go:121`]
- **IE field widths narrower than IANA defaults** — tcpControlBits 1B (IANA: 2B); ingressInterface/egressInterface 2B (IANA: 4B); bgpSourceAsNumber/bgpDestinationAsNumber 2B (IANA: 4B). Collectors decode correctly via template advertisement; conscious NF9 parity trade-off. [`ipfix.go:ipfixFields`]
- **`octetDeltaCount` (IE 1) 4B instead of IANA `unsigned64` 8B** — same decision as NF9 IN_BYTES; values >4 GB clamped. GPU/Storage profiles can exceed 4 GB. [`ipfix.go:encodeIPFIXRecord`]
- **`EndMs < StartMs` propagates to IPFIX wire** — pre-existing in `flow_cache.go`; `FlowCache.Add()` sets EndMs unconditionally without inversion guard. Violates RFC 7011 §3.7 on the wire. [`flow_cache.go:105`]

## Deferred from: code review of technical-netflow-v9-ipfix-l8opensim-research-2026-04-16 PR #33 (2026-04-16)

- **`/api/v1/flows/status` endpoint** — Phase 2 spec table listed it in `api.go`, but Phase 4 acceptance table is authoritative. Handler would be skeletal without per-device stats counters; belongs in Phase 4 Polish alongside perf benchmarks and docs. [`api.go`, `web.go`]
- **~45–90 MB heap overhead at 30k devices** — Each `FlowCache` allocates per-device memory. Pre-existing design trade-off between realism and memory. Consider lazy init or smaller profiles if scale-to-30k becomes a priority. [`flow_cache.go`, `flow_exporter.go`]
- **`FlowEncoder.EncodePacket` extra `uptimeMs` parameter** — IPFIX encoders can ignore it; conscious decision for NF9/IPFIX parity in the shared interface. Revisit if adding more protocol encoders that find the param confusing. [`flow_exporter.go:33–35`]
- **`NewFlowExporter` per-device timeout params** — Redundant with `SimulatorManager` fields; harmless but could be simplified. [`flow_exporter.go:54`]
- **`FlowExporter.templateInterval` per-device field** — Minor memory overhead (~240 kB at 30k devices); central `SimulatorManager.flowTemplateInterval` already exists. [`flow_exporter.go:48`]
- **`InitFlowExport` does not propagate `templateInterval` change to live devices** — Runtime reconfiguration won't affect already-running exporters. Relevant only if hot-reconfiguration is added later. [`flow_exporter.go:147`]

## Deferred from: code review of Phase 4 flows/status endpoint (2026-04-17)

- **`flowProtocol`/`flowCollectorStr` unprotected strings** — These plain `string` fields are written by `InitFlowExport` and read by `GetFlowStatus` without any lock. Currently safe because the atomic happens-before chain (`flowActive.Store` after writes, `flowActive.Load` before reads) provides ordering, and the re-init guard prevents concurrent writes. Becomes a real race if Shutdown/re-init is enabled. Should be protected by `sm.mu` when re-init is allowed. [`types.go:179-180`, `flow_exporter.go:309-310`]
- **Counter gate couples three independent atomics** — `if totalPackets > 0` gates all three counter updates in `tickAllFlowExporters`. Invariant holds today (PacketsSent incremented before RecordsSent), but future refactoring could silently drop record counts. Consider updating counters independently. [`flow_exporter.go:276-280`]
- **`flowStatLastTmpl` unconditional Store is non-monotonic** — `tickAllFlowExporters` calls `sm.flowStatLastTmpl.Store(lastTemplMs)` without a compare-and-swap. Safe today (single ticker goroutine), but would roll the timestamp back if a second ticker were ever started. Use an atomic CAS loop if concurrent tickers are added. [`flow_exporter.go:281-283`]
- **Silent record drop on `EncodePacket` error** — When `EncodePacket` returns an error, remaining `expired` records in the current pagination loop are discarded without incrementing counters or logging. Pre-existing behaviour from earlier phases. [`flow_exporter.go:139-141`]
- **`cap` variable shadows built-in `cap()` function** — In `Tick()`, `cap := (len(buf) - overhead) / recSize` shadows the built-in. Pre-existing, not introduced by this diff. Will surface under `go vet -shadow`. [`flow_exporter.go:125`]
- **HTTP 200 returned for both enabled and disabled flow status** — `flowStatusHandler` returns `{"success":true, "data":{"enabled":false}}` with HTTP 200 when flow export is off. Consistent with the project's existing API pattern but non-RESTful. Low priority; document the behaviour. [`web.go:90-93`]

## Deferred from: code review of technical-netflow-v9-ipfix-l8opensim-research-2026-04-16 (2026-04-16)

- **Phase 2 goroutine will have no shutdown channel** — `DeviceSimulator` has no `stopCh chan struct{}` or `context.Context`. A FlowExporter goroutine wired in Phase 2 will leak after `device.Stop()`, keeping the device's FlowCache and UDP socket alive. Add `stopCh chan struct{}` to `DeviceSimulator` and close it in `Stop()` before Phase 2 goroutines are launched. [`types.go`, `device.go`]
