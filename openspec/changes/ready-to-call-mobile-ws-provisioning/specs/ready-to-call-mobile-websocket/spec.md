## ADDED Requirements

### Requirement: Mobile platform is required for auto provisioning

The system SHALL require a real mobile platform value when a user-realm WebSocket connection uses automatic SIP trunk provisioning.

#### Scenario: Android client connects

- **WHEN** a user-realm client connects to `/ws` with `access_token=<jwt>` and `devicePlatform=android`
- **THEN** the gateway MUST accept `android` as the platform for automatic SIP trunk provisioning

#### Scenario: iOS client connects

- **WHEN** a user-realm client connects to `/ws` with `access_token=<jwt>` and `devicePlatform=ios`
- **THEN** the gateway MUST accept `ios` as the platform for automatic SIP trunk provisioning

#### Scenario: Mobile platform is missing

- **WHEN** automatic SIP trunk provisioning is enabled and a user-realm client connects to `/ws` without `devicePlatform`
- **THEN** the gateway MUST reject the WebSocket connection setup

#### Scenario: Mobile platform is invalid

- **WHEN** automatic SIP trunk provisioning is enabled and a user-realm client connects to `/ws` with an unsupported `devicePlatform`
- **THEN** the gateway MUST reject the WebSocket connection setup

### Requirement: Notification user is bound after trunk upsert

The system SHALL bind the verified JWT subject to the provisioned trunk after SIP credential upsert and clear stale bindings for the same subject.

#### Scenario: New SIP identity is provisioned for existing user

- **WHEN** the SIP client auth register response returns credentials for a new username/domain/port for a JWT subject
- **THEN** the gateway MUST persist or update the trunk credentials
- **AND** the gateway MUST set `notify_user_id` on the resulting trunk to the verified JWT subject
- **AND** the gateway MUST clear `notify_user_id` from old trunks for the same subject whose SIP identity differs from the resulting trunk

#### Scenario: Android platform is persisted

- **WHEN** the provisioned WebSocket connection uses `devicePlatform=android`
- **THEN** the resulting trunk MUST store `last_online_platform=android`

#### Scenario: iOS platform is persisted

- **WHEN** the provisioned WebSocket connection uses `devicePlatform=ios`
- **THEN** the resulting trunk MUST store `last_online_platform=ios`

### Requirement: Connection becomes ready only after SIP registration

The system SHALL treat an auto-provisioned mobile WebSocket connection as ready for calls only after SIP trunk registration succeeds.

#### Scenario: Provisioning and registration succeed

- **WHEN** JWT verification, SIP client auth registration, trunk persistence, notification binding, and SIP REGISTER all succeed
- **THEN** the gateway MUST mark the WebSocket client as resolved to the provisioned trunk
- **AND** the gateway MUST send a `trunk_resolved` message containing `trunkId` and `trunkPublicId`
- **AND** the WebSocket connection MUST remain open for normal call signaling

#### Scenario: SIP registration fails

- **WHEN** SIP REGISTER fails for the provisioned trunk
- **THEN** the gateway MUST reject the WebSocket connection setup
- **AND** the gateway MUST NOT expose the raw access token or SIP secret in logs or WebSocket messages

### Requirement: Outbound calls use resolved connection trunk by default

The system SHALL use the resolved trunk on the WebSocket connection for outbound calls when the client does not provide explicit SIP authentication fields.

#### Scenario: Auto-provisioned client calls without trunk ID

- **WHEN** an auto-provisioned WebSocket client sends a `call` message with `sessionId` and `destination` but without `trunkId`, `trunkPublicId`, or public SIP credentials
- **THEN** the gateway MUST place the call using the client's resolved trunk

#### Scenario: Explicit trunk selection remains supported

- **WHEN** a WebSocket client sends a `call` message with `trunkId` or `trunkPublicId`
- **THEN** the gateway MUST use the existing explicit trunk validation behavior

#### Scenario: Public SIP mode remains supported

- **WHEN** a WebSocket client sends a `call` message with public SIP credentials
- **THEN** the gateway MUST use the existing public SIP mode behavior
