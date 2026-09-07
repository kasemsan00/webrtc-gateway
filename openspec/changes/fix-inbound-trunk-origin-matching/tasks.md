## 1. Reproduce and Protect the Failure

- [x] 1.1 Add a focused matcher test for user `00025` with local Request-URI `203.151.21.121:5090`, transport/Via origin `203.150.245.41:5060`, and a desired trunk configured as `sipagent.ttrs.or.th:5060`.
- [x] 1.2 Add duplicate fixtures covering owned/current, owned/stale, unowned, disabled, and same-username trunks on different PBXs.
- [x] 1.3 Add assertions that self-targeted Request-URI and To URI cannot produce an early `ruri_user_domain` or domain-only ambiguous result.
- [x] 1.4 Add negative tests proving indistinguishable current owned trunks return ambiguity instead of selecting by ID, map order, registration timestamp, or agent name.

## 2. Registration Identity Model

- [x] 2.1 Define immutable normalized registrar endpoint and per-trunk successful registration identity types with generation, username, hostname, port, transport, addresses, success time, and current-expiry metadata.
- [x] 2.2 Add an injectable registrar endpoint resolver that supports configured IP literals, A/AAAA hostname resolution, explicit/default ports, and existing SRV behavior without adding a new runtime dependency.
- [x] 2.3 Capture the actual successful REGISTER response source endpoint when available and combine it with configured/resolved endpoints without storing credentials.
- [x] 2.4 Add pure normalization and endpoint-equivalence tests for case-insensitive hostnames, IPv4, bracketed IPv6, default port 5060, explicit ports, TCP/UDP, duplicate addresses, and malformed input.

## 3. Registration Snapshot Lifecycle

- [x] 3.1 Add per-trunk operation generations so late REGISTER or refresh completion cannot overwrite newer register, unregister, lease-loss, or rebind state.
- [x] 3.2 Publish a complete registration identity snapshot only after a successful initial, forced, or refresh REGISTER, using DNS and SIP I/O outside trunk manager lock scopes.
- [x] 3.3 Preserve the last still-current successful snapshot across a transient refresh failure and replace it atomically after the next successful refresh.
- [x] 3.4 Invalidate snapshots on explicit unregister start/success, lease loss, disable, removal from the loaded enabled set, identity-changing trunk update, and manager shutdown.
- [x] 3.5 Add lifecycle tests for successful publication, failed registration, failed refresh, DNS target replacement, unregister/register races, agent reconnect races, lease loss, and stale-generation suppression.

## 4. Deterministic INVITE Matcher

- [x] 4.1 Refactor `MatchTrunkFromInviteDetailed` to copy required trunk, ownership, local Contact, and registration snapshot state under `RLock`, release the lock, and run a pure selector.
- [x] 4.2 Classify Request-URI and To URI as local Gateway Contact targets using normalized public host/IP and listening port; skip registrar-domain rules only for URIs classified as local.
- [x] 4.3 Build eligible username candidates from enabled trunks owned by the receiving instance with a current successful registration identity before evaluating ambiguity.
- [x] 4.4 Extract confidence-tagged origin evidence from the transport peer, top Via, and Contact while excluding From and SDP media addresses.
- [x] 4.5 Select by exact address+port+transport first, then permit address+transport relaxation only when it yields one candidate and does not override a unique transport-peer result.
- [x] 4.6 Preserve direct non-local Request-URI user+domain+port matching for compatible deployments while applying ownership/current-registration safety before final selection.
- [x] 4.7 Replace the current recent-registration and agent-name tie-breakers with unique-eligible selection or deterministic `503` ambiguity.
- [x] 4.8 Add tests for trusted transport peer precedence, conflicting Via/Contact, NAT source-port rewrite, shared proxy endpoint, missing source, unresolved hostname, and multiple hostnames resolving to the same address.

## 5. Diagnostics and SIP Rejection Behavior

- [x] 5.1 Extend the detailed match result with local-target flags, normalized origin summaries, eligible candidate IDs, selected registrar evidence, and a stable ambiguity/rejection reason.
- [x] 5.2 Update inbound INVITE console and logstore events to report origin-based selection and safe ambiguity context without logging passwords, Authorization headers, push tokens, or complete credentials.
- [x] 5.3 Preserve existing handler behavior: selected owned trunk continues through incoming-call admission, unresolved ambiguity returns `503 Service Unavailable`, and no eligible trunk follows the existing non-trunk rejection policy.
- [x] 5.4 Add handler tests confirming the selected trunk reaches the existing `/ws-agent` or mobile notification path and ambiguous matching does not create a session or offer a call.

## 6. Concurrency and Performance Verification

- [x] 6.1 Add tests with a resolver spy proving INVITE matching performs no DNS or database calls and registration resolution occurs outside trunk manager locks.
- [x] 6.2 Add concurrent matcher/register/refresh/unregister/lease-loss tests and run the SIP package with the Go race detector where supported.
- [x] 6.3 Benchmark or measure the pure matcher with representative trunk counts and confirm no material regression in inbound INVITE handling latency.
- [x] 6.4 Run `go test -v ./internal/sip -run "TestMatchTrunkFromInvite|TestInboundTrunk"` from `apps/gateway`.
- [x] 6.5 Run `go test ./...` from `apps/gateway` and resolve all failures without changing media behavior.

## 7. Interoperability and Rollout Validation

- [x] 7.1 Verify with an Asterisk trace that REGISTER advertises `sip:00025@203.151.21.121:5090` while INVITE source/Via `203.150.245.41:5060` selects the trunk configured as `00025@sipagent.ttrs.or.th:5060`.
- [x] 7.2 Verify duplicate stale or unowned `00025` rows do not block the current owned registration, while two indistinguishable current owned registrations still return `503`.
- [ ] 7.3 Verify incoming accept, decline (`486`), busy (`486`), no-answer (`480`), remote CANCEL (`200` to CANCEL and `487` to INVITE), and established BYE behavior remain unchanged.
- [x] 7.4 Verify Opus passthrough, H.264-only video, SPS/PPS handling, RTP/RTCP forwarding, and keyframe recovery are unchanged in one accepted real call.
- [x] 7.5 Document the new match rules and diagnostic fields in Gateway troubleshooting or operations documentation, including how to identify and soft-disable obsolete duplicate trunks.
- [ ] 7.6 Run the SIP/WebRTC call-flow verifier and record production rollout and rollback evidence; no database migration rollback should be required.
