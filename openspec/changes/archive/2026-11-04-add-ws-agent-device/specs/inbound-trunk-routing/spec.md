## ADDED Requirements

### Requirement: Agent and agent-device trunks for one SIP AOR are one match group

The system SHALL NOT treat two owned trunks as an ambiguous INVITE miss when they are an agent trunk and an agent-device trunk for the same username, domain, and port.

#### Scenario: Pair of agent plus agent-device trunks

- **WHEN** origin-aware matching finds both `sipclient-agent-<user>@<domain>:<port>` and `sipclient-agent-device-<user>@<domain>:<port>` owned and eligible
- **THEN** the gateway MUST select a single session trunk (preferring the agent-device trunk)
- **AND** the gateway MUST still notify live WebSocket clients bound to either trunk
