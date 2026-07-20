## ADDED Requirements

### Requirement: Uplink keyframe kick on first remote video SSRC learn

When the gateway first learns a remote SIP video SSRC for a session (SSRC transitions from unset/zero to a non-zero value), it MUST request a fresh keyframe from the WebRTC browser on the uplink path. The request MUST include at least one FIR to the WebRTC peer connection and MUST include a short bounded PLI burst to the WebRTC peer connection. This kick MUST run without blocking the SIP video RTP read loop (e.g. on an existing or dedicated goroutine).

#### Scenario: First remote video SSRC learn kicks browser

- **WHEN** a session receives SIP video RTP and learns remote video SSRC for the first time
- **THEN** the gateway MUST send FIR to the WebRTC browser
- **AND** the gateway MUST send one or more PLI messages to the WebRTC browser within a short bounded burst
- **AND** existing SIP-directed recovery on SSRC learn (FIR/PLI to Asterisk) MUST continue to run

#### Scenario: Subsequent SSRC change does not re-arm first-join kick

- **WHEN** a session that already completed the first-join uplink keyframe kick later observes a different remote video SSRC
- **THEN** the gateway MUST NOT fire the first-join uplink keyframe kick again solely because of that SSRC change
- **AND** mid-call `@switch` / switch-recovery paths MAY still request browser keyframes through their own mechanisms

### Requirement: Periodic startup PLI remains available

The existing startup periodic PLI-to-browser window MUST remain enabled as an early-call safety net. The remote-join uplink kick MUST NOT replace that window; both MAY produce browser keyframe requests during the same call.

#### Scenario: Early answer still covered by periodic PLI

- **WHEN** remote SIP video SSRC is learned while the startup periodic PLI-to-browser window is still active
- **THEN** the gateway MUST still perform the first-join uplink keyframe kick
- **AND** periodic PLI-to-browser MAY continue until its normal deadline

### Requirement: Observability for uplink kick

The gateway MUST log when the first-join uplink keyframe kick is executed, including the session identifier and a stable reason token indicating remote SSRC learn.

#### Scenario: Kick is logged

- **WHEN** the first-join uplink keyframe kick runs for a session
- **THEN** gateway logs MUST include the session ID and reason `remote-ssrc-learn` (or equivalent stable token)
