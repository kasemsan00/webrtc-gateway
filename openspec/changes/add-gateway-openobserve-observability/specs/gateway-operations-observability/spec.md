## ADDED Requirements

### Requirement: Gateway telemetry export is optional and collector-oriented

The gateway SHALL keep telemetry export disabled by default and, when explicitly enabled, SHALL send telemetry only to a configured OpenTelemetry Collector endpoint using OTLP. The gateway SHALL identify every exported signal with stable service name, service version, deployment environment, and gateway instance metadata. OpenObserve-specific query or ingestion credentials SHALL NOT be required by the gateway process.

#### Scenario: Telemetry is not configured

- **WHEN** the gateway starts with telemetry export disabled
- **THEN** it starts and handles calls without contacting a collector or reporting telemetry failure

#### Scenario: Telemetry is enabled

- **WHEN** the gateway starts with telemetry enabled and a valid collector endpoint
- **THEN** exported signals contain the configured service and environment metadata plus the gateway instance identifier

#### Scenario: Telemetry configuration is invalid

- **WHEN** telemetry is enabled with an invalid endpoint, protocol, timeout, queue, or sampling value
- **THEN** startup fails before listeners accept traffic with a diagnostic that identifies the invalid field and does not reveal credentials or headers

### Requirement: Telemetry cannot interrupt real-time call processing

Telemetry production and export SHALL use bounded resources and SHALL NOT perform collector or OpenObserve network I/O synchronously in SIP, WebSocket, RTP/RTCP, media-forwarding, persistence-worker, or translation hot paths. Queue saturation, collector unavailability, export timeout, and shutdown timeout SHALL result in bounded drops or degradation reporting rather than a panic, unbounded wait, or call interruption.

#### Scenario: Collector becomes unavailable during active calls

- **WHEN** the collector is unreachable while one or more calls are active
- **THEN** calls and media forwarding continue, telemetry retry remains bounded, and local health records export degradation and cumulative drop or failure counters

#### Scenario: Telemetry queue reaches capacity

- **WHEN** a telemetry record cannot be accepted without exceeding the configured bounded queue
- **THEN** the gateway drops telemetry according to its documented policy, increments a cumulative drop counter, and does not block the producer

#### Scenario: Telemetry shutdown exceeds its budget

- **WHEN** a collector does not acknowledge pending telemetry before the configured shutdown deadline
- **THEN** the gateway records the bounded flush failure locally and completes shutdown without waiting indefinitely

### Requirement: Centralized operational logs are structured and safe

Selected high-signal gateway lifecycle and control-plane logs SHALL expose stable machine-queryable fields for timestamp, severity, service, environment, instance, component, event name, and relevant safe correlation identifiers. Logs sent toward centralized collection SHALL redact or omit raw credentials, authorization headers, tokens, passwords, DSNs, private keys, push tokens, message or transcript bodies, full SIP messages, full SDP, and unreviewed arbitrary payload maps at the source. Collector-side filtering SHALL be defense in depth and SHALL NOT be the sole protection.

#### Scenario: Operator correlates a call event

- **WHEN** the gateway records a selected call, WebSocket, SIP, resume, persistence, push, or translation lifecycle event
- **THEN** the structured record includes a stable event name, component, severity, gateway instance, and session correlation identifier when one is safely available

#### Scenario: Sensitive signaling data is supplied

- **WHEN** a diagnostic source contains SIP authorization, a password, access token, raw SDP, full SIP content, an IP address, a message body, or another classified sensitive field
- **THEN** centralized structured output contains only an approved omission marker, bounded metadata, masked value, or one-way correlation value and never the raw sensitive value

#### Scenario: Legacy text logging remains during migration

- **WHEN** an existing code path still emits a legacy text log
- **THEN** local logging and `/api/logs/*` remain functional and the collector can ingest the record as legacy text without requiring a frontend or wire-contract change

### Requirement: Gateway operational metrics are bounded

The gateway SHALL emit low-cardinality counters, gauges, and histograms for gateway lifecycle, active and completed calls, WebSocket connections, SIP outcomes and setup latency, resume behavior, persistence queue utilization and drops, translator outcomes and latency, and aggregate media-health or recovery behavior. Metric attributes SHALL use a documented bounded vocabulary and SHALL NOT contain session identifiers, SIP Call-IDs, phone numbers, usernames, IP addresses, device identifiers, raw error messages, or other unbounded values.

#### Scenario: A call completes

- **WHEN** a call reaches a terminal state
- **THEN** call outcome counters and duration histograms are updated using only bounded attributes such as direction, normalized outcome, and status class

#### Scenario: Media health is observed

- **WHEN** the gateway records RTP/RTCP loss, jitter, round-trip, keyframe, PLI, FIR, NACK, or recovery information
- **THEN** it exports aggregate counters or sampled histograms and does not emit a metric or span for each media packet

#### Scenario: An unbounded identifier is available

- **WHEN** metric instrumentation has access to a session ID, SIP Call-ID, phone number, username, IP address, or arbitrary error text
- **THEN** that value is excluded from metric attributes while safe correlation remains available in structured logs or sampled traces

### Requirement: Tracing covers sampled control-plane work only

The gateway SHALL create sampled traces for bounded HTTP operations, selected WebSocket control messages, SIP transactions, call lifecycle phases, and configured external dependency calls. Trace production SHALL NOT modify existing REST, WebSocket, SIP, or SDP contracts and SHALL NOT create per-RTP/RTCP-packet spans. Trace attributes and error events SHALL follow the same sensitive-data policy as centralized logs.

#### Scenario: A sampled call is established

- **WHEN** sampling selects a call-control flow that crosses WebSocket or HTTP handling and SIP signaling
- **THEN** spans describe the bounded control-plane phases and include safe session correlation without adding required client fields or SIP headers

#### Scenario: A call is not selected by sampling

- **WHEN** sampling excludes a call or operation
- **THEN** the gateway performs no trace export for that flow while required logs, metrics, and call behavior remain unchanged

#### Scenario: Media packets flow

- **WHEN** RTP or RTCP packets are read, transformed, or forwarded
- **THEN** the gateway creates no span per packet and relies on aggregate metrics and throttled anomaly logs

### Requirement: Telemetry health is locally inspectable

Authenticated operational health SHALL report bounded local telemetry state including `disabled`, `connected`, `degraded`, `unavailable`, or `unknown`, last successful export time when known, queue utilization, cumulative dropped records, cumulative export failures, and configuration-safe identity fields. It SHALL NOT query OpenObserve synchronously or expose collector credentials, authorization headers, private endpoints classified as secret, or telemetry payload content.

#### Scenario: Collector exports are healthy

- **WHEN** telemetry is enabled and recent batches have been accepted by the configured collector
- **THEN** operational health reports telemetry as connected with a freshness timestamp and bounded queue counters

#### Scenario: Collector status cannot be determined

- **WHEN** no bounded recent export result is available
- **THEN** operational health reports telemetry as unknown or degraded from cached local state without probing the collector on the request path

### Requirement: Existing evidence and compatibility surfaces remain authoritative

PostgreSQL LogStore SHALL remain the source of truth for sessions, events, payloads, dialogs, statistics, session directory, gateway registry, and trunk coordination. The gateway SHALL preserve existing `/api/logs/*`, authenticated admin APIs, WebSocket messages, SIP behavior, SDP behavior, codec behavior, and RTP/RTCP forwarding behavior while centralized telemetry is introduced. The change SHALL require no frontend modification.

#### Scenario: OpenObserve or collector is unavailable

- **WHEN** centralized observability is unavailable
- **THEN** LogStore-backed operations, local gateway logs, `/api/logs/*`, call control, resume, and media behavior continue independently

#### Scenario: Existing client uses the gateway

- **WHEN** a current mobile, web, agent, SIP, or admin API client uses the updated gateway
- **THEN** it observes the existing contract and is not required to send or consume telemetry fields

#### Scenario: Operator deploys the provided collector example

- **WHEN** an operator configures the reference collector with an existing OpenObserve endpoint and secrets supplied outside source control
- **THEN** the collector can receive gateway telemetry and export it to OpenObserve without placing OpenObserve credentials in the gateway configuration or frontend
