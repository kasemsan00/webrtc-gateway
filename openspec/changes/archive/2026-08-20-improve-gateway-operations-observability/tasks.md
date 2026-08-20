## 1. Operational Health Contracts

- [x] 1.1 Define the shared typed component-health states (`disabled`, `connected`, `degraded`, `unavailable`, and `unknown`) and bounded snapshot fields for readiness, reasons, freshness, and numeric details.
- [x] 1.2 Add LogStore readiness reporting that distinguishes the disabled no-op store from a configured usable database pool without exposing connection configuration.
- [x] 1.3 Derive the existing dashboard `dbConnected` field from connected persistence readiness and add a focused test proving database-disabled mode returns `dbConnected: false`.
- [x] 1.4 Expose accepted, dropped, depth, and capacity metrics for the bounded event and stats persistence queues using concurrency-safe counters.
- [x] 1.5 Add narrow cached or bounded readiness providers for configured SIP listeners, translator connectivity, push providers, and gateway/session-directory freshness.
- [x] 1.6 Implement the authenticated `GET /api/health/details` endpoint and assemble independent component snapshots without unbounded dependency calls.
- [x] 1.7 Add API tests for connected, disabled, degraded, unavailable, and unknown mappings, including persistence-drop degradation and compatibility-field behavior.
- [x] 1.8 Add response-safety tests proving detailed health excludes credentials, DSNs, tokens, push identifiers, payload bodies, and unbounded resource identifiers.

## 2. Periodic Session Statistics

- [x] 2.1 Define a thread-safe session observability snapshot containing the existing media-recovery and RTCP counters plus only reviewed quality metrics already available outside packet hot paths.
- [x] 2.2 Implement one gateway-owned periodic collector that enumerates active sessions at `DB_STATS_INTERVAL_MS` and submits snapshots through the existing `RecordStats` path without per-session goroutines.
- [x] 2.3 Wire collector startup and idempotent shutdown to gateway lifecycle, and make database-disabled mode skip collection and persistence work cleanly.
- [x] 2.4 Preserve non-blocking enqueue semantics so a full stats queue drops the newest sample, increments the cumulative drop counter, and never blocks SIP or RTP/RTCP processing.
- [x] 2.5 Add focused tests for periodic record contents, disabled persistence, full-queue drop accounting, and non-blocking enqueue behavior.
- [x] 2.6 Add lifecycle tests proving collector cancellation stops the ticker/work loop without leaking per-session or collector goroutines.

## 3. Persisted Session Overview API

- [x] 3.1 Extend LogStore session reads to select the persisted auth mode, trunk ID/name, SIP username, RTP/RTCP ports, Opus payload type, media profiles, video rejection, timing, and outcome fields required by the overview.
- [x] 3.2 Add a typed LogStore read operation for one persisted session while preserving neutral/absent values for legacy rows and schemas.
- [x] 3.3 Define an allowlisted session-overview API response grouped around call timing/outcome, routing/auth identity, and negotiated media evidence, without returning raw `meta`.
- [x] 3.4 Implement authenticated `GET /api/sessions/{sessionId}/overview`, including a typed live-state overlay when the session is active on the current instance.
- [x] 3.5 Keep `/api/session/{sessionId}` and `/api/sessions/history` compatible, adding only selected optional routing fields to history if the frontend needs them for filtering or links.
- [x] 3.6 Add API and LogStore tests for complete current rows, absent legacy fields, unknown sessions, active overlays, and stable existing response fields.
- [x] 3.7 Add explicit overview safety tests proving passwords, account keys, credentials, raw tokens, push tokens, and unreviewed metadata never appear in serialized responses.

## 4. Session Evidence Frontend

- [x] 4.1 Add frontend types and service methods for detailed health and typed session overview responses with safe fallback handling for absent legacy fields.
- [x] 4.2 Add an Overview tab to the existing Session Detail route for timing, outcome, SIP identity, auth/trunk routing, negotiated profiles, payload type, media ports, and video rejection; do not add a new route or manually edit `routeTree.gen.ts`.
- [x] 4.3 Enhance Events so SIP Call-ID and the presence of structured `data` are visible and bounded formatted details can be expanded and copied.
- [x] 4.4 Enhance Payloads with distinct raw and parsed views plus bounded binary metadata and explicit copy/download actions instead of rendering base64 directly in the table.
- [x] 4.5 Enhance Stats to show timestamped PLI, recovery, and RTCP counters plus a bounded details view for the reviewed quality-metric schema.
- [x] 4.6 Add focused Vitest coverage for overview fallback rendering, event data, parsed/text/binary payload evidence, stats evidence, expansion bounds, and text-only rendering of backend values.

## 5. Operational Metadata and Navigation

- [x] 5.1 Surface Active Sessions metadata for SIP Call-ID, trunk identity, SIP username, translator voice, and created/updated times using optional columns or details.
- [x] 5.2 Surface WS Clients metadata for connected time or age, multi-call mode, and active-call count.
- [x] 5.3 Surface Trunks metadata for last-online platform/time and updated time while continuing to omit push tokens.
- [x] 5.4 Surface Client Diagnostics metadata for record ID, auth realm, app version, device hash, trace identity, and a related session when available.
- [x] 5.5 Add stable links from session IDs, trunk public/numeric IDs, and instance ownership to the existing detail or filtered views, adding only backward-compatible query filtering where it is missing.
- [x] 5.6 Add frontend tests for optional-column visibility, missing-value fallbacks, filters, and cross-resource link construction without credentials or secrets in URLs.

## 6. Documentation and Compatibility Verification

- [x] 6.1 Document the authenticated operational-health and session-overview REST contracts, health-state meanings, queue/drop interpretation, stats interval behavior, and reviewed quality metrics.
- [x] 6.2 Verify REST additions remain backward compatible and confirm no WebSocket call-control contract changed; update `docs/gateway/ws-contract.md` only if implementation actually changes that contract.
- [x] 6.3 Run `go test ./...` from `apps/gateway` and resolve failures with focused tests in the affected packages.
- [x] 6.4 Run the relevant frontend Vitest suites plus `pnpm check-types`, `pnpm lint`, and `pnpm build` from the repository root.
- [x] 6.5 Review the implementation diff to confirm there are no changes to SIP state behavior, SDP negotiation, Opus passthrough, H.264-only handling, SPS/PPS or keyframe recovery, RTP/RTCP forwarding, Pion ownership, database schema identity, or authentication policy.
