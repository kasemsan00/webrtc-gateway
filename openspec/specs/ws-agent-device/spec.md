# ws-agent-device Specification

## Purpose

Provide a dedicated opt-in `/ws-agent-device` WebSocket endpoint for mobile agent devices with required mobile platform identification, optional JWT upgrade verification, client-supplied SIP credential registration, sticky SIP presence, gateway-stored FCM push wake on both live and offline incoming calls, and explicit logout unregister.

## Requirements

### Requirement: Agent-device WebSocket is a separate opt-in path

The system SHALL expose `/ws-agent-device` independently from `/ws`, `/ws-public`, and `/ws-agent` when enabled, strictly requiring `devicePlatform=android|ios` at WebSocket upgrade while treating `access_token` as optional and verifying any supplied token before upgrade.

#### Scenario: Flag disabled

- **WHEN** `API_ENABLE_AGENT_DEVICE_WS` is false
- **THEN** the gateway MUST NOT serve `/ws-agent-device`

#### Scenario: Upgrade requires valid mobile devicePlatform

- **WHEN** agent-device WebSocket is enabled and a client connects to `/ws-agent-device` without `devicePlatform=android|ios`
- **THEN** the gateway MUST reject the upgrade with `401 Unauthorized`
- **AND** the gateway MUST NOT invoke the mobile SIP trunk provisioner

#### Scenario: Upgrade accepts valid platform without access token

- **WHEN** agent-device WebSocket is enabled and a client connects to `/ws-agent-device` with `devicePlatform=android` or `devicePlatform=ios` and no `access_token`
- **THEN** the gateway MUST accept the upgrade without JWT claims
- **AND** the gateway MUST NOT call `SIPCLIENT_AUTH_REGISTER_URL`

#### Scenario: Upgrade verifies optional access token when provided

- **WHEN** agent-device WebSocket is enabled, a token verifier is configured, and a client connects to `/ws-agent-device` with `devicePlatform=android` or `devicePlatform=ios` and a valid `access_token`
- **THEN** the gateway MUST verify the JWT, attach the verified claims to the connection, and accept the upgrade
- **AND** the gateway MUST NOT call `SIPCLIENT_AUTH_REGISTER_URL`

#### Scenario: Upgrade rejects invalid access token or missing verifier

- **WHEN** agent-device WebSocket is enabled and a client connects to `/ws-agent-device` with `devicePlatform=android` or `devicePlatform=ios` and a non-empty `access_token` while `tokenVerifier` is nil or JWT verification fails
- **THEN** the gateway MUST reject the upgrade with `401 Unauthorized`
- **AND** the gateway MUST NOT invoke the mobile SIP trunk provisioner

### Requirement: Device registers from client-provided SIP credentials

The system SHALL bind an agent-device WebSocket to a sticky SIP trunk using `device_register` credentials after connect, binding `notify_user_id` to the verified JWT subject or to `"sip:<username>@<domain>:<port>"` when unauthenticated, and requiring verified JWT claims for outbound trunk calls when `AUTH_ENABLE=true`.

#### Scenario: Successful device register with verified JWT

- **WHEN** an `/ws-agent-device` client connected with a verified `access_token` sends `device_register` with `sipDomain`, `sipUsername`, and `sipPassword`
- **THEN** the gateway MUST upsert a deterministic agent-device trunk named `sipclient-agent-device-<username>@<domain>:<port>`
- **AND** the gateway MUST SIP REGISTER the trunk when this instance does not already own it
- **AND** the gateway MUST bind `notify_user_id` to the verified JWT subject and `last_online_platform` to the device platform
- **AND** the gateway MUST send `trunk_resolved` without the SIP password

#### Scenario: Successful device register without access token

- **WHEN** an `/ws-agent-device` client connected without `access_token` sends `device_register` with `sipDomain`, `sipUsername`, and `sipPassword`
- **THEN** the gateway MUST upsert and SIP REGISTER the deterministic agent-device trunk when this instance does not already own it
- **AND** the gateway MUST bind `notify_user_id` to `"sip:<username>@<domain>:<port>"` (using port `5060` when `sipPort` is omitted) and `last_online_platform` to the device platform
- **AND** the gateway MUST send `trunk_resolved` without the SIP password

#### Scenario: Outbound trunk call requires JWT claims when auth is enabled

- **WHEN** `AUTH_ENABLE=true` (`tokenVerifier` is configured) and an `/ws-agent-device` client attempts an outbound trunk `call` without verified JWT claims
- **THEN** the gateway MUST reject the `call` message with `Token authentication required to use trunk`

#### Scenario: Device trunk identity is distinct

- **WHEN** an agent-device trunk is created
- **THEN** its name MUST NOT collide with `sipclient-mobile-<subject>` or `sipclient-agent-<username>@<domain>:<port>`

#### Scenario: Same subject moves to a new SIP identity

- **WHEN** the same notify identity (`sub` or `"sip:<username>@<domain>:<port>"`) device-registers a different username/domain/port
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

The system SHALL persist an FCM token from `device_push_token` and dispatch that stored FCM token without TTRS lookup for incoming calls on an agent-device trunk both when live idle WebSocket clients exist and when zero live clients are connected.

#### Scenario: Store FCM token

- **WHEN** a resolved agent-device client sends `device_push_token` with `pnType=fcm` and a non-empty token
- **THEN** the gateway MUST store the token on that trunk
- **AND** the gateway MUST NOT treat the token as an Apple PushKit hex `trunk_push_token`

#### Scenario: Live idle incoming also dispatches stored FCM

- **WHEN** an incoming INVITE matches an agent-device trunk that has one or more live idle WebSocket clients and a stored FCM token
- **THEN** the gateway MUST present `incoming` over WebSocket to the idle live client(s)
- **AND** the gateway MUST also dispatch FCM using the stored token (without calling the TTRS notification API) to wake or foreground the mobile app, whereas desktop `/ws-agent` trunks remain WebSocket-only while live

#### Scenario: Offline incoming uses stored FCM

- **WHEN** an incoming INVITE matches an agent-device trunk with zero live WebSocket clients and a stored FCM token
- **THEN** the gateway MUST send FCM using the stored token
- **AND** the gateway MUST NOT fetch tokens from the TTRS notification API for that send

### Requirement: Agent and agent-device share an incoming identity group

The system SHALL route inbound INVITEs for the same SIP username/domain/port across `/ws-agent` and `/ws-agent-device` trunks without an ambiguous miss, presenting `incoming` to idle live WebSocket clients on either trunk while dispatching stored device FCM for both live and offline agent-device incoming calls.

#### Scenario: Live clients on either trunk receive the call and device wakes via FCM

- **WHEN** an incoming INVITE matches an owned agent trunk and an owned agent-device trunk for the same SIP identity
- **THEN** the gateway MUST NOT reject the match as ambiguous
- **AND** the gateway MUST present `incoming` to idle live WebSocket clients bound to either trunk
- **AND** when the agent-device trunk has a stored FCM token, the gateway MUST also dispatch that stored FCM push to wake the mobile app while keeping desktop `/ws-agent` clients WebSocket-only

#### Scenario: No live clients uses device FCM

- **WHEN** that identity group has no live WebSocket clients and the agent-device trunk has a stored FCM token
- **THEN** the gateway MUST dispatch FCM and wait for the ring timeout

### Requirement: Secrets are not exposed

The system SHALL avoid exposing SIP passwords and FCM tokens in logs and `trunk_resolved` payloads.

#### Scenario: Successful bind does not echo secrets

- **WHEN** `device_register` or `device_push_token` succeeds
- **THEN** WebSocket replies MUST NOT include the SIP password or FCM token
