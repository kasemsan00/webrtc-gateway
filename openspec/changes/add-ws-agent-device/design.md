## Context

Today the gateway has three WebSocket entry points:

- `/ws` — JWT + TTRS mobile provisioner, sticky REGISTER, FCM via TTRS `notify_user_id`.
- `/ws-public` — unauthenticated per-call SIP, no presence.
- `/ws-agent` — SIP credentials in `agent_register`, refcount REGISTER, last disconnect UNREGISTER, no FCM.

EMS Android logs in with Keycloak, decrypts a provisioned SIP secret, and needs `/ws`-like reachability after kill, without the TTRS provisioner.

## Goals / Non-Goals

**Goals:**

- Dedicated `/ws-agent-device` with its own enable flag.
- JWT + `devicePlatform` on upgrade; client SIP bind after connect.
- Sticky REGISTER and stored FCM across socket drop.
- Explicit `unregister` on app logout.
- Identity-group incoming with `/ws-agent` for the same SIP AOR.

**Non-Goals:**

- No change to `/ws` provisioner or `/ws-agent` last-disconnect UNREGISTER.
- No iOS PushKit in this change.
- No TTRS notification lookup for EMS-ID.

## Decisions

### 1. New endpoint, not an extension of `/ws` or `/ws-agent`

Independent flag `API_ENABLE_AGENT_DEVICE_WS`. Mixing sticky FCM into `/ws-agent` would break Electron last-disconnect offline. Mixing client SIP passwords into `/ws` would couple EMS to TTRS provisioner.

### 2. JWT required, provisioner skipped

Upgrade requires `access_token` and `devicePlatform`. EMS-ID must be a configured JWKS realm. The SIP secret still comes from the client after login.

### 3. Distinct trunk namespace

`sipclient-agent-device-<username>@<domain>:<port>` cannot collide with `sipclient-mobile-<sub>` or `sipclient-agent-<user>@<domain>:<port>`. `notify_user_id` is the JWT `sub`. Re-binding the same `sub` to a different SIP identity UNREGISTERs and clears FCM on the old device trunk.

### 4. Sticky presence vs logout

Disconnect hangs up sessions owned by that socket only. REGISTER and FCM stay. `unregister` SIP UNREGISTERs, clears FCM/`notify_user_id`, then the client may close.

### 5. FCM stored on the trunk

`fcm_token` / `fcm_updated_at` columns. `device_push_token` with `pnType=fcm` (not Apple hex `trunk_push_token`). Incoming offline calls `FCMSender.SendPush` with that token.

### 6. Identity-group inbound routing

If owned agent + agent-device trunks share username/domain/port, matching is not ambiguous. Prefer the device trunk as the SIP session owner. Fan-out `incoming`/`cancel` to live WS on either trunk. If no live WS, push using the device trunk FCM.

## Risks / Trade-offs

- Dual SIP REGISTER for the same AOR (PC + phone) is accepted; matching must collapse the pair.
- SIP password and FCM token on WSS after JWT — never log them.
- Sticky REGISTER after kill means logout must be explicit; a crashed app stays reachable until `unregister` or admin UNREGISTER.
