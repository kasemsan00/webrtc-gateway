# Remote Media Ready Specification

## Requirements

### Requirement: Remote video ready WebSocket notification

The gateway MUST notify the WebSocket client bound to a session when remote SIP video first becomes decode-ready. Decode-ready MUST require that the gateway has learned a remote SIP video source/SSRC, has SPS and PPS available for that stream, and has observed at least one complete IDR access unit on the SIP→WebRTC video path. The notification MUST use an additive message:

`{"type":"media","sessionId":"<id>","kind":"video","direction":"remote","state":"receiving"}`

This notification MUST NOT replace or alter outbound call-progress `state` messages (`connecting` / `ringing` / `active` / `ended`).

#### Scenario: First complete remote IDR after SIP answer

- **WHEN** an outbound or answered session is receiving SIP video and the first complete IDR access unit is observed with parameter sets available
- **THEN** the bound WebSocket client MUST receive exactly one `type: media` message with `kind: video`, `direction: remote`, and `state: receiving` for that session
- **AND** SIP `state: active` (if any) MUST remain independently emitted based on SIP dialog progress

#### Scenario: No false ready on SSRC-only

- **WHEN** the gateway learns a remote video SSRC but has not yet observed a complete IDR
- **THEN** the gateway MUST NOT emit remote video `media` `receiving`

#### Scenario: Dedupe per session

- **WHEN** additional IDR frames arrive after the first remote video ready notification
- **THEN** the gateway MUST NOT emit another remote video `media` `receiving` for the same session

### Requirement: Optional remote audio ready notification

The gateway MAY emit the same `type: media` shape for audio when the first remote SIP audio RTP packet is accepted for forwarding to the WebRTC client, with `kind: audio`, `direction: remote`, `state: receiving`, at most once per session.

#### Scenario: First remote audio packet

- **WHEN** the first SIP audio RTP packet for a session is received and accepted toward the WebRTC audio path
- **THEN** if audio media-ready signaling is enabled, the bound client MUST receive one `type: media` message with `kind: audio`, `direction: remote`, `state: receiving`

### Requirement: Missing client is non-fatal

If no WebSocket client is bound to the session when media becomes ready, the gateway MUST skip the notification without failing the media path.

#### Scenario: Media ready without WS client

- **WHEN** remote video becomes decode-ready and no client is registered in the session WebSocket map
- **THEN** media forwarding MUST continue and the gateway MUST NOT treat the missing notify as a call failure

### Requirement: Contract documentation

`docs/gateway/ws-contract.md` MUST document `type: media` fields (`sessionId`, `kind`, `direction`, `state`) and state that call-progress `active` is not equivalent to remote video receiving.

#### Scenario: Contract lists media message

- **WHEN** an integrator reads the WebSocket contract
- **THEN** they MUST be able to find the `media` server→client message and its readiness meaning for remote video
