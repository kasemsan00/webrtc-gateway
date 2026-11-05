## Purpose

Keep `/ws-agent` trunk registration and active replacement connections correct when reconnect overlaps delayed cleanup from an older WebSocket connection.

## ADDED Requirements

### Requirement: Stale disconnect cleanup cannot invalidate replacement presence

The gateway SHALL preserve a SIP registration when an agent connection binds to a trunk before cleanup from an older connection to that trunk completes.

#### Scenario: Replacement binds before last-disconnect decision

- **WHEN** the apparent last agent disconnects and a replacement agent binds to the same trunk before unregister begins
- **THEN** the gateway MUST keep the trunk registered
- **AND** the stale cleanup MUST NOT terminate sessions created by the replacement connection

#### Scenario: Replacement binds while unregister is in flight

- **WHEN** a replacement agent binds while stale unregister network work is already in flight
- **THEN** the gateway MUST restore registration ownership after the stale unregister completes
- **AND** the trunk MUST finish with an active registration while replacement presence remains

### Requirement: Bound agents recover missing trunk ownership

The gateway SHALL verify trunk ownership independently from the in-memory agent binding refcount before declaring an agent call-ready.

#### Scenario: Existing binding has no owned lease

- **WHEN** an agent registers and its connection is already present in the trunk binding set but the gateway does not own the trunk lease
- **THEN** the gateway MUST perform SIP registration and lease acquisition again
- **AND** it MUST send `trunk_resolved` only after that repair succeeds

### Requirement: True last disconnect still unregisters immediately

The gateway SHALL retain existing last-disconnect cleanup when no replacement presence appears.

#### Scenario: No replacement agent reconnects

- **WHEN** the final bound agent disconnects and no replacement binds during cleanup
- **THEN** the gateway MUST terminate the disconnected agent's remaining trunk sessions
- **AND** the gateway MUST unregister the trunk and release its lease immediately

#### Scenario: Mobile WebSocket disconnects

- **WHEN** a mobile `/ws` connection disconnects
- **THEN** the gateway MUST NOT apply `/ws-agent` unregister or reconnect-repair semantics to the mobile trunk
