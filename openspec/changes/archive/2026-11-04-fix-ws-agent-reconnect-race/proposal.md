## Why

An old `/ws-agent` disconnect can finish cleanup after a replacement connection has already bound and started SIP registration. The stale cleanup can then unregister the trunk, release its lease, and terminate sessions owned by the replacement connection, leaving the gateway stuck until restart.

## What Changes

- Make last-agent disconnect cleanup conditional on the trunk still having zero bound agent connections when cleanup commits.
- Prevent stale disconnect cleanup from unregistering or terminating sessions after a newer agent generation has bound to the trunk.
- Repair agent registration when a client is bound but the gateway no longer owns an active trunk lease.
- Add deterministic concurrency tests for disconnect/register interleavings and registration recovery.
- Preserve the existing WebSocket contract and mobile sticky-registration behavior.

## Capabilities

### New Capabilities

- `ws-agent-reconnect-safety`: Defines observable registration and call-preservation guarantees when an agent reconnect overlaps stale disconnect cleanup.

### Modified Capabilities

None.

## Impact

- Gateway `/ws-agent` presence lifecycle in `internal/api/ws_agent.go` and related tests.
- Trunk registration/lease coordination exposed through the existing trunk manager interface.
- No WebSocket message schema, SIP media, SDP, mobile provisioning, or frontend contract changes.
