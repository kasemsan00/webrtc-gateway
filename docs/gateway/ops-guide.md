# Gateway Operations Guide

For agents debugging live gateway behavior without host or database credentials.

---

## Centralized observability (Collector to OpenObserve)

The optional gateway observability path is:

```text
gateway --OTLP--> OpenTelemetry Collector --OTLP/HTTPS--> existing OpenObserve
```

OpenObserve credentials exist only on the Collector. PostgreSQL LogStore and
the local `/api/logs/*` endpoints remain independent and authoritative for their
existing purposes. Full configuration, stream/RBAC/retention guidance and both
exclusive log-source examples are in
[`deploy/observability/README.md`](../../deploy/observability/README.md).

Enable and verify:

1. Select exactly one reference config: Kubernetes stdout, or Docker/host
   read-only gateway files. Never mount the Docker socket or combine both log
   sources.
2. Inject the existing OpenObserve endpoint, organization, per-signal streams,
   TLS settings and authorization into the Collector from a secret store.
3. Run `pwsh deploy/observability/verify.ps1`, deploy the Collector, then verify
   its cached health on port 13133 and scrape self-metrics on port 8888.
4. Enable one non-production gateway with `OTEL_ENABLE=true` and its Collector
   endpoint/protocol. Generate one HTTP/WS call flow and find logs, metrics and
   sampled traces with matching service, environment and instance fields.
5. Verify only one `log.source`, no seeded secret, no packet-level span, no
   high-cardinality metric label, and no regression to local logs, LogStore,
   call control or media before expanding rollout.

Failure diagnosis:

1. Read cached gateway telemetry health; the health request never probes the
   Collector. Check local cumulative drops/failures and last-success freshness.
2. Check Collector health, self-metrics, exporter queue/capacity, send failures,
   retries and persistent-storage free space. Alert on refused records, queue
   growth, gateway drops, stale export and export failures.
3. Verify Collector-to-OpenObserve DNS/TLS, endpoint, organization, stream
   permission and injected authorization without printing the credential.
4. Leave active calls alone. Collector/OpenObserve failure must degrade or drop
   bounded telemetry, never interrupt SIP/WebSocket/media work.

Disable and roll back:

1. Set `OTEL_ENABLE=false` and restart gateway instances one at a time.
2. Verify local logs, `/api/logs/*`, PostgreSQL session APIs, SIP/WS control,
   resume and Opus/H.264 media continue normally.
3. Stop the Collector after its bounded drain deadline. Preserve its file-backed
   queue until rollback is final; deleting it discards buffered telemetry.
4. Re-enable by restoring the last validated Collector config and secret,
   starting the Collector, then enabling one gateway canary first. No database,
   frontend or protocol migration is involved.

---

## Direct database access for agent work

- When work on `apps/gateway` needs live database data or schema verification, read from the MCP/database connection named `database-dev-k2-gateway`.
- Treat direct database queries as read-only unless the user explicitly asks for a write or migration.
- Prefer source-controlled schema files (`init.sql`, `migrations/`) for expected structure, then use `database-dev-k2-gateway` only to verify the live dev state.

## Operational log access for agents

Default public gateway base URL:

- `https://gateway.example.com`

Use the HTTP API before asking the user for host or database access.

Gateway process logs:

- `GET /api/logs` publicly lists gateway-managed
  `webrtc-sip-gateway-*.log` files without a bearer token.
- `GET /api/logs/current?tail=500` publicly reads the current gateway log tail
  without a bearer token.
- `GET /api/logs/{name}?tail=500` publicly reads a selected gateway log file
  tail without a bearer token.

Runtime configuration (read-only):

- `GET /api/config` returns the effective gateway configuration loaded from environment variables at startup.
- Sensitive values (passwords, DSN credentials, push credential file paths, OAuth client secrets) are redacted in the response.
- Requires the same bearer auth as other protected `/api/*` routes whenever a
  JWT verifier or `FRONTEND_PASSWORD` is configured. The bearer value may be a
  valid JWT or the exact configured admin password.
- The frontend ops UI exposes this at `/settings`.

## Operational health and session evidence

- `GET /api/health/details` returns authenticated, cached operational health.
  Component states are `disabled`, `connected`, `degraded`, `unavailable`, or
  `unknown`. The response contains only allowlisted readiness facts, queue
  depth/capacity, accepted/dropped totals, and freshness timestamps; it never
  includes a DSN, credentials, tokens, payload bodies, or push identifiers.
- `GET /api/dashboard` keeps the existing `dbConnected` field. It is `true`
  only when persistent database logging is actually connected; `DB_ENABLE=false`
  reports `false` even though the gateway uses an internal no-op LogStore.
- `GET /api/sessions/{sessionId}/overview` is an authenticated, typed view of
  safe persisted call timing/outcome, SIP identity, auth/trunk routing, media
  profiles, Opus payload type, RTP/RTCP ports, and video-rejection evidence.
  Raw session metadata, account keys, passwords, and tokens are deliberately
  excluded.
- When `DB_ENABLE=true`, one gateway-owned collector records an active-session
  snapshot every `DB_STATS_INTERVAL_MS` (minimum one second). It stores existing
  PLI/keyframe and inbound RTCP report counters using the bounded stats queue.
  A non-zero `statsQueue.dropped` value indicates persistence backpressure;
  samples are dropped rather than delaying media or SIP processing.

WebSocket clients real-time stream:

- `GET /api/ws-clients/stream` — SSE stream of WS client connect/disconnect/update events (each event carries the full `WSClientResponse`).

TTRS VRI client diagnostics uploaded to gateway:

- `POST /api/client-diagnostics` accepts authenticated mobile uploads, max 100 events/request.
- `GET /api/client-diagnostics?page=1&pageSize=100` reads non-session TTRS VRI diagnostics from all clients.
- Optional list filters: `clientTraceId`, `authSubject`, `source`, `level`, `name`.
- `GET /api/client-diagnostics/sessions/{sessionId}/events?page=1&pageSize=100` reads TTRS VRI diagnostics stored as `call_events` category `client`.
- `GET /api/client-diagnostics/sessions/{sessionId}/payloads?page=1&pageSize=100` lists large session diagnostics payloads.
- `GET /api/client-diagnostics/payloads/{payloadId}` reads one stored diagnostics payload.
- The `GET /api/client-diagnostics*` read endpoints are intentionally available without a bearer token.

PowerShell examples:

```powershell
$base = "https://gateway.example.com"
Invoke-RestMethod "$base/api/logs" | ConvertTo-Json -Depth 8
Invoke-RestMethod "$base/api/logs/current?tail=500" | ConvertTo-Json -Depth 8
Invoke-RestMethod "$base/api/logs/<log-file-name>?tail=500" | ConvertTo-Json -Depth 8
$token = "<jwt-or-frontend-password>"
$headers = @{ Authorization = "Bearer $token" } # required by protected non-log APIs
Invoke-RestMethod "$base/api/client-diagnostics?page=1&pageSize=100" -Headers $headers | ConvertTo-Json -Depth 12
Invoke-RestMethod "$base/api/client-diagnostics?level=error&page=1&pageSize=100" -Headers $headers | ConvertTo-Json -Depth 12
Invoke-RestMethod "$base/api/client-diagnostics/sessions/<sessionId>/events?page=1&pageSize=100" -Headers $headers | ConvertTo-Json -Depth 12
```

Use a real session ID in session URLs. The placeholder `<sessionId>` or encoded
`%3CsessionId%3E` is not meaningful and should return no matching events.
