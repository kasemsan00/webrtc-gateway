# Task 4 Report — hook notifyWSClientChanged at all WSClient state-change points (+ RLock race fix)

## Status: COMPLETE

## Commit

- `8e71d0c` — `feat(gateway): emit ws-client SSE events on state changes (+ RLock race fix)`
- Files: 7 changed, 10 insertions(+), 0 deletions(-)
  - `apps/gateway/internal/api/handlers_sse.go` (Step 0: RLock race fix)
  - `apps/gateway/internal/api/ws_conn.go` (Step 1: connect + disconnect)
  - `apps/gateway/internal/api/ws_trunk.go` (Step 2: trunk resolve)
  - `apps/gateway/internal/api/ws_midcall.go` (Step 3: client_state)
  - `apps/gateway/internal/api/ws_call.go` (Step 4: session register + trunk reset)
  - `apps/gateway/internal/api/ws_resume.go` (Step 5: resume register)
  - `apps/gateway/internal/api/ws_incoming.go` (Step 6: accept register)

## What I Implemented

### Step 0 — RLock race fix on `notifyWSClientChanged`

In `handlers_sse.go:55-70`, wrapped the `s.buildWSClientResponse(client)` call with `s.mu.RLock()` / `s.mu.RUnlock()`:

```go
func (s *Server) notifyWSClientChanged(eventType string, client *WSClient) {
    if client == nil {
        return
    }
    s.mu.RLock()
    resp := s.buildWSClientResponse(client)
    s.mu.RUnlock()
    payload, err := json.Marshal(WSClientStreamEvent{...})
    ...
    s.broadcastWSClientStream(payload)
}
```

`buildWSClientResponse` reads `client.sessionID`, `callState`, `availability`, `trunkResolved`, `resolvedTrunkID`, `authClaims` — all of which are mutated under `s.mu.Lock()` elsewhere. The RLock makes the read side safe. The RUnlock is released before `broadcastWSClientStream` (which acquires its own RLock) to avoid recursive RLock semantics issues when a writer is waiting.

### Steps 1–6 — Notify calls at state-change points

Added `s.notifyWSClientChanged(...)` at 7 call sites across 6 files (Step 4 has two sites in `ws_call.go`).

## Lock State Analysis at Each Call Site

| Step | File:Line | Event | `s.mu` held before notify? | Where notify placed | Safe? |
|------|-----------|-------|----------------------------|---------------------|-------|
| 1a | `ws_conn.go:109` | `connected` | NO — `s.mu.Unlock()` on line 108 | After unlock, before `go s.wsWritePump` | YES |
| 1b | `ws_conn.go:152` | `disconnected` | NO — `s.mu.Unlock()` on line 151 | After unlock, end of `handleWebSocketConn` | YES |
| 2 | `ws_trunk.go:234` | `updated` | NO — surrounding code at lines 231–233 writes `client.trunkResolved`/`resolvedTrunkID` without acquiring `s.mu` (pre-existing pattern) | Immediately after the trunk-resolved writes | YES |
| 3 | `ws_midcall.go:136` | `updated` | NO — `s.mu.Unlock()` on line 135 | After unlock, before `s.logEvent` | YES |
| 4a | `ws_call.go:66` | `updated` | NO — `s.mu.Unlock()` on line 65 | After unlock, before SPS/PPS caching | YES |
| 4b | `ws_call.go:313` | `updated` | NO — `validation.reason != ""` branch (lines 309–315) does not acquire `s.mu` (writes `client.trunkResolved`/`resolvedTrunkID` without lock, pre-existing pattern) | Immediately after the trunk-reset writes, before `sendWSMessage` | YES |
| 5 | `ws_resume.go:218` | `updated` | NO — `s.mu.Unlock()` on line 217 | After unlock, before SDP renegotiation block | YES |
| 6 | `ws_incoming.go:520` | `updated` | NO — `s.mu.Unlock()` on line 519, inside `if client.sessionID == "" || client.sessionID != callSession.ID` block | After unlock, end of `if` block | YES |

**Verification:** At every call site, `s.mu` is NOT held when `notifyWSClientChanged` is invoked. Since `notifyWSClientChanged` acquires `s.mu.RLock()` internally, and no caller holds `s.mu.Lock()`, there is no risk of Go RWMutex deadlock (Lock blocks RLock).

## State-Change Point Coverage Audit

I searched for all mutations of `WSClient` state fields and `s.wsConnections`/`s.wsClients` maps to verify completeness.

### Registrations (`s.wsConnections[...] = ...` and `s.wsClients[...] = ...`)

| Location | Covered? |
|----------|----------|
| `ws_conn.go:107` — `s.wsConnections[client] = struct{}{}` (WS connect) | YES — Step 1a `connected` event |
| `ws_call.go:64` — `s.wsClients[sess.ID] = client` (offer) | YES — Step 4a `updated` event |
| `ws_resume.go:215` — `s.wsClients[msg.SessionID] = client` (resume) | YES — Step 5 `updated` event |
| `ws_incoming.go:518` — `s.wsClients[callSession.ID] = client` (accept) | YES — Step 6 `updated` event |

### Deletions (`delete(s.wsConnections, ...)` and `delete(s.wsClients, ...)`)

| Location | Covered? |
|----------|----------|
| `ws_conn.go:146` — `delete(s.wsConnections, client)` (WS disconnect) | YES — Step 1b `disconnected` event |
| `ws_conn.go:149` — `delete(s.wsClients, client.sessionID)` (conditional, on disconnect) | YES — covered by Step 1b `disconnected` event (the disconnect event reflects the cleared session mapping) |

### Field mutations (`client.{trunkResolved,resolvedTrunkID,availability,callState,sessionID} = ...`)

| Location | Field | Covered? |
|----------|-------|----------|
| `ws_conn.go:103-104` | `trunkResolved`, `resolvedTrunkID` (mobile provision at connect) | YES — covered by Step 1a `connected` event (writes happen before `s.wsConnections[client] = struct{}{}`, so the connected event reflects them) |
| `ws_trunk.go:232-233` | `trunkResolved`, `resolvedTrunkID` (trunk resolve) | YES — Step 2 `updated` event |
| `ws_call.go:62` | `sessionID` (offer) | YES — Step 4a `updated` event |
| `ws_call.go:311-312` | `trunkResolved`, `resolvedTrunkID` (trunk reset on validation failure) | YES — Step 4b `updated` event |
| `ws_midcall.go:133-134` | `availability`, `callState` (client_state) | YES — Step 3 `updated` event |
| `ws_resume.go:211` | `oldClient.sessionID = ""` (resume swap) | See note below |
| `ws_resume.go:216` | `sessionID` (resume) | YES — Step 5 `updated` event |
| `ws_incoming.go:516` | `sessionID` (accept) | YES — Step 6 `updated` event |

### Note on `ws_resume.go:211` (`oldClient.sessionID = ""`)

This write clears the sessionID of the OLD client when a new client takes over a session (network reconnect scenario). The brief's Step 5 only specifies a notify for the NEW client (the one being registered), so I followed the brief literally and did not add a notify for `oldClient`. The old client's eventual `disconnected` event will reflect its final state.

If a `disconnected` event for `oldClient` is desired at swap time (rather than when its WS closes), that would require:
1. Capturing `oldClient` before releasing `s.mu`
2. Releasing `s.mu`
3. Calling `s.notifyWSClientChanged("updated", oldClient)` separately

This is a deliberate per-brief decision; flagged as a concern below.

## Test Results

### Full gateway suite (no race detector)

```
$ cd apps/gateway && go test ./...
ok  k2-gateway/internal/api  2.755s
ok  k2-gateway/internal/audio  (cached)
ok  k2-gateway/internal/auth  (cached)
ok  k2-gateway/internal/config  (cached)
ok  k2-gateway/internal/logstore  (cached)
ok  k2-gateway/internal/push  (cached)
ok  k2-gateway/internal/session  (cached)
ok  k2-gateway/internal/sip  (cached)
ok  k2-gateway/internal/sipclientauth  (cached)
ok  k2-gateway/internal/translator  (cached)
ok  k2-gateway/internal/translator/pb  (cached)
```

All packages PASS.

### Race detector — `go test -race ./internal/api/...`

Required `CGO_ENABLED=1` and a gcc + pkg-config + opus toolchain. I installed `pkgconf` and `mingw-w64-ucrt-x86_64-opus` via msys2 pacman and placed a fixed `opus.pc` at `C:\msys64\usr\lib\pkgconfig\opus.pc` (with `prefix=C:/msys64/ucrt64` so gcc can resolve the include/lib paths).

Result: 7 data races reported, 5 tests FAIL — **all pre-existing, none caused by my changes.**

The 7 races are all in test stub structs that lack synchronization:

| # | Test | Race |
|---|------|------|
| 1 | `TestHandleWSCallAllowsSamePublicIdentity` | `stubSIPCallMaker.makeCallCount` write in `MakeCall` (goroutine from `handleWSCall` → `runWSCall` → `MakeCall` at `server_call_identity_test.go:21`) vs read in `waitForMakeCallCount` at `server_call_identity_test.go:49` |
| 2 | `TestHandleWSCallAllowsSamePublicIdentity` | `stubSIPCallMaker.lastDest/lastFrom/lastSessionID` same pattern |
| 3 | `TestHandleWSCall_PublicOnlyAllowsPublicCredentials` | Same as #1 |
| 4 | `TestHandleWSCall_AllowsIdentityChangeForNonPublicMode` | Same as #1 |
| 5 | `TestHandleWSCallUsesResolvedTrunkWhenNoAuthFieldsProvided` | Same as #1 |
| 6 | `TestNotifyIncomingCall_PushPathTimesOutNoAnswer` | `incomingTestSIPCallMaker.RejectCall` write in `startIncomingRingTimeout` goroutine (at `server_incoming_test.go:53`) vs test goroutine read |
| 7 | `TestNotifyIncomingCall_PushPathTimesOutNoAnswer` | Same as #6, different field |

### Proof races are pre-existing

I stashed my 7 file changes (via `git stash push -- <files>`) and re-ran the race detector on the original code:

```
$ git stash push -m "task-4-temp" apps/gateway/internal/api/handlers_sse.go apps/gateway/internal/api/ws_call.go apps/gateway/internal/api/ws_conn.go apps/gateway/internal/api/ws_incoming.go apps/gateway/internal/api/ws_midcall.go apps/gateway/internal/api/ws_resume.go apps/gateway/internal/api/ws_trunk.go

$ go test -race -run "TestHandleWSCallAllowsSamePublicIdentity|TestHandleWSCall_PublicOnlyAllowsPublicCredentials|TestHandleWSCall_AllowsIdentityChangeForNonPublicMode|TestHandleWSCallUsesResolvedTrunkWhenNoAuthFieldsProvided|TestNotifyIncomingCall_PushPathTimesOutNoAnswer" ./internal/api/...
WARNING: DATA RACE
WARNING: DATA RACE
--- FAIL: TestHandleWSCallAllowsSamePublicIdentity (0.01s)
WARNING: DATA RACE
--- FAIL: TestHandleWSCall_PublicOnlyAllowsPublicCredentials (0.00s)
WARNING: DATA RACE
--- FAIL: TestHandleWSCall_AllowsIdentityChangeForNonPublicMode (0.00s)
WARNING: DATA RACE
--- FAIL: TestHandleWSCallUsesResolvedTrunkWhenNoAuthFieldsProvided (0.00s)
WARNING: DATA RACE
WARNING: DATA RACE
--- FAIL: TestNotifyIncomingCall_PushPathTimesOutNoAnswer (1.01s)
FAIL    k2-gateway/internal/api  1.225s
FAIL
```

All 7 races and 5 failures reproduce on the original code (without my changes). Then restored my changes via `git stash pop`.

### Race detector with pre-existing-race tests excluded

To confirm my changes introduce no NEW races:

```
$ go test -race -skip "TestHandleWSCallAllowsSamePublicIdentity|TestHandleWSCall_PublicOnlyAllowsPublicCredentials|TestHandleWSCall_AllowsIdentityChangeForNonPublicMode|TestHandleWSCallUsesResolvedTrunkWhenNoAuthFieldsProvided|TestNotifyIncomingCall_PushPathTimesOutNoAnswer" ./internal/api/...
ok  k2-gateway/internal/api  1.431s
```

All other tests (including those that exercise my modified code paths: `TestHandleWSClientsStream_Contract`, `TestHandleWSResume*`, `TestHandleWSTrunkResolve*`, `TestHandleWSAccept*`, `TestHandleWSClientState*`, `TestNotifyIncomingCall_*` (other than the skipped one)) PASS with `-race` and report NO data races.

## Self-Review Checklist

- [x] **No `notifyWSClientChanged` call placed while caller holds `s.mu.Lock()`** — verified at all 7 call sites (see lock state analysis table above). Every notify is placed after `s.mu.Unlock()` (or in a code path where `s.mu` was never acquired).
- [x] **No new data races introduced** — confirmed via race detector: with pre-existing-race tests skipped, full `./internal/api/...` suite passes with `-race`.
- [x] **All 4 WSClient registrations covered** — `ws_conn.go:107` (connect), `ws_call.go:64` (offer), `ws_resume.go:215` (resume), `ws_incoming.go:518` (accept).
- [x] **WSClient disconnection covered** — `ws_conn.go:146` (disconnect).
- [x] **All trunkResolved/resolvedTrunkID mutations covered** — `ws_conn.go:103-104` (reflected in `connected` event), `ws_trunk.go:232-233` (`updated`), `ws_call.go:311-312` (`updated`).
- [x] **All availability/callState mutations covered** — `ws_midcall.go:133-134` (`updated`).
- [x] **All sessionID mutations covered (per brief)** — `ws_call.go:62`, `ws_resume.go:216`, `ws_incoming.go:516`. Exception: `ws_resume.go:211` (`oldClient.sessionID = ""`), see Concerns.
- [x] **`notifyWSClientChanged` RLock protects all field reads in `buildWSClientResponse`** — Step 0 applied.
- [x] **`buildWSClientResponse` calls `s.trunkManager.GetTrunkByID` which uses `tm.mu` (disjoint from `s.mu`)** — no deadlock risk.
- [x] **`clientID` read after RUnlock is safe** — set once at connect, never mutated.
- [x] **Build passes** — `go build ./...` succeeds.
- [x] **Full gateway test suite passes** — `go test ./...` all PASS.
- [x] **Commit message matches brief** — `feat(gateway): emit ws-client SSE events on state changes (+ RLock race fix)`.
- [x] **Only the 7 brief-specified files committed** — the unrelated `docs/superpowers/plans/2026-07-08-ws-clients-realtime.md` modification was left unstaged.

## Concerns

### 1. Pre-existing test infrastructure races (not blockers)

The 5 failing race-detector tests are pre-existing bugs in test stubs:
- `stubSIPCallMaker` (`server_call_identity_test.go:13-28`) — `MakeCall` writes `makeCallCount`/`lastDest`/`lastFrom`/`lastSessionID` from the goroutine spawned by `handleWSCall` (`go s.runWSCall(...)` at `ws_call.go:427`), while `waitForMakeCallCount` (`server_call_identity_test.go:45-55`) polls these fields from the test goroutine with no synchronization.
- `incomingTestSIPCallMaker` (`server_incoming_test.go`) — `RejectCall` writes from the `startIncomingRingTimeout` goroutine while the test reads.

These are out of scope for Task 4 (Task 4 is about hooking `notifyWSClientChanged`, not fixing test stubs). The proper fix would be to add a `sync.Mutex` (or `atomic.Int32` for the count) to the stub structs. Flagging for follow-up.

### 2. `oldClient.sessionID = ""` in `ws_resume.go:211` is not notified

When a resume swaps an old client for a new one, the old client's `sessionID` is cleared to prevent the old client's cleanup from deleting the new client's `wsClients` mapping. The brief's Step 5 only specifies a notify for the NEW client. As a result, SSE subscribers will not see an `updated` event for `oldClient` reflecting its cleared `sessionID` until the old client's WebSocket disconnects (which emits a `disconnected` event).

If real-time consistency for the old client's state matters, a follow-up could capture `oldClient` before `s.mu.Unlock()` and emit an `updated` event for it after the unlock. Per-brief decision.

### 3. Unprotected writes to `client.trunkResolved`/`resolvedTrunkID` in `ws_trunk.go:232-233` and `ws_call.go:311-312`

These two sites write `client.trunkResolved` and `client.resolvedTrunkID` WITHOUT holding `s.mu.Lock()`. This is a pre-existing inconsistency with `ws_midcall.go` (which DOES hold the lock for availability/callState writes). The Step 0 RLock fix on `notifyWSClientChanged` protects the READ side, but the WRITE side at these two sites remains unlocked.

Scenarios that could race:
- `handleWSTrunkResolve` writes `client.trunkResolved = true` while a concurrent `handleWSCall` validation writes `client.trunkResolved = false` for the same client.
- A concurrent `notifyWSClientChanged` (from a different event) reads these fields under RLock while the unlocked write is happening — the RLock vs unlocked write IS a data race.

In practice, these writes happen on the same WS read goroutine for the same client (WS messages are processed serially per connection), so the most likely race is between a `notifyWSClientChanged` read and the write. The race detector did NOT flag this in our tests because the tests don't exercise concurrent notify + write on the same client.

This is a pre-existing issue, out of scope for Task 4 (the brief explicitly says the trunk-resolve path is "called without holding `s.mu`" and accepts that as given). Flagging for follow-up: either acquire `s.mu.Lock()` around these writes, or document why the WS serial processing makes the lock unnecessary.

### 4. Environment setup for race detector on Windows

The race detector requires `CGO_ENABLED=1`, which requires gcc + pkg-config + opus on Windows. I installed `pkgconf` and `mingw-w64-ucrt-x86_64-opus` via msys2 pacman and created a fixed `opus.pc` at `C:\msys64\usr\lib\pkgconfig\opus.pc` (because the msys2 one uses `prefix=/ucrt64` which gcc can't resolve from a Windows working directory). This setup is local to my machine and not committed. Future agents running the race detector on this Windows machine may need to replicate it or use the existing msys64 install.
