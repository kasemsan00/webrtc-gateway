## Context

The gateway already verifies WebSocket `access_token` values with configured JWT/JWKS verifiers before upgrading `/ws`. After verification, the connection stores `auth.VerifiedClaims` on `WSClient` and trunk flows use `trunk_resolve` to mark a client as eligible for outbound and inbound trunk calls.

Mobile clients now need a simpler path: the gateway should exchange the same verified mobile access token with `sipclient-auth.ttrs.or.th/auth/register`, receive the user's SIP credentials, persist them in `sip_trunks`, register the trunk, and bind the WebSocket client to that trunk before normal call messages are processed.

## Goals / Non-Goals

**Goals:**

- Provision a mobile user's SIP trunk automatically during authenticated WebSocket connection setup.
- Use the external register response `data.domain`, `data.ext`, and `data.secret` as the SIP registration credentials.
- Update an existing mobile user's trunk when the external service returns changed SIP data.
- Reject WebSocket connection setup when provisioning or SIP registration fails.
- Reuse existing trunk-based outbound call, incoming call, push, and session resume behavior after provisioning succeeds.

**Non-Goals:**

- Do not replace local JWT verification with the external SIP client auth service.
- Do not change public SIP mode behavior.
- Do not use `domain_video`, `domain_caption`, or the returned `websocket` field for SIP trunk registration in this change.
- Do not introduce per-trunk authorization beyond binding the authenticated mobile client to its provisioned trunk.

## Decisions

### Provision after local JWT verification

The gateway will continue to verify the token locally first. Only after JWT verification succeeds will it call the external SIP client auth register API.

Alternative considered: use the external service as the only token verifier. This would weaken the existing issuer/audience/signature checks and make the gateway unavailable whenever the external service is unavailable.

Provisioning applies only to authenticated user-realm mobile clients. Employee/admin realm WebSocket clients keep the existing authenticated WebSocket behavior and are not auto-provisioned into mobile SIP trunks.

### Use `data.domain` for SIP registration

The provisioned trunk will use `data.domain` from the external response as the SIP domain. `domain_video`, `domain_caption`, and `websocket` are ignored for this change.

Alternative considered: use `domain_video` because this gateway handles video. The explicit decision is to use `domain`, keeping registration aligned with the requested behavior.

### Use a deterministic mobile trunk identity

The gateway will identify the auto-provisioned trunk by authenticated user identity rather than mutable SIP credentials. The recommended trunk name is:

```text
sipclient-mobile-<auth-subject>
```

The persisted trunk will store:

- `name`: deterministic mobile trunk name
- `domain`: external response `data.domain`
- `username`: external response `data.ext`
- `password`: external response `data.secret`
- `port`: `5060`
- `transport`: `tcp`
- `enabled`: `true`
- `notify_user_id`: JWT `sub`
- `last_online_platform`: `mobile`

Alternative considered: find trunks by `ext/domain/secret`. That creates duplicates when `secret` changes and does not model the authenticated mobile user as the owner.

### Upsert before registration

The gateway will create the trunk when missing and update the existing deterministic trunk when the external service returns changed SIP credentials. After persistence, it will call `RegisterTrunk(trunkID, true)` so the local gateway instance acquires the lease and performs normal SIP REGISTER.

Alternative considered: use the existing `ResolveOrCreateTrunk` helper. It keys by credentials and creates `auto-<username>` names, which is not suitable for stable mobile identity or credential rotation.

### Bind the WebSocket client to the provisioned trunk

After successful SIP REGISTER, the gateway will set the WebSocket client as trunk-resolved for the provisioned trunk. This allows existing `call`, incoming fanout, push contact, and pending incoming notification paths to operate without a new client message.

Alternative considered: require the client to send `trunk_resolve` after connection. This keeps old behavior but does not satisfy automatic provisioning on connect.

### Fail closed

If the external auth register API fails, returns invalid required fields, trunk persistence fails, or SIP registration fails, the gateway rejects the WebSocket connection setup.

Alternative considered: accept the WebSocket and later return `trunk_not_ready`. The selected behavior gives mobile clients a clear connection-level failure and prevents partially connected clients from attempting calls without a registered SIP trunk.

## Risks / Trade-offs

- External service latency increases WebSocket connection latency -> use a bounded timeout and log elapsed time without logging token or secret values.
- External service outage blocks mobile WebSocket connections -> fail closed as requested; operators can disable the feature or restore service.
- Secret rotation while a trunk is already registered may require forced re-register -> update trunk first, then call `RegisterTrunk(..., true)` so registration uses current credentials.
- Deterministic trunk names include stable user identifiers -> avoid exposing full trunk names in unauthenticated logs or public UI beyond existing admin surfaces.
- Multiple simultaneous connections for the same user may race provisioning -> implement upsert with conflict handling or transaction-level retry.

## Migration Plan

1. Add configuration for the external register endpoint and request timeout.
2. Add trunk upsert support for deterministic mobile trunks.
3. Add WebSocket provisioning after JWT verification and before normal read loop processing.
4. Deploy with the feature disabled or endpoint unset in non-mobile environments if needed.
5. Enable configuration in environments where `AUTH_ENABLE=true`, `DB_ENABLE=true`, and trunk registration is available.

Rollback: disable the provisioning configuration or deploy the previous build. Existing auto-created trunks can remain disabled or be managed like normal trunks.

## Open Questions

- None.
