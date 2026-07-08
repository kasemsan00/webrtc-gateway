# WS Clients Real-Time View Design

## Goal

Add a new admin page showing all connected WebSocket clients in real time with their resolved trunk ID/UUID, availability, call state, and auth subject, plus admin hangup/DTMF actions for active calls.

## Background

The gateway already tracks per-connection state (`WSClient.trunkResolved`, `resolvedTrunkID`, `availability`, `callState`, `authClaims`) and exposes a snapshot via `GET /api/ws-clients`. The Instances page shows a small preview (`WSClientCard`, max 8). There is no dedicated real-time view and no SSE stream for WS client changes; the Instances page polls `/api/dashboard` + `/api/ws-clients` every 30s.

## Architecture

- **Gateway:** Currently `GET /api/ws-clients` iterates `s.wsClients` (keyed by sessionID), so only WS clients that have placed/received a call show up — idle trunk-resolved clients are invisible. Fix: add a `clientID` (UUID, `github.com/google/uuid` already in `go.mod`) to `WSClient` assigned at connect, change `handleListWSClients` to iterate `s.wsConnections` (all connected WS), and add `ClientID` + `SessionID` (optional) to `WSClientResponse`. Then add SSE endpoint `/api/ws-clients/stream` mirroring the existing `sessions/stream`/`trunks/stream` pattern: emit `"connected"` on subscribe, then `"ws-client"` events carrying the full `WSClientResponse` whenever a WSClient connects, disconnects, or its `trunkResolved`/`resolvedTrunkID`/`availability`/`callState`/`sessionID` changes.
- **Frontend:** New page `/ws-clients` with menu entry "WS Clients" between Active Sessions and Gateway Logs. Subscribe to the SSE stream, upsert/remove rows by `clientID`, show a trunk badge per row, and reuse the existing `hangupSession`/`sendSessionDtmf` helpers (from `active-sessions/services/session-control-api`) for active rows (callState not idle). Fallback to polling `/api/ws-clients` every 10s when the stream drops.

## Components

| File | Responsibility |
|------|----------------|
| `apps/gateway/internal/api/handlers_sse.go` | `WSClientStreamEvent`, `notifyWSClientChanged`, `handleWSClientsStream` |
| `apps/gateway/internal/api/ws_util.go` | `subscribeWSClientStream`, `unsubscribeWSClientStream`, `broadcastWSClientStream` |
| `apps/gateway/internal/api/server.go` | `wsClientStreams`/`wsClientStreamSeq` fields, `NewServer` init, `ClientID` on `WSClient`, route `/ws-clients/stream`, `ClientID`/`SessionID` on `WSClientResponse` |
| `apps/gateway/internal/api/handlers_ops.go` | change `handleListWSClients` to iterate `wsConnections`; add `ClientID`/`SessionID`/`PublicOnly` to `WSClientResponse` |
| `apps/gateway/internal/api/ws_conn.go` | assign `clientID` (uuid) at connect; call `notifyWSClientChanged` on register + disconnect |
| `apps/gateway/internal/api/ws_trunk.go` | call `notifyWSClientChanged` after `trunkResolved=true` |
| `apps/gateway/internal/api/ws_midcall.go` | call `notifyWSClientChanged` after `availability`/`callState` set |
| `apps/gateway/internal/api/ws_call.go` | call `notifyWSClientChanged` on session register + trunk reset |
| `apps/gateway/internal/api/ws_resume.go` | call `notifyWSClientChanged` on register |
| `apps/gateway/internal/api/ws_incoming.go` | call `notifyWSClientChanged` on register |
| `apps/frontend/src/features/ws-clients/types.ts` | `WSClient` type (shared shape with gateway-instances) |
| `apps/frontend/src/features/ws-clients/services/ws-clients-api.ts` | `fetchWSClients`, `subscribeWSClientEvents` |
| `apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx` | real-time table + trunk badge + actions |
| `apps/frontend/src/routes/ws-clients.tsx` | route |
| `apps/frontend/src/components/Header.tsx` | menu entry |

## Data flow

```
WSClient register / trunk resolve / call-state change / disconnect
  → s.notifyWSClientChanged(eventType, &sessionID)
  → broadcastWSClientStream(payload)   (full WSClientResponse snapshot)
  → /api/ws-clients/stream  (SSE event "ws-client")
  → frontend subscribeWSClientEvents
  → upsert (connected/updated) or remove (disconnected) row by sessionId
  → table re-render
```

Each event carries the **full** `WSClientResponse` so the frontend does not need a follow-up fetch; YAGNI for delta-only payloads.

## Error handling

- SSE handler mirrors `handleSessionStream`: sends `"connected"` first, then 25s heartbeats, drops on context done.
- Frontend: on SSE error, show "reconnecting" banner and poll `/api/ws-clients` every 10s until the stream reconnects.
- Admin hangup/DTMF errors show inline `actionError` banner (same pattern as Active Sessions).

## Testing

- **Gateway:** `handlers_api_test.go`-style test asserting `handleWSClientsStream` writes the `"connected"` event and a subsequent `"ws-client"` event after `notifyWSClientChanged` is called. Mirror the existing trunk/session stream tests.
- **Frontend:** Vitest test for `subscribeWSClientEvents` upsert/remove; test for actions calling `hangupSession`/`sendSessionDtmf` only on active rows.

## Out of scope (YAGNI)

- No filter/search (typical client count is small).
- No pagination (SSE snapshot sends all).
- No removal of `WSClientCard` preview on Instances page (kept as quick glance + link to new page).
- No config/env exposure changes.
