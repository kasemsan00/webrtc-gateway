# Mobile SIP Trunk Provisioning Specification

## Requirements

### Requirement: Mobile WebSocket provisioning

The system SHALL provision a SIP trunk for an authenticated mobile WebSocket client during connection setup by exchanging the verified access token with the configured SIP client auth register endpoint.

#### Scenario: Successful mobile provisioning

- **WHEN** a mobile client connects to `/ws` with a valid `access_token`
- **THEN** the gateway MUST call the SIP client auth register endpoint with form fields `token=<access_token>` and `type=mobile`
- **AND** the gateway MUST continue WebSocket setup only after the SIP trunk is persisted and registered

#### Scenario: Missing or invalid gateway token

- **WHEN** a client connects to `/ws` without a valid `access_token`
- **THEN** the gateway MUST reject the WebSocket request before calling the SIP client auth register endpoint

### Requirement: External SIP auth response mapping

The system SHALL map the SIP client auth register response into trunk credentials using `data.domain`, `data.ext`, and `data.secret`.

#### Scenario: Valid SIP auth response

- **WHEN** the SIP client auth register endpoint returns status `OK` with `data.domain`, `data.ext`, and `data.secret`
- **THEN** the gateway MUST use `data.domain` as trunk domain
- **AND** the gateway MUST use `data.ext` as trunk username
- **AND** the gateway MUST use `data.secret` as trunk password

#### Scenario: Response contains video and caption domains

- **WHEN** the SIP client auth register endpoint returns `data.domain_video`, `data.domain_caption`, or `data.websocket`
- **THEN** the gateway MUST NOT use those fields for SIP trunk registration

#### Scenario: Invalid SIP auth response

- **WHEN** the SIP client auth register endpoint fails or returns a response without required SIP credential fields
- **THEN** the gateway MUST reject the WebSocket connection setup

### Requirement: Deterministic mobile trunk persistence

The system SHALL create or update a deterministic trunk record for the authenticated mobile user instead of creating duplicate trunks when SIP credentials change.

#### Scenario: First mobile connection

- **WHEN** a valid mobile user connects and no deterministic trunk exists for that authenticated subject
- **THEN** the gateway MUST create an enabled trunk with the received SIP credentials
- **AND** the trunk MUST be bound to the authenticated subject for notifications
- **AND** the trunk MUST store the latest online mobile platform

#### Scenario: Existing mobile trunk with changed credentials

- **WHEN** a valid mobile user connects and the deterministic trunk already exists with different SIP credentials
- **THEN** the gateway MUST update the existing trunk with the latest `domain`, `ext`, and `secret`
- **AND** the gateway MUST NOT create a second trunk for that user

### Requirement: Automatic trunk registration and client binding

The system SHALL register the provisioned trunk and bind the WebSocket client to that trunk before normal call handling.

#### Scenario: Provisioned trunk registers successfully

- **WHEN** the gateway has created or updated the mobile trunk
- **THEN** the gateway MUST register the trunk on the current gateway instance
- **AND** the gateway MUST mark the WebSocket client as resolved to the registered trunk
- **AND** outbound calls and inbound call notifications MUST use the existing trunk flow for that trunk

#### Scenario: Provisioned trunk registration fails

- **WHEN** SIP registration for the provisioned trunk fails
- **THEN** the gateway MUST reject the WebSocket connection setup

### Requirement: Sensitive data handling

The system SHALL avoid exposing mobile access tokens and SIP secrets in logs, diagnostics, and WebSocket messages.

#### Scenario: Provisioning failure is logged

- **WHEN** mobile SIP trunk provisioning fails
- **THEN** logs MUST include enough context to diagnose the failed stage
- **AND** logs MUST NOT include the raw access token or SIP secret
