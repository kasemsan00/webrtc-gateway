# Gateway API LLM-Friendly Refactor — Design Spec

**Date:** 2026-07-06  
**Status:** Approved  
**Scope:** `apps/gateway/internal/api/` mechanical file split + `AGENTS.md` slimming

---

## Problem

`internal/api/server.go` (~2,925 lines) and `handlers.go` (~2,244 lines) concentrate most API logic in two files. LLM agents working on a single WebSocket or REST flow must load ~5,200 lines of context. `AGENTS.md` (~466 lines) duplicates config reference and is partially stale.

## Goals

1. Reduce per-task LLM context to ~700–1,200 lines by splitting files by domain.
2. Improve human maintainability with clear file responsibilities.
3. Zero behavior change — mechanical move only in this phase.
4. Slim `AGENTS.md` to ~120–150 lines with file map and "where to start" guide.

## Non-Goals (Phase 1)

- Sub-packages (`api/ws`, `api/rest`)
- Refactoring `internal/sip/` (phase 2)
- Logic changes, renames of public APIs, or new features

## Approach

Split within `package api` following existing precedent (`auth_http.go`, `client_diagnostics.go`, `mobile_sip_provision.go`). All methods remain `(s *Server)` receivers. Existing tests are the regression gate.

## Target File Layout

```
internal/api/
├── server.go              # Server struct, interfaces, types, NewServer, Start, routing
├── auth_http.go           # (existing)
├── mobile_sip_provision.go # (existing)
├── client_diagnostics.go  # (existing)
├── ws_conn.go             # connect, upgrade, write pump
├── ws_dispatch.go         # message router, public-only guards
├── ws_call.go             # offer, ice, call, hangup, dtmf
├── ws_incoming.go         # accept, reject, push, ring timeout
├── ws_resume.go           # resume, SDP diagnostics
├── ws_trunk.go            # trunk_resolve, trunk_push_token
├── ws_midcall.go          # renegotiate_answer, keyframe, client_state, ping
├── ws_translate.go        # translate, translate_stop, translation captions
├── ws_notify.go           # Notify* callbacks, send_message WS handler
├── ws_util.go             # sendWSMessage, logging helpers, SSE broadcast internals
├── handlers.go            # shared REST types, respondJSON/Error
├── handlers_call.go       # offer, call, hangup, dtmf, switch REST
├── handlers_trunk.go      # trunk CRUD, register/unregister, heartbeat
├── handlers_session.go    # sessions, history, events, payloads, dialogs, stats
├── handlers_ops.go        # dashboard, instances, ws-clients, public accounts
├── handlers_sse.go        # trunk/session SSE streams
└── handlers_log.go        # /api/logs/*
```

## AGENTS.md Restructure

Slim `apps/gateway/AGENTS.md` to mission rules, architecture, api file map, WS summary, where-to-start table, build/test.

Extract to `docs/gateway/`:

- `ws-contract.md` — full WS message types
- `config-reference.md` — env vars
- `ops-guide.md` — log API, diagnostics, PowerShell
- `troubleshooting.md` — 488, video, auth

Update gateway repo map with `push/`, `translator/`, `audio/`, `sipclientauth/`.

## Agent Rules (add to AGENTS.md)

1. Use file map — do not read `server.go` wholesale for WS handler work.
2. WS message changes: update `ws_dispatch.go` + handler file + `docs/gateway/ws-contract.md` + frontend `gateway-store.ts`.
3. New files should stay ≤ ~600 lines; split further if exceeded.
4. Do not merge unrelated handlers back into monolith files.
5. Mechanical moves only — no behavior changes in this refactor.

## Success Criteria

| Metric | Before | After |
|--------|--------|-------|
| `server.go` lines | ~2,925 | ≤ 400 |
| `handlers.go` lines | ~2,244 | ≤ 200 |
| Largest `api/` file | ~2,925 | ≤ 700 |
| `AGENTS.md` lines | ~466 | ≤ 150 |
| `go test ./...` | pass | pass |

## Risks

| Risk | Mitigation |
|------|------------|
| Missed shared helper | Move `ws_util.go` first; unexported helpers stay in `package api` |
| Import errors after split | `go build` after each task |
| `handleWSMessage` case drift | Do not edit switch logic during moves |
| Media regression | Do not touch `session/` or `sip/` in phase 1 |

## Phase 2 (Future)

Apply same mechanical split to `internal/sip/trunk_manager.go` and `call.go`.
