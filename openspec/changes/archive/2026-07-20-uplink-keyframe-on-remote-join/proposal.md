## Why

When a WebRTC client calls a SIP endpoint (e.g. Linphone via Asterisk), Asterisk often answers the gateway immediately while the human callee answers later. During that wait the browser already emits IDRs in response to the gateway's short periodic PLI window (~20s). If the callee's decoder joins after that window, Linphone shows a black remote video while the WebRTC client still sees downlink video normally—because SIP→WebRTC recovery continues via watchdog PLI/FIR, but WebRTC→SIP has no further keyframe requests (Linphone does not send PLI/FIR in this path).

## What Changes

- On first remote SIP video SSRC learn for a session, the gateway will also request an uplink keyframe from the WebRTC browser (FIR + short PLI burst), not only recover the SIP→WebRTC direction.
- Keep the existing startup periodic PLI-to-browser window as an early-call safety net; do not rely on extending that timer alone.
- Dedupe the new uplink kick so mid-call SSRC churn / `@switch` authority paths do not produce unbounded RTCP storms.
- Add focused tests covering first-learn uplink kick vs subsequent SSRC change behavior.

## Capabilities

### New Capabilities

- `uplink-keyframe-on-remote-join`: When remote SIP video first appears (SSRC learned), gateway MUST request a fresh WebRTC uplink keyframe so late-joining SIP decoders can start cleanly.

### Modified Capabilities

- (none) — `remote-media-ready` stays a WebSocket readiness notification only; this change is media-plane RTCP recovery, not WS contract.

## Impact

- `apps/gateway/internal/sip/rtp.go` — SSRC-learn recovery path (add browser FIR/PLI kick alongside existing Asterisk recovery).
- `apps/gateway/internal/session/` — optional session flag / helper for first-learn uplink kick dedupe; may reuse patterns from `@switch` / `SendPLItoWebRTC` / `SendFIRToWebRTC`.
- No WebSocket contract change; no client SDK requirement for the fix to work.
- Production symptom: Linphone desktop black video on slow answer (>~15s); WebRTC remote video remains OK.
