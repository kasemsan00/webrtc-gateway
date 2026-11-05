# Gateway Operations Guide

For agents debugging live gateway behavior without host or database credentials.

---

## Direct database access for agent work

- When work on `apps/gateway` needs live database data or schema verification, read from the MCP/database connection named `database-dev-k2-gateway`.
- Treat direct database queries as read-only unless the user explicitly asks for a write or migration.
- Prefer source-controlled schema files (`schema/bootstrap-baseline.sql` + `migrations/`, applied via `cmd/db-bootstrap`) as the complete authoritative schema (`init.sql` alone is an older bootstrap file and lacks newer columns such as `call_sessions` auth columns), then use `database-dev-k2-gateway` only to verify the live dev state.

## Operational log access for agents

Default public gateway base URL:

- `https://gateway.example.com`

Use the HTTP API before asking the user for host or database access.

Gateway process logs:

- `GET /api/logs` publicly lists gateway-managed
  `webrtc-sip-gateway-*.log` files in `./logs` (retaining at most 10 log files, newest first) without a bearer token.
- `GET /api/logs/current?tail=500` publicly reads the current gateway log tail
  without a bearer token (`tail` defaults to `500`, clamps above `5000` to `5000`, and returns `400` if `<= 0` or non-numeric).
- `GET /api/logs/{name}?tail=500` publicly reads a selected gateway log file
  tail without a bearer token (same `tail` default `500` and max `5000`).

Runtime configuration (read-only):

- `GET /api/config` returns the effective gateway configuration loaded from `internal/config/config.go` at startup (variables read outside `config.go` such as `SIP_FORCE_AVP`, `SIP_VIDEO_REORDER_TIMEOUT_MS`, `PUSH_DEBUG_TTRS_TOKEN`, and `DB_BOOTSTRAP_*` are not included).
- Sensitive values (passwords, DSN credentials, push credential file paths, OAuth client secrets) are redacted in the response.
- Requires the same bearer auth as other protected `/api/*` routes whenever a
  JWT verifier (`AUTH_ENABLE=true`) or `FRONTEND_PASSWORD` is configured. The bearer value may be a
  valid JWT or the exact configured admin password.
- The frontend ops UI exposes this at `/settings`.

## Operational health and session evidence

- `GET /api/health/details` returns authenticated, cached operational health.
  Component states are `disabled`, `connected`, `degraded`, `unavailable`, or
  `unknown`. Component `details` maps expose only the allowlisted keys `depth`,
  `capacity`, `accepted`, `dropped`, `exported`, and `failed`, alongside
  readiness/freshness fields; the response never includes a DSN, credentials,
  tokens, payload bodies, or push identifiers. Note that `sipListener`,
  `translator` (when enabled), and `push` (when enabled) currently report
  `"state": "unknown"` (`"reason": "readiness_not_reported"`) because
  `SetOperationalHealthProvider` is not wired in `main.go`.
- Request-log / HTTP telemetry exclusions (`requestLogMiddleware`): `/ws*`,
  `/api/logs*`, `/api/version`, and `/api/health*` (including `/api/health/details`)
  are excluded from per-request HTTP telemetry logs.
- `GET /api/version` returns the running build without auth (even when `API_ENABLE_REST=false`), and is excluded from HTTP telemetry: `{"version","commit","branch","buildTime","dirty","goVersion"}`. Docker builds get the values from the `GIT_COMMIT`, `GIT_BRANCH`, `GIT_VERSION`, `BUILD_TIME`, `GIT_DIRTY` build args (they are also the image's `org.opencontainers.image.revision|version|created` labels). **Both** `docker-ci.sh` and `docker-ci.ps1` pass those build args from the local git checkout. A local `go build` without `-ldflags` falls back to Go's embedded `vcs.*` info (`version` defaults to `dev`; anything missing reads `unknown`). The same values are logged once at startup as `gateway_version version=... commit=... branch=... build_time=... dirty=... go=...`, whereas structured JSON telemetry records (`telemetry.NewStructuredLogger`) currently emit `"service.version":"development"`.
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

Admin test incoming push (no SIP session):

- `POST /api/trunk/{id}/test-incoming-push` sends an `incoming_call` FCM/APNs payload for that trunk. Body `{ "style": "data" | "message" }` defaults to `data` (Android data-only). `message` also includes a visible notification title/body. It does not create a SIP or WebRTC session and does not send a WebSocket `incoming`. Answering on the device will expire. Requires the same bearer auth as other trunk write routes. The Trunks UI exposes this as **Test push**.

TTRS VRI client diagnostics uploaded to gateway:

- `POST /api/client-diagnostics` accepts authenticated mobile uploads (when REST auth is enabled): max body `128 KiB`, max `100` events/request, rate-limited to `30` requests/minute per JWT `sub` (or client IP when unauthenticated), returning `202 Accepted`. When a `sessionId` is supplied and the serialized batch JSON is `>= 2048` bytes, the gateway also archives a `client_diagnostics_batch` row in `call_payloads`.
- `GET /api/client-diagnostics?page=1&pageSize=100` reads non-session TTRS VRI diagnostics from all clients.
- Optional list filters on `GET /api/client-diagnostics`: `clientTraceId`, `authSubject`, `source`, `level`, `name`.
- `GET /api/client-diagnostics/sessions/{sessionId}/events?page=1&pageSize=100` reads TTRS VRI diagnostics stored as `call_events` category `client` (supports optional `?name=` filter).
- `GET /api/client-diagnostics/sessions/{sessionId}/payloads?page=1&pageSize=100` lists archived session diagnostics payloads (`client_diagnostics_batch`).
- `GET /api/client-diagnostics/payloads/{payloadId}` reads one stored diagnostics payload.
- The four `GET /api/client-diagnostics*` read endpoints are intentionally public (available without a bearer token).

PowerShell examples (public read endpoints — no bearer header needed):

```powershell
$base = "https://gateway.example.com"
Invoke-RestMethod "$base/api/logs" | ConvertTo-Json -Depth 8
Invoke-RestMethod "$base/api/logs/current?tail=500" | ConvertTo-Json -Depth 8
Invoke-RestMethod "$base/api/logs/<log-file-name>?tail=500" | ConvertTo-Json -Depth 8
Invoke-RestMethod "$base/api/client-diagnostics?page=1&pageSize=100" | ConvertTo-Json -Depth 12
Invoke-RestMethod "$base/api/client-diagnostics?level=error&page=1&pageSize=100" | ConvertTo-Json -Depth 12
Invoke-RestMethod "$base/api/client-diagnostics/sessions/<sessionId>/events?page=1&pageSize=100" | ConvertTo-Json -Depth 12
```

Use a real session ID in session URLs. The placeholder `<sessionId>` or encoded
`%3CsessionId%3E` is not meaningful and should return no matching events.
