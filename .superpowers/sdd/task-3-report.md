# Task 3 Report — notifyWSClientChanged + handleWSClientsStream + route + DRY refactor

## Status

**Complete.** All steps executed via TDD (RED → GREEN), full suite green, committed.

## What was implemented

### 1. `WSClientStreamEvent` type (`handlers_sse.go`)
New SSE event struct mirroring `SessionStreamEvent`/`TrunkStreamEvent`, carrying `Type`, `ClientID`, `At`, and an optional `*WSClientResponse` payload.

### 2. `buildWSClientResponse` helper (`handlers_sse.go`)
Extracts the per-client response-building logic that was previously inlined in `handleListWSClients` (`handlers_ops.go`). Builds a `WSClientResponse` from a `*WSClient`, including:
- core fields (clientID, ConnectedAt, trunkResolved, resolvedTrunkID, availability, callState, publicOnly)
- conditional sessionID and authClaims.Subject
- trunk public-ID enrichment via `s.trunkManager.GetTrunkByID` → `*sip.Trunk` type assertion

### 3. `notifyWSClientChanged` (`handlers_sse.go`)
Broadcasts a ws-client SSE event for a given `*WSClient`. Nil-safe. Marshals a `WSClientStreamEvent` and calls `s.broadcastWSClientStream` (from Task 2). This is the producer hook for Task 4's state-change call sites.

### 4. `handleWSClientsStream` handler (`handlers_sse.go`)
SSE handler mirroring `handleSessionStream`/`handleTrunkStream`:
- Sets `text/event-stream` headers
- Requires `http.Flusher`; returns 500 "Streaming unsupported" otherwise
- Subscribes via `subscribeWSClientStream` (defers unsubscribe)
- Writes initial `connected` event, flushes
- 25s heartbeat ticker
- Forwards channel payloads as `event: ws-client`; heartbeats as `event: heartbeat`
- Exits on `r.Context().Done()`

### 5. DRY refactor — `handleListWSClients` (`handlers_ops.go`)
Replaced ~30 lines of inline per-client logic + post-unlock trunk enrichment loop with a single call to `buildWSClientResponse` inside the `s.mu.RLock()` iteration. Removed the now-unused `"k2-gateway/internal/sip"` import.

### 6. Route registration (`server.go`)
Added `api.HandleFunc("/ws-clients/stream", s.handleWSClientsStream).Methods("GET", "OPTIONS")` immediately after the `/ws-clients` route.

### 7. Test — `TestHandleWSClientsStream_Contract` (`handlers_api_test.go`)
Contract test mirroring `TestHandleSSEStreams_Contract`: spawns the handler in a goroutine with a `flushRecorder`, asserts the `connected` event is written within 750ms, then cancels the context and asserts the handler exits within 1s.

## Lock safety analysis — `GetTrunkByID` under `s.mu.RLock()`

**Verdict: Safe. No restructure needed.**

Evidence from `internal/sip/trunk_manager.go:981-986`:
```go
func (tm *TrunkManager) GetTrunkByID(id int64) (interface{}, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	trunk, ok := tm.trunks[id]
	return trunk, ok
}
```

- `GetTrunkByID` acquires `tm.mu.RLock()` — the **TrunkManager's own** `sync.RWMutex`.
- `handleListWSClients` acquires `s.mu.RLock()` — the **API Server's** `sync.RWMutex`.
- These are **two independent mutexes** on two different structs. Acquiring `tm.mu.RLock()` while already holding `s.mu.RLock()` is a read-read on different locks — no lock-ordering inversion, no deadlock, no contention.
- `GetTrunkByID` performs only a map read and returns the pointer; it does not mutate state, so it is safe to invoke from within `s.mu.RLock()`'s critical section.

The original `handleListWSClients` did trunk enrichment in a **separate loop after `s.mu.RUnlock()`** (operating on the snapshot `[]WSClientResponse`). The refactored version moves enrichment **inside** the RLock scope (via `buildWSClientResponse`). This is safe because the trunk lookup uses a disjoint mutex. The behavioral result is identical: each response carries the trunk's public ID when resolved. The window during which `GetTrunkByID` returns a pointer that is then read (`trunk.PublicID`) is the same read-only access pattern used elsewhere in the codebase (e.g., `handlers_trunk.go`).

## Test results

### RED phase (pre-implementation)
```
internal\api\handlers_api_test.go:1286:7: srv.handleWSClientsStream undefined
FAIL k2-gateway/internal/api [build failed]
```
Confirmed test fails for the expected reason (handler missing).

### Focused tests (post-implementation)
```
go test ./internal/api -run "TestHandleWSClientsStream_Contract|TestHandleSSEStreams|TestHandleListWSClients" -v
=== RUN   TestHandleSSEStreams_Contract           --- PASS (0.01s)
=== RUN   TestHandleListWSClients_IncludesResolvedTrunkAndClientState --- PASS (0.00s)
=== RUN   TestHandleListWSClients_IncludesIdleAndClientID             --- PASS (0.00s)
=== RUN   TestHandleWSClientsStream_Contract     --- PASS (0.01s)
PASS
ok      k2-gateway/internal/api    1.441s
```
The two pre-existing `TestHandleListWSClients_*` tests pass unchanged — the DRY refactor preserves behavior.

### Full suite
```
go test ./...
ok      k2-gateway/internal/api    2.705s
ok      k2-gateway/internal/audio  (cached)
ok      k2-gateway/internal/auth   (cached)
ok      k2-gateway/internal/config  (cached)
ok      k2-gateway/internal/logstore (cached)
ok      k2-gateway/internal/push   (cached)
ok      k2-gateway/internal/session (cached)
ok      k2-gateway/internal/sip     (cached)
ok      k2-gateway/internal/sipclientauth (cached)
ok      k2-gateway/internal/translator (cached)
ok      k2-gateway/internal/translator/pb (cached)
```
All packages pass. No failures.

### `go vet ./internal/api/...`
Clean (no output).

### `gofmt -l` on modified files
Clean (no files listed — all already formatted).

## Commit

- Hash: **d5dcbc4** (full: `d5dcbc41c4447054610e88e4f2a30a6486fc59b2`)
- Branch: `1.3.1`
- Message: `feat(gateway): add /ws-clients/stream SSE endpoint + DRY refactor handleListWSClients`
- Files (4):
  - `apps/gateway/internal/api/handlers_api_test.go` (+35)
  - `apps/gateway/internal/api/handlers_ops.go` (-30 net: removed inline logic + sip import)
  - `apps/gateway/internal/api/handlers_sse.go` (+98: event type, notify fn, build helper, handler, sip import)
  - `apps/gateway/internal/api/server.go` (+1: route)
- Total: 135 insertions, 30 deletions

Only the 4 task-scoped files were staged. An unrelated modified file (`docs/superpowers/plans/2026-07-08-ws-clients-realtime.md`) and untracked frontend/UI files were left out of the commit.

## Self-review

| Check | Result |
|-------|--------|
| DRY refactor doesn't break existing tests | ✅ `TestHandleListWSClients_*` both pass unchanged |
| Lock safety of `GetTrunkByID` under `s.mu.RLock()` | ✅ Safe — `tm.mu` ≠ `s.mu` (disjoint mutexes) |
| `handlers_sse.go` imports include `k2-gateway/internal/sip` | ✅ Added (used by `*sip.Trunk` assertion) |
| `handlers_ops.go` no longer imports `sip` (now unused) | ✅ Removed |
| `handlers_api_test.go` imports unchanged | ✅ `context`, `strings`, `time`, `httptest` already present |
| `server.go` route registered after `/ws-clients` | ✅ Line 275 |
| TDD: test failed first for the right reason | ✅ RED confirmed (undefined handler), then GREEN |
| `go vet` clean | ✅ |
| `gofmt` clean | ✅ |
| Full `go test ./...` passes | ✅ |

## Concerns

- **None blocking.** The `notifyWSClientChanged` producer is wired but has **no call sites yet** — by design, Task 4 will invoke it from WS state-change hooks (offer/call/hangup/accept/reject/resume/trunk_resolve). No dead code warning (it's a method, not a package-level func), but static analysis may flag it until Task 4 lands.
- **Behavioral equivalence note:** the original `handleListWSClients` enriched trunk public IDs after `RUnlock()` (operating on snapshot copies). The refactor moves enrichment inside the RLock via `buildWSClientResponse`. Both call `GetTrunkByID` read-only; the only observable difference would be if a concurrent trunk mutation happened between snapshot and enrichment in the old code — the new code is actually *more* consistent (all client fields and trunk enrichment are read under the same RLock). This is strictly an improvement; existing tests confirm equivalence.
- The test does not assert the `ws-client` event *payload* shape (only the `connected` event). This matches the brief's verbatim test and the existing `TestHandleSSEStreams_Contract` pattern. Payload-level assertions are deferred to Task 4 integration tests where `notifyWSClientChanged` is exercised end-to-end.
