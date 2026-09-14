# Gateway WebSocket Contract

Source of truth for `/ws`, `/ws-public`, and `/ws-agent` JSON messages.
When changing message types, also update `internal/api/ws_dispatch.go`, the
relevant `internal/api/ws_*.go` handler, and each client implementation that
consumes the contract.

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
- Mobile presence is sticky: WebSocket disconnect does **not** SIP UNREGISTER (push wake remains available).

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
  `sessionId`.
- Supported session messages are `call`, `ice`, `hangup`, `dtmf`,
  `request_keyframe`, `renegotiate_answer`, `translate`, `translate_stop`,
  `send_message`, and `resume`, subject to their normal validation.
- `media_health` is obsolete: although a legacy policy entry still recognizes
  the name, the dispatcher intentionally returns `Unknown message type` and the
  message is not part of the supported contract.

---

Endpoint: `/ws-agent` (opt-in via `API_ENABLE_AGENT_WS=true`)
Payload format: JSON

Auth behavior:

- No `access_token` and no mobile SIP provisioner.
- Client MUST send `agent_register` with `sipDomain` (host or IP), `sipUsername`, `sipPassword`, optional `sipPort` before placing/receiving calls. Browser/KMP clients that support multiple simultaneous call legs also send `multiCall: true`.
- Gateway upserts a deterministic agent trunk (`sipclient-agent-<username>@<domain>:<port>`) distinct from mobile trunks, SIP REGISTERs when the first agent WebSocket binds that trunk, and replies with `trunk_resolved`.
- Multiple `/ws-agent` clients may share the same SIP identity (refcount). REGISTER stays while refcount ≥ 1.
- When the last bound agent WebSocket disconnects: hang up any remaining call sessions for that trunk, then SIP UNREGISTER immediately (no grace period).
- No FCM/APNs push for agent offline incoming; zero bound clients → offline reject.
- Allowed messages: `agent_register`, `offer`, `ice`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `hold`, `unhold`, `ping`, `request_keyframe`, `renegotiate_answer`, `client_state`.
- Rejected on `/ws-agent`: `trunk_push_token`, `trunk_resolve`, `resume`, and other non-allowlisted types.

#### `/ws-agent` multi-call negotiation

- Multi-call is backward compatible and connection-scoped. The gateway enables it only after `agent_register` or `client_state` includes `multiCall: true`; omitted/false keeps the original single-call busy admission behavior.
- A multi-call connection can own multiple `sessionId` values. Every call-control/media command after `answer` MUST carry the intended `sessionId`; the gateway rejects access to sessions not owned or explicitly presented to that connection.
- A busy multi-call agent remains eligible for call waiting. For a presented incoming call, prepare WebRTC in place with `offer.sessionId` equal to the incoming `sessionId`, then send `accept` with the same ID. The ID stays stable through answer, hold, resume, and hangup.
- Multiple `/ws-agent` clients may share one SIP identity. Incoming INVITEs are fanned out to every idle/multi-call client on that trunk. The first `offer` or `accept` claims the session (first-claim-wins). Other clients then receive `cancel` with `reason=answered_elsewhere` and must stop ringing and tear down any local media. They must not receive `state: active` for a claim they lost, and a later `hangup` from a losing client must not BYE the winner's SIP dialog.
- `hold` and `unhold` perform an in-dialog SIP re-INVITE (`inactive` and `sendrecv`). Success returns `hold_state`; an error leaves the previous hold state unchanged. `491` glare and other non-2xx responses are reported as errors without ending the call.
- `client_state` may include `activeCalls` for diagnostics/admission visibility. The gateway still derives session ownership from successful offers and presented incoming calls.
- Disconnect ends every call owned by that WebSocket. The last connection for the SIP identity then unregisters the agent trunk.

### Client -> Server message types

- `agent_register` -> `/ws-agent` only; requires `sipDomain`, `sipUsername`, `sipPassword`; optional `sipPort`, `multiCall`
- `offer` -> requires `sdp` (`sessionId` optional for existing session)
- `ice` -> requires `candidate` and the owning `sessionId` once an offer has been answered
- `call` -> requires `sessionId`, `destination` (`from` optional)
  - Public mode: include `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort`
  - Trunk mode: include `trunkId` or `trunkPublicId`
  - Auto-provisioned mobile connections and resolved `/ws-agent` connections may omit trunk fields and public SIP credentials; the gateway uses the connection's resolved trunk.
- `hangup` -> requires `sessionId`
- `accept` -> requires `sessionId`
- `reject` -> requires `sessionId` (`reason` optional, defaults to `busy`)
- `dtmf` -> requires `sessionId`, `digits`
- `hold` / `unhold` -> `/ws-agent` multi-call only; requires an active, owned `sessionId`
- `send_message` -> requires `body`; when a session exists, sends an out-of-dialog PBX-routed SIP MESSAGE using the remote/local identities and SIP credentials stored on that session. This is intentional for Asterisk B2BUA interoperability: a `2xx` response to an in-dialog MESSAGE does not guarantee forwarding to the other call leg. Without a session, `destination` is required. On `/ws-agent`, it requires an owned active-call `sessionId`, supports held calls, and never trusts caller-supplied `destination`/`from`. Success returns `messageSent` with the same `sessionId` and `body`. Chat bootstrap/control payloads are initiated by the Electron/Web client so the client can supply the dynamic queue extension and agent identity.
- `resume` -> requires `sessionId`, optional `sdp`
- `trunk_resolve` -> requires `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort` (resolve-only; no auto-create)
  - Mobile clients may include `devicePlatform` (`ios` or `android`) so the gateway can persist the latest online platform for incoming push routing.
- `trunk_push_token` -> authenticated `/ws` only after trunk resolution;
  requires `pnAppId`, `pnType:"apple"`, and a hexadecimal `pnToken` of 32–256
  characters; optional `devicePlatform` must be `ios` or `android`, and optional
  `trunkId` or `trunkPublicId` must match the connection's resolved trunk.
- `ping` -> keepalive
- `request_keyframe` -> requires `sessionId` (or the connection's active session) and uses bounded legacy SIP-directed recovery.
- `renegotiate_answer` -> requires the owning `sessionId` and matching
  `renegotiationId`; includes optional `sdp`, `status`, and `reason`.
- `client_state` -> optional `availability`, `callState`, `sessionId`; multi-call clients also send `multiCall: true` and `activeCalls`.
- `translate` -> enables translation for the owning session; optional
  `sourceLang`, `targetLang`, and `ttsVoice` override configured defaults.
- `translate_stop` -> disables translation for the owning session.

### Server -> Client message types

- `answer`, `state`, `incoming`, `ringing`, `media`, `hold_state`
  - `hold_state` includes `sessionId` and `held: true|false` and confirms a successful or idempotent `hold`/`unhold` request.
- `message`, `messageSent`, `dtmf`
  - `message` is `{type:"message", sessionId?, from, to, body, contentType}` for inbound SIP MESSAGE.
  - In-dialog MESSAGE is sent only to the WebSocket client bound to the matching active session.
  - Out-of-dialog MESSAGE (no matching call session, e.g. PBX DND `DND0`/`DND1`/`DND2`) is sent to every resolved `/ws` or `/ws-agent` client whose trunk username matches the SIP `To` user. `sessionId` is omitted.
- `renegotiate`, `renegotiate_result`
  - `renegotiate` is additive mid-call WebRTC assistance for SIP re-INVITE/UPDATE media changes and for an accepted `@switch` MESSAGE (`reason=agent_switch`). It includes `sessionId`, `renegotiationId`, optional `sdp`, `reason`, `mediaDirection`, `hasVideo`, and `requiresAnswer`.
  - SIP re-INVITE/UPDATE uses a gateway offer in `sdp`; the client answers with `renegotiate_answer`.
  - For `agent_switch`, `sdp` is empty. The client must create an offer (resume-style, make-before-break: do not close the live PeerConnection first) and send it in `renegotiate_answer`. The gateway applies that client offer, creates an answer, and returns it in `renegotiate_result.sdp`. SIP→WebRTC video is held until that offer is applied; the previous PeerConnection stays parked until ICE connected.
  - Clients that support it respond with `renegotiate_answer` (`sessionId`, `renegotiationId`, optional `sdp`, `status`, optional `reason`). Unsupported clients that ignore `renegotiate` will lose the WebRTC media path after `@switch` because the gateway has already created a replacement PeerConnection.
- `resumed`, `resume_failed`, `resume_redirect`
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

If you add/change a message type, update all of:

1. `internal/api/ws_dispatch.go`, the relevant `internal/api/ws_*.go` handler,
   and the `WSMessage` payload in `internal/api/server.go`
2. TTRS VRI client handlers/types in `lib/gateway/`
3. this document
