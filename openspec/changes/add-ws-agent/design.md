## Context

Today the gateway has two WebSocket entry points:

- `/ws` — JWT-authenticated; user-realm clients with `devicePlatform=android|ios` auto-provision a sticky mobile trunk (`sipclient-mobile-<sub>`), SIP REGISTER, and keep REGISTER after WS disconnect so incoming can wake via FCM/APNs.
- `/ws-public` — unauthenticated outbound-only; per-call SIP credentials; no `trunk_resolve`, no persistent REGISTER, no incoming.

PC agent web needs persistent REGISTER while connected (for inbound + outbound), multi-machine sharing of one SIP user, and **immediate** cleanup when the last agent WS disconnects — without JWT mobile provisioning and without changing ios/android behavior.

## Goals / Non-Goals

**Goals:**

- Add `/ws-agent` as a parallel presence path for agent web clients.
- Register using client-provided SIP username/password/domain-or-IP (optional port).
- Use a trunk identity namespace distinct from mobile.
- Refcount WS bindings per agent trunk; REGISTER while refcount ≥ 1.
- On refcount → 0: hang up any active/ringing call for that trunk, then UNREGISTER immediately (no grace timer).
- Fan out incoming to all agent WS clients bound to that trunk.
- Leave `/ws` mobile provisioning, sticky presence, push routing, and `/ws-public` guards unchanged.

**Non-Goals:**

- No 3-minute (or any) unregister grace period.
- No access_token / SIPCLIENT mobile provision for agents.
- No push (FCM/APNs) for agent offline incoming.
- No merge of agent and mobile trunks for the same human user.
- No changes to `devicePlatform=android|ios` validation on `/ws`.
- No agent softphone mobile app work in this change (gateway + contract only).

## Decisions

### 1. New endpoint `/ws-agent` (not `/ws-public` extension)

- **Choice:** Dedicated route registered alongside `/ws` / `/ws-public`.
- **Why:** `/ws-public` is intentionally narrow (outbound-only). Extending it would blur security and message allowlists. A separate endpoint keeps mobile and public contracts untouched.
- **Alternatives:** Extend `/ws-public` (rejected — mixes policies); reuse `/ws` without token (rejected — conflicts with AUTH_ENABLE and mobile provisioner).

### 2. Credential-based agent register message after connect

- **Choice:** Accept WS upgrade on `/ws-agent` without JWT. Client MUST send an agent bind/register message (e.g. `agent_register` or reuse a clearly agent-scoped variant) with `sipDomain` (host or IP), `sipUsername`, `sipPassword`, optional `sipPort` before call use.
- **Why:** Credentials come from the agent web app, not Keycloak/SIPCLIENT.
- **On success:** upsert agent trunk, SIP REGISTER (if not already registered for this trunk on this instance), bind client → trunk, increment refcount, send `trunk_resolved`.
- **On failure:** send error; do not leave a half-bound client as call-ready.
- **Alternatives:** Query-string credentials on upgrade (rejected — secrets in URLs/logs); require pre-created admin trunk only (rejected — user wants client-provided creds with upsert).

### 3. Separate trunk namespace from mobile

- **Choice:** Deterministic agent trunk key derived from SIP identity (domain/host + port + username), e.g. `sipclient-agent-<username>@<domain>:<port>` (exact formatting TBD in implementation, must not collide with `sipclient-mobile-<sub>`).
- **Why:** Mobile and agent must be different trunks even if the same person uses both.
- **Alternatives:** Share mobile trunk (rejected — unregister would drop mobile presence / push).

### 4. Presence = WebSocket refcount, unregister immediately at zero

- **Choice:** Track bound agent WS connections per `trunkID`. First bind → REGISTER. Additional binds → refcount++ only. Last disconnect → hangup active sessions for that trunk (if any) → UNREGISTER → release lease as appropriate.
- **Why:** Matches product rule: multiple PCs can share one SIP user; only when all disconnect should the extension go offline. No grace — avoid hung REGISTER.
- **Alternatives:** Per-connection REGISTER (rejected — SIP identity is one AOR); grace timer (rejected by product).

### 5. Last disconnect during a call = hangup + unregister together

- **Choice:** When refcount hits 0, terminate any non-ended session associated with that trunk (or bound agent clients), then UNREGISTER.
- **Why:** Agent has no push/resume expectation; leaving REGISTER or media without a controlling WS is worse than a clean tear-down.
- **Note:** If refcount > 0 because another PC is still connected, do **not** hang up solely because one sibling disconnected (unless that sibling uniquely owned the session — hang up only sessions owned by the disconnecting client or, on last disconnect, all trunk sessions). Preferred rule:
  - Disconnect of non-last client: hang up only sessions owned by that client; keep trunk REGISTER.
  - Disconnect of last client (refcount → 0): hang up any remaining trunk sessions, then UNREGISTER.

### 6. Isolation from mobile platform / push code

- **Choice:** Do not add `pc`/`agent` into mobile `normalizeDevicePlatform` used by `/ws` provisioning. Agent presence is endpoint + trunk mode (`ephemeral` / agent flag), not a mobile `devicePlatform` value.
- **Why:** Avoid accidental push fallback (`unknown_platform_fallback`) and avoid weakening “android|ios required” mobile specs.
- **Incoming offline:** When no WS bound to agent trunk, reject with existing offline path (no push targets).

### 7. Message allowlist on `/ws-agent`

- **Choice:** Allow call-control needed for softphone agent: `agent_register` (or equivalent), `offer`, `ice`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `ping`, `request_keyframe`, `renegotiate_answer`, `client_state`, optional `send_message` / translate if already safe for trunk mode. Reject `trunk_push_token` and mobile-only provision semantics. Do not require `access_token`.
- **Why:** Agents need inbound+outbound; must not open mobile push token APIs without auth.

### 8. Config / ops

- **Choice:** Feature flag or always-on route consistent with gateway style (prefer explicit enable flag akin to `API_ENABLE_PUBLIC_WS`, e.g. `API_ENABLE_AGENT_WS`, default false until rolled out — or default true if product wants always available; **prefer opt-in flag** for safer rollout).
- Expose additive fields on WS client listing/SSE: `agent`/`presenceMode`, `resolvedTrunkID`, optional refcount for debugging.
- Never log SIP passwords.

## Risks / Trade-offs

- **[Risk] Unauthenticated `/ws-agent` accepts SIP passwords over WS** → Mitigation: TLS-only in deployment docs; opt-in flag; rate-limit/bind failures; no password in logs or `trunk_resolved`.
- **[Risk] Refcount races (reconnect vs unregister)** → Mitigation: serialize bind/unbind per trunkID; unregister only after observing refcount 0 under lock; reconnect increments before REGISTER if already registered.
- **[Risk] Accidental collision with mobile trunk names** → Mitigation: distinct name prefix + tests asserting non-collision with `sipclient-mobile-`.
- **[Risk] Sibling disconnect hangs up wrong call** → Mitigation: session ownership tied to WS client; last-disconnect cleanup is the only trunk-wide hangup.
- **[Risk] Implementer extends `normalizeDevicePlatform` for agent** → Mitigation: design/tasks explicitly forbid; keep mobile specs unchanged.
- **[Trade-off] Immediate unregister vs brief network flap** → Accepted: product chose immediate offline over sticky presence.

## Migration Plan

1. Ship gateway with `/ws-agent` behind enable flag (recommended).
2. Update `docs/gateway/ws-contract.md` and config reference.
3. Agent web points to `/ws-agent` and sends credentials; no mobile client changes.
4. Rollback: disable flag / stop routing agent web to `/ws-agent`; mobile unaffected.

## Open Questions

- Exact WS message name: `agent_register` vs extending `trunk_resolve` with auto-register only on `/ws-agent` (prefer new `agent_register` to avoid changing resolve-only semantics on `/ws`).
- Default for `API_ENABLE_AGENT_WS` (recommend `false` until agent web is ready).
- Whether agent trunks appear in admin trunk UI the same as manual trunks (yes by default — they are normal DB trunks with agent naming).
