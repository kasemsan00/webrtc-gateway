## Why

A mobile caller needs to share their screen while the agent keeps seeing the front camera. Today a call carries exactly one video stream end to end: the session owns one video track, one SIP video socket pair and one SSRC, and the SIP SDP has one `m=video`. A second WebRTC video track would be written onto the camera's SSRC and sequence space and corrupt the picture.

Asterisk `chan_pjsip` can carry a second video stream in the same dialog (`max_video_streams = 2`) and, in a two-party bridge, re-INVITEs the other leg when one side adds a stream. The gateway is the missing piece on both legs. No extra media service (LiveKit or similar) is wanted.

## What Changes

- Add one optional auxiliary video stream per session with the fixed role `screen`, carried as a second `m=video` in the existing SIP dialog and as a second video m-line on the existing PeerConnection. Limit: one camera plus one screen.
- Add a client-originated flow: WS `stream_update` (add/remove) makes the gateway send an in-dialog re-INVITE that opens or closes the second `m=video`.
- Add a peer-originated flow: an inbound re-INVITE that opens a second `m=video` is answered per m-line, and the bound client is asked to renegotiate in place (`renegotiate` with `reason: stream_added`).
- Add in-place WebRTC renegotiation on the live PeerConnection. Every existing mid-call flow replaces the PeerConnection; this one must not.
- Decide support per call from the SIP answer. There is no configuration flag. `chan_sip`, or a `chan_pjsip` endpoint left at `max_video_streams = 1`, rejects the second m-line and the client is told screen share is not supported on this call. There is no single-track fallback.
- Add a far-end confirmation over in-dialog SIP MESSAGE so the sharing side learns when the other leg cannot receive the stream.
- Harden SIP SDP handling for more than one `m=video`: parse by m-line position, answer with the same number of m-lines as the offer, and never let a second `m=video` move the camera's RTP destination. This part runs for every call.
- Add client opt-in `multiVideo: true` on `agent_register`, `device_register` and `client_state`, following the `multiCall` pattern.
- Document the new messages in `docs/gateway/ws-contract.md`.

## Capabilities

### New Capabilities

- `screen-share-video-stream`: a session can carry one auxiliary screen video stream alongside the camera, negotiated mid-call over SIP re-INVITE and in-place WebRTC renegotiation, with per-call support detection and far-end confirmation.
- `sip-sdp-media-slots`: SIP SDP offers and answers are handled per m-line position, so multi-video SDP is answered correctly and does not disturb the primary audio and video streams.

### Modified Capabilities

- `ws-agent`: the `/ws-agent` message allowlist gains `stream_update`.

## Impact

- **Gateway:** `internal/sip/sdp.go`, `hold.go`, `handlers.go` (re-INVITE and MESSAGE), `rtp.go` (port allocation), new `internal/sip/rtp_aux_video.go`; `internal/session` (`OnTrack` routing, `midcall.go`, `media_endpoints.go`, `peer_connection_replace.go`, new `aux_video.go`); `internal/api` (`ws_dispatch.go`, `ws_midcall.go`, `ws_notify.go`, `server.go` `WSMessage`); `docs/gateway/ws-contract.md`, `docs/gateway/config-reference.md`.
- **Asterisk:** `chan_pjsip` endpoints on both legs need `max_video_streams = 2` and `direct_media = no`. No dialplan change for `Dial()` or `Queue()`.
- **Capacity:** a shared screen takes one more RTP port slot. The default `RTP_PORT_MIN`/`RTP_PORT_MAX` range (10500–10600, four ports per slot) holds 25 slots and must be widened where screen share is used.
- **Clients (follow-on):** `webrtc-gateway-sdk` (codec, second remote track, sender flow), the Android app (MediaProjection capture), the Electron agent app (second video view).
- **Not in scope:** more than two video streams, ConfBridge/SFU rooms, simulcast, a single-track fallback for `chan_sip`, a second audio stream.
