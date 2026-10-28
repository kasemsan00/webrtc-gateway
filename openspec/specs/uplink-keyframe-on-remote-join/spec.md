# Uplink Keyframe On Remote Join Specification

## Requirements

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

### Requirement: Fresh-IDR suppression ignores IDRs the far end never saw

The remote-join kick (`remote-ssrc-learn`) and the first-SIP-RTCP kick (`sip-rtcp-first`) MAY be skipped when an uplink IDR is in flight or a complete uplink IDR was forwarded within the last 10 seconds. An uplink IDR that completed (or, if still in flight, last progressed) before the remote video SSRC was learned or before the first SIP video RTP/RTCP was seen MUST NOT count for that suppression. When such a pre-learn IDR is ignored, the kick MUST send FIR and PLI to the browser and at most one follow-up PLI about 1 second later, which MUST be skipped if an uplink IDR forwarded after the kick was already written.

#### Scenario: IDR forwarded before SSRC learn does not suppress the kick

- **WHEN** an uplink IDR was forwarded after 200 OK but before the remote video SSRC was learned
- **AND** the remote video SSRC is then learned
- **THEN** the gateway MUST still send FIR and PLI to the browser
- **AND** gateway logs MUST include `uplink_keyframe_kick reason=remote-ssrc-learn pre_learn_idr=ignored`

#### Scenario: IDR forwarded after the far end was seen suppresses the kick

- **WHEN** a complete uplink IDR was forwarded after the remote video SSRC was learned and less than 10 seconds ago
- **THEN** the gateway MUST skip the kick and log `uplink_keyframe_kick_skipped reason=<kind> skip=idr-fresh`

### Requirement: Hold WebRTC-to-SIP video until the first uplink IDR

The gateway MUST cache SPS/PPS from the WebRTC uplink as they arrive, MUST keep requesting a browser keyframe with existing PLI/FIR paths, and MUST NOT write WebRTC video RTP to SIP until an IDR (or STAP-A containing IDR) is forwarded. After that first uplink IDR, subsequent P-frames and fragments SHALL be forwarded as today.

#### Scenario: P-frames before the first IDR are not sent to SIP

- **WHEN** WebRTC video arrives and no uplink IDR has been written to SIP yet
- **THEN** the gateway MUST drop those packets instead of forwarding them to Asterisk
- **AND** the gateway MUST still cache SPS/PPS and request a browser keyframe

#### Scenario: First IDR opens uplink forwarding

- **WHEN** a WebRTC IDR (single NAL, FU-A start, or STAP-A containing IDR) is ready and media forward is ready
- **THEN** the gateway MUST inject cached SPS/PPS as needed and write that IDR to SIP
- **AND** later P-frames on that media epoch SHALL be forwarded

#### Scenario: Mid-call picture-size change is held until its IDR

- **WHEN** `SIP_UPLINK_PICTURE_SIZE_CHANGE=accept` (default) and, after the first uplink IDR, the browser SPS changes picture size
- **THEN** the gateway MUST hold non-IDR packets of the new size (including in-band SPS/PPS) until the first IDR of the new size
- **AND** MUST inject exactly one cached SPS/PPS pair before that IDR
- **AND** MUST send a browser PLI at most once per second while a held picture arrives without an IDR

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
