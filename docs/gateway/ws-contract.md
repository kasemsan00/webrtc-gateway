# Gateway WebSocket Contract

Source of truth for `/ws`, `/ws-public`, `/ws-agent`, and `/ws-agent-device` JSON messages.
When changing message types, also update `internal/api/ws_dispatch.go`, the
relevant `internal/api/ws_*.go` handler, and each client implementation that
consumes the contract.

---

Endpoint: `/ws`
Payload format: JSON

Auth behavior:

- When `AUTH_ENABLE=true`, `/ws` requires `access_token` query parameter (`/ws?access_token=<jwt>`). When `AUTH_ENABLE=false`, any `access_token` query parameter on `/ws` is ignored (whereas `/ws-agent` and `/ws-agent-device` reject a non-empty `access_token` with HTTP `401` when `AUTH_ENABLE=false`).
- Token is validated against configured JWKS/issuer/audience before WebSocket upgrade (`30s` clock-skew leeway). `/ws`, `/ws-agent`, and `/ws-agent-device` accept an optional realm hint via `X-Auth-Type` header or `auth_type` query parameter (`user|users|client` for user realm, `employee|employees|agent` for employee realm); if omitted or if verification in the hinted realm fails, hybrid verification tries the other configured realm.
- When `SIPCLIENT_AUTH_REGISTER_URL` is configured, authenticated user-realm `/ws` clients are provisioned as mobile SIP trunks before upgrade completes:
  - clients must include `devicePlatform=android|ios` in the WebSocket URL;
  - if mobile provisioning or `unregister` is already in progress for the same JWT `sub`, upgrade fails immediately with HTTP `409 Conflict` (`Account operation in progress; retry connection`);
  - gateway posts form-data `token=<jwt>` and `type=mobile` to the configured register URL;
  - response `data.domain`, `data.ext`, and `data.secret` become the trunk domain, username, and password;
  - trunk identity is deterministic by JWT subject (`sipclient-mobile-<sub>`);
  - `notify_user_id` is bound from verified JWT `sub`, and `last_online_platform` is updated from `devicePlatform`;
  - provisioning or SIP REGISTER failure rejects the WebSocket connection;
  - after successful provisioning and SIP REGISTER, the gateway sends `trunk_resolved` with `trunkId` and `trunkPublicId`.
- Mobile presence is sticky: WebSocket disconnect does **not** SIP UNREGISTER (push wake remains available).
- `/ws` has no pre-dispatch allowlist: every type handled by the dispatcher in
  `internal/api/ws_dispatch.go` reaches its handler, subject to handler-level
  endpoint checks (`agent_register` is `/ws-agent` only; `device_register` and
  `device_push_token` are `/ws-agent-device` only).

---

Endpoint: `/ws-public` (opt-in via `API_ENABLE_PUBLIC_WS=true`)
Payload format: JSON

Auth and scope:

- No `access_token`; this endpoint is limited to one public SIP session owned by
  the connection.
- `call` requires per-call `sipDomain`, `sipUsername`, and `sipPassword`, with
  optional `sipPort`. Trunk IDs are rejected.
- Before the session is established, only `offer` and `ping` are generally
  accepted. Session-scoped call/media commands must target the connection's own
  `sessionId` (see the `sessionId` fallback rule below).
- Supported session messages are `call`, `ice`, `hangup`, `dtmf`,
  `request_keyframe`, `renegotiate_answer`, `translate`, `translate_stop`,
  `send_message`, and `resume`, subject to their normal validation.
  `request-keyframe` (hyphen), `hold`, `unhold`, `accept`, and `reject` are not
  accepted on `/ws-public`.
- `media_health` is obsolete: although a legacy policy entry still recognizes
  the name, the dispatcher intentionally returns `Unknown message type` and the
  message is not part of the supported contract.

---

Endpoint: `/ws-agent` (opt-in via `API_ENABLE_AGENT_WS=true`)
Payload format: JSON

Auth behavior:

- `access_token` is optional (`/ws-agent?access_token=<jwt>`, optional `auth_type` query parameter or `X-Auth-Type` header). When present, the JWT is verified and attached to the connection; when `AUTH_ENABLE=false` (no verifier), when verification fails, or when the token is otherwise invalid, upgrade is rejected with HTTP `401`. When absent, the connection is accepted without JWT. The mobile SIP provisioner is not invoked. With `AUTH_ENABLE=true`, `/ws-agent` has two call modes:
  - Token + trunk: connect with `access_token`, send `agent_register`, then `call` using the resolved trunk (trunk fields may be omitted). `/ws` and `/ws-agent-device` trunk calls still require claims. A trunk call without claims is rejected with `Token authentication required to use trunk`.
  - No token + SIP user/pass: connect with no `access_token`. Either send `agent_register` with `sipDomain`, `sipUsername`, and `sipPassword` and then `call` with only `sessionId` and `destination` (Electron local SIP), or put those SIP fields on `call` itself (public mode) and omit trunk fields. Neither form requires a JWT.
- Client MUST send `agent_register` with `sipDomain` (host or IP), `sipUsername`, `sipPassword`, optional `sipPort` before placing/receiving calls. Browser/KMP clients that support multiple simultaneous call legs also send `multiCall: true`.
- Gateway upserts a deterministic agent trunk (`sipclient-agent-<username>@<domain>:<port>`) distinct from mobile trunks, SIP REGISTERs when the first agent WebSocket binds that trunk, and replies with `trunk_resolved`.
- Multiple `/ws-agent` clients may share the same SIP identity (refcount). REGISTER stays while refcount ≥ 1.
- When the last bound agent WebSocket disconnects: hang up any remaining call sessions for that trunk, then SIP UNREGISTER immediately (no grace period).
- No FCM/APNs push for agent offline incoming; zero bound clients → offline reject.
- Allowed messages: `agent_register`, `offer`, `ice`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `hold`, `unhold`, `ping`, `request_keyframe`, `request-keyframe`, `renegotiate_answer`, `client_state`. Every session-scoped type must carry an explicit `sessionId` the connection owns (or, for `offer`/`accept`/`reject`, an incoming call presented to it); the connection's legacy active session is not used as a fallback.
- Rejected on `/ws-agent`: `trunk_push_token`, `trunk_resolve`, `resume`, `translate`, `translate_stop`, `unregister`, `device_register`, `device_push_token`, and other non-allowlisted types.

#### `/ws-agent` multi-call negotiation

- Multi-call is backward compatible and connection-scoped. The gateway enables it only after `agent_register` or `client_state` includes `multiCall: true`; omitted/false keeps the original single-call busy admission behavior.
- A multi-call connection can own multiple `sessionId` values. Every call-control/media command after `answer` MUST carry the intended `sessionId`; the gateway rejects access to sessions not owned or explicitly presented to that connection.
- A busy multi-call agent remains eligible for call waiting. For a presented incoming call, negotiate WebRTC in place with `offer.sessionId` equal to the incoming `sessionId`, then send `accept` with the same ID. The ID stays stable through answer, hold, resume, and hangup.
- Multiple `/ws-agent` clients may share one SIP identity. Incoming INVITEs are fanned out to every idle/multi-call client on that trunk. The first `offer` or `accept` claims the session (first-claim-wins). Other clients then receive `cancel` with `reason=answered_elsewhere` and must stop ringing and tear down any local media. They must not receive `state: active` for a claim they lost, and a later `hangup` from a losing client must not BYE the winner's SIP dialog.
- `hold` and `unhold` perform an in-dialog SIP re-INVITE (`inactive` and `sendrecv`). Success returns `hold_state`; an error leaves the previous hold state unchanged. `491` glare and other non-2xx responses are reported as errors without ending the call.
- `client_state` may include `activeCalls` for diagnostics/admission visibility. The gateway still derives session ownership from successful offers and presented incoming calls.
- Disconnect ends every call owned by that WebSocket. The last connection for the SIP identity then unregisters the agent trunk.

---

Endpoint: `/ws-agent-device` (opt-in via `API_ENABLE_AGENT_DEVICE_WS=true`)
Payload format: JSON

Auth behavior:

- Strictly requires `devicePlatform=android|ios` in the WebSocket upgrade query string (rejected with HTTP `401` when missing or invalid, even when `AUTH_ENABLE=false`).
- `access_token` is **optional** at WebSocket upgrade (`/ws-agent-device?devicePlatform=android|ios[&access_token=<jwt>][&auth_type=...]`). When `access_token` is present, it is verified against configured JWKS realms (and rejected with HTTP `401` before upgrade if invalid or if `AUTH_ENABLE=false`). When absent, upgrade succeeds without JWT. The mobile SIP provisioner is **not** invoked.
- Client MUST send `device_register` with `sipDomain`, `sipUsername`, `sipPassword`, and optional `sipPort`, `multiCall`, and `devicePlatform` (`android` or `ios`) before placing/receiving calls.
- Gateway upserts a deterministic agent-device trunk (`sipclient-agent-device-<username>@<domain>:<port>`, default port `5060`), SIP REGISTERs when this instance does not already own it, binds `notify_user_id` to the verified JWT `sub` when authenticated or to synthetic `"sip:<username>@<domain>:<port>"` when connected without JWT, and replies with `trunk_resolved`.
- Inbound calls, `device_push_token`, and `unregister` work without JWT. Only outbound trunk `call` when `AUTH_ENABLE=true` requires verified JWT claims (`Token authentication required to use trunk`); per-call public SIP credentials or legacy global SIP fallback on `call` do not require JWT.
- Presence is sticky: WebSocket disconnect hangs up sessions owned by that connection but does **not** SIP UNREGISTER and does **not** clear FCM.
- Client `unregister` SIP UNREGISTERs the agent-device trunk, clears FCM and notify-user binding, then the client may disconnect.
- Client `device_push_token` with `pnType:"fcm"` stores the FCM token on the trunk (no WS acknowledgement message is sent on success). Offline incoming sends that token via FCM and does not look up TTRS notification tokens.
- A call that got an incoming FCM and stops ringing unanswered also gets a data-only `call_ended` FCM on the same Android targets: `{type:"call_ended", sessionId, reason, caller, callee, startedAt, platform}`, with `reason` one of `caller_cancelled`, `no_answer`, `answered_elsewhere` (the reasons of the WebSocket `cancel`). Sent at most once per call, not after a reject, and it never carries `expiresAt` or `ringTimeoutSeconds`. `answered_elsewhere` skips a stored-token device that answered and is normal priority with a 30 s TTL; the others are high priority with a 24 h TTL. No APNs equivalent yet.
- `/ws-agent` last-disconnect UNREGISTER must not unregister an agent-device trunk.
- Owned `/ws-agent` and `/ws-agent-device` trunks for the same SIP username/domain/port are one incoming identity group: live WS clients on either trunk are presented the call. Stored device FCM is sent for every agent-device incoming call that is presented or waiting offline, including while that device WebSocket is connected, so the phone app is woken to answer. A live desktop `/ws-agent` client does not receive that push.
- Allowed messages: `device_register`, `device_push_token`, `unregister`, `offer`, `ice`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `hold`, `unhold`, `ping`, `request_keyframe`, `request-keyframe`, `renegotiate_answer`, `client_state`, `resume`. Session-scoped types follow the same explicit, owned `sessionId` rule as `/ws-agent` (note: `resume` currently binds `wsClients[sessionId]` and single-session `client.sessionID` rather than `ownedSessionIDs`, so multi-session ownership in `ownedSessionIDs` applies to sessions claimed via `offer`/`accept`).
- Rejected on `/ws-agent-device`: `agent_register`, `trunk_resolve`, `trunk_push_token`, `translate`, `translate_stop`.

### Client -> Server message types

- **Session ID fallback rule:** On `/ws` and `/ws-public`, omitting `sessionId` falls back to `client.sessionID` in the handlers for `ice`, `send_message`, `request_keyframe`, `request-keyframe` (`/ws` only), `renegotiate_answer`, `translate`, and `translate_stop` (`publicClientOwnsSession` on `/ws-public` also falls back to `client.sessionID` at the allowlist layer for `hangup` and `dtmf`, though `handleWSHangup`, `handleWSDTMF`, and `handleWSHold` require a non-empty `msg.sessionId` in the message itself). On `/ws-agent` and `/ws-agent-device`, every session-scoped command requires an explicit `sessionId` owned by (or presented as incoming to) that connection.
- `agent_register` -> `/ws-agent` only; requires `sipDomain`, `sipUsername`, `sipPassword`; optional `sipPort`, `multiCall`
- `device_register` -> `/ws-agent-device` only; requires `sipDomain`, `sipUsername`, `sipPassword`; optional `sipPort`, `multiCall`, `devicePlatform` (`android` or `ios`)
- `device_push_token` -> `/ws-agent-device` only after `device_register`; requires `pnType:"fcm"` and `pnToken` (FCM registration token); optional `devicePlatform`. Sends no WS acknowledgement on success (only `error` on failure).
- `unregister` -> provisioned user-realm `/ws` or `/ws-agent-device`; SIP UNREGISTER the connection's mobile/device trunk and clear stored FCM and notify-user binding. Optional `requestId` is echoed in the acknowledgement or error.
- `offer` -> requires `sdp` (`sessionId` optional for existing session)
- `ice` -> requires `candidate` and the owning `sessionId` once an offer has been answered (omitted `sessionId` falls back to `client.sessionID` on `/ws` and `/ws-public`). Empty, `"null"`, or blank `candidate.candidate` payloads (end-of-candidates) are silently ignored.
- `call` -> requires `sessionId`, `destination` (`from` optional)
  - Public mode: include `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort`. On `/ws-agent` and `/ws-agent-device` this works with no `access_token` even when JWT auth is enabled.
  - Trunk mode: include `trunkId` or `trunkPublicId`. Explicit `trunkId`/`trunkPublicId` still requires that trunk to already be resolved on the same WebSocket connection (`Trunk must be resolved before placing call`). When `AUTH_ENABLE=true`, trunk calls require verified JWT claims (`Token authentication required to use trunk`), except on a `/ws-agent` socket whose trunk was bound by `agent_register`.
  - Auto-provisioned `/ws` mobile connections and resolved `/ws`, `/ws-agent`, or `/ws-agent-device` connections (with verified JWT claims when `AUTH_ENABLE=true` on `/ws` and `/ws-agent-device`) may omit trunk fields and public SIP credentials; the gateway uses the connection's resolved trunk.
  - Legacy global SIP fallback: on `/ws`, `/ws-agent`, and `/ws-agent-device`, when neither a trunk (explicit or resolved on the connection) nor per-call `sipDomain`+`sipUsername`+`sipPassword` are supplied, `call` falls back to legacy global SIP credentials configured on the gateway (`/ws-public` never falls back and always requires per-call SIP credentials).
- `hangup` -> requires `sessionId`; optional `reason`
- `accept` -> requires `sessionId`
- `reject` -> requires `sessionId` (`reason` optional, defaults to `busy`; `reasonSource` optional, defaults to `client_unspecified` and recorded as `client:<reasonSource>`)
- `dtmf` -> requires `sessionId`, `digits`
- `hold` / `unhold` -> requires `sessionId` of an active call. Accepted on `/ws`, `/ws-agent`, and `/ws-agent-device` (agent sockets must own the session); not accepted on `/ws-public`. Already in the requested state returns `hold_state` without a re-INVITE.
- `send_message` -> requires `body` (`contentType` defaults to `text/plain;charset=UTF-8`). The target session is `msg.sessionId`; on `/ws` and `/ws-public` an omitted `sessionId` falls back to the connection's current session, while `/ws-agent` and `/ws-agent-device` never fall back and must name an owned session.
  - `/ws` and `/ws-public` with a session: an optional `destination` updates the session's chat peer, then the gateway sends an out-of-dialog PBX-routed SIP MESSAGE using the remote/local identities and SIP credentials stored on that session. This is intentional for Asterisk B2BUA interoperability: a `2xx` response to an in-dialog MESSAGE does not guarantee forwarding to the other call leg. When the session is bridged to an agent session on this gateway, the text is relayed to that agent directly instead.
  - `/ws-agent` and `/ws-agent-device` (agent mode, SIP MESSAGE supported): the gateway sends an **in-dialog** SIP MESSAGE when the session has dialog state, otherwise the PBX-routed out-of-dialog form. Held calls are supported. Caller-supplied `destination` is ignored; `from` is used only when the session has no SIP username. Without a session the request fails with `Active SIP session required for agent message`. A copy is relayed to a bridged public session on this gateway.
  - Without any session (`/ws` only; `/ws-public` always requires its own session), `destination` is required and the gateway sends a plain out-of-dialog MESSAGE.
  - Success returns `messageSent` with `body` plus `sessionId` (when session-scoped) and `destination` (when known). Chat bootstrap/control payloads are initiated by the Electron/Web client so the client can supply the dynamic queue extension and agent identity.
- Chat images are not sent as binary in `send_message`. Clients `POST /api/chat-images` (multipart `file` + live `sessionId`) and then send the returned URL as the whole SIP MESSAGE body, for example `https://host/api/chat-images/{uuid}`, with `contentType` `text/plain;charset=UTF-8`. Receivers render it when the origin matches the gateway and the path is `/api/chat-images/{uuid}`; others show the URL as text (desktop VRS can linkify it). Legacy whole-body `<image>URL</image>` tags are still accepted. Control tokens (`@switch`, `@open_chat`, `<operator>`) are unrelated.
- `resume` -> requires `sessionId`, optional `sdp`
- `trunk_resolve` -> `/ws` only; accepts `trunkId` (`> 0`), `trunkPublicId` (UUID), **or** `sipDomain` + `sipUsername` + `sipPassword` (optional `sipPort`) — resolve-only, no auto-create.
  - Mobile clients may include optional `devicePlatform` (`ios` or `android`) so the gateway can persist the latest online platform for incoming push routing.
  - Authenticated clients may also include optional inline Apple PushKit SIP Contact fields (`pnAppId`, `pnType`, `pnToken`); when any of the three is non-empty, they are validated and saved using the same rules as `trunk_push_token` before lease acquisition/registration.
- `trunk_push_token` -> authenticated `/ws` only after trunk resolution;
  requires `pnAppId` exact-matching `PUSH_TRUNK_PN_APP_ID` (default
  `th.or.ttrs.video.prod`; mismatch returns error `pnAppId must be <expected>`),
  `pnType:"apple"`, and a hexadecimal `pnToken` of 32–256 characters; optional
  `devicePlatform` must be `ios` or `android`, and optional `trunkId` or
  `trunkPublicId` must match the connection's resolved trunk. Sends no WS
  acknowledgement on success (only `error` on failure).
- `ping` -> keepalive
- `request_keyframe` (underscore) -> **SIP→browser recovery**: the client is not receiving a decodable picture from the SIP leg. Requires `sessionId` (on `/ws` and `/ws-public` the connection's active session is used when omitted). The gateway calls `Session.SendBrowserRecoveryToAsterisk`, which returns one of 6 actions (`replay|webrtc|none|pli|fir|both`): it replays a cached SIP IDR when one is available (`replay`), otherwise sends a bounded SIP-leg FIR/PLI (`pli|fir|both`, or `none` when throttled/suppressed; `SIP_KEYFRAME_REQUEST_MODE`, default `info`, turns SIP-leg requests into SIP INFO `picture_fast_update`). When the SIP video destination, socket, or SSRC is not ready yet, the action is `webrtc`: it defers to WebRTC PLI, marks a pending browser recovery request to flush on SSRC learn, and emits session log `request_keyframe_deferred target=webrtc ...` instead of session log `request_keyframe_handled` (while the WS handler logs `request_keyframe_handled session=... action=webrtc`).
- `request-keyframe` (hyphen) -> **browser→SIP uplink IDR**: the agent replaced its outbound picture (e.g. after `@switch`) and needs the browser encoder to send a new IDR toward SIP. Requires `sessionId` (same fallback rule on `/ws`; explicit owned `sessionId` required on `/ws-agent` and `/ws-agent-device`). The gateway calls `Session.HandleSwitchKeyframeRequest`: it arms the post-switch IDR path, allows one uplink picture-size change, and sends a bounded PLI burst to the browser that stops once an IDR from after the request is forwarded. Accepted on `/ws`, `/ws-agent`, and `/ws-agent-device`; not on `/ws-public`. Log: `switch_keyframe_request received`.
- `renegotiate_answer` -> requires the owning `sessionId` (falls back to `client.sessionID` on `/ws` and `/ws-public`) and matching
  `renegotiationId`; includes optional `sdp`, `status`, and `reason`.
- `client_state` -> optional `availability`, `callState`, `sessionId`; multi-call clients also send `multiCall: true` and `activeCalls`.
- `translate` -> `/ws` and `/ws-public` only (rejected on `/ws-agent` and `/ws-agent-device`); enables translation for the owning session (omitted `sessionId` falls back to `client.sessionID`). Optional `sourceLang` and `targetLang` override `TRANSLATOR_SOURCE_LANG` / `TRANSLATOR_TARGET_LANG`. Voice selection (`ttsVoice`): the 9 built-in base target languages (`th`, `en`, `zh`, `ko`, `ja`, `ru`, `hi`, `de`, `fr`) **always** use their mapped Neural voice, ignoring `msg.ttsVoice`; `msg.ttsVoice` only applies to unmapped outbound target languages (falling back to `en-US-AriaNeural` outbound when empty, while the reverse inbound leg falls back to `TRANSLATOR_TTS_VOICE` and then `en-US-AriaNeural`).
- `translate_stop` -> `/ws` and `/ws-public` only (rejected on `/ws-agent` and `/ws-agent-device`); disables translation for the owning session (omitted `sessionId` falls back to `client.sessionID`).

### Server -> Client message types

- `unregistered` -> successful explicit logout; optional `requestId` matches the request. Sent only after SIP unregister and notification cleanup succeed.
- Unregister failures return `error` with `operation:"unregister"`, `error`, and the request's optional `requestId`. Clients must retain logout state and allow retry instead of reporting success.

### Explicit mobile logout

Send `{"type":"unregister","requestId":"logout-1"}` and wait for
`{"type":"unregistered","requestId":"logout-1"}` before closing the socket
and clearing authentication. Existing clients may omit `requestId`.
Ordinary mobile WebSocket disconnect retains sticky SIP registration and push.

On `/ws`, logout requires a verified user-realm subject and the mobile trunk
provisioned during that connection's authentication. Supplied trunk IDs cannot
select another account's trunk. On `/ws-agent-device`, it releases the registered
device trunk. Logout disables automatic SIP registration, clears notification
bindings, and ends the connection's owned sessions. Partial failures preserve the
logout target for retry; repeated successful requests are idempotent.

If another connected client shares the registration, unregister is refused.
Disconnect that client before retrying so logout does not stop its calls or
registration. Concurrent account provisioning/logout is refused for retry.

Deploy the Gateway change before clients that require this acknowledgement.
Older Gateways without user-mode unregister support cannot complete this logout
flow; clients should display the pending failure and retain authentication.

### Call and media events

- `answer`, `state`, `incoming`, `ringing`, `media`, `hold_state`
  - `incoming` is `{type:"incoming", sessionId, from, to, hasVideo}` where `hasVideo` is a JSON string (`"true"` or `"false"`).
  - `hold_state` includes `sessionId` and `held: true|false` and confirms a successful or idempotent `hold`/`unhold` request.
- `message`, `messageSent`, `dtmf`
  - `message` is `{type:"message", sessionId?, from, to, body, contentType}` for inbound SIP MESSAGE.
  - `messageSent` is `{type:"messageSent", sessionId?, destination?, body}` confirming outbound `send_message` delivery.
  - Inbound SIP MESSAGE routing: first matches an active call session by SIP `Call-ID` (`sessionId` included, sent only to the bound WS client). If no `Call-ID` matches, the gateway checks active non-ended call sessions whose local/target SIP user matches the SIP `To` header (`sessionId` attached and delivered to that session's bound WS client).
  - Otherwise (out-of-dialog MESSAGE with no matching active session, e.g. PBX DND `DND0`/`DND1`/`DND2`), the message is fanned out with `sessionId` omitted to every resolved `/ws`, `/ws-agent`, and `/ws-agent-device` client whose resolved trunk username matches the SIP `To` user.
- `renegotiate`, `renegotiate_result`
  - `renegotiate` is additive mid-call WebRTC assistance for SIP re-INVITE/UPDATE media changes and for an accepted `@switch` MESSAGE (`reason=agent_switch`). Payload: `{type:"renegotiate", sessionId, renegotiationId, sdp?, reason, mediaDirection, hasVideo, requiresAnswer, status:"pending"}`, where `hasVideo` is a JSON string (`"true"` or `"false"`).
  - `renegotiate_result` completes a pending renegotiation with `{type:"renegotiate_result", sessionId, renegotiationId, status, reason?, sdp?}`, where `status` is `"ok"`, `"failed"`, or `"timeout"` (`sdp` carries the gateway SDP answer for `agent_switch`).
  - SIP re-INVITE/UPDATE uses a gateway offer in `sdp`; the client answers with `renegotiate_answer`.
  - For `agent_switch`, `sdp` is empty. The client must create an offer (resume-style, make-before-break: do not close the live PeerConnection first) and send it in `renegotiate_answer`. The gateway may still be preparing the replacement PeerConnection; it waits internally, applies that client offer, creates an answer (returning as soon as a local ICE candidate exists rather than waiting for full gather), and returns it in `renegotiate_result.sdp`. SIP→WebRTC video is held until that offer is applied; the previous PeerConnection stays parked until ICE connected.
  - Clients that support it respond with `renegotiate_answer` (`sessionId`, `renegotiationId`, optional `sdp`, `status`, optional `reason`). Unsupported clients that ignore `renegotiate` will lose the WebRTC media path after `@switch` because the gateway creates a replacement PeerConnection.
- `resumed`, `resume_failed`, `resume_redirect`
  - `resumed`: `{type:"resumed", sessionId, state, from, to, sdp?}` (`sdp` is included when the client's `resume` request supplied an SDP offer).
  - `resume_failed`: `{type:"resume_failed", sessionId, reason}`.
  - `resume_redirect`: `{type:"resume_redirect", sessionId, redirectUrl}` when `session_directory` points to another gateway instance.
- `trunk_resolved`, `trunk_redirect`, `trunk_not_found`, `trunk_not_ready`
  - `trunk_resolved` now returns both `trunkId` and `trunkPublicId`
- `pong`, `error`
  - Recoverable `send_message` failures include `operation:"send_message"` and `operationSessionId`. They intentionally omit `sessionId` so SDK versions through `0.1.30` do not mistake a chat delivery failure for a terminal call failure and close the active PeerConnection.
- `cancel` -> cancels a previously presented incoming call and includes
  `sessionId` plus `reason`. Known reasons: `caller_cancelled`, `no_answer`,
  and `answered_elsewhere` (another client on the same SIP identity claimed
  the call). After a successful `accept`, the gateway sends
  `answered_elsewhere` to every other resolved client on that trunk. The
  winning connection receives `state: active` and must not receive that
  cancel.
- `translation_caption` -> SIP-to-WebRTC caption event with `sessionId`,
  direction, source/target languages, recognized/translated text, and
  `isFinal`.
- `translate`, `translate_stop` -> acknowledge translation enabled/disabled
  state for the session.

### Outbound call progress (`state` / `ringing`)

Outbound WebRTC→SIP call progress is SIP dialog progress, **not** WebRTC ICE readiness.

| `state` value  | Meaning                                     |
| -------------- | ------------------------------------------- |
| `connecting`   | Outbound INVITE initiated / dialing         |
| `ringing`      | SIP `180`/`183` received (far end alerting) |
| `active`       | SIP `200 OK` — far end answered             |
| `ended`        | Call or attempt terminated                  |
| `reconnecting` | Post-answer ICE recovery                    |

Rules:

- `state` messages may include an optional additive `reason` string. Older clients may ignore it.
- If ICE becomes terminal before the outbound SIP INVITE starts, the gateway emits
  `{"type":"state","state":"ended","reason":"ice_failed_pre_sip","sessionId":"..."}`
  and suppresses the less actionable generic `Failed to make call: context canceled` error.
- WebRTC ICE connected during `connecting`/`ringing` does **not** emit `active`.
- After client `call`, the gateway acknowledges with `connecting` (or current SIP progress if already `ringing`/`active`).
- On transition to ringing, the gateway also sends additive `{"type":"ringing","sessionId":"..."}` for softphone clients that listen for that message type.
- `type: answer` remains the WebRTC SDP answer to `offer`, unrelated to SIP answer.
- **`state: active` is not equivalent to remote video receiving.** Asterisk may answer immediately while Linphone video arrives later.

### Remote media presence (`media`)

Additive server→client signal when remote SIP media is first observed as ready toward the WebRTC client:

```json
{
  "type": "media",
  "sessionId": "<id>",
  "kind": "video",
  "direction": "remote",
  "state": "receiving"
}
```

| Field       | Values             | Meaning                 |
| ----------- | ------------------ | ----------------------- |
| `kind`      | `video` \| `audio` | Media type              |
| `direction` | `remote`           | SIP→WebRTC (v1)         |
| `state`     | `receiving`        | First ready observation |

Video `receiving` is emitted once per session when the gateway has parameter sets (SPS/PPS) and observes a complete IDR (or legacy keyframe with cached sets). Audio `receiving` is emitted once on the first accepted SIP audio RTP write to the WebRTC track. SSRC-learn alone does not emit video ready. Missing WS clients are non-fatal.

### Admin WebSocket connection diagnostics

`GET /api/ws-clients` and the `client` object in `/api/ws-clients/stream`
SSE events include optional `sipUsername`, `sipDomain`, and `sipPort` fields.
These identify the SIP account associated with the connection, including idle
connections with a resolved trunk. The gateway uses the resolved trunk's account
or falls back to the current session's SIP authentication context for public
connections and sessions whose trunk is unavailable. Fields are omitted when
the SIP account is not yet known. SIP passwords are never included.

If you add/change a message type, update all of:

1. `internal/api/ws_dispatch.go`, the relevant `internal/api/ws_*.go` handler,
   and the `WSMessage` payload in `internal/api/server.go`
2. TTRS VRI client handlers/types in `lib/gateway/`
3. this document
