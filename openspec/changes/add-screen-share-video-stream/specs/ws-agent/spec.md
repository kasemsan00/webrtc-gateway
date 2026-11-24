## MODIFIED Requirements

### Requirement: Agent WebSocket enforces an endpoint-specific message allowlist

The system SHALL permit only `agent_register`, `ping`, `client_state`, `offer`, `call`, `ice`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `request_keyframe`, `request-keyframe`, `renegotiate_answer`, `hold`, `unhold`, and `stream_update` on `/ws-agent` (requiring session access for session-scoped messages), and SHALL reject non-agent message types including `unregister`, `trunk_push_token`, `trunk_resolve`, and `resume`.

#### Scenario: Allowed agent signaling and call-control messages

- **WHEN** a `/ws-agent` client sends `agent_register`, `ping`, `client_state`, an initial `offer`, or a session-authorized `offer`, `call`, `ice`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `request_keyframe`, `request-keyframe`, `renegotiate_answer`, `hold`, `unhold`, or `stream_update`
- **THEN** the gateway MUST allow the message through the `/ws-agent` policy check

#### Scenario: Disallowed message types are rejected on /ws-agent

- **WHEN** a `/ws-agent` client sends `unregister`, `trunk_push_token`, `trunk_resolve`, `resume`, or another non-allowlisted message type
- **THEN** the gateway MUST reject the message with an error stating that the message type is not allowed on agent WebSocket

#### Scenario: stream_update requires session access

- **WHEN** a `/ws-agent` client sends `stream_update` with a `sessionId` it does not own
- **THEN** the gateway MUST reject the message and MUST NOT send a SIP re-INVITE
