## Why

Mobile WebRTC clients currently authenticate to the gateway with an access token, but the SIP credentials needed for outbound and inbound calls must already exist as a trunk or be provided separately. The gateway should use the verified mobile access token to obtain the user's SIP account from `sipclient-auth.ttrs.or.th`, persist it as a trunk, and register it automatically so mobile clients can use the normal WebRTC gateway call flow.

## What Changes

- Add mobile SIP trunk provisioning during authenticated WebSocket connection setup.
- After local JWT verification succeeds, call the SIP client auth register API with `token=<access_token>` and `type=mobile`.
- Use the response `data.domain` as the SIP trunk domain, `data.ext` as username, and `data.secret` as password.
- Create or update one deterministic trunk for the authenticated mobile user.
- Register the provisioned trunk immediately and mark the WebSocket client as resolved to that trunk.
- Reject the WebSocket connection if SIP auth registration, trunk persistence, or SIP trunk registration fails.
- Preserve existing trunk call, incoming call, push, and resume behavior once the trunk is provisioned.

## Capabilities

### New Capabilities

- `mobile-sip-trunk-provisioning`: Authenticated mobile WebSocket clients can be provisioned with SIP credentials from the external SIP client auth service and use the resulting trunk for calls.

### Modified Capabilities

- None.

## Impact

- `apps/gateway/internal/api/server.go`: WebSocket auth flow, client trunk resolution state, error handling.
- `apps/gateway/internal/auth` or a new gateway client package: external SIP client auth API request/response handling.
- `apps/gateway/internal/config/config.go`: configuration for SIP client auth register URL and timeout.
- `apps/gateway/internal/sip/trunk_manager.go` and/or `apps/gateway/internal/logstore/logstore.go`: deterministic trunk upsert/update support for mobile users.
- `apps/gateway/init.sql`: no required schema change if deterministic `name` and existing `notify_user_id` are used; migration may be added only if implementation needs stronger uniqueness.
- Tests for WebSocket auth provisioning success, update, external auth failure, trunk registration failure, and normal trunk usage after auto-resolution.
