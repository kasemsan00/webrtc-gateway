## ADDED Requirements

### Requirement: Agent-device WebSocket is a separate opt-in path

The system SHALL expose `/ws-agent-device` independently from `/ws`, `/ws-public`, and `/ws-agent`.

#### Scenario: Flag disabled

- **WHEN** `API_ENABLE_AGENT_DEVICE_WS` is false
- **THEN** the gateway MUST NOT serve `/ws-agent-device`

#### Scenario: Upgrade requires JWT and platform

- **WHEN** agent-device WebSocket is enabled and a client connects without `access_token` or without `devicePlatform=android|ios`
- **THEN** the gateway MUST reject the upgrade as unauthorized
- **AND** the gateway MUST NOT invoke the mobile SIP trunk provisioner

#### Scenario: Valid JWT does not provision a mobile trunk

- **WHEN** a client connects to `/ws-agent-device` with a valid `access_token` and `devicePlatform=android`
- **THEN** the gateway MUST accept the upgrade
- **AND** the gateway MUST NOT call `SIPCLIENT_AUTH_REGISTER_URL`

### Requirement: Device registers from client-provided SIP credentials

The system SHALL bind an agent-device WebSocket to a sticky SIP trunk using `device_register` credentials after connect.

#### Scenario: Successful device register

- **WHEN** a resolved JWT client sends `device_register` with sipDomain, sipUsername, and sipPassword
- **THEN** the gateway MUST upsert a deterministic agent-device trunk named `sipclient-agent-device-<username>@<domain>:<port>`
- **AND** the gateway MUST SIP REGISTER the trunk when this instance does not already own it
- **AND** the gateway MUST bind `notify_user_id` to the JWT subject and `last_online_platform` to the device platform
- **AND** the gateway MUST send `trunk_resolved` without the SIP password

#### Scenario: Device trunk identity is distinct

- **WHEN** an agent-device trunk is created
- **THEN** its name MUST NOT collide with `sipclient-mobile-<subject>` or `sipclient-agent-<username>@<domain>:<port>`

#### Scenario: Same subject moves to a new SIP identity

- **WHEN** the same JWT subject device-registers a different username/domain/port
- **THEN** the gateway MUST UNREGISTER the previous agent-device trunk for that subject
- **AND** the gateway MUST clear that previous trunk's FCM token and notify user

### Requirement: Presence is sticky until explicit unregister

The system SHALL keep SIP REGISTER and stored FCM after the WebSocket disconnects, and SHALL unregister only on `unregister`.

#### Scenario: Socket disconnect keeps REGISTER

- **WHEN** the last `/ws-agent-device` client for a trunk disconnects without sending `unregister`
- **THEN** the gateway MUST keep the trunk SIP registered
- **AND** the gateway MUST NOT clear the stored FCM token
- **AND** the gateway MUST hang up only sessions owned by the disconnecting client

#### Scenario: Logout unregisters

- **WHEN** a bound `/ws-agent-device` client sends `unregister`
- **THEN** the gateway MUST SIP UNREGISTER that agent-device trunk
- **AND** the gateway MUST clear FCM and notify-user binding on that trunk

#### Scenario: Agent last-disconnect does not unregister a device trunk

- **WHEN** the last `/ws-agent` client for a sibling agent trunk disconnects
- **THEN** the gateway MUST NOT SIP UNREGISTER the agent-device trunk for the same SIP user

### Requirement: Gateway stores FCM and sends it directly

The system SHALL persist an FCM token from `device_push_token` and use it for offline incoming without TTRS lookup.

#### Scenario: Store FCM token

- **WHEN** a resolved agent-device client sends `device_push_token` with `pnType=fcm` and a non-empty token
- **THEN** the gateway MUST store the token on that trunk
- **AND** the gateway MUST NOT treat the token as an Apple PushKit hex `trunk_push_token`

#### Scenario: Offline incoming uses stored FCM

- **WHEN** an incoming INVITE matches an agent-device trunk with zero live WebSocket clients and a stored FCM token
- **THEN** the gateway MUST send FCM using the stored token
- **AND** the gateway MUST NOT fetch tokens from the TTRS notification API for that send

### Requirement: Agent and agent-device share an incoming identity group

The system SHALL route inbound INVITEs for the same SIP username/domain/port across `/ws-agent` and `/ws-agent-device` trunks without an ambiguous miss.

#### Scenario: Live clients on either trunk receive the call

- **WHEN** an incoming INVITE matches an owned agent trunk and an owned agent-device trunk for the same SIP identity
- **THEN** the gateway MUST NOT reject the match as ambiguous
- **AND** the gateway MUST present `incoming` to idle live WebSocket clients bound to either trunk

#### Scenario: No live clients uses device FCM

- **WHEN** that identity group has no live WebSocket clients and the agent-device trunk has a stored FCM token
- **THEN** the gateway MUST dispatch FCM and wait for the ring timeout

### Requirement: Secrets are not exposed

The system SHALL avoid exposing SIP passwords and FCM tokens in logs and `trunk_resolved` payloads.

#### Scenario: Successful bind does not echo secrets

- **WHEN** `device_register` or `device_push_token` succeeds
- **THEN** WebSocket replies MUST NOT include the SIP password or FCM token
