## Context

Outbound WebRTC→SIP calls often receive SIP `200 OK` (and thus WS `state: active`) from Asterisk before Linphone remote video RTP arrives. Call-progress signaling (`outbound-call-progress-signals`) correctly separates ICE from SIP answer, but does not tell the client when remote video is actually present. The gateway already observes remote video SSRC learn, SPS/PPS, and `h264_au_normalized status=complete-idr` on the SIP→WebRTC path; those observations are log-only today.

Constraints: additive WS contract; no change to `active`/`ringing` meaning; Opus/H.264 media path behavior preserved; notify only the WebSocket client bound to the session.

## Goals / Non-Goals

**Goals:**

- Emit a one-shot (per kind) WS media-ready event when remote SIP video first becomes decode-ready.
- Optionally emit the same message shape for first remote SIP audio RTP.
- Keep call-progress and media-presence as independent axes.
- Log emissions for production correlation.

**Non-Goals:**

- Replacing or delaying `state: active`.
- Client-side-only detection via WebRTC stats (rejected as primary approach).
- Continuous media quality telemetry / bitrate stats streams.
- Changing `@switch` / renegotiate `hasVideo` semantics (document coexistence only).
- Implementing softphone-kmp-sdk UI in the same gateway change (contract + gateway emit first; SDK task listed as follow-on or coordinated).

## Decisions

### 1. Message shape: `type: "media"`

```json
{
  "type": "media",
  "sessionId": "<id>",
  "kind": "video",
  "direction": "remote",
  "state": "receiving"
}
```

- **`kind`:** `video` | `audio`
- **`direction`:** `remote` (SIP→WebRTC toward the client). Local/uplink not in v1.
- **`state`:** `receiving` for first ready observation. Reserve room for future `lost` / `recovered` without committing now.

**Alternatives:** `remote_video_ready` dedicated type — rejected; one family scales to audio. Reuse `renegotiate` — rejected; that is mid-call SDP assistance, not first-packet presence.

### 2. Video readiness criteria (strict)

Emit remote video `receiving` only when **all** are true once:

1. Remote SIP video SSRC learned (or equivalent RTP source established)
2. SPS and PPS cached from SIP stream (or injected path has parameter sets available)
3. At least one **complete IDR** access unit normalized/forwardable (`h264_au_normalized` complete-idr path)

Hook near existing AU normalize / first-IDR success (same place that logs `complete-idr` / settles startup recovery), not merely first small RTP packet.

**Alternatives:** Emit on SSRC-learn only — too early (black/corrupt). Emit only on “Startup recovery settled” — acceptable fallback but slightly later; prefer first complete-IDR for snappier UX, with settle as optional second confirmation **not** required for v1.

### 3. Audio readiness criteria (optional v1)

Emit remote audio `receiving` on first successfully received SIP audio RTP packet forwarded (or accepted) toward the WebRTC audio track. Dedupe once per session.

### 4. Notify path and dedupe

- Session flags: `remoteVideoReadyNotified`, `remoteAudioReadyNotified` (atomic / mutex).
- Call into API notifier (mirror `NotifySessionState` / mid-call pattern): `NotifyRemoteMedia(sessionID, kind, direction, state)`.
- If no WS client bound, skip (best-effort); do not buffer across reconnect for v1 (resume clients can rely on subsequent frames / optional future replay).

### 5. Coexistence with call progress

```
SIP 200        → state: active
first remote IDR → media video remote receiving
hangup         → state: ended
```

Clients that ignore `media` remain unchanged. Clients that want “show remote tile” wait for `media` (or timeout UX).

## Risks / Trade-offs

- **[Risk] Video-only calls without IDR never emit** → Mitigation: FIR/PLI recovery already requests keyframes; if still no IDR, no false ready (correct).
- **[Risk] @switch changes SSRC mid-call** → Mitigation: v1 one-shot only; do not re-emit on switch (document; future `recovered` if needed).
- **[Risk] Notify from RTP hot path blocks** → Mitigation: non-blocking send to WS channel; take flag under short lock before notify.
- **[Risk] SDK not updated yet** → Mitigation: additive message; gateway can ship first; UX incomplete until SDK handles it.

## Migration Plan

1. Deploy gateway with `media` emits + contract doc.
2. Update softphone-kmp-sdk / apps to handle `media`.
3. Rollback: revert gateway; unknown `type` ignored by old clients.

## Open Questions

- Should resume after WS reconnect re-send sticky `media` if flags already set? **Default v1: no.**
- Include `ssrc` in payload for diagnostics? **Default: omit from wire; log only.**
