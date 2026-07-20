## Context

Outbound WebRTC→SIP calls through Asterisk often get SIP `200 OK` almost immediately, while the human callee on Linphone answers seconds later. Production logs on k2-gateway show:

- Gateway marks `Call answered (authenticated)!` within ~40ms of INVITE.
- Periodic PLI-to-browser starts at RTP setup and ends after a fixed ~20s startup window.
- First remote SIP video SSRC (Linphone actually sending video) often arrives at ~15–29s — sometimes after that window.
- Linphone sends RR/SR/SDES but not PLI/FIR in observed sessions.
- On SSRC learn, gateway already bursts FIR/PLI **to Asterisk** (SIP→WebRTC recovery) but does **not** request a keyframe from the browser (WebRTC→SIP).

Symptom: Linphone shows black remote video on slow answer; WebRTC client remote video remains fine.

Constraints: preserve Opus/H.264 passthrough invariants; no WS contract change; avoid RTCP storms; coexist with `@switch` recovery which already kicks both directions.

## Goals / Non-Goals

**Goals:**

- On first remote SIP video SSRC learn per session, request a fresh uplink keyframe from the WebRTC browser (FIR + short PLI burst).
- Keep existing periodic PLI-to-browser as an early-call safety net.
- Dedupe so only the first remote-join kick fires for a normal call (not every SSRC change).
- Log the kick for production correlation.

**Non-Goals:**

- Extending the periodic PLI window as the primary fix.
- Changing WS `state` / `media` contract or requiring client SDK updates.
- Teaching Linphone to send PLI (interop-dependent; not reliable here).
- Redesigning `@switch` recovery (it already kicks both ends; leave as-is).
- Solving unrelated mid-call freeze after the first successful decode.

## Decisions

### 1. Trigger: first remote video SSRC learn

Hook the existing SSRC-learn path in `apps/gateway/internal/sip/rtp.go` (same place that starts Asterisk FIR/PLI burst and flushes pending browser keyframe requests).

Why: this is the earliest reliable signal that the SIP peer’s media path is live. Waiting for complete remote IDR (`remote-media-ready`) is later than needed for uplink kick and couples to downlink decode readiness.

**Alternatives:**

- Extend periodic PLI to 60s — rejected as primary; still fails if answer > window; wastes RTCP on fast answers.
- Kick only on `media video remote receiving` — workable but later; SSRC learn is enough and already runs recovery goroutine.
- Kick on SIP 200 — too early; Asterisk answers before Linphone joins.

### 2. Action: FIR + short PLI burst to WebRTC

Mirror the shape used for `@switch` immediate kick / startup recovery:

1. `SendFIRToWebRTC()` once
2. Short guarded `SendPLItoWebRTC()` burst (reuse existing startup attempt/interval constants or a small dedicated count)

Do this in the same goroutine that already sends Asterisk FIR/PLI on SSRC learn, so the hot RTP read loop stays non-blocking.

**Alternatives:** Single PLI only — often enough for Chrome, but FIR helps stubborn encoders; cheap to send both once.

### 3. Dedupe: once per session (first learn)

Session flag e.g. `uplinkKeyframeKickOnRemoteJoinDone` (mutex-protected):

- First time `RemoteVideoSSRC` goes from `0` → non-zero: arm kick.
- Later SSRC changes: do **not** re-arm this kick ( `@switch` / switch recovery remains responsible for mid-call target changes).

**Alternatives:** Kick on every SSRC change — risk of storms with flaky peers. Align with switch authority — over-coupled for v1.

### 4. Keep periodic PLI unchanged

Leave `startPeriodicPLIForSession` (~20s) as-is. Remote-join kick covers the late-answer gap; periodic PLI covers early media before SSRC learn.

### 5. Observability

Log a clear line such as `uplink_keyframe_kick reason=remote-ssrc-learn` with session ID when the kick runs (and a skip reason when deduped).

## Risks / Trade-offs

- **[Risk] Extra RTCP to browser on every call that receives remote video** → Mitigation: one short burst per session; already sending similar volume early via periodic PLI.
- **[Risk] SSRC learn happens before Linphone decoder is ready** → Mitigation: FIR/PLI still produce IDRs that Asterisk can forward when bridge is up; better than never requesting after window end. Optional follow-up: second kick on first remote complete-IDR if needed.
- **[Risk] Interaction with `@switch`** → Mitigation: first-learn-only flag; switch path keeps its own dual-direction bursts.
- **[Risk] Browser ignores PLI** → Mitigation: include FIR; existing SPS/PPS inject on IDR path remains.

## Migration Plan

1. Deploy gateway with uplink kick on first SSRC learn.
2. Verify in logs: late-answer sessions show `uplink_keyframe_kick` near remote SSRC learn and subsequent `KEYFRAME (Forwarded)` toward Asterisk.
3. Rollback: revert gateway binary/config; no client/DB migration.

## Open Questions

- Should a second kick fire on first remote complete-IDR if no uplink keyframe was observed after the SSRC-learn kick? **Default v1: no; add only if production still shows black after first kick.**
- Should first Linphone RR (without RTP yet) also arm a kick? **Default v1: no; SSRC learn from video RTP is stronger.**
