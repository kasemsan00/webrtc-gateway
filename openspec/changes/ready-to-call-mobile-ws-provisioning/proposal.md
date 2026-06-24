## Why

Mobile sample apps should be able to fetch a JWT, connect to the gateway WebSocket, and immediately be ready to place outbound calls without manually resolving or echoing trunk IDs. The current mobile SIP provisioning design registers a trunk during WebSocket auth, but it still needs sharper behavior for real device platform tracking, stale `notify_user_id` cleanup, and default outbound trunk selection.

## What Changes

- Add ready-to-call mobile WebSocket provisioning behavior after JWT verification and SIP client auth registration.
- Require mobile clients to identify their real platform as `android` or `ios` when connecting or immediately after connection.
- Store `last_online_platform` using the real platform instead of the generic `mobile` value.
- Persist `notify_user_id` from the verified JWT subject, not from the SIP client auth response.
- Reassign stale `notify_user_id` values by clearing old rows for the same subject when the SIP identity changes.
- Register the provisioned trunk before the WebSocket connection is considered ready.
- Send `trunk_resolved` after auto-provisioning succeeds.
- Allow outbound `call` messages on an auto-provisioned connection to use the connection's resolved trunk when the client does not provide `trunkId`, `trunkPublicId`, or public SIP credentials.

## Capabilities

### New Capabilities

- `ready-to-call-mobile-websocket`: Authenticated mobile clients become ready for outbound trunk calls immediately after WebSocket connection and automatic SIP trunk provisioning.

### Modified Capabilities

- None.

## Impact

- `apps/gateway/internal/api/server.go`: WebSocket auth/provisioning flow, platform parsing, trunk resolved notification, outbound call default trunk selection.
- `apps/gateway/internal/api/mobile_sip_provision.go`: provisioning orchestration and platform/user notification binding.
- `apps/gateway/internal/sip/trunk_manager.go`: mobile trunk upsert behavior and `notify_user_id` reassignment sequencing.
- `E:\dev\softphone-kmp-sdk`: sample/SDK WebSocket URL or connect message may need to provide `devicePlatform=android|ios`.
- Tests covering Android/iOS platform persistence, stale `notify_user_id` cleanup, auto `trunk_resolved`, and outbound call without explicit trunk ID.
