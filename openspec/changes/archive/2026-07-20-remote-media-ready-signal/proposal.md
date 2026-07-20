## Why

Outbound call `state: active` means the SIP dialog was answered (often by Asterisk immediately), not that remote video from Linphone/the far endpoint is actually flowing. WebRTC clients already have a receive video track after offer/answer, so local track events cannot tell “picture is available yet.” Operators and softphone UIs need a separate, gateway-observed media signal when the first decode-ready remote video arrives from SIP.

## What Changes

- Add an additive WebSocket server→client message when the gateway first observes remote SIP video that is ready to show (SSRC learned + SPS/PPS + at least one complete IDR).
- Optionally emit a paired first-remote-audio signal when the first SIP audio RTP is received (same message family, different `kind`).
- Emit each media-ready signal at most once per session (dedupe); do not redefine `state: active` / call-progress semantics.
- Document the new message in `docs/gateway/ws-contract.md`.
- Softphone-kmp-sdk / web clients consume the event in a follow-on or coordinated update (gateway ships the contract first; client handling is required for UX).

## Capabilities

### New Capabilities

- `remote-media-ready`: Gateway notifies the bound WebSocket client when remote SIP media (video, optionally audio) is first observed as receiving/decode-ready, independent of SIP call-progress state.

### Modified Capabilities

- (none)

## Impact

- **Gateway:** SIP→WebRTC video path (`internal/session` RTP / H.264 AU normalize / SSRC learn), WS notify (`internal/api/ws_notify.go` or equivalent), session dedupe flags, tests, `docs/gateway/ws-contract.md`.
- **Clients (follow-on):** `softphone-kmp-sdk` message codec + event; test-call / RN / gateway frontend handlers for UI (“remote video ready”).
- **Not in scope:** Changing when SIP `active`/`ringing` is emitted; media codec/SDP changes; mid-call renegotiation video-added (`renegotiate`) semantics beyond documenting coexistence.
