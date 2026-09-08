## Context

See `proposal.md` for motivation. The gateway currently has two independent observability surfaces: mixed-format process output mirrored synchronously to console and per-start local files, and a PostgreSQL LogStore that persists typed call/session evidence and participates in multi-instance coordination. The local `/api/logs/*` API tails only files on the serving instance. Event and stats persistence already follows the correct real-time invariant by using bounded, non-blocking queues and drop counters.

The service contains many legacy `fmt.Print*` and `log.Print*` sites, including high-frequency media diagnostics, so an all-at-once logging rewrite would be high risk. Gateway startup currently initializes logging before loading runtime configuration. The application is deployed in both split and unified containers and must remain usable when database logging or external telemetry is disabled.

OpenObserve already exists outside this repository. The gateway will integrate through an operator-managed OpenTelemetry Collector, not through OpenObserve-specific application code. OpenObserve credentials and retention remain deployment concerns owned by the collector/backend.

## Goals / Non-Goals

**Goals:**

- Establish an optional, vendor-neutral telemetry boundary between the gateway and a nearby collector.
- Keep telemetry production non-blocking and bounded under collector failure, overload, startup, and shutdown.
- Centralize existing logs quickly while enabling incremental structured logging.
- Add low-cardinality operational metrics and sampled control-plane traces without instrumenting individual media packets.
- Make telemetry degradation observable locally without probing the backend from health request paths.
- Redact classified secrets and private content before they enter local or centralized structured logs.
- Supply a deployable collector example for forwarding to an existing OpenObserve service.

**Non-Goals:**

- Deploying, upgrading, backing up, or administering OpenObserve.
- Changing the frontend or embedding the OpenObserve UI.
- Removing or changing the `/api/logs/*` response contract in this change.
- Replacing PostgreSQL LogStore, changing its schema/retention, or dual-writing every domain record.
- Adding per-packet logs, metrics with per-session labels, or RTP/RTCP spans.
- Propagating trace context through existing WebSocket messages or SIP headers, which would change externally visible contracts.
- Converting every legacy logging call site in one release.

## Decisions

### 1. Use the collector as the only OpenObserve-specific boundary

The gateway exports OTLP to a configured collector and contains no OpenObserve URL shape, organization, stream, or authorization logic. The collector owns the OpenObserve exporter headers, batching, retry, persistent queue, TLS, and backend routing.

This keeps OpenObserve credentials out of the gateway and makes the application portable to another OTLP backend. Direct OpenObserve HTTP ingestion was rejected because it couples hot application code to backend availability and authentication. An OpenObserve-specific Go client was rejected for the same reason.

### 2. Use a hybrid signal path during migration

Metrics and traces are emitted from the gateway through the OpenTelemetry Go SDK and OTLP exporter to the collector. Runtime logs initially continue to reach console/local files; the collector ingests that output using the environment-appropriate receiver, while selected high-signal call/control events are incrementally emitted in structured form.

For Kubernetes, the collector or node agent consumes container stdout. For Docker/host deployments, the reference design supports either a read-only shared gateway log volume with the collector `filelog` receiver or host-level container log collection. The application does not mount the Docker socket. The chosen deployment variant must avoid collecting both stdout and the mirrored file simultaneously, which would duplicate every record.

Replacing all existing logging with an OTLP log exporter immediately was rejected because of the number of legacy sites, mixed stdout writers, and the risk of recursive exporter diagnostics. Keeping only opaque legacy text forever was rejected because it prevents reliable field queries and alerts.

### 3. Keep telemetry disabled by default and use an explicit configuration boundary

Add an `ObservabilityConfig` loaded before telemetry initialization. An explicit gateway flag such as `OTEL_ENABLE=false` controls initialization; standard OpenTelemetry variables configure endpoint, protocol, service name, resource attributes, sampling, batching, and timeouts where their semantics fit. Project-specific settings are limited to behavior not covered safely by standard variables, including bounded local health accounting and shutdown budget.

The initial supported exporter protocols are OTLP/HTTP protobuf and OTLP/gRPC. The endpoint is the collector, normally on the same host, pod, or trusted network. Configuration validation rejects an enabled exporter with a missing/invalid endpoint, unsupported protocol, negative/zero resource limits, malformed sampling ratio, or unsafe timeout. Public configuration and startup diagnostics expose only safe effective fields; exporter headers are never returned or logged.

Telemetry setup occurs after local logging is available but before API/SIP listeners accept traffic. A disabled configuration installs no-op providers. Shutdown runs after listeners and call work begin draining, uses a dedicated deadline, and never prevents process exit after the deadline.

### 4. Isolate producers from export with bounded SDK processors and local accounting

Application instrumentation records to OpenTelemetry APIs only. Batch processors/exporters own network activity outside producer goroutines. Queue size, export batch size, schedule delay, export timeout, retry limits, and shutdown timeout are finite and validated. The implementation must verify that the selected SDK processor's queue-full behavior does not block producers; if the SDK cannot provide that guarantee for a signal, a small bounded non-blocking adapter records and drops before the SDK boundary.

Local atomics/cached state track accepted, dropped, failed, and successfully exported batches plus last success/error timestamps. Error messages are normalized to bounded reason codes for health and metrics, while a throttled local diagnostic may include a redacted error. The telemetry health provider never calls the collector.

Collector persistent buffering is recommended and configured in the example, but it does not replace the gateway-side bound: an unreachable local collector must still be harmless.

### 5. Introduce a central event schema and redaction boundary

Selected structured events use stable fields:

- Resource: `service.name`, `service.version`, `deployment.environment`, `service.instance.id`.
- Record: timestamp, severity, `component`, `event.name`, and bounded `outcome`/`reason` values.
- Correlation when safe: `session.id`, a reviewed call correlation value, trace/span IDs, and numeric trunk ID.
- Measurements: typed integer/double duration, count, byte, queue, and media-quality fields.

The logger accepts typed reviewed fields instead of arbitrary maps for centralized high-signal events. A central sanitizer applies case-insensitive deny rules to credential/header keys and handles structured values before serialization. Full SIP, SDP, message/transcript bodies, push tokens, passwords, JWTs, private keys, DSNs, TURN credentials, and raw arbitrary payloads are excluded. SIP identities and IP/device identifiers are masked, omitted, or converted to a keyed/one-way correlation value according to policy. Length, type, status, codec, candidate type, and similar bounded metadata can be retained.

Redaction at the collector remains a second layer for legacy text. High-risk legacy dump sites are audited and either kept behind existing production-off debug flags, sanitized, or excluded from the collector stream. `DB_LOG_FULL_SIP`, debug SIP dumps, and TURN address logging remain off in production examples.

### 6. Define a low-cardinality metric catalog

Start with a reviewed catalog rather than automatically translating log fields into labels:

- Gateway: process start/shutdown, uptime, telemetry state.
- Calls: active, started, completed, setup duration, duration, normalized outcome and direction.
- WebSocket: active connections, connect/disconnect, selected message outcomes, dropped outbound messages.
- SIP: transaction count/latency grouped by method and bounded status class/code policy.
- Persistence: event/stats queue utilization, drops, batch failures and latency.
- Translator/push/external dependencies: request count, latency and normalized outcome.
- Media health: periodic aggregate packet-loss/jitter/RTT samples and PLI/FIR/NACK/keyframe/recovery counters already maintained by sessions.

Allowed attributes are enumerated and tested. Session IDs, Call-IDs, numbers, usernames, IPs, device IDs, payload text, raw errors, URLs with query strings, and arbitrary reason strings are forbidden as metric attributes. Media instruments update existing counters/state or periodic snapshots rather than creating an observation per packet where avoidable.

### 7. Trace only control-plane boundaries with parent-based ratio sampling

HTTP middleware creates spans for bounded API operations while excluding or heavily sampling noisy health/log-tail endpoints. Selected WebSocket message handlers, SIP transactions, call lifecycle stages, resume, push, database operations, and translator calls create child spans where context is already available. When protocols do not carry trace context, the gateway starts a local trace and correlates using safe session fields; it does not add fields to WebSocket messages or headers to SIP traffic.

Default production sampling is parent-based ratio sampling with a conservative configurable ratio. Errors may be represented in metrics and structured logs even if a trace was not sampled. Span names and attributes are bounded. No RTP/RTCP packet, codec-loop iteration, jitter-buffer item, or keyframe NAL receives an individual span.

### 8. Extend existing operational health additively

Register a telemetry health provider alongside existing components. It reports a cached state from local exporter observations: disabled, connected, degraded, unavailable, or unknown; queue occupancy/capacity; dropped records; export failures; and freshness timestamps. No OpenObserve query or collector health request occurs while serving the gateway health API.

The existing compatibility fields retain their meanings. New telemetry fields are additive and safe for current consumers. This provides a break-glass diagnostic when centralized observability itself is failing.

### 9. Preserve LogStore and local log fallback

PostgreSQL remains authoritative for session history, typed evidence, payload/dialog/stat retrieval, session directory, gateway registry, and trunk coordination. OpenObserve is an operational search/alert copy, not transactional state. There is no requirement that every LogStore row appear in OpenObserve, and telemetry loss never rolls back a call or database operation.

`/api/logs/*` and current local log creation remain available through the rollout. Their content may progressively include structured lines, but endpoint paths and JSON response shapes remain unchanged. Removal or replacement of the file viewer requires a later proposal and is intentionally outside the frontend-free scope.

### 10. Provide deployment examples without secrets

Add a collector example under the deployment documentation with:

- OTLP HTTP/gRPC receivers for gateway metrics and traces.
- One explicitly selected legacy log receiver appropriate to the example environment.
- memory limiter, resource/attribute enrichment, sensitive-field filtering, batching, retry, and a persistent sending queue.
- OTLP export to the existing OpenObserve endpoint using environment-substituted organization, stream and authorization settings.
- collector self-telemetry and guidance for alerting on refusal, queue growth and export failure.

Example secret values are placeholders only and documentation requires runtime secret injection. OpenObserve single-node/HA lifecycle and credentials are not added to gateway Compose files unless an operator explicitly chooses to wire the example into their deployment.

## Risks / Trade-offs

- [Legacy text remains inconsistently structured] -> Parse only reliable common fields in the collector, migrate high-signal events first, and retain raw message text for bounded transition periods.
- [Telemetry SDK or exporter allocates in hot paths] -> Benchmark representative call/media load, avoid packet-level instruments, use periodic aggregation, and require no material regression before rollout.
- [Queue saturation loses telemetry] -> Prefer loss over call disruption, expose drop counters locally and centrally, use collector disk buffering, and alert before sustained saturation.
- [File and stdout receivers duplicate logs] -> Document one receiver per deployment, add a source identifier, and verify record counts during rollout.
- [Centralized logs increase privacy impact] -> Redact at source, add collector defense-in-depth filters, separate service accounts/environments, apply RBAC and short stream-specific retention.
- [Trace cardinality or cost grows unexpectedly] -> Use parent-based sampling, bounded span names/attributes, dashboards for ingestion volume, and a kill switch that leaves metrics/logging operational.
- [Collector failure produces recursive error logs] -> Route SDK diagnostics through a throttled local-only sink that is not re-exported.
- [Startup validation prevents a deploy when telemetry is misconfigured] -> Telemetry remains disabled by default; enabled-but-invalid is fail-fast so operators do not assume observability is active when it is not.
- [Graceful shutdown cannot flush all records] -> Apply a hard deadline, report the local failure, and allow process exit.

## Migration Plan

1. Inventory and classify current gateway logs, especially full SIP/SDP, auth, TURN/ICE, push and translation output; define the approved field and redaction catalog.
2. Add validated disabled-by-default observability configuration, no-op providers, lifecycle hooks, and additive telemetry health.
3. Deploy a collector against one non-production gateway using exactly one legacy log source and export to a dedicated OpenObserve stream with short retention.
4. Add baseline low-cardinality metrics and selected structured gateway lifecycle/call-control events.
5. Add sampled HTTP/WS/SIP/call/dependency traces, leaving media forwarding untraced at packet level.
6. Run unit, race, failure-injection and representative call-load tests with the collector healthy, slow, unavailable, recovering and disk-constrained. Compare call setup, RTP loss/jitter, CPU, allocations and memory with telemetry disabled and enabled.
7. Roll out by environment and gateway instance with conservative sampling, bounded queues and alerts for drops/export failure. Keep `/api/logs/*` and PostgreSQL LogStore unchanged.
8. Incrementally convert additional high-value legacy logs only after field/cardinality/privacy review.

Rollback is configuration-first: disable telemetry export and restart the gateway, leaving existing local logging, LogStore and contracts intact. Collector configuration can be rolled back independently. Code rollback requires no database or protocol migration.

## Open Questions

- Exact queue, batch, retry, timeout and sampling defaults should be finalized from staging volume and load-test measurements; the accepted ranges and non-blocking behavior are fixed by the specification.
- The production deployment can choose OTLP/HTTP or OTLP/gRPC based on its proxy/network path; both preserve the same gateway behavior.
- Stream names and retention periods are owned by the existing OpenObserve installation and will be supplied to the collector at deployment time.
