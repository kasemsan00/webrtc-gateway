## Purpose

Provide a dedicated `/ws-agent` WebSocket endpoint for desktop and web agent clients with optional JWT verification, client-supplied SIP credential registration, multi-connection refcount presence, immediate unregister on last disconnect, and an agent-scoped message allowlist.

## ADDED Requirements

### Requirement: Agent WebSocket endpoint is separate from mobile and public

The system SHALL expose a dedicated WebSocket endpoint `/ws-agent` for PC agent clients that accepts connections with an optional `access_token`, verifies any supplied token before upgrade, does not use mobile JWT provisioning, and does not reuse `/ws-public` outbound-only policy.

#### Scenario: Agent endpoint accepts connection without access token

- **WHEN** agent WebSocket is enabled and a client connects to `/ws-agent` without an `access_token`
- **THEN** the gateway MUST accept the WebSocket upgrade
- **AND** the gateway MUST NOT invoke the mobile SIP trunk provisioner

#### Scenario: Agent endpoint verifies optional access token when provided

- **WHEN** agent WebSocket is enabled, a token verifier is configured, and a client connects to `/ws-agent` with a valid `access_token`
- **THEN** the gateway MUST verify the JWT, attach the verified claims to the connection, and accept the WebSocket upgrade
- **AND** the gateway MUST NOT invoke the mobile SIP trunk provisioner

#### Scenario: Agent endpoint rejects invalid token or missing verifier

- **WHEN** agent WebSocket is enabled and a client connects to `/ws-agent` with a non-empty `access_token` while `tokenVerifier` is nil or token verification fails
- **THEN** the gateway MUST reject the WebSocket upgrade with `401 Unauthorized`

#### Scenario: Mobile endpoint behavior remains unchanged

- **WHEN** a user-realm client connects to `/ws` with `access_token` and `devicePlatform=android` or `devicePlatform=ios`
- **THEN** the gateway MUST continue to use the existing mobile provisioning and sticky registration behavior
- **AND** the gateway MUST NOT apply agent refcount unregister semantics to that mobile trunk

#### Scenario: Unsupported mobile platform values remain rejected on /ws

- **WHEN** automatic SIP trunk provisioning is enabled and a user-realm client connects to `/ws` with an unsupported `devicePlatform`
- **THEN** the gateway MUST reject the WebSocket connection setup
- **AND** values used only for agent presence MUST NOT be accepted as mobile `devicePlatform` on `/ws`

### Requirement: Agent registers from client-provided SIP credentials

The system SHALL bind an agent WebSocket client to a SIP trunk using credentials supplied by the agent client (`sipDomain` or IP host, `sipUsername`, `sipPassword`, optional `sipPort`) via `agent_register` after connect.

#### Scenario: Successful agent register

- **WHEN** an `/ws-agent` client sends a valid `agent_register` message with SIP domain/host, username, and password
- **THEN** the gateway MUST create or update a deterministic agent trunk for that SIP identity
- **AND** the gateway MUST SIP REGISTER the trunk when it is not already registered for active agent presence
- **AND** the gateway MUST mark the WebSocket client as resolved to that trunk
- **AND** the gateway MUST send `trunk_resolved` with `trunkId` and `trunkPublicId`

#### Scenario: Agent register fails

- **WHEN** agent trunk persistence or SIP REGISTER fails
- **THEN** the gateway MUST NOT treat the client as call-ready on a resolved agent trunk
- **AND** the gateway MUST signal failure to the client without exposing the SIP password in logs or WebSocket payloads

#### Scenario: Agent trunk identity is distinct from mobile

- **WHEN** an agent trunk is created for a SIP username/domain
- **THEN** the trunk identity MUST use an agent-specific namespace that cannot collide with `sipclient-mobile-<subject>` mobile trunks

### Requirement: Multiple agent connections share one SIP registration via refcount

The system SHALL keep a single SIP REGISTER for an agent trunk while one or more WebSocket clients are bound to that trunk, and MUST unregister only when the bound connection count reaches zero.

#### Scenario: Second agent machine connects with same SIP user

- **WHEN** an agent trunk is already registered and another `/ws-agent` client registers with the same SIP identity
- **THEN** the gateway MUST bind the new client to the same agent trunk
- **AND** the gateway MUST keep the existing SIP registration
- **AND** the gateway MUST increase the bound connection count for that trunk

#### Scenario: One of two agent machines disconnects

- **WHEN** two agent WebSocket clients are bound to the same trunk and one disconnects
- **THEN** the gateway MUST keep the trunk SIP registered
- **AND** the gateway MUST NOT unregister the trunk

#### Scenario: Last agent machine disconnects

- **WHEN** the last WebSocket client bound to an agent trunk disconnects
- **THEN** the gateway MUST hang up any remaining non-ended call sessions for that trunk
- **AND** the gateway MUST SIP UNREGISTER the trunk immediately
- **AND** the gateway MUST NOT wait for a grace period before unregistering

### Requirement: Agent disconnect during an owned call hangs up that call

The system SHALL terminate call sessions owned by a disconnecting agent client. When the disconnect causes agent trunk refcount to reach zero, the system SHALL also terminate any remaining trunk sessions before unregister.

#### Scenario: Non-last agent disconnects during its own call

- **WHEN** an agent client that owns an active or ringing session disconnects and at least one other agent client remains bound to the trunk
- **THEN** the gateway MUST hang up the disconnecting client's session
- **AND** the gateway MUST keep the trunk registered for the remaining client(s)

#### Scenario: Last agent disconnects during a call

- **WHEN** the last bound agent client disconnects while a call session for that trunk is still active or ringing
- **THEN** the gateway MUST hang up the call
- **AND** the gateway MUST unregister the trunk as part of the same last-disconnect cleanup

### Requirement: Agent clients can place and receive calls in token or credential modes

The system SHALL allow `/ws-agent` clients to place outbound calls in token+trunk mode or in no-token SIP user/pass mode (using either the trunk bound by `agent_register` or per-call public SIP credentials) and receive inbound calls on their resolved trunk.

#### Scenario: Outbound call in token plus trunk mode

- **WHEN** a `/ws-agent` client connected with a verified `access_token` sends `offer` and `call` using its resolved agent trunk
- **THEN** the gateway MUST place the outbound SIP call using that agent trunk

#### Scenario: Outbound call without token using agent-registered trunk or public SIP credentials

- **WHEN** a `/ws-agent` client connected without `access_token` sends `offer` and `call` either using the trunk bound by `agent_register` or supplying per-call `sipDomain`, `sipUsername`, and `sipPassword` without trunk fields
- **THEN** the gateway MUST place the outbound SIP call without requiring a JWT

#### Scenario: Incoming call fanout to all bound agent clients

- **WHEN** an incoming SIP call arrives for an agent trunk that has multiple idle bound WebSocket clients
- **THEN** the gateway MUST notify each eligible bound client with an `incoming` message

#### Scenario: Incoming call while no agent WebSocket is bound

- **WHEN** an incoming SIP call arrives for an agent trunk with zero bound WebSocket clients
- **THEN** the gateway MUST reject the call as offline
- **AND** the gateway MUST NOT dispatch mobile push (FCM/APNs) for that agent offline case

### Requirement: Agent WebSocket enforces an endpoint-specific message allowlist

The system SHALL permit only `agent_register`, `ping`, `client_state`, `offer`, `call`, `ice`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `request_keyframe`, `request-keyframe`, `renegotiate_answer`, `hold`, and `unhold` on `/ws-agent` (requiring session access for session-scoped messages), and SHALL reject non-agent message types including `unregister`, `trunk_push_token`, `trunk_resolve`, and `resume`.

#### Scenario: Allowed agent signaling and call-control messages

- **WHEN** a `/ws-agent` client sends `agent_register`, `ping`, `client_state`, an initial `offer`, or a session-authorized `offer`, `call`, `ice`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `request_keyframe`, `request-keyframe`, `renegotiate_answer`, `hold`, or `unhold`
- **THEN** the gateway MUST allow the message through the `/ws-agent` policy check

#### Scenario: Disallowed message types are rejected on /ws-agent

- **WHEN** a `/ws-agent` client sends `unregister`, `trunk_push_token`, `trunk_resolve`, `resume`, or another non-allowlisted message type
- **THEN** the gateway MUST reject the message with an error stating that the message type is not allowed on agent WebSocket

### Requirement: Agent secrets are not exposed

The system SHALL avoid exposing agent SIP passwords in logs, diagnostics, and WebSocket server messages.

#### Scenario: Successful bind does not echo password

- **WHEN** agent register succeeds
- **THEN** `trunk_resolved` MUST NOT include the SIP password
- **AND** logs MUST NOT print the SIP password
