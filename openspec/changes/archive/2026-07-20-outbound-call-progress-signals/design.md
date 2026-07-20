## Context

Outbound WebRTC→SIP calls establish the browser↔gateway PeerConnection (offer/answer/ICE) before or while the SIP INVITE is pending. Today, `OnICEConnectionStateChange` promotes `connecting` → `active` when ICE connects. Separately, `handleWScall` immediately echoes `sess.GetState()` to the client. Together these produce a false `{"type":"state","state":"active"}` while Linphone is still ringing.

SIP progress is partially tracked (`StateRinging` on 180/183, `StateActive` on 200) but `sip.Server.notifySessionStateChange` only forwards `active` and `ended` to WebSocket clients—so ringing never reaches softphone/web clients that already understand `state: ringing` and optional `type: ringing`.

Constraints: keep Opus/H.264 media paths unchanged; preserve WS contract compatibility for existing clients; do not break ICE reconnect recovery that legitimately returns to `active` after `reconnecting`.

## Goals / Non-Goals

**Goals:**

- Make outbound WS call-progress mean SIP dialog progress, not ICE readiness.
- Deliver `connecting` → `ringing` → `active` → `ended` to the calling WebSocket client.
- Keep ICE-connected keyframe/FIR recovery behavior without mutating call-progress to `active` on first connect.
- Make progress emissions observable in gateway logs.

**Non-Goals:**

- Changing RTP forwarding, SDP generation, or video recovery algorithms beyond the state promotion bug.
- Redesigning incoming-call signaling.
- Requiring client upgrades (additive signals; remove false early `active`).
- Fixing digest-auth provisional-response edge cases unless they block progress emits (track separately if found).

## Decisions

### 1. ICE connected must not set `active` on initial outbound setup

- **Choice:** On first ICE `connected`, if session is `connecting` (and not yet SIP-answered), leave call state as `connecting` (or whatever SIP progress already set: `ringing`). Still run existing FIR/PLI startup recovery.
- **Only promote to `active` from ICE when recovering from `reconnecting`** (post-answer media path recovery), matching resume semantics.
- **Alternatives:** Introduce a parallel `mediaReady` WS event — deferred; clients already key off `state`, and adding a second axis is more churn than fixing the false `active`.

### 2. Single primary progress channel: `type: "state"`

- **Choice:** Canonical progress remains `{"type":"state","sessionId","state"}` with values `connecting` | `ringing` | `active` | `ended` | `reconnecting`.
- **Additive:** On SIP 180/183, also send `{"type":"ringing","sessionId"}` for softphone-kmp-sdk `RingingMessage` compatibility. Duplicate transition is acceptable (SDK handles both).
- **Alternatives:** Only `type: ringing` without `state: ringing` — rejected; web gateway-store already uses `state`.

### 3. Widen SIP→WS notify filter

- **Choice:** `notifySessionStateChange` forwards `connecting`, `ringing`, `active`, `ended` (and keep existing reconnect/`reconnecting` paths that already notify where applicable).
- Deduplicate consecutive identical state emissions per session to avoid spam if both ICE and SIP touch nearby.

### 4. Post-`call` ack state is explicit, not a raw snapshot

- **Choice:** After accepting `call`, send `state: connecting` (or current SIP progress if already advanced), never an ICE-promoted `active`. Prefer setting session to `connecting` synchronously before the ack if INVITE send has not started.
- **Alternatives:** Omit the immediate ack and only notify from SIP — rejected; clients benefit from immediate “dialing” feedback.

### 5. Logging

- **Choice:** Log one line per outbound progress WS send: session ID, state/type, trigger (`sip-180`, `sip-200`, `ws-call-ack`, `sip-ended`, etc.). Do not log full SDP.

## Risks / Trade-offs

- **[Risk] Clients that treated early `active` as “start UI timer / request_keyframe” will wait until real SIP 200** → **Mitigation:** intended; softphone already sends keyframe on real `IN_CALL`. Document in ws-contract.
- **[Risk] Early media (183 with SDP) might deserve `ringing` vs a future `early_media` state** → **Mitigation:** map 183 to `ringing` for now (matches current session state); revisit if product needs distinct early-media UX.
- **[Risk] Duplicate `active` if both reconnect ICE and SIP notify** → **Mitigation:** skip notify when state unchanged.
- **[Risk] Auth INVITE path returning on provisional** → **Mitigation:** verify `handleInviteAuth` still reaches 200 notify; add test if broken (out of band if unrelated).

## Migration Plan

1. Deploy gateway with progress fix; no DB migration.
2. Existing clients: false early `active` disappears; they gain `ringing` if they already handle it.
3. Rollback: revert gateway build; media behavior unchanged by this change.

## Open Questions

- Should `100 Trying` emit anything? **Default: no** (noise).
- Exact duplicate policy for `type: ringing` + `state: ringing` on every 180 retransmission — **Default: emit once per transition into ringing**.
