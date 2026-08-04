## Why

PC agent web clients need the same outbound/inbound SIP calling capability as mobile, but must not leave SIP REGISTER presence hanging after the last WebSocket disconnect. Mobile (`android`/`ios`) correctly stays registered for push wake; agent presence should be ephemeral and credential-driven without JWT mobile provisioning.

## What Changes

- Add a new WebSocket endpoint `/ws-agent` for PC agent clients (no `access_token`, no mobile SIP provisioner).
- Agent clients supply SIP credentials (`sipDomain` or IP, `sipUsername`, `sipPassword`, optional port) and the gateway upserts a **separate** trunk identity from mobile (`sipclient-mobile-<sub>`).
- Maintain a per-trunk WebSocket refcount: REGISTER while at least one agent connection is bound; when refcount reaches 0, hang up any active call for that trunk and UNREGISTER immediately (no grace period).
- Support multiple agent machines sharing one SIP user: incoming fanout to all bound WS clients; unregister only when all are disconnected.
- Keep `/ws` mobile path (`devicePlatform=android|ios` + JWT provision + sticky REGISTER + push) and `/ws-public` outbound-only behavior unchanged.
- Document the agent contract and add ops visibility for agent presence (platform/mode, refcount) without changing existing ios/android wire semantics.

## Capabilities

### New Capabilities

- `ws-agent`: Unauthenticated (credential-based) agent WebSocket lifecycle — connect, register from client-provided SIP creds, call in/out, multi-connection refcount presence, immediate hangup+unregister on last disconnect.

### Modified Capabilities

- (none — no existing OpenSpec capability specs cover `/ws` mobile or `/ws-public`; this change introduces agent behavior as a parallel path)

## Impact

- **Gateway API:** new route `/ws-agent`; new/extended WS handlers for agent register bind and disconnect cleanup; trunk manager upsert/register/unregister for agent trunks.
- **SIP:** additional trunk namespace for agents; REGISTER/UNREGISTER driven by WS refcount (not JWT provision).
- **Incoming:** reuse existing trunk-scoped WS fanout; agent offline with refcount 0 rejects as offline (no push).
- **Docs/contract:** `docs/gateway/ws-contract.md`, config reference, AGENTS.md notes.
- **Clients:** agent web must connect to `/ws-agent` and send SIP credentials; mobile/ios/android clients unchanged.
- **Non-goals:** no 3-minute grace timer; no access-token provision for agents; no changes to mobile sticky presence or push routing for `android`/`ios`.
