# Gateway API LLM-Friendly Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split `apps/gateway/internal/api/server.go` and `handlers.go` into domain-focused files and slim `AGENTS.md` so LLM agents load ≤700 lines per task — with zero behavior change.

**Architecture:** Mechanical file split within `package api` (same pattern as `auth_http.go` / `client_diagnostics.go`). Move `(s *Server)` methods and unexported helpers to new files; leave `Server` struct, interfaces, `NewServer`, and `Start()` routing in `server.go`. Existing `go test ./internal/api/...` tests are the regression gate — no new tests required.

**Tech Stack:** Go 1.26.2, gorilla/mux, gorilla/websocket, existing `k2-gateway/internal/api` package.

**Spec:** `docs/superpowers/specs/2026-07-06-gateway-api-llm-refactor-design.md`

---

## File Responsibility Map (lock before coding)

| File | Functions / types to contain |
|------|------------------------------|
| `server.go` | `Server`, interfaces, `WSClient`, `WSMessage`, `NewServer`, setters, `Start`, `corsMiddleware`, shared WS constants not moved elsewhere |
| `ws_util.go` | `sendWSMessage`, `sendWSError`, `logTerminalAction`, `logSessionSnapshot`, `buildSessionMeta`, `logEvent`, `storePayload`, `subscribeTrunkStream`, `unsubscribeTrunkStream`, `broadcastTrunkStream`, `subscribeSessionStream`, `unsubscribeSessionStream`, `broadcastSessionStream` |
| `ws_conn.go` | `writeWait`, `pongWait`, `pingPeriod`, `maxMessageSize`, `handleWebSocket`, `handlePublicWebSocket`, `handleWebSocketConn`, `wsWritePump` |
| `ws_dispatch.go` | `handleWSMessage`, `allowPublicWSMessage`, `publicClientOwnsSession`, `publicClientCanResume`, `sessionIsPublic` |
| `ws_call.go` | `trunkOutboundValidation`, `validateTrunkReadyForOutboundCall`, `handleWSoffer`, `handleWSIce`, `handleWSCall`, `runWSCall`, `handleWSHangup`, `handleWSDTMF` |
| `ws_incoming.go` | `clientAvailability*` constants, `incomingRingTimeout`, `incomingPushTrunkLookupTimeout`, `defaultIncomingRingTimeout`, `incomingOfflinePolicyPush480`, `normalizeClientAvailability`, `normalizeClientCallState`, `isBusyCallState`, `isClientAvailableForIncoming`, `incrementIncomingCounter`, `incomingPushRoute`, `hasIncomingPushTarget`, `trunkHasFCMPushTarget`, `trunkHasApplePushKitTarget`, `normalizeDevicePlatform`, `selectIncomingPushRoute`, `incomingSessionHasVideo`, `dispatchIncomingPush`, `startIncomingRingTimeout`, `rejectIncomingSession`, `NotifyIncomingCall`, `NotifyIncomingCancel`, `handleWSAccept`, `handleWSReject`, `isBenignIncomingRejectError`, `notifyPendingIncomingForClient` |
| `ws_resume.go` | `resumeSlowLogThreshold`, `resumeVideoOfferDiagnostics`, `analyzeResumeOfferVideoSDP`, `hasActiveVideoMedia`, `handleWSResume` |
| `ws_trunk.go` | `normalizeTrunkPNAppID`, `hasTrunkPushContact`, `validateTrunkPushContact`, `updateTrunkPushContact`, `handleWSTrunkPushToken`, `handleWSTrunkResolve` |
| `ws_midcall.go` | `defaultMidCallRenegotiationTimeout`, `handleWSRenegotiateAnswer`, `scheduleMidCallRenegotiationTimeout`, `handleWSClientState`, `handleWSRequestKeyframe`, `handleWSPing` |
| `ws_translate.go` | `handleWSTranslate`, `handleWSTranslateStop`, `handleTranslationCaption`, `translatorVoiceForTargetLang`, `translatorLanguageBase` |
| `ws_notify.go` | `NotifySessionState`, `NotifyMidCallRenegotiation`, `sipURIUsername`, `sipAddressMatches`, `findSIPMessageSessionID`, `selectSIPMessageTargets`, `NotifySIPMessage`, `NotifyDTMF`, `handleWSSendMessage` |
| `handlers.go` | All REST request/response struct types, `respondJSON`, `respondError`, `ErrorResponse` |
| `handlers_sse.go` | `notifyTrunkListChanged`, `notifySessionListChanged`, `writeSSE`, `handleTrunkStream`, `handleSessionStream`, `TrunkStreamEvent`, `SessionStreamEvent` |
| `handlers_log.go` | `LogFileResponse`, `LogFileListResponse`, `LogTailResponse`, `handleListLogFiles`, `handleGetCurrentLog`, `handleGetLogFile`, `parseLogTailQuery`, `respondLogFileError`, `logTailResponse` |
| `handlers_call.go` | `handleOffer`, `handleCall`, `handleHangup`, `handleDTMF`, `handleSwitch`, `handleListSessions`, `handleGetSession` |
| `handlers_trunk.go` | `trunkActiveCallInfo`, `handleCreateTrunk`, `handleListTrunks`, `handleGetTrunk`, `handleUpdateTrunk`, `handleRefreshTrunks`, `collectActiveCallsByTrunk`, `handleTrunkRegister`, `handleTrunkUnregister`, `trunkResponseFrom`, `maskPushToken`, `formatOptionalTime`, `handleUserTrunkHeartbeat` |
| `handlers_session.go` | `SessionHistoryResponse` through `PayloadListResponse`, `handleListSessionHistory`, `handleListSessionEvents`, `payloadResponseFrom`, `handleListSessionPayloads`, `handleGetPayload`, `DialogResponse` through `StatsListResponse`, `handleListSessionDialogs`, `handleListSessionStats` |
| `handlers_ops.go` | `DashboardResponse` through `SessionDirectoryListResponse`, `handleListGatewayInstances`, `handleListSessionDirectory`, `handleListPublicAccounts`, `handleListWSClients`, `handleDashboard`, `dashboardSummaryLocation`, `parseDashboardSummaryRange`, `handleDashboardSummary` |

---

### Task 0: Baseline

**Files:** none (read-only verification)

- [ ] **Step 1: Record baseline line counts**

Run from repo root:

```powershell
Get-ChildItem apps/gateway/internal/api/*.go -Exclude *_test.go | ForEach-Object {
  $n = (Get-Content $_.FullName | Measure-Object -Line).Lines
  "$n $($_.Name)"
} | Sort-Object { [int]($_ -split ' ')[0] } -Descending
```

Expected: `server.go` ~2925 lines, `handlers.go` ~2244 lines.

- [ ] **Step 2: Confirm tests pass before any edits**

Run:

```bash
cd apps/gateway && go test ./internal/api/...
```

Expected: `ok` for all packages (no failures).

- [ ] **Step 3: Confirm full gateway build**

Run:

```bash
cd apps/gateway && go build -o k2-gateway .
```

Expected: exit code 0, binary created.

---

### Task 1: Extract `ws_util.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_util.go`
- Modify: `apps/gateway/internal/api/server.go` (remove moved functions)

- [ ] **Step 1: Create `ws_util.go`**

Create `apps/gateway/internal/api/ws_util.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)
```

Cut these functions from `server.go` into `ws_util.go` (unchanged bodies):

- `sendWSMessage`
- `sendWSError`
- `logTerminalAction`
- `logSessionSnapshot`
- `buildSessionMeta`
- `logEvent`
- `storePayload`
- `subscribeTrunkStream`
- `unsubscribeTrunkStream`
- `broadcastTrunkStream`
- `subscribeSessionStream`
- `unsubscribeSessionStream`
- `broadcastSessionStream`

Add only the imports each function needs (minimum: `context`, `encoding/json`, `log`, `time`, `logstore`, `session`).

- [ ] **Step 2: Remove duplicates from `server.go`**

Delete the moved functions from `server.go`. Do not change function bodies.

- [ ] **Step 3: Format and verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_util.go internal/api/server.go
cd apps/gateway && go test ./internal/api/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/gateway/internal/api/ws_util.go apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): extract ws_util.go from server.go"
```

---

### Task 2: Extract `ws_conn.go` and `ws_dispatch.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_conn.go`
- Create: `apps/gateway/internal/api/ws_dispatch.go`
- Modify: `apps/gateway/internal/api/server.go`

- [ ] **Step 1: Create `ws_conn.go`**

Move from `server.go`:

Constants block:

```go
const (
	writeWait      = 10 * time.Second
	pongWait       = 180 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 16384
)
```

Functions:

- `handleWebSocket`
- `handlePublicWebSocket`
- `handleWebSocketConn`
- `wsWritePump`

Imports needed: `context` (if used), `encoding/json`, `fmt`, `log`, `net/http`, `time`, `gorilla/websocket`, `auth`, `session`, `sip` (check actual usage in bodies).

- [ ] **Step 2: Create `ws_dispatch.go`**

Move from `server.go`:

- `handleWSMessage` — **do not edit the switch cases**
- `allowPublicWSMessage`
- `publicClientOwnsSession`
- `publicClientCanResume`
- `sessionIsPublic`

Imports needed: `encoding/json`, `log`, `session`.

- [ ] **Step 3: Format and verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_conn.go internal/api/ws_dispatch.go internal/api/server.go
cd apps/gateway && go test ./internal/api/... -run "TestWebSocketAuth|TestHandleWSCall|TestPublicWebSocket"
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/gateway/internal/api/ws_conn.go apps/gateway/internal/api/ws_dispatch.go apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): extract ws_conn.go and ws_dispatch.go"
```

---

### Task 3: Extract `ws_call.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_call.go`
- Modify: `apps/gateway/internal/api/server.go`

- [ ] **Step 1: Move call-related code**

Move type + functions from `server.go` to `ws_call.go`:

- `trunkOutboundValidation` struct
- `validateTrunkReadyForOutboundCall`
- `handleWSoffer`
- `handleWSIce`
- `handleWSCall`
- `runWSCall`
- `handleWSHangup`
- `handleWSDTMF`

- [ ] **Step 2: Verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_call.go internal/api/server.go
cd apps/gateway && go test ./internal/api/... -run "TestHandleWSCall|TestWebSocketAuth|Ice"
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/gateway/internal/api/ws_call.go apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): extract ws_call.go"
```

---

### Task 4: Extract `ws_incoming.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_incoming.go`
- Modify: `apps/gateway/internal/api/server.go`

- [ ] **Step 1: Move incoming-call block**

Move from `server.go`:

Constants (if still there):

```go
const (
	clientAvailabilityIdle        = "idle"
	clientAvailabilityBusy        = "busy"
	clientAvailabilityUnavailable = "unavailable"
	incomingOfflinePolicyPush480  = "push_then_480"
	incomingPushTrunkLookupTimeout = 5 * time.Second
	defaultIncomingRingTimeout    = 30 * time.Second
)
```

Types:

- `incomingPushRoute`

Functions (full list in file map above for `ws_incoming.go`).

- [ ] **Step 2: Verify incoming tests**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_incoming.go internal/api/server.go
cd apps/gateway && go test ./internal/api/... -run "Incoming"
```

Expected: PASS (`server_incoming_test.go`, `incoming_video_test.go`).

- [ ] **Step 3: Commit**

```bash
git add apps/gateway/internal/api/ws_incoming.go apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): extract ws_incoming.go"
```

---

### Task 5: Extract `ws_resume.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_resume.go`
- Modify: `apps/gateway/internal/api/server.go`

- [ ] **Step 1: Move resume code**

Move from `server.go`:

- `resumeSlowLogThreshold` constant
- `resumeVideoOfferDiagnostics` struct
- `analyzeResumeOfferVideoSDP`
- `hasActiveVideoMedia`
- `handleWSResume`

- [ ] **Step 2: Verify resume tests**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_resume.go internal/api/server.go
cd apps/gateway && go test ./internal/api/... -run "Resume"
```

Expected: PASS (`server_resume_test.go`, `server_resume_sdp_diagnostics_test.go`).

- [ ] **Step 3: Commit**

```bash
git add apps/gateway/internal/api/ws_resume.go apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): extract ws_resume.go"
```

---

### Task 6: Extract `ws_trunk.go`, `ws_midcall.go`, `ws_translate.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_trunk.go`
- Create: `apps/gateway/internal/api/ws_midcall.go`
- Create: `apps/gateway/internal/api/ws_translate.go`
- Modify: `apps/gateway/internal/api/server.go`
- Modify: `apps/gateway/internal/api/handlers.go` (remove translate handlers)

- [ ] **Step 1: Create `ws_trunk.go`**

Move from `server.go`:

- `normalizeTrunkPNAppID`
- `hasTrunkPushContact`
- `validateTrunkPushContact`
- `updateTrunkPushContact`
- `handleWSTrunkPushToken`
- `handleWSTrunkResolve`

- [ ] **Step 2: Create `ws_midcall.go`**

Move from `server.go`:

- `defaultMidCallRenegotiationTimeout`
- `handleWSRenegotiateAnswer`
- `scheduleMidCallRenegotiationTimeout`
- `handleWSClientState`
- `handleWSRequestKeyframe`
- `handleWSPing`

- [ ] **Step 3: Create `ws_translate.go`**

Move from `handlers.go`:

- `handleWSTranslate`
- `handleWSTranslateStop`
- `translatorVoiceForTargetLang`
- `translatorLanguageBase`

Move from `server.go`:

- `handleTranslationCaption`

- [ ] **Step 4: Verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_trunk.go internal/api/ws_midcall.go internal/api/ws_translate.go internal/api/server.go internal/api/handlers.go
cd apps/gateway && go test ./internal/api/... -run "TrunkResolve|MidCall|Translator"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/gateway/internal/api/ws_trunk.go apps/gateway/internal/api/ws_midcall.go apps/gateway/internal/api/ws_translate.go apps/gateway/internal/api/server.go apps/gateway/internal/api/handlers.go
git commit -m "refactor(gateway): extract ws_trunk, ws_midcall, ws_translate"
```

---

### Task 7: Extract `ws_notify.go`

**Files:**
- Create: `apps/gateway/internal/api/ws_notify.go`
- Modify: `apps/gateway/internal/api/server.go`

- [ ] **Step 1: Move notify callbacks**

Move from `server.go`:

- `NotifySessionState`
- `NotifyMidCallRenegotiation`
- `sipURIUsername`
- `sipAddressMatches`
- `findSIPMessageSessionID`
- `selectSIPMessageTargets`
- `NotifySIPMessage`
- `NotifyDTMF`
- `handleWSSendMessage`

- [ ] **Step 2: Verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/ws_notify.go internal/api/server.go
cd apps/gateway && go test ./internal/api/...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/gateway/internal/api/ws_notify.go apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): extract ws_notify.go"
```

---

### Task 8: Slim `server.go` verification

**Files:**
- Modify: `apps/gateway/internal/api/server.go` (cleanup only)

- [ ] **Step 1: Confirm `server.go` contents**

`server.go` should contain only:

- package declaration and imports
- `Server` struct and interfaces (`TokenVerifier`, `PublicAccountRegistry`, `TrunkManager`, `SIPCallMaker`)
- `WSClient`, `WSMessage` structs
- `NewServer`, all `Set*` methods
- `Start`, `corsMiddleware`

Remove any leftover duplicate functions. Target ≤ 400 lines.

- [ ] **Step 2: Line count check**

Run:

```powershell
(Get-Content apps/gateway/internal/api/server.go | Measure-Object -Line).Lines
```

Expected: ≤ 400.

- [ ] **Step 3: Full api tests**

Run:

```bash
cd apps/gateway && go test ./internal/api/...
```

Expected: PASS.

- [ ] **Step 4: Commit (if cleanup diff non-empty)**

```bash
git add apps/gateway/internal/api/server.go
git commit -m "refactor(gateway): slim server.go to core routing and types"
```

---

### Task 9: Extract REST `handlers_sse.go` and `handlers_log.go`

**Files:**
- Create: `apps/gateway/internal/api/handlers_sse.go`
- Create: `apps/gateway/internal/api/handlers_log.go`
- Modify: `apps/gateway/internal/api/handlers.go`

- [ ] **Step 1: Create `handlers_sse.go`**

Move from `handlers.go`:

- `notifyTrunkListChanged`
- `notifySessionListChanged`
- `writeSSE`
- `handleTrunkStream`
- `handleSessionStream`
- `TrunkStreamEvent`, `SessionStreamEvent` types

- [ ] **Step 2: Create `handlers_log.go`**

Move from `handlers.go`:

- `LogFileResponse`, `LogFileListResponse`, `LogTailResponse`
- `handleListLogFiles`
- `handleGetCurrentLog`
- `handleGetLogFile`
- `parseLogTailQuery`
- `respondLogFileError`
- `logTailResponse`

- [ ] **Step 3: Verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/handlers_sse.go internal/api/handlers_log.go internal/api/handlers.go
cd apps/gateway && go test ./internal/api/... -run "SSE|Log"
```

Expected: PASS (`handlers_api_test.go` SSE tests, `log_files_test.go`).

- [ ] **Step 4: Commit**

```bash
git add apps/gateway/internal/api/handlers_sse.go apps/gateway/internal/api/handlers_log.go apps/gateway/internal/api/handlers.go
git commit -m "refactor(gateway): extract handlers_sse.go and handlers_log.go"
```

---

### Task 10: Extract `handlers_call.go` and `handlers_trunk.go`

**Files:**
- Create: `apps/gateway/internal/api/handlers_call.go`
- Create: `apps/gateway/internal/api/handlers_trunk.go`
- Modify: `apps/gateway/internal/api/handlers.go`

- [ ] **Step 1: Create `handlers_call.go`**

Move REST handlers:

- `handleOffer`
- `handleCall`
- `handleHangup`
- `handleDTMF`
- `handleSwitch`
- `handleListSessions`
- `handleGetSession`

Keep related request/response types (`OfferRequest` … `SwitchResponse`, `SessionResponse`, `DTMFRequest`) in `handlers.go` for now — they are shared.

- [ ] **Step 2: Create `handlers_trunk.go`**

Move:

- `trunkActiveCallInfo`
- `handleCreateTrunk` through `handleTrunkUnregister`
- `collectActiveCallsByTrunk`
- `trunkResponseFrom`
- `maskPushToken`
- `formatOptionalTime`
- `handleUserTrunkHeartbeat`
- `TrunkResponse`, `UpdateTrunkRequest`, `CreateTrunkRequest`, `TrunkListResponse` types

- [ ] **Step 3: Verify trunk and call REST tests**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/handlers_call.go internal/api/handlers_trunk.go internal/api/handlers.go
cd apps/gateway && go test ./internal/api/... -run "HandleOffer|HandleCall|HandleCreateTrunk|HandleUpdateTrunk|HandleListTrunks|Heartbeat"
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/gateway/internal/api/handlers_call.go apps/gateway/internal/api/handlers_trunk.go apps/gateway/internal/api/handlers.go
git commit -m "refactor(gateway): extract handlers_call.go and handlers_trunk.go"
```

---

### Task 11: Extract `handlers_session.go` and `handlers_ops.go`

**Files:**
- Create: `apps/gateway/internal/api/handlers_session.go`
- Create: `apps/gateway/internal/api/handlers_ops.go`
- Modify: `apps/gateway/internal/api/handlers.go`

- [ ] **Step 1: Create `handlers_session.go`**

Move session history/events/payloads/dialogs/stats handlers and their response types (see file map).

- [ ] **Step 2: Create `handlers_ops.go`**

Move dashboard, gateway instances, session directory, public accounts, ws-clients handlers and their response types (see file map).

- [ ] **Step 3: Slim `handlers.go`**

`handlers.go` should retain:

- Shared REST types still used across files
- `respondJSON`
- `respondError`
- `ErrorResponse`

Target ≤ 200 lines. Move types with their sole consumer if needed to stay under limit.

- [ ] **Step 4: Verify**

Run:

```bash
cd apps/gateway && gofmt -w internal/api/handlers_session.go internal/api/handlers_ops.go internal/api/handlers.go
cd apps/gateway && go test ./internal/api/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/gateway/internal/api/handlers_session.go apps/gateway/internal/api/handlers_ops.go apps/gateway/internal/api/handlers.go
git commit -m "refactor(gateway): extract handlers_session.go and handlers_ops.go"
```

---

### Task 12: Slim `AGENTS.md` and add `docs/gateway/*`

**Files:**
- Modify: `apps/gateway/AGENTS.md`
- Create: `docs/gateway/ws-contract.md`
- Create: `docs/gateway/config-reference.md`
- Create: `docs/gateway/ops-guide.md`
- Create: `docs/gateway/troubleshooting.md`

- [ ] **Step 1: Create `docs/gateway/ws-contract.md`**

Move §5 WebSocket Contract from `AGENTS.md` verbatim (client/server message types, auth behavior, renegotiate notes). Add header:

```markdown
# Gateway WebSocket Contract

Source of truth for `/ws` and `/ws-public` JSON messages.
When changing message types, also update `internal/api/ws_dispatch.go` and frontend `gateway-store.ts`.
```

- [ ] **Step 2: Create `docs/gateway/config-reference.md`**

Move §7 Configuration Reference from `AGENTS.md` verbatim.

- [ ] **Step 3: Create `docs/gateway/ops-guide.md`**

Move from `AGENTS.md`:

- Operational log access section (REST log endpoints, client-diagnostics endpoints, PowerShell examples)
- Direct database MCP note (`database-dev-k2-gateway`)

- [ ] **Step 4: Create `docs/gateway/troubleshooting.md`**

Move §11 Troubleshooting Quick Guide from `AGENTS.md`.

- [ ] **Step 5: Rewrite `AGENTS.md` (target ≤150 lines)**

Keep:

1. Mission-critical rules (§1)
2. Project snapshot (§2) — update libs list if needed
3. Runtime architecture (§3)
4. **Updated repo map** including `push/`, `translator/`, `audio/`, `sipclientauth/`
5. **New `internal/api/` file map** (from design spec)
6. **Where to start table** (task type → file)
7. Critical media behaviors summary (§6 shortened to bullets + file refs)
8. Build/test (§10)
9. Agent workflow (§12) + new agent rules 1–5
10. Links to `docs/gateway/*.md` for WS contract, config, ops, troubleshooting

Remove duplicated full config and WS message lists from `AGENTS.md`.

- [ ] **Step 6: Verify line count**

Run:

```powershell
(Get-Content apps/gateway/AGENTS.md | Measure-Object -Line).Lines
```

Expected: ≤ 150.

- [ ] **Step 7: Commit**

```bash
git add apps/gateway/AGENTS.md docs/gateway/
git commit -m "docs(gateway): slim AGENTS.md and split reference docs"
```

---

### Task 13: Final verification

**Files:** all touched files

- [ ] **Step 1: Format entire api package**

Run:

```bash
cd apps/gateway && gofmt -w ./internal/api/
```

- [ ] **Step 2: Full gateway test suite**

Run:

```bash
cd apps/gateway && go test ./...
```

Expected: all packages PASS.

- [ ] **Step 3: Build binary**

Run:

```bash
cd apps/gateway && go build -o k2-gateway .
```

Expected: exit code 0.

- [ ] **Step 4: Success metrics**

Run:

```powershell
Get-ChildItem apps/gateway/internal/api/*.go -Exclude *_test.go | ForEach-Object {
  $n = (Get-Content $_.FullName | Measure-Object -Line).Lines
  [PSCustomObject]@{ Lines = $n; File = $_.Name }
} | Sort-Object Lines -Descending | Format-Table -AutoSize
```

Expected:

- `server.go` ≤ 400 lines
- `handlers.go` ≤ 200 lines
- no single file > 700 lines
- `AGENTS.md` ≤ 150 lines

- [ ] **Step 5: Diff hygiene**

Run:

```bash
git diff --check
git diff --stat
```

Expected: no whitespace errors; only `internal/api/`, `AGENTS.md`, `docs/gateway/`, and spec/plan docs changed.

- [ ] **Step 6: Final commit (only if uncommitted doc/plan files remain)**

```bash
git add docs/superpowers/specs/2026-07-06-gateway-api-llm-refactor-design.md docs/superpowers/plans/2026-07-06-gateway-api-llm-refactor.md
git commit -m "docs: add gateway api llm refactor spec and plan"
```

---

## Self-Review Checklist

| Spec requirement | Task |
|-----------------|------|
| Split `server.go` by WS domain | Tasks 1–8 |
| Split `handlers.go` by REST domain | Tasks 9–11 |
| Slim `AGENTS.md` | Task 12 |
| Extract `docs/gateway/*` | Task 12 |
| Zero behavior change | All tasks: move only, `go test` gate |
| Success line counts | Task 13 |
| Phase 2 sip split | Out of scope |

No placeholders. Every task has exact paths and verification commands.
