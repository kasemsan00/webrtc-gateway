# Fix Report — ws-clients-realtime final-branch review (I1 + M1)

- **Branch:** `1.3.1`
- **Commit:** `f40b7d2adedd6261cfa08e6fb99693a4d73d034d`
- **Message:** `fix(gateway): lock trunk-resolved writes + notify oldClient on resume-swap`
- **Files changed (3, +7):** `apps/gateway/internal/api/ws_trunk.go`, `ws_call.go`, `ws_resume.go`

---

## Finding 1 (I1) — Unlocked writes to `trunkResolved`/`resolvedTrunkID` race with RLock reader

### Root cause
`notifyWSClientChanged` → `buildWSClientResponse` reads `client.trunkResolved` and
`client.resolvedTrunkID` under `s.mu.RLock()` (`handlers_sse.go:59`, fields read at
`handlers_sse.go:78-79`). Two call sites wrote those same fields with **no lock held**,
then immediately called `notifyWSClientChanged` — a classic write-vs-read data race.

### Site A — `ws_trunk.go` (`handleWSTrunkResolve`, now lines 231-236)

**Lock-state analysis:** `handleWSTrunkResolve` (starts line 128) acquires no `s.mu`
lock prior to this point (verified by reading the full function prologue 128-231 and a
repo-wide grep of `s.mu.(R?Lock|Unlock)`). The write was plain, then notify.

Before:
```go
if *leaseOwner == s.gatewayConfig.InstanceID {
    client.trunkResolved = true
    client.resolvedTrunkID = trunkID
    s.notifyWSClientChanged("updated", client)
```

After:
```go
if *leaseOwner == s.gatewayConfig.InstanceID {
    s.mu.Lock()
    client.trunkResolved = true
    client.resolvedTrunkID = trunkID
    s.mu.Unlock()
    s.notifyWSClientChanged("updated", client)
```

`Unlock` is called **before** `notifyWSClientChanged` (which takes `RLock` internally) —
no self-deadlock. Locked section contains no I/O.

### Site B — `ws_call.go` (`handleWSCall`, now lines 310-315)

**Lock-state analysis:** `handleWSCall` (starts line 228) acquires no `s.mu` lock prior
to this point (verified by reading 228-311 and the lock grep — nearest `s.mu.Lock` in
this file is `ws_call.go:63`, in a different function). The write was plain, then notify.

Before:
```go
if validation.reason != "" {
    client.trunkResolved = false
    client.resolvedTrunkID = 0
    s.notifyWSClientChanged("updated", client)
```

After:
```go
if validation.reason != "" {
    s.mu.Lock()
    client.trunkResolved = false
    client.resolvedTrunkID = 0
    s.mu.Unlock()
    s.notifyWSClientChanged("updated", client)
```

Same safe pattern: `Unlock` before the `RLock`-taking notify; no I/O in the critical
section.

### Out-of-scope write site verified safe (NOT changed)
A third write to these fields exists at `ws_conn.go:103-104`
(`client.trunkResolved = true; client.resolvedTrunkID = provisioned.TrunkID`).
**This is safe and intentionally left untouched.** The `client` is freshly constructed
(`ws_conn.go:90-98`) and is not yet published to `s.wsConnections`/`s.wsClients`
(those registrations happen at `ws_conn.go:106-108`) nor handed to any goroutine (the
write pump starts at `ws_conn.go:112`). There is no concurrent reader, so no race —
this is benign field initialization. The review finding correctly scoped to Sites A
and B only.

---

## Finding 2 (M1) — `oldClient.sessionID=""` on resume-swap not notified

### Root cause
In `handleWSResume` (`ws_resume.go:207-218`), on a resume-swap the old client's
`sessionID` is cleared to `""` under `s.mu.Lock()` so its cleanup won't delete the new
client's mapping — but no `notifyWSClientChanged` was fired for the old client. The
WS Clients SSE stream therefore kept showing a stale row claiming a `sessionId` the
old client no longer owns.

### Fix — `ws_resume.go` (now lines 217-221)

**Lock-state analysis:** the writes (`oldClient.sessionID = ""`, the `s.wsClients`
reassignment, `client.sessionID = msg.SessionID`) all occur **inside** an existing
`s.mu.Lock()` block (`ws_resume.go:207`, `Unlock` at `:217`). The new client's notify
already runs **after** `Unlock` at `:218`. So the oldClient notify must also be placed
after `Unlock` (it takes `RLock` internally).

After:
```go
s.wsClients[msg.SessionID] = client
client.sessionID = msg.SessionID
s.mu.Unlock()
s.notifyWSClientChanged("updated", client)
if hadOldClient && oldClient != client {
    s.notifyWSClientChanged("updated", oldClient)
}
```

`hadOldClient` and `oldClient` are locals captured under the lock; reading them after
`Unlock` is safe (they are not mutated by anyone else). The `oldClient != client`
guard avoids a redundant notify in the no-swap case (where the new client's notify
already covers the row). The notify itself is safe: `notifyWSClientChanged` nil-checks
the client first (`handlers_sse.go:56-58`); the old client's own WS connection may still
be draining, but the SSE response is built from its current fields under `RLock`, which
is the desired "row now shows empty sessionId" semantics.

---

## Verification

| Check | Command | Result |
|---|---|---|
| Build | `go build ./...` | PASS (no output) |
| Format | `gofmt -l <3 files>` | PASS (no files listed) |
| Full test suite | `go test ./...` | PASS — `internal/api 2.804s`; all other packages OK/cached |
| Targeted HandleWS tests | `go test ./internal/api/... -run "TestHandleWS" -v` | PASS — all `TestHandleWSTrunk*` / `TestHandleWSResume*` etc. green, incl. trunk-resolve paths that exercise Site A |
| Static checks | `go vet ./internal/api/...` | PASS (clean) |
| Race detector | `go test -race ./internal/api/... -run "TestHandleWS"` | **NOT RUN — environment limitation (see below)** |

### Race detector — could not be built (environment limitation, not a code issue)
Go's race detector on Windows **requires gcc** (MinGW-w64); MSVC `cl.exe`/clang are not
supported by cgo's race build. This machine has:
- No `gcc` / MinGW anywhere on PATH or common install dirs (Git for Windows, scoop,
  chocolatey, msys64, Strawberry, etc. — all checked, none present).
- VS 2022 + Windows SDK 10.0.22621/26100 are installed, so `cl.exe` is available via
  `vcvars64`, but cgo passes gcc-style flags (`/Werror`) that `cl` rejects:
  `cl : Command line error D8021 : invalid numeric argument '/Werror'`.
- A `clang` from the Swift toolchain exists but has no sysroot (`stdlib.h`,
  `windows.h`, `errno.h` not found).

Per Go's official docs, the race detector requires gcc on Windows; this environment
simply lacks a working MinGW toolchain. The requested `-race` run therefore fails at
*build* time, before any test executes — unrelated to these changes. The race cannot be
exercised in CI here either (no `.github/workflows`).

### Confidence reasoning for the lock fixes (in lieu of `-race`)
- The two field writes at Sites A/B are now enclosed by `s.mu.Lock()/Unlock()`, matching
  the `s.mu.RLock()` reader in `buildWSClientResponse` — the happens-before edge the
  race detector would have flagged is now established.
- `Unlock` precedes every `notifyWSClientChanged` call (which takes `RLock`), so there
  is no `Lock`-then-`RLock` self-deadlock.
- Critical sections are minimal and contain no network I/O (per gateway AGENTS.md
  concurrency rule).
- Finding 2's notify is placed strictly after `Unlock` and reuses the existing
  notify helper, so it inherits the same `RLock` discipline.

---

## Summary

- **Status:** COMPLETE. Both findings fixed, committed.
- **Commit hash:** `f40b7d2adedd6261cfa08e6fb99693a4d73d034d`
- **Test summary:** `go test ./...` green; `TestHandleWS*` green; `go vet` clean;
  `gofmt` clean; `go build` clean.
- **Concerns:**
  1. The `-race` test could not be executed in this environment (no MinGW gcc). To
     enable race detection here, install MinGW-w64 (e.g.
     `winget install MartinStorsjo.LLVM-Mingw` or MSYS2's `mingw-w64-x86_64-gcc`) and
     ensure `gcc` is on `PATH`. Recommended to add to a future CI job as well, since
     the repo currently has no CI workflows (`.github/workflows/` is empty).
  2. Out-of-scope: the benign `ws_conn.go:103-104` init-time write is intentionally
     left as-is (no reader exists before the client is published).
