# Gateway Operations Guide

For agents debugging live gateway behavior without host or database credentials.

---

## Direct database access for agent work

- When work on `apps/gateway` needs live database data or schema verification, read from the MCP/database connection named `database-dev-webrtc-sip-gateway`.
- Treat direct database queries as read-only unless the user explicitly asks for a write or migration.
- Prefer source-controlled schema files (`init.sql`, `migrations/`) for expected structure, then use `database-dev-webrtc-sip-gateway` only to verify the live dev state.

## Operational log access for agents

Default public gateway base URL:

- `https://gateway.example.com`

Use the HTTP API before asking the user for host or database access.

Gateway process logs:

- `GET /api/logs` lists gateway-managed `webrtc-sip-gateway-*.log` files.
- `GET /api/logs/current?tail=500` reads the current gateway log tail.
- `GET /api/logs/{name}?tail=500` reads a selected gateway log file tail.
- When API auth is enabled, these endpoints require the same bearer token as other `/api/*` operations endpoints. They remain public only when no token verifier is configured.

Runtime configuration (read-only):

- `GET /api/config` returns the effective gateway configuration loaded from environment variables at startup.
- Sensitive values (passwords, DSN credentials, push credential file paths, OAuth client secrets) are redacted in the response.
- Requires the same bearer JWT auth as other `/api/*` ops endpoints when `AUTH_ENABLE=true`.
- The frontend ops UI exposes this at `/settings`.

WebSocket clients real-time stream:

- `GET /api/ws-clients/stream` — SSE stream of WS client connect/disconnect/update events (each event carries the full `WSClientResponse`).

Softphone mobile diagnostics uploaded to gateway:

- `POST /api/client-diagnostics` accepts authenticated mobile uploads, max 100 events/request.
- `GET /api/client-diagnostics?page=1&pageSize=100` reads non-session mobile diagnostics from all clients.
- Optional list filters: `clientTraceId`, `authSubject`, `source`, `level`, `name`.
- `GET /api/client-diagnostics/sessions/{sessionId}/events?page=1&pageSize=100` reads mobile diagnostics stored as `call_events` category `client`.
- `GET /api/client-diagnostics/sessions/{sessionId}/payloads?page=1&pageSize=100` lists large session diagnostics payloads.
- `GET /api/client-diagnostics/payloads/{payloadId}` reads one stored diagnostics payload.
- The `GET /api/client-diagnostics*` read endpoints are intentionally available without a bearer token.

PowerShell examples:

```powershell
$base = "https://gateway.example.com"
$token = "<jwt>"
$headers = @{ Authorization = "Bearer $token" } # required when API auth is enabled
Invoke-RestMethod "$base/api/logs/current?tail=500" -Headers $headers | ConvertTo-Json -Depth 8
Invoke-RestMethod "$base/api/client-diagnostics?page=1&pageSize=100" -Headers $headers | ConvertTo-Json -Depth 12
Invoke-RestMethod "$base/api/client-diagnostics?level=error&page=1&pageSize=100" -Headers $headers | ConvertTo-Json -Depth 12
Invoke-RestMethod "$base/api/client-diagnostics/sessions/<sessionId>/events?page=1&pageSize=100" -Headers $headers | ConvertTo-Json -Depth 12
```

Use a real session ID in session URLs. The placeholder `<sessionId>` or encoded
`%3CsessionId%3E` is not meaningful and should return no matching events.
