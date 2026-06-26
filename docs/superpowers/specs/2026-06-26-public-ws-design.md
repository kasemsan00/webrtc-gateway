# Public WebSocket Endpoint Design

Date: 2026-06-26

## Goal

Allow clients that provide per-call public SIP credentials, such as `E:\dev\n1669-react-native`, to place calls without JWT auth while keeping the existing authenticated `/ws` behavior unchanged.

## Non-Goals

- Do not weaken `/ws` authentication.
- Do not allow unauthenticated trunk calls.
- Do not expose trunk resolve, push token registration, mobile auto-provisioning, incoming call control, or management flows through the public endpoint.
- Do not change SIP media behavior, SDP handling, or trunk database behavior.

## Approach

Add a separate WebSocket endpoint, `/ws-public`, controlled by a new opt-in environment flag. The default remains disabled.

Proposed config:

- `API_ENABLE_PUBLIC_WS=false` by default.
- When `API_ENABLE_PUBLIC_WS=true`, register `/ws-public`.
- `AUTH_ENABLE=true` continues to protect `/ws` and `/api/*` as it does today.

The public endpoint reuses the existing WebSocket transport, session creation, public SIP registration, and call handling code, but marks the connection as public-only. Public-only clients are not assigned `authClaims` and are not eligible for mobile SIP provisioning.

## Public-Only Message Policy

Unauthenticated clients on `/ws-public` may use only the public outgoing call lifecycle:

- `offer`
- `ice`
- `call`, only when `sipDomain`, `sipUsername`, and `sipPassword` are present and no `trunkId` or `trunkPublicId` is present
- `hangup`
- `dtmf`
- `ping`
- `request_keyframe`
- `renegotiate_answer`, only for the same public session
- `resume`, only when the target session exists and its auth mode is `public`

All other messages are rejected with a WebSocket error response. In particular, public clients cannot use:

- `trunk_resolve`
- `trunk_push_token`
- trunk calls by `trunkId` or `trunkPublicId`
- connection-resolved trunk calls
- legacy calls that omit public SIP credentials
- `accept` or `reject` for incoming calls
- translation control
- `client_state`

## Data Flow

1. A public client connects to `/ws-public`.
2. Gateway accepts the WebSocket only if `API_ENABLE_PUBLIC_WS=true`.
3. Client sends `offer`; gateway creates a normal WebRTC session and returns `answer`.
4. Client sends `call` with public SIP fields:
   - `sipDomain`
   - `sipUsername`
   - `sipPassword`
   - optional `sipPort`
   - optional `from`
5. Gateway acquires/registers the public SIP account through `PublicAccountRegistry`.
6. Gateway sets the session SIP auth context to `public`.
7. Gateway places the SIP call.
8. Hangup decrements the public account ref count through existing cleanup.

## Session Ownership

For `/ws-public`, a client may act only on its own WebSocket-associated session unless the handler already has a stronger session check.

For `resume`, the gateway may accept the request only if:

- the session exists locally or resolves through the existing session directory flow, and
- the session auth mode is `public`.

If the session is trunk, legacy, missing, or expired, the gateway returns the existing `resume_failed` or an error response and does not attach the public client to that session.

## Error Handling

- If `/ws-public` is disabled, handshake returns `404` by not registering the route.
- If an unsupported message is received, return a WebSocket `error` message.
- If `call` is missing public SIP credentials, return `Public SIP credentials required on public WebSocket`.
- If `call` includes trunk fields, return `Trunk calls require authenticated WebSocket`.
- Keep existing public SIP registration errors unchanged so clients receive the same failure details they receive today after reaching `handleWSCall`.

## Logging

Add concise logs for:

- `/ws-public` connection accepted.
- rejected public-only message type.
- rejected public call due to missing public credentials or trunk fields.

Do not log `sipPassword`.

## Tests

Gateway tests should cover:

- `/ws` missing token still returns `401` when auth is enabled.
- `/ws-public` is unavailable when `API_ENABLE_PUBLIC_WS=false`.
- `/ws-public` accepts unauthenticated WebSocket when enabled.
- public-only client can complete `offer` then send a `call` with `sipDomain`, `sipUsername`, and `sipPassword`.
- public-only client cannot send trunk call fields.
- public-only client cannot send `trunk_resolve` or `trunk_push_token`.
- public-only `resume` is allowed only for sessions whose SIP auth context is `public`.

## Client Impact

`E:\dev\n1669-react-native` already sends public SIP credentials in the `call` message. It only needs its gateway URL changed from `/ws` to `/ws-public` for deployments that enable the new endpoint.

No mobile trunk client behavior changes. Existing authenticated clients continue using `/ws`.
