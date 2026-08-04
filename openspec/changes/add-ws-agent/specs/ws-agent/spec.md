## ADDED Requirements

### Requirement: Agent WebSocket endpoint is separate from mobile and public

The system SHALL expose a dedicated WebSocket endpoint `/ws-agent` for PC agent clients that does not use mobile JWT provisioning and does not reuse `/ws-public` outbound-only policy.

#### Scenario: Agent endpoint accepts connection without access token

- **WHEN** agent WebSocket is enabled and a client connects to `/ws-agent` without an `access_token`
- **THEN** the gateway MUST accept the WebSocket upgrade (subject to the agent enable configuration)
- **AND** the gateway MUST NOT invoke the mobile SIP trunk provisioner

#### Scenario: Mobile endpoint behavior remains unchanged

- **WHEN** a user-realm client connects to `/ws` with `access_token` and `devicePlatform=android` or `devicePlatform=ios`
- **THEN** the gateway MUST continue to use the existing mobile provisioning and sticky registration behavior
- **AND** the gateway MUST NOT apply agent refcount unregister semantics to that mobile trunk

#### Scenario: Unsupported mobile platform values remain rejected on /ws

- **WHEN** automatic SIP trunk provisioning is enabled and a user-realm client connects to `/ws` with an unsupported `devicePlatform`
- **THEN** the gateway MUST reject the WebSocket connection setup
- **AND** values used only for agent presence MUST NOT be accepted as mobile `devicePlatform` on `/ws`

### Requirement: Agent registers from client-provided SIP credentials

The system SHALL bind an agent WebSocket client to a SIP trunk using credentials supplied by the agent client (`sipDomain` or IP host, `sipUsername`, `sipPassword`, optional `sipPort`) after connect.

#### Scenario: Successful agent register

- **WHEN** an `/ws-agent` client sends a valid agent register message with SIP domain/host, username, and password
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

### Requirement: Agent clients can place and receive calls on the resolved trunk

The system SHALL allow `/ws-agent` clients that have successfully registered to use normal trunk call signaling for outbound and inbound calls on their resolved trunk.

#### Scenario: Outbound call on resolved agent trunk

- **WHEN** a resolved `/ws-agent` client sends `offer` and `call` for a destination using the resolved trunk
- **THEN** the gateway MUST place the outbound SIP call using that agent trunk

#### Scenario: Incoming call fanout to all bound agent clients

- **WHEN** an incoming SIP call arrives for an agent trunk that has multiple idle bound WebSocket clients
- **THEN** the gateway MUST notify each eligible bound client with an `incoming` message

#### Scenario: Incoming call while no agent WebSocket is bound

- **WHEN** an incoming SIP call arrives for an agent trunk with zero bound WebSocket clients
- **THEN** the gateway MUST reject the call as offline
- **AND** the gateway MUST NOT dispatch mobile push (FCM/APNs) for that agent offline case

### Requirement: Agent secrets are not exposed

The system SHALL avoid exposing agent SIP passwords in logs, diagnostics, and WebSocket server messages.

#### Scenario: Successful bind does not echo password

- **WHEN** agent register succeeds
- **THEN** `trunk_resolved` MUST NOT include the SIP password
- **AND** logs MUST NOT print the SIP password
