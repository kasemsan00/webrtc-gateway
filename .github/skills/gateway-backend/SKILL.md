---
name: gateway-backend
description: 'Work safely in apps/gateway, the Go WebRTC to SIP bridge. Use when changing Go handlers, session flows, SIP or SDP logic, RTP or RTCP forwarding, WebSocket payloads, auth, trunking, resume, or LogStore behavior in apps/gateway. Includes media invariants, concurrency checks, contract sync rules, and required validation.'
argument-hint: 'Describe the apps/gateway change or investigation'
user-invocable: false
---

# Gateway Backend

Use this skill for tasks in `apps/gateway` where correctness under live media load matters more than local code elegance.

## When To Use

- Editing Go code under `apps/gateway/internal/**` or `apps/gateway/main.go`
- Changing WebSocket or REST request and response payloads
- Modifying SIP, SDP, RTP, RTCP, session resume, trunking, auth, or LogStore behavior
- Reviewing gateway backend changes for regressions, protocol compatibility, or missing validation

## Non-Negotiable Constraints

- No panics in hot paths.
- Do not hold locks while doing network I/O.
- Preserve audio as Opus passthrough.
- Preserve video as H.264 only.
- Keep SPS or PPS caching, keyframe requests, and reinjection behavior intact unless the task explicitly changes media behavior.
- Treat WebSocket and SIP contract changes as breaking until clients and docs are updated together.

## Hotspot Anchors

Start from these files before widening scope:

- `apps/gateway/internal/api/server.go` for WebSocket handlers, payload routing, call control, resume entry points, and most client contract changes
- `apps/gateway/internal/session/session.go`, `session_state.go`, and `session_media.go` for session lifecycle and runtime state
- `apps/gateway/internal/session/rtp_forward.go`, `keyframe.go`, `h264_paramsets.go`, and `sdp_h264.go` for media-path changes and H.264 behavior
- `apps/gateway/internal/sip/server.go`, `handlers.go`, `call.go`, `dialog.go`, and `registration.go` for SIP signaling behavior
- `apps/gateway/internal/sip/sdp.go` and `rtp.go` for SIP media negotiation and RTP flow
- `apps/gateway/internal/sip/trunk_manager.go` and `trunk_public_id.go` for trunk resolution and multi-instance routing
- `apps/gateway/docs/dual-flow.md` for end-to-end flow expectations
- `apps/gateway/docs/call-resume.md` for reconnect and resume behavior

## Procedure

1. Start from the owning code path.
   - Anchor on the specific handler, session method, SIP flow, or media helper that directly controls the behavior.
   - Prefer the nearest deciding code over wide repository exploration.
   - Use the hotspot list above to choose the first file instead of scanning broadly.

2. Classify the change before editing.
   - Control-plane changes: auth, HTTP or WS handlers, JSON payloads, trunk resolution, resume orchestration, LogStore.
   - Media-path changes: SDP negotiation, RTP or RTCP forwarding, keyframe logic, codec configuration, SIP dialog media behavior.
   - Treat media-path changes as high risk and minimize scope.

3. Reconfirm the local invariants.
   - For concurrency changes, check shared-state ownership and lock boundaries.
   - For signaling changes, check request and response compatibility across client and server.
   - For media changes, verify Opus passthrough and H.264-only assumptions still hold.

4. Make the smallest root-cause edit.
   - Favor narrow changes in the controlling abstraction.
   - Avoid speculative cleanup or unrelated refactors in `apps/gateway`.
   - Prefer explicit errors and logs over panic paths.

5. Sync adjacent contract surfaces when required.
   - If a WebSocket message type or payload changes, update the gateway server handling, the frontend client contract in `apps/frontend`, and gateway docs.
   - If auth behavior changes, verify both HTTP and WebSocket auth entry points.
   - If resume or trunk behavior changes, check the cross-instance or redirect path as well as the local happy path.

6. Validate immediately after the first real edit.
   - Run the narrowest useful check first.
   - Preferred order:
     1. focused Go test for the touched package if one exists
     2. `go test ./...` from `apps/gateway`
     3. `go build -o k2-gateway .` from `apps/gateway` when compilation coverage is the main concern
   - If the change affects payload contracts or docs, verify those updates before expanding scope.

7. Finish with regression checks.
   - Confirm no new panic or race risk was introduced.
   - Confirm protocol changes are documented.
   - Call out any untested media-path risk explicitly if no automated coverage exists.

## Required References

- Read `apps/gateway/AGENTS.md` before substantial gateway changes.
- Use `apps/gateway/docs/dual-flow.md` for call flow or signaling behavior.
- Use `apps/gateway/docs/call-resume.md` for resume or reconnect changes.
- Check `apps/gateway/project.json` for standard build and test commands.

## Completion Criteria

- The change is scoped to the controlling gateway code path.
- Media and signaling invariants still hold.
- Any contract changes are updated in code and docs together.
- At least one executable validation step was run, unless the environment prevents it.
- Remaining risks are stated plainly when validation coverage is incomplete.