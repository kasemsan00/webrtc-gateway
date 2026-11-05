## Context

See `proposal.md` for motivation and `specs/ws-agent-reconnect-safety/spec.md` for the required outcomes. Agent presence is protected by the API server mutex, but SIP REGISTER/UNREGISTER and lease operations occur after releasing that mutex. The current last-disconnect path therefore acts on a refcount snapshot that can become stale while network I/O is pending.

The gateway must not hold its shared server mutex during SIP or database I/O. Agent WebSocket clients do not resume calls after socket loss, so sessions owned by the disconnected connection may still be ended, while sessions created after a replacement bind must be preserved.

## Goals / Non-Goals

**Goals:**

- Make the final registration state follow current agent presence despite disconnect/register interleavings.
- Keep network I/O outside the shared server lock.
- Preserve cleanup of sessions owned by the disconnected client and true-last-disconnect unregister behavior.
- Recover a bound client whose trunk lease/registration ownership is missing.

**Non-Goals:**

- Change WebSocket message schemas, authentication, mobile provisioning, SIP media, SDP, or agent call resume behavior.
- Introduce a process-wide lock around SIP operations.

## Decisions

### Revalidate presence and snapshot cleanup targets

Last-disconnect cleanup will re-read current trunk refcount immediately before committing trunk-wide cleanup. It will snapshot the IDs of pre-existing trunk sessions before network work and act only on that snapshot, so a replacement connection cannot have newly created sessions swept into stale cleanup.

This is preferred over iterating all trunk sessions after unregister begins, which can include sessions created by a replacement connection.

### Serialize lifecycle operations per trunk and compensate for changed presence

No shared server mutex will be held across SIP network calls. A dedicated mutex per trunk serializes agent-triggered REGISTER/UNREGISTER operations without blocking unrelated trunks or the presence map. Presence may still change while an operation is in flight, so cleanup revalidates the refcount and performs a compensating `RegisterTrunk` after unregister returns when a replacement appeared.

This guarantees that a replacement registration cannot complete before a stale unregister on the same trunk, while the final presence recheck also handles a replacement binding during the serialized unregister. A later true-last disconnect waits for the same trunk operation lock and restores the unregistered final state.

### Verify ownership independently from binding membership

Agent registration will require `RegisterTrunk` when either the client is the first binding or `TrunkManager.IsTrunkOwned` reports that the instance does not hold the lease. This checks the manager's actual `ownedLeases` state rather than its loaded-trunk cache, so an existing binding cannot suppress repair after lease ownership was lost.

## Risks / Trade-offs

- [Risk] A reconnect during unregister can cause an extra REGISTER request → Mitigation: registration is idempotent and the compensating request occurs only when presence exists after unregister.
- [Risk] Compensating REGISTER can fail after the client already began its own registration → Mitigation: log the repair failure at high signal; the normal agent-register path also verifies ownership and retries before declaring a future registration call-ready.
- [Risk] Snapshot cleanup may leave an unrelated orphan session created after the snapshot → Mitigation: only a currently bound replacement can create such a session, so preserving it is safer than stale teardown; normal session lifecycle owns later cleanup.

## Migration Plan

Deploy as an in-place gateway update with no protocol or database migration. Rollback is the previous binary. After deployment, verify overlapping reconnect, true-last disconnect, two-agent sharing, outbound call, and incoming callback behavior.
