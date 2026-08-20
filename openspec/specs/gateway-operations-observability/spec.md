## Purpose

Provide operators with truthful runtime health, durable call and media evidence, and connected console views that make gateway failures diagnosable without exposing secrets or changing call behavior.

## Requirements

### Requirement: Operational health reflects actual component readiness

The gateway SHALL expose authenticated operational health that distinguishes `disabled`, `connected`, `degraded`, `unavailable`, and `unknown` component states. Database state SHALL be derived from configured and usable persistence rather than the presence of a LogStore interface, and the existing dashboard compatibility boolean SHALL be true only when persistence is connected.

#### Scenario: Database logging is disabled

- **WHEN** the gateway runs with database logging disabled and therefore uses no persistent database connection
- **THEN** operational health reports the database component as `disabled` and the compatibility `dbConnected` value is false

#### Scenario: Database is usable

- **WHEN** database logging is enabled and the gateway has a usable database pool
- **THEN** operational health reports the database component as `connected` and includes a recent successful readiness timestamp

#### Scenario: A configured dependency is unhealthy

- **WHEN** a configured database, translator, push provider, SIP listener, or persistence worker is not ready or begins dropping work
- **THEN** operational health identifies the affected component as `degraded` or `unavailable` with a machine-readable reason while unrelated components retain their own states

### Requirement: Operational health is safe and bounded

Operational-health responses SHALL exclude credentials, tokens, DSNs, private payload bodies, and unbounded identifiers. Health collection SHALL use bounded-time or cached readiness checks so an operator request cannot block SIP, WebSocket, RTP/RTCP, persistence, or translation hot paths.

#### Scenario: Operator reads detailed health

- **WHEN** an authenticated operator requests detailed gateway health
- **THEN** the response contains only bounded component state, listener/readiness facts, queue utilization, drop counters, and freshness timestamps and contains no secret configuration values

#### Scenario: A dependency probe is slow

- **WHEN** an external dependency cannot respond within its health-check budget
- **THEN** the gateway returns a cached, `unknown`, `degraded`, or `unavailable` state without waiting indefinitely or interrupting active calls

### Requirement: Active session statistics are persisted periodically

When database logging is enabled, the gateway SHALL capture a thread-safe observability snapshot for each active session at the configured statistics interval and enqueue it for persistence without blocking media forwarding. The collector SHALL stop with gateway lifecycle and SHALL NOT create unbounded per-session goroutines.

#### Scenario: Active session reaches a stats interval

- **WHEN** an active session exists when `DB_STATS_INTERVAL_MS` elapses
- **THEN** the gateway records a statistics sample containing the session identifier, timestamp, media-recovery counters, RTCP counters, and any defined typed quality metrics available at that time

#### Scenario: Stats persistence queue is full

- **WHEN** a statistics sample cannot be queued without blocking
- **THEN** the gateway drops that sample, increments a cumulative drop counter, exposes the degradation through operational health, and continues the call without panic or media interruption

#### Scenario: Database logging is disabled

- **WHEN** database logging is disabled
- **THEN** periodic collection performs no database work and does not report a false persistence failure

### Requirement: Persisted session overview exposes safe routing and media evidence

The authenticated admin API SHALL provide a typed session overview for persisted sessions containing call timing and outcome, SIP identity, auth mode, trunk identity, negotiated audio/video profiles, Opus payload type, RTP/RTCP port assignments, and video rejection state when recorded. It SHALL omit passwords, raw tokens, and unreviewed metadata maps.

#### Scenario: Operator opens a completed session

- **WHEN** a persisted session is requested by session identifier
- **THEN** the response includes all available safe call, routing, and negotiated-media fields from the stored record, including auth and trunk columns that exist in the current schema

#### Scenario: Operator opens a legacy session

- **WHEN** an older persisted session does not contain one or more newer observability fields
- **THEN** the API returns the available fields with absent values represented compatibly and does not fail the whole session view

#### Scenario: Active session is inspected

- **WHEN** the requested session is still active
- **THEN** the console can show its live state together with persisted evidence without requiring a changed WebSocket call-control message

### Requirement: Structured session evidence is inspectable

The operations console SHALL allow operators to inspect structured event data, SIP call identity, parsed payload content, text payload content, binary-payload metadata, and persisted statistics for a session. Large or binary evidence SHALL remain bounded and SHALL require an explicit operator action to expand, copy, or download.

#### Scenario: Event contains structured diagnostic data

- **WHEN** a session event contains a non-empty `data` object
- **THEN** the event row indicates that details are available and the operator can inspect formatted structured data without losing the existing event columns and filters

#### Scenario: Payload has parsed or binary content

- **WHEN** a payload includes parsed content or base64-encoded bytes
- **THEN** the payload detail distinguishes raw text, parsed structure, and binary metadata and does not render an unbounded binary value directly in the table

#### Scenario: Session statistics exist

- **WHEN** persisted statistics are available for a session
- **THEN** the console shows timestamped recovery and RTCP metrics and makes defined quality metrics inspectable instead of presenting an empty or opaque tab

### Requirement: Existing operational metadata is visible and connected

The console SHALL surface useful metadata already returned by the gateway for active sessions, WebSocket clients, trunks, and client diagnostics, and SHALL provide navigation between related resources when a stable identifier is available.

#### Scenario: Operator inspects live clients and calls

- **WHEN** active-session or WebSocket-client data includes connection time, call count, multi-call mode, SIP call identity, trunk identity, SIP username, translation voice, or update time
- **THEN** the console exposes those values as visible details or optional columns and links stable session and trunk identifiers to their corresponding views

#### Scenario: Operator inspects trunk presence

- **WHEN** a trunk response includes last-online platform, last-online time, or update time
- **THEN** the console shows the latest client-presence and configuration freshness information without exposing a push token

#### Scenario: Operator correlates client diagnostics

- **WHEN** client diagnostics include app version, device hash, auth realm, trace identifier, or related session evidence
- **THEN** the console makes those identifiers visible and filterable and links to a session when a session identifier is safely available

### Requirement: Existing signaling and media contracts remain compatible

The observability change SHALL be additive for authenticated admin APIs and SHALL NOT rename or remove existing REST fields, WebSocket message types or fields, SIP methods or state mappings, database schema objects, SDP behavior, codec support, or RTP/RTCP forwarding behavior.

#### Scenario: Existing client connects after the change

- **WHEN** a mobile, web, or agent client using the existing WebSocket and call-control contract connects to the updated gateway
- **THEN** it can establish and control calls without supplying or consuming any new observability field

#### Scenario: Existing console-compatible API consumer reads a response

- **WHEN** an existing consumer reads dashboard, session, trunk, client, or diagnostic responses
- **THEN** all previously documented fields retain their names and meanings while new observability fields are additive
