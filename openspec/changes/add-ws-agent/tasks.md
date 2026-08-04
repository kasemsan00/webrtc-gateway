## 1. Config and routing

- [x] 1.1 Add `API_ENABLE_AGENT_WS` (default `false`) to gateway config and `.env.example`
- [x] 1.2 Register `/ws-agent` only when the flag is enabled (mirror `/ws-public` pattern in `server.go`)
- [x] 1.3 Add agent connection handler that upgrades without JWT and without mobile provisioner
- [x] 1.4 Document the flag in `docs/gateway/config-reference.md`

## 2. Agent trunk identity and upsert

- [x] 2.1 Add deterministic agent trunk naming distinct from `sipclient-mobile-<sub>` (domain/host + port + username)
- [x] 2.2 Implement upsert-from-credentials for agent trunks (create/update enabled trunk; no mobile notify/push binding required)
- [x] 2.3 Add unit tests proving agent trunk names cannot collide with mobile trunk names

## 3. Agent register + refcount presence

- [x] 3.1 Add WS message type `agent_register` (sipDomain/host, sipUsername, sipPassword, optional sipPort) on `/ws-agent` only
- [x] 3.2 On success: upsert trunk, REGISTER if needed, bind client, increment per-trunk refcount, send `trunk_resolved`
- [x] 3.3 On failure: leave client unresolved, send error, never log/echo SIP password
- [x] 3.4 Track agent-bound WS connections per `trunkID` with lock-safe inc/dec
- [x] 3.5 On non-last disconnect: hang up sessions owned by that client only; keep REGISTER
- [x] 3.6 On last disconnect (refcount → 0): hang up remaining trunk sessions, UNREGISTER immediately, release lease as appropriate
- [x] 3.7 Ensure reconnect after unregister can REGISTER again cleanly (no stuck state)

## 4. Call path and incoming behavior

- [x] 4.1 Allow `/ws-agent` message allowlist for softphone control (`offer`/`ice`/`call`/`hangup`/`accept`/`reject`/`dtmf`/`ping`/`request_keyframe`/`renegotiate_answer`/`client_state`, plus `agent_register`)
- [x] 4.2 Reject mobile-only messages such as `trunk_push_token` on `/ws-agent`
- [x] 4.3 Reuse resolved-trunk outbound `call` path for agent clients
- [x] 4.4 Confirm incoming fanout notifies all idle clients bound to the agent trunk
- [x] 4.5 Confirm incoming with zero bound agent clients rejects offline without FCM/APNs

## 5. Isolation from ios/android

- [x] 5.1 Do not extend mobile `normalizeDevicePlatform` to accept agent/pc values for `/ws` provisioning
- [x] 5.2 Add/adjust tests that `/ws` still rejects unsupported `devicePlatform` and still provisions android/ios unchanged
- [x] 5.3 Verify mobile sticky REGISTER-after-disconnect is unchanged by agent disconnect logic

## 6. Contract, ops, and verification

- [x] 6.1 Update `docs/gateway/ws-contract.md` with `/ws-agent`, `agent_register`, and presence/refcount semantics
- [x] 6.2 Note agent endpoint in `apps/gateway/AGENTS.md` file map / start table as needed
- [x] 6.3 Optionally expose additive ops fields (`presenceMode` / agent flag / refcount) on WS client list/SSE
- [x] 6.4 Add focused Go tests for: agent register success/fail, two-client refcount keep-alive, last disconnect hangup+unregister, no password leakage
- [x] 6.5 Run `go test ./...` from `apps/gateway` and fix regressions
