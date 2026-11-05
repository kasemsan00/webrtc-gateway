## Why

EMS Android needs sticky SIP REGISTER and Firebase wake after the app is killed, while still binding SIP credentials from Keycloak/EMS login instead of the TTRS `/ws` mobile provisioner. `/ws-agent` cannot do this: last disconnect UNREGISTERs immediately and there is no FCM. `/ws` cannot do this: it ignores client SIP passwords and looks up FCM through TTRS.

## What Changes

- Add `/ws-agent-device` behind `API_ENABLE_AGENT_DEVICE_WS` (default false).
- Require JWT `access_token` and `devicePlatform=android|ios` on upgrade. Do not call `SIPCLIENT_AUTH_REGISTER_URL`.
- Client sends `device_register` with SIP domain/user/pass, then optional `device_push_token` with an FCM token stored on the trunk.
- Presence is sticky: socket drop keeps REGISTER and FCM. Logout sends `unregister`, then the client disconnects.
- Incoming for the same SIP user@domain:port treats `/ws-agent` and `/ws-agent-device` as one identity group (live WS fan-out; offline uses stored FCM).
- Leave `/ws`, `/ws-public`, and `/ws-agent` last-disconnect semantics unchanged.

## Capabilities

### New Capabilities

- `ws-agent-device`: JWT-authenticated device WebSocket with client-supplied SIP credentials, sticky REGISTER, gateway-stored FCM, and explicit logout unregister.

### Modified Capabilities

- `inbound-trunk-routing`: same SIP user on agent + agent-device trunks is not an ambiguous miss.

## Impact

- **Gateway API:** new route, handlers, trunk namespace `sipclient-agent-device-<user>@<domain>:<port>`, `sip_trunks.fcm_token`.
- **Push:** send FCM from the stored token; skip TTRS for this path.
- **Clients:** KMP `connectAgentDevice` and Android `softphone-mobile`; Electron `/ws-agent` unchanged.
- **Non-goals:** iOS PushKit, merging into `sipclient-agent-*`, TTRS Notification API for EMS-ID.
