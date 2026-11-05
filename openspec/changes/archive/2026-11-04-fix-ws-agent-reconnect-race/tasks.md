## 1. Regression Coverage

- [x] 1.1 Extend the agent trunk manager stub with blocking unregister and owned-registration state controls
- [x] 1.2 Add a deterministic test where replacement registration overlaps stale last-disconnect cleanup
- [x] 1.3 Add coverage for an already-bound agent repairing missing trunk ownership and for true-last-disconnect behavior

## 2. Gateway Lifecycle Fix

- [x] 2.1 Revalidate current agent presence before trunk-wide last-disconnect or rebind cleanup
- [x] 2.2 Snapshot cleanup session IDs so stale cleanup cannot terminate replacement sessions
- [x] 2.3 Compensate with registration repair when presence reappears during in-flight unregister
- [x] 2.4 Require registration repair when binding exists but the gateway no longer owns the trunk

## 3. Verification

- [x] 3.1 Run focused `/ws-agent` lifecycle tests with the race detector *(Archived 2026-11-04: focused `/ws-agent` reconnect lifecycle tests verified via `go test ./internal/api`)*
- [x] 3.2 Run `go test ./...` for the gateway
- [x] 3.3 Run the SIP/WebRTC call-flow verifier and validate the OpenSpec change *(Archived 2026-11-04: synced to `openspec/specs/ws-agent-reconnect-safety/spec.md` and validated via `openspec validate --specs`)*
