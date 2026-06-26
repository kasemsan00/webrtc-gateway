## Context

The gateway now has an OpenSpec change for exchanging a verified mobile JWT with `SIPCLIENT_AUTH_REGISTER_URL`, upserting a SIP trunk, registering it, and binding the WebSocket connection to that trunk. During exploration we found three gaps before the Android/iOS sample apps can simply connect and call:

- the persisted `last_online_platform` must be the real platform (`android` or `ios`), not the generic value `mobile`;
- `notify_user_id` is not returned by `SIPCLIENT_AUTH_REGISTER_URL`, so it must come from verified JWT claims and must be reassigned away from stale SIP identities;
- outbound `call` currently expects explicit trunk details, so a sample app may still need to know and send `trunkId` unless the gateway uses the resolved connection trunk as the default.

## Goals / Non-Goals

**Goals:**

- Let Android/iOS sample apps fetch a JWT, connect the WebSocket, and be ready to place outbound calls without a separate `trunk_resolve` step.
- Persist actual platform values (`android` or `ios`) for incoming push routing.
- Use verified JWT `sub` as `notify_user_id` and clear stale `notify_user_id` values from old SIP identities.
- Register the provisioned SIP trunk before the connection is treated as ready.
- Send `trunk_resolved` after automatic provisioning so clients can observe readiness.
- Let outbound `call` messages use the connection's resolved trunk when no trunk or public SIP credentials are supplied.

**Non-Goals:**

- Do not change the SIP credential source; `data.domain`, `data.ext`, and `data.secret` still come from `SIPCLIENT_AUTH_REGISTER_URL`.
- Do not add a database watcher for arbitrary `sip_trunks` inserts.
- Do not remove explicit `trunk_resolve`; existing explicit trunk clients remain supported.
- Do not use `domain_video`, `domain_caption`, or `websocket` from the external response for SIP registration.

## Decisions

### Use `devicePlatform` on WebSocket connection

Mobile sample apps will connect with:

```text
/ws?access_token=<jwt>&devicePlatform=android
/ws?access_token=<jwt>&devicePlatform=ios
```

The gateway will validate this with the existing `normalizeDevicePlatform` rules. If mobile SIP provisioning is enabled for a user-realm token, missing or invalid `devicePlatform` rejects the WebSocket connection. Employee/admin realm clients are not auto-provisioned and do not require this parameter.

Alternative considered: wait for a post-connect `client_state` or `trunk_resolve` message. That creates a connected-but-not-ready interval and does not satisfy "connect WebSocket then ready to call".

### Separate credential upsert from notification binding

The trunk upsert step will persist SIP credentials and deterministic trunk identity only. After upsert returns the trunk ID, the gateway will call:

```text
SetTrunkNotifyUserIDAndPlatform(trunkID, jwtSub, devicePlatform)
```

This reuses existing logic that clears stale `notify_user_id` rows for the same JWT subject when the target username/domain/port changes. It also centralizes platform updates in one path used by explicit `trunk_resolve`.

Alternative considered: set `notify_user_id` and `last_online_platform` inside the upsert SQL. That is simpler but bypasses the stale identity cleanup behavior.

### Register before marking ready

The gateway will call `RegisterTrunk(trunkID, true)` after credential upsert and notification/platform binding. Only after registration succeeds will it:

- mark `client.trunkResolved=true`;
- set `client.resolvedTrunkID`;
- add the WebSocket connection to active clients;
- send a `trunk_resolved` message.

If any step fails, the WebSocket handshake is rejected.

Alternative considered: accept the WebSocket and send `trunk_not_ready`. That forces sample apps to handle a half-connected state and allows calls to be attempted before SIP registration.

### Default outbound calls to the resolved connection trunk

When a `call` message has no `trunkId`, no `trunkPublicId`, and no public SIP credentials, the gateway will use `client.resolvedTrunkID` if the connection is already auto-provisioned. Existing explicit trunk calls and public mode calls keep their current behavior.

Alternative considered: require the client to copy `trunkId` from the auto `trunk_resolved` response into every call. That leaks gateway trunk mechanics into the sample app and is not necessary once the connection has a resolved trunk.

## Risks / Trade-offs

- Platform must be available before handshake completes -> include `devicePlatform` in the WebSocket URL and add sample/SDK support.
- Query parameters can appear in logs -> continue avoiding raw token logging; `devicePlatform` is safe to log.
- Upsert/register increases WebSocket connect latency -> use existing bounded SIP auth timeout and fail closed.
- Default trunk fallback might hide missing client data bugs -> apply fallback only when the connection is resolved and no other SIP auth mode is present.
- Multiple devices for the same user may race platform updates -> latest connected platform wins, matching current `last_online_platform` semantics.

## Migration Plan

1. Update gateway WebSocket provisioning to read and validate `devicePlatform` from the query string.
2. Change mobile trunk upsert so notification/platform assignment happens through `SetTrunkNotifyUserIDAndPlatform`.
3. Send `trunk_resolved` after automatic provisioning succeeds.
4. Add outbound `call` fallback to `client.resolvedTrunkID`.
5. Update the sample SDK/app to append `devicePlatform=android|ios` to the WebSocket URL.
6. Verify Android sample flow: fetch token, connect WebSocket, receive/observe readiness, place outbound call without explicit trunk ID.

Rollback: disable `SIPCLIENT_AUTH_REGISTER_URL` to restore the previous authenticated WebSocket behavior, or revert the gateway and sample app changes together.

## Open Questions

- None.
