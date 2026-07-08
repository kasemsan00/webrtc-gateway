# Task 1 Report — add clientID to WSClient + update WSClientResponse + change handleListWSClients

## Status: DONE_WITH_CONCERNS

## What I Implemented

Followed the task brief verbatim (TDD: RED → GREEN → verify).

1. **`apps/gateway/internal/api/server.go`** — Added `clientID string` field to `WSClient` struct (after `conn`), with comment `// UUID assigned at connect, stable across session assignment`. Did **not** add `github.com/google/uuid` import here (see Concern 1 — Go forbids unused imports; `uuid` is only referenced in `ws_conn.go`).
2. **`apps/gateway/internal/api/handlers_ops.go`** — Updated `WSClientResponse`: added `ClientID string \`json:"clientId"\``, made `SessionID` `omitempty` (now optional), added `PublicOnly bool \`json:"publicOnly,omitempty"\``. Rewrote `handleListWSClients` to iterate `s.wsConnections` (all connected WS clients, including idle trunk-resolved ones) instead of `s.wsClients` (session-active only). Now populates `ClientID`, `PublicOnly`, and only sets `SessionID` when `client.sessionID != ""`. Trunk public-ID lookup block unchanged (still runs outside the RLock).
3. **`apps/gateway/internal/api/ws_conn.go`** — Added `github.com/google/uuid` import and `clientID: uuid.NewString()` to the `WSClient` literal at connect (line ~89).
4. **`apps/gateway/internal/api/handlers_api_test.go`** — Added new test `TestHandleListWSClients_IncludesIdleAndClientID` (idle + active client in `wsConnections`, asserts both surface via `ClientID`, idle has empty `SessionID` + `TrunkResolved`, active has `sess-active`). Updated existing `TestHandleListWSClients_IncludesResolvedTrunkAndClientState` to register the client in `srv.wsConnections` (not `srv.wsClients`), add `clientID: "ws-1"`, and assert on `ClientID`/`TrunkResolved`/`ResolvedTrunkID`.

### TDD evidence
- **RED:** New test failed to compile before implementation with exactly the expected errors: `unknown field clientID in struct literal of type WSClient` and `resp[i].ClientID undefined (type WSClientResponse has no field or method ClientID)`. Confirms the test exercises new behavior (feature missing), not typos.
- **GREEN:** After implementing Steps 3-7, both `TestHandleListWSClients_*` tests pass; full suite green.

## Test Results

### Focused (Step 8)
```
cd apps/gateway && go test ./internal/api -run "TestHandleListWSClients" -v
=== RUN   TestHandleListWSClients_IncludesResolvedTrunkAndClientState
--- PASS: TestHandleListWSClients_IncludesResolvedTrunkAndClientState (0.00s)
=== RUN   TestHandleListWSClients_IncludesIdleAndClientID
--- PASS: TestHandleListWSClients_IncludesIdleAndClientID (0.00s)
PASS
ok  	k2-gateway/internal/api	1.429s
```

### Full gateway suite (Step 9)
```
cd apps/gateway && go test ./...
ok  	k2-gateway/internal/api	2.704s
ok  	k2-gateway/internal/audio	(cached)
ok  	k2-gateway/internal/auth	0.755s
ok  	k2-gateway/internal/config	(cached)
ok  	k2-gateway/internal/logstore	(cached)
ok  	k2-gateway/internal/push	(cached)
ok  	k2-gateway/internal/session	(cached)
ok  	k2-gateway/internal/sip	(cached)
ok  	k2-gateway/internal/sipclientauth	0.621s
ok  	k2-gateway/internal/translator	(cached)
ok  	k2-gateway/internal/translator/pb	(cached)
```
All packages pass; no regressions. `go vet ./internal/api/...` clean. `gofmt -l` clean on all 4 files.

### Regression scan
The dashboard handler (`handleDashboardSummary`) still counts `len(s.wsClients)` for its `WSClients` metric, so the dashboard test at `handlers_api_test.go:1153` (which seeds `wsClients["session-1"]`) is unaffected — it calls `handleDashboardSummary`, not `handleListWSClients`. Other `wsClients` usages in `server_call_identity_test.go` and `server_incoming_test.go` call call-identity/incoming handlers, not `handleListWSClients`, so they are unaffected. Only the two `TestHandleListWSClients_*` tests exercise the changed handler.

## Commits

- `32457970f7f516bdeed35a981ab5aca75abe8a50` — `feat(gateway): add clientID to WSClient and show all connections in /ws-clients`
  - 4 files changed, 67 insertions(+), 31 deletions(-)
  - Files: `server.go`, `handlers_ops.go`, `ws_conn.go`, `handlers_api_test.go`

Only the 4 files specified in the brief's Step 10 were staged. An unrelated modified file (`docs/superpowers/plans/2026-07-08-ws-clients-realtime.md`) and untracked frontend files were left out of the commit.

## Concerns / Notes

1. **`uuid` import placement (deviation from brief Step 3, required for compilation).** The brief's Step 3 showed adding `"github.com/google/uuid"` to `server.go`'s import block, but `server.go` never references the `uuid` package (the new `clientID` field is a plain `string`; `uuid.NewString()` is only called in `ws_conn.go`). Go rejects unused imports, so adding it to `server.go` would break the build. I added the import only to `ws_conn.go`, which is where the brief's Step 6 explicitly says to add it and where it is actually used. The brief's Step 3 import snippet was illustrative (`// existing imports...`).

2. **gofmt compliance (whitespace-only fix to verbatim test code).** The brief's verbatim test snippet for `TestHandleListWSClients_IncludesIdleAndClientID` was not gofmt-clean (struct field values were not column-aligned). I ran `gofmt -w` on `handlers_api_test.go` after inserting the verbatim code. This changed only whitespace alignment (e.g., aligning `ConnectedAt:`/`availability:`/`callState:` under the longest key `resolvedTrunkID:`), preserving the exact fields and logic from the brief. Go convention requires gofmt compliance; the change is whitespace-only.

3. **Test coverage reduction (property of the plan, not a defect).** The brief's Step 7 replacement for `TestHandleListWSClients_IncludesResolvedTrunkAndClientState` is a simplified version that drops the assertions previously covering: (a) `ResolvedTrunkPublicID` lookup via `trunkManager` (the new test passes `nil` trunkMgr), (b) `AuthSubject` from `authClaims`, and (c) `ConnectedAt` formatting. The new `TestHandleListWSClients_IncludesIdleAndClientID` also uses `nil` trunkMgr and no `authClaims`, so neither of the two `handleListWSClients` tests now directly exercises the `s.trunkManager != nil` → `ResolvedTrunkPublicID` branch or the `authClaims != nil` → `AuthSubject` branch in the rewritten handler. The production code retains those branches (verbatim from the brief); only their direct test coverage was removed. Suggest a small follow-up test that seeds a `WSClient` with a non-zero `resolvedTrunkID` + `authClaims` in `wsConnections` and a non-nil `trunkManager` stub to re-cover those two branches. This does not block the task.

4. **Lock discipline in new test (test-only, matches brief verbatim).** The new test writes `srv.wsConnections[idle] = struct{}{}` without holding `srv.mu`, whereas the old test used `srv.mu.Lock()/Unlock()`. Tests are run without `-race` (brief's commands don't include it) and the handler call is synchronous single-goroutine, so this is safe in practice. Flagging only because it diverges from the lock-acquisition pattern used elsewhere in the file; behavior matches the brief's verbatim code.
