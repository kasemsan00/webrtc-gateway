# Gateway WebSocket Contract

Source of truth for `/ws` and `/ws-public` JSON messages.
When changing message types, also update `internal/api/ws_dispatch.go` and frontend `gateway-store.ts`.

---

Endpoint: `/ws`  
Payload format: JSON

Auth behavior:

- When `AUTH_ENABLE=true`, `/ws` requires `access_token` query parameter (`/ws?access_token=<jwt>`).
- Token is validated against configured JWKS/issuer/audience before WebSocket upgrade.
- When `SIPCLIENT_AUTH_REGISTER_URL` is configured, authenticated user-realm `/ws` clients are provisioned as mobile SIP trunks before upgrade completes:
  - clients must include `devicePlatform=android|ios` in the WebSocket URL;
  - gateway posts form-data `token=<jwt>` and `type=mobile` to the configured register URL;
  - response `data.domain`, `data.ext`, and `data.secret` become the trunk domain, username, and password;
  - trunk identity is deterministic by JWT subject (`sipclient-mobile-<sub>`);
  - `notify_user_id` is bound from verified JWT `sub`, and `last_online_platform` is updated from `devicePlatform`;
  - provisioning or SIP REGISTER failure rejects the WebSocket connection;
  - after successful provisioning and SIP REGISTER, the gateway sends `trunk_resolved` with `trunkId` and `trunkPublicId`.

### Client -> Server message types

- `offer` -> requires `sdp` (`sessionId` optional for existing session)
- `call` -> requires `sessionId`, `destination` (`from` optional)
  - Public mode: include `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort`
  - Trunk mode: include `trunkId` or `trunkPublicId`
  - Auto-provisioned mobile connections may omit trunk fields and public SIP credentials; the gateway uses the connection's resolved trunk.
- `hangup` -> requires `sessionId`
- `accept` -> requires `sessionId`
- `reject` -> requires `sessionId` (`reason` optional, defaults to `busy`)
- `dtmf` -> requires `sessionId`, `digits`
- `send_message` -> requires `body`; use in-dialog if session exists, otherwise requires `destination`
- `resume` -> requires `sessionId`, optional `sdp`
- `trunk_resolve` -> requires `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort` (resolve-only; no auto-create)
  - Mobile clients may include `devicePlatform` (`ios` or `android`) so the gateway can persist the latest online platform for incoming push routing.
- `ping` -> keepalive
- `request_keyframe` -> requires `sessionId` (or the connection's active session) and uses bounded legacy SIP-directed recovery.

### Server -> Client message types

- `answer`, `state`, `incoming`
- `message`, `messageSent`, `dtmf`
- `renegotiate`, `renegotiate_result`
  - `renegotiate` is additive mid-call WebRTC assistance for SIP re-INVITE/UPDATE media changes. It includes `sessionId`, `renegotiationId`, optional `sdp`, `reason`, `mediaDirection`, `hasVideo`, and `requiresAnswer`.
  - Clients that support it respond with `renegotiate_answer` (`sessionId`, `renegotiationId`, optional `sdp`, `status`, optional `reason`).
- `resumed`, `resume_failed`, `resume_redirect`
- `trunk_resolved`, `trunk_redirect`, `trunk_not_found`, `trunk_not_ready`
  - `trunk_resolved` now returns both `trunkId` and `trunkPublicId`
- `pong`, `error`

If you add/change a message type, update all of:

1. `internal/api/server.go` switch + payload struct
2. Frontend client handlers/senders in `frontend` (`src/features/gateway/store/gateway-store.ts`)
3. this document
