## Context

See `proposal.md` for motivation and `specs/gateway-operations-observability/spec.md` for observable requirements. The gateway already has admin endpoints and console views for dashboard, sessions, trunks, WebSocket clients, instances, diagnostics, logs, and configuration. It also has a bounded asynchronous LogStore event/stats queue and persisted `call_sessions`/`call_stats` models, but runtime code does not currently submit production stats samples. Database-disabled mode returns a non-nil no-op store, so interface presence is not a readiness signal.

This work crosses API, persistence, session snapshots, service readiness, and multiple frontend features. It must preserve Opus passthrough, H.264-only behavior, keyframe recovery, Pion ownership, WebSocket compatibility, and non-blocking RTP/RTCP/SIP loops. Existing generated frontend routes are not edited manually.

## Goals / Non-Goals

**Goals:**

- Establish a single typed operational-health model whose states have consistent semantics across runtime dependencies.
- Make the existing stats persistence path real with one bounded collector and visible backpressure.
- Provide a safe, typed overview of persisted and active sessions without exposing raw internal metadata.
- Turn existing event, payload, stats, trunk, client, and diagnostic data into operator-usable evidence and navigation.
- Keep all new admin contract changes additive and testable independently of media behavior.

**Non-Goals:**

- Replace logs or introduce Prometheus, OpenTelemetry, a time-series database, or an external monitoring dependency.
- Add active call origination, SIP switching, or client simulation controls to the operations console.
- Modify SDP negotiation, codecs, RTP packet handling, SIP state transitions, mobile push routing, or WebSocket call-control payloads.
- Expose secrets, raw credentials, unmasked push tokens, DSNs, arbitrary process environment, or unbounded payload content.
- Redesign all console tables or add a database migration unless implementation discovers a missing index required for the existing queries.

## Decisions

### 1. Add a typed health snapshot and keep the compatibility boolean

Introduce a small health snapshot contract implemented by the LogStore and other runtime services that can report readiness. The admin API assembles component snapshots with a shared state enum: `disabled`, `connected`, `degraded`, `unavailable`, `unknown`. Each component includes only a short reason, last-check/success timestamps, and bounded numeric details.

The existing dashboard `dbConnected` field remains for compatibility and is derived as `database.state == connected`. A new detailed-health endpoint (preferred path `/api/health/details`) carries the component model; the dashboard may include a compact overall state and database state without duplicating the full response.

Readiness checks are cached or bounded. The request handler does not synchronously call a dependency with an unbounded timeout. LogStore exposes whether it is disabled and whether a usable pool exists, plus queue depth/capacity/drop totals. SIP listener, translator, and push readiness are supplied through narrow provider interfaces or immutable snapshots rather than importing their concrete implementations into the API package.

Alternative considered: infer readiness from configured environment and non-nil interfaces. Rejected because configured is not equivalent to usable and the current no-op store proves interface presence is misleading.

### 2. Use one gateway-owned periodic stats collector

Run a single collector goroutine owned by gateway/API lifecycle. On each configured interval it obtains the current session list, requests a new thread-safe observability snapshot from each session, and submits a `StatsRecord` to LogStore. It does not create one goroutine per session and performs no database I/O while holding a session lock.

`RecordStats` remains non-blocking. Both event and stats queues gain atomic accepted/dropped counters and expose depth, capacity, and cumulative drops through LogStore health. A full queue drops the newest sample and marks persistence degraded; it never blocks media forwarding. Collector shutdown follows the server context/stop path and is idempotent.

The first implementation records counters already maintained by `session.Session` plus a reviewed `data` schema for metrics that do not yet have fixed database columns. If stable quality metrics become broadly queried, they can receive typed columns in a later migration. This change does not calculate expensive metrics in packet loops solely for display.

Alternative considered: one ticker goroutine per session. Rejected because lifecycle coordination and goroutine growth are harder to bound. Alternative considered: collect inside RTP/RTCP handlers. Rejected because persistence pressure must not enter hot paths.

### 3. Add a dedicated typed session-overview read path

Add `GET /api/sessions/{sessionId}/overview` for authenticated console use. It reads the persisted session record and, if the session is active on the current instance, overlays a separately typed live snapshot for state and duration. The response groups call timing/outcome, routing/auth identity, and negotiated media fields while keeping JSON fields simple and additive.

Update the LogStore session query/read model to select the already persisted `auth_mode`, `trunk_id`, `trunk_name`, and `sip_username` columns as well as RTP/RTCP/profile fields. Legacy-schema fallback remains supported: missing or zero fields are omitted or returned as neutral values. Raw `meta` is not returned. In particular, `accountKey`, credentials, passwords, and tokens are never exposed through the overview.

The existing `/api/session/{sessionId}` active-only endpoint and `/api/sessions/history` list remain compatible. The history list may gain selected additive routing fields useful for filtering/columns, but the complete evidence belongs in the overview endpoint to avoid bloating every row.

Alternative considered: expose the persistence model directly. Rejected because it contains implementation-oriented metadata and would couple public JSON to database structure.

### 4. Render evidence through bounded, explicit detail views

Add an Overview tab to Session Detail and keep Events, Payloads, Dialogs, Stats, and Client Diagnostics as separate evidence sources. Events show an expandable formatted `data` object and SIP Call-ID. Payload dialogs offer Raw and Parsed views; binary payloads show encoding and decoded byte size with an explicit copy/download action rather than rendering base64 in the table. Stats show fixed counters and a formatted details panel for the reviewed quality schema.

Frontend formatters must tolerate absent legacy data and unknown future keys. Large JSON/raw views receive size/line bounds and copy actions. Values are treated as text; no backend-provided HTML is rendered.

Alternative considered: add every nested field as a table column. Rejected because it makes tables unusable and creates brittle coupling to diagnostic payload evolution.

### 5. Add optional columns and stable cross-resource links

Existing table-column visibility patterns are reused for lower-frequency fields. Active Sessions adds SIP Call-ID, SIP username, timestamps, trunk identity, and translator voice as optional details. WS Clients adds connection age, multi-call mode, and active-call count. Trunks adds last-online platform/time and updated time. Client Diagnostics adds app version, device hash, auth realm, and record ID.

Links use stable identifiers already present in responses: session ID opens Session Detail, trunk public ID (falling back to numeric ID) opens the Trunks page with a server-supported filter, and instance ownership opens or filters Instances where possible. Navigation does not synthesize credentials or place secrets in URLs.

Alternative considered: duplicate full related-resource cards into each page. Rejected because links preserve a single source of truth and reduce repeated polling.

### 6. Preserve protocol boundaries and test contracts at response edges

New routes live only under authenticated `/api`. No WebSocket message types or mobile client requirements change. Backend tests assert health state mapping, secret exclusion, session overview shaping, legacy empty values, queue-drop behavior, collector shutdown, and no blocking enqueue. Frontend tests assert fallback rendering, structured evidence, optional fields, and link construction.

No generated route file is edited manually; the normal TanStack build may regenerate it only if a new file-based route is actually introduced. The preferred UI change reuses the existing session-detail route and feature pages, so a new route should not be necessary.

## Risks / Trade-offs

- **[Periodic collection adds lock and database pressure]** → Use one ticker, short snapshot locks, bounded queues, existing batch persistence, configurable interval, and measurable drops.
- **[A health endpoint becomes a secret-discovery surface]** → Use typed allowlisted fields, authenticated routing, short sanitized reasons, and tests that reject DSN/token/credential material.
- **[Health states flap during transient failures]** → Report last check/success timestamps and keep component states independent; avoid deriving overall health from a single transient request.
- **[Legacy rows or schemas lack newer fields]** → Preserve fallback reads and render absent values safely rather than failing the overview.
- **[Raw diagnostic data can freeze the browser]** → Bound preview sizes, expand on demand, avoid direct binary rendering, and paginate existing evidence lists.
- **[Stats `data` becomes an unversioned dumping ground]** → Define and test a small reviewed schema; promote stable frequently queried metrics to typed fields only through a future explicit migration.
- **[Cross-page links depend on server filters]** → Use existing `sessionId`, `trunkId`, and `trunkPublicId` filters and add only backward-compatible query handling where missing.

## Migration Plan

1. Add health/readiness interfaces and correct the compatibility `dbConnected` mapping without changing existing route fields.
2. Add queue metrics and the single stats collector behind existing database-enable and interval configuration; verify disabled mode performs no persistence work.
3. Extend LogStore session reads and introduce the typed overview endpoint with legacy-row tests.
4. Add frontend Overview/evidence rendering, optional metadata columns, and stable navigation links.
5. Update admin/operations documentation and deploy normally; no database migration or client coordination is required because contracts are additive.
6. Monitor stats queue depth/drop totals and database write load after rollout. Increase the interval or disable collection through existing DB configuration if pressure is unacceptable.

Rollback returns to the prior image. Existing `call_stats` rows and additive response consumers remain harmless; no database rollback is required.

## Open Questions

- Which reviewed quality metrics beyond the existing PLI and RTCP counters can be obtained cheaply from the current session state without introducing packet-path aggregation? Implementation should include only metrics already available safely and document any deferred metrics.
