## 1. Configuration and External Client

- [x] 1.1 Add gateway configuration for the SIP client auth register URL and request timeout.
- [x] 1.2 Implement a small SIP client auth register client that posts form fields `token` and `type=mobile`.
- [x] 1.3 Validate the external response requires `status=OK`, `data.domain`, `data.ext`, and `data.secret`.
- [x] 1.4 Add unit tests for successful response parsing, non-OK response handling, missing field handling, and request timeout/error handling.

## 2. Mobile Trunk Persistence

- [x] 2.1 Add deterministic mobile trunk naming based on authenticated subject.
- [x] 2.2 Add trunk manager support to create or update the deterministic mobile trunk with `domain`, `ext`, `secret`, port `5060`, transport `tcp`, and enabled state.
- [x] 2.3 Persist `notify_user_id` as the authenticated subject and `last_online_platform` as `mobile`.
- [x] 2.4 Handle concurrent create/update races without producing duplicate trunks.
- [x] 2.5 Add tests for first create, credential update, no-op update, and duplicate/race conflict handling.

## 3. WebSocket Provisioning Flow

- [x] 3.1 Invoke mobile SIP provisioning after `/ws` JWT verification succeeds for user-realm mobile clients.
- [x] 3.2 Reject the WebSocket connection when external provisioning fails, response validation fails, trunk persistence fails, or SIP registration fails.
- [x] 3.3 Call `RegisterTrunk(trunkID, true)` after persistence so the current gateway owns and registers the trunk.
- [x] 3.4 Mark the WebSocket client as trunk-resolved to the provisioned trunk after registration succeeds.
- [x] 3.5 Ensure logs include provisioning stage, subject, and trunk ID where available without logging access tokens or SIP secrets.

## 4. Call Flow Integration

- [x] 4.1 Verify outbound trunk calls can proceed without a client-sent `trunk_resolve` after auto-provisioning.
- [x] 4.2 Verify incoming call fanout targets auto-provisioned clients through existing trunk notification logic.
- [x] 4.3 Preserve existing explicit `trunk_resolve`, public SIP mode, and employee/admin WebSocket behavior.
- [x] 4.4 Update gateway documentation for the new mobile provisioning configuration and failure behavior.

## 5. Verification

- [x] 5.1 Add focused API/WebSocket tests covering successful auto-provisioning and client trunk binding.
- [x] 5.2 Add focused API/WebSocket tests covering external auth failure and SIP registration failure rejection.
- [x] 5.3 Run `go test ./...` from `apps/gateway`.
- [x] 5.4 Run any relevant frontend tests only if the frontend WebSocket contract or UI behavior changes.
