## 1. Gateway Platform Handshake

- [x] 1.1 Add WebSocket query parsing for `devicePlatform` during authenticated `/ws` setup.
- [x] 1.2 Require `devicePlatform=android|ios` when user-realm mobile SIP provisioning is enabled.
- [x] 1.3 Preserve employee/admin WebSocket behavior without requiring `devicePlatform`.
- [x] 1.4 Add API tests for Android, iOS, missing platform, invalid platform, and employee/admin skip behavior.

## 2. Gateway Provisioning Semantics

- [x] 2.1 Change mobile trunk upsert to persist SIP credentials without setting `notify_user_id` or hardcoded `last_online_platform=mobile`.
- [x] 2.2 Pass the verified JWT subject and actual platform into the provisioning orchestration.
- [x] 2.3 Call `SetTrunkNotifyUserIDAndPlatform(trunkID, jwtSub, devicePlatform)` after upsert and before SIP REGISTER.
- [x] 2.4 Verify stale `notify_user_id` values are cleared from old SIP identities for the same subject.
- [x] 2.5 Add trunk manager/provisioner tests for Android/iOS platform persistence and stale binding reassignment.

## 3. Gateway Ready-To-Call Behavior

- [x] 3.1 Send `trunk_resolved` to the WebSocket client after automatic provisioning and SIP REGISTER succeed.
- [x] 3.2 Keep rejecting WebSocket setup when SIP client auth, trunk persistence, notification binding, or SIP REGISTER fails.
- [x] 3.3 Add outbound `call` fallback that uses `client.resolvedTrunkID` when no explicit trunk or public SIP credentials are supplied.
- [x] 3.4 Preserve existing explicit trunk mode and public SIP mode behavior.
- [x] 3.5 Add API tests for auto `trunk_resolved`, call without explicit trunk ID, explicit trunk call, and public SIP call.

## 4. Softphone SDK and Samples

- [x] 4.1 Update the KMP SDK WebSocket URL builder or connect flow to include `devicePlatform=android|ios`.
- [x] 4.2 Ensure Android sample connects with `devicePlatform=android`.
- [x] 4.3 Ensure iOS sample connects with `devicePlatform=ios`.
- [x] 4.4 Update sample docs/tests to describe fetch JWT -> connect WebSocket -> ready to call without manual trunk resolve.

## 5. Verification

- [x] 5.1 Run `go test ./...` from `apps/gateway`.
- [x] 5.2 Run relevant softphone KMP SDK/sample tests for WebSocket URL/platform behavior.
- [x] 5.3 Validate the OpenSpec change with `openspec validate ready-to-call-mobile-ws-provisioning`.
- [x] 5.4 Manually verify the expected Android sample sequence in logs or an integration test where available.
