# Task 2 Report — Gateway: SSE subscribe/broadcast helpers for ws-client stream

## What I implemented

Three changes verbatim from the task brief:

1. **`apps/gateway/internal/api/server.go` (struct fields)** — Added `wsClientStreams map[int]chan []byte` and `wsClientStreamSeq int` after `sessionStreamSeq` on the `Server` struct.

2. **`apps/gateway/internal/api/server.go` (NewServer init)** — Added `wsClientStreams: make(map[int]chan []byte),` after `sessionStreams: make(...)` in the `NewServer` constructor.

3. **`apps/gateway/internal/api/ws_util.go` (helpers)** — Appended three methods after `broadcastSessionStream`, mirroring the existing trunk/session stream pattern (`ws_util.go:160-224`):
   - `subscribeWSClientStream() (int, chan []byte)` — allocates id + buffered(32) chan under `s.mu` write lock
   - `unsubscribeWSClientStream(id int)` — removes entry under write lock
   - `broadcastWSClientStream(payload []byte)` — snapshots channels under `s.mu.RLock`, then non-blocking send to each (drops on full buffer, preserving the "no panics in hot paths" invariant)

The helpers are intentionally unused in this task — Task 3 wires them into the SSE handler.

## Verification

### Build
```
$ go build ./...
(no output — success)
```

### go vet
```
$ go vet ./internal/api/...
(no output — clean)
```

### Targeted test (per brief Step 4)
```
$ go test ./internal/api -run "TestHandleSSEStreams" -v
=== RUN   TestHandleSSEStreams_Contract
--- PASS: TestHandleSSEStreams_Contract (0.01s)
PASS
ok      k2-gateway/internal/api  1.424s
```

### Full internal/api suite (regression check)
```
$ go test ./internal/api/...
ok      k2-gateway/internal/api  2.683s
```

All green. No regressions.

## Commits

- `2ba1e00` feat(gateway): add ws-client SSE stream subscribe/broadcast helpers
  - 2 files changed, 36 insertions(+)

Only the two files specified by the brief were staged (`server.go`, `ws_util.go`). A pre-existing unstaged change to `docs/superpowers/plans/2026-07-08-ws-clients-realtime.md` and other untracked files were intentionally left out of the commit.

## Self-review

- ✅ Code is verbatim from the brief (whitespace and structure match the trunk/session stream pattern).
- ✅ Field alignment: `wsClientStreams`/`wsClientStreamSeq` placed in the SSE-streams region of the struct alongside `trunkStreams`/`sessionStreams`.
- ✅ Lock discipline mirrors existing pattern: write lock for subscribe/unsubscribe (mutates map + seq), read lock for broadcast snapshot, no I/O under lock.
- ✅ Non-blocking send in broadcast (`select` with `default`) upholds "no panics / no blocking in hot paths" rule from gateway AGENTS.md.
- ✅ Unused fields/methods expected for this task — Task 3 will wire them into the SSE handler. `go vet` does not flag them (methods on `*Server` are exported, so no unused-symbol errors).
- ✅ No wire/API contract changes in this task.

## Concerns

None.
