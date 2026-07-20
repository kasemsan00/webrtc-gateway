## ADDED Requirements

### Requirement: Outbound call progress reflects SIP dialog, not ICE readiness

The gateway MUST NOT set outbound session call state to `active` solely because the WebRTC ICE connection becomes connected during initial call setup. Session call state `active` MUST mean the SIP dialog has been answered (`200 OK`) or an equivalent answered path (e.g. accepted incoming call). ICE connected during `connecting` or `ringing` MUST leave SIP call-progress state unchanged while still allowing media recovery actions (FIR/PLI).

#### Scenario: ICE connects while outbound call still ringing

- **WHEN** an outbound session is in `connecting` or `ringing` and WebRTC ICE transitions to connected
- **THEN** the gateway MUST NOT emit WebSocket `{"type":"state","state":"active"}` for that reason alone
- **AND** the session call state MUST remain `connecting` or `ringing` until SIP answer or terminal end

#### Scenario: ICE reconnect after answered call

- **WHEN** an already answered session is in `reconnecting` and ICE returns to connected
- **THEN** the gateway MAY restore call state to `active` and notify the client

### Requirement: WebSocket progress signals for outbound SIP responses

For an outbound call bound to a WebSocket client, the gateway MUST notify that client of SIP call progress via WebSocket messages:

- `connecting` when the outbound INVITE has been initiated
- `ringing` when a SIP `180` or `183` provisional response is received
- `active` when a SIP `200 OK` to the INVITE is received
- `ended` when the outbound attempt or established call terminates unsuccessfully or by hangup/BYE failure paths already used for terminal notify

The primary message shape MUST be `{"type":"state","sessionId":"<id>","state":"<state>"}`.

#### Scenario: Far end ringing

- **WHEN** the SIP peer returns `180 Ringing` or `183 Session Progress` for an outbound INVITE
- **THEN** the calling WebSocket client MUST receive `type: state` with `state: ringing`

#### Scenario: Far end answers

- **WHEN** the SIP peer returns `200 OK` to the outbound INVITE
- **THEN** the calling WebSocket client MUST receive `type: state` with `state: active`
- **AND** that `active` MUST NOT have been emitted earlier solely due to ICE connected

#### Scenario: Optional ringing message type

- **WHEN** the gateway emits outbound ringing progress
- **THEN** it SHOULD also send `{"type":"ringing","sessionId":"<id>"}` once per transition into ringing for clients that listen for that message type

### Requirement: Post-call acknowledgment must not leak ICE-promoted active

When the gateway accepts a client `call` message and starts the outbound INVITE, the immediate WebSocket state acknowledgment MUST report dialing progress (`connecting`), not a stale ICE-promoted `active` snapshot.

#### Scenario: call after ICE already connected

- **WHEN** WebRTC ICE is already connected and the client sends `call`
- **THEN** the immediate state response MUST NOT claim `active` unless SIP has already answered
- **AND** the response MUST indicate `connecting` (or `ringing` if SIP progress already advanced)

### Requirement: Progress notify filter includes connecting and ringing

The SIP→WebSocket session state notifier MUST forward at least `connecting`, `ringing`, `active`, and `ended` to the bound client. Consecutive duplicate identical state values for the same session MUST NOT be re-emitted.

#### Scenario: ringing reaches the client

- **WHEN** session state updates to `ringing` from SIP handling
- **THEN** `NotifySessionState` (or equivalent) MUST deliver that state to the WebSocket client

### Requirement: Observable progress logging

The gateway MUST log a concise, session-scoped line when it sends an outbound call-progress WebSocket message, including session ID, message type/state, and trigger reason.

#### Scenario: operator can correlate accept with WS emit

- **WHEN** SIP `200 OK` causes `state: active` to be sent
- **THEN** gateway logs MUST include a visible record of that WebSocket send for the session ID
