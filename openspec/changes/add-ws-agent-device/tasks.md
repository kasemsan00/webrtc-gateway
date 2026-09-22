## 1. Config, route, and contract

- [x] 1.1 Add `API_ENABLE_AGENT_DEVICE_WS` (default false) to config, `.env.example`, and config-reference
- [x] 1.2 Register `/ws-agent-device` only when the flag is enabled
- [x] 1.3 Document `/ws-agent-device` in `docs/gateway/ws-contract.md` and AGENTS.md
- [x] 1.4 Add `fcm_token` / `fcm_updated_at` migration and schema

## 2. Trunk identity and FCM

- [x] 2.1 Add `BuildAgentDeviceTrunkName` / `UpsertAgentDeviceTrunk`
- [x] 2.2 Store/clear FCM on the trunk; bind JWT `sub` + platform
- [x] 2.3 Re-bind of the same subject to a new SIP identity unregisters the old device trunk
- [x] 2.4 Collapse agent + agent-device identity groups in INVITE matching

## 3. WebSocket lifecycle

- [x] 3.1 JWT + devicePlatform upgrade without mobile provisioner
- [x] 3.2 `device_register`, `device_push_token`, `unregister` handlers
- [x] 3.3 Disconnect hangs up owned sessions only; keeps REGISTER and FCM
- [x] 3.4 `/ws-agent` last disconnect does not unregister agent-device trunks
- [x] 3.5 Allowlist call/media + `resume`; reject `agent_register`, `trunk_resolve`, `trunk_push_token`, `translate`

## 4. Incoming and push

- [x] 4.1 Fan-out incoming/cancel across the identity group
- [x] 4.2 Offline incoming sends stored FCM without TTRS
- [x] 4.3 Agent-only offline still has no FCM

## 5. Tests

- [x] 5.1 Flag off, missing JWT/platform, register success, no password echo
- [x] 5.2 Sticky disconnect vs logout unregister
- [x] 5.3 Isolation from `/ws-agent` last disconnect
- [x] 5.4 Stored FCM dispatch and identity-group match
