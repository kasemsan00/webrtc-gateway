## 1. Telemetry Contract and Sensitive-Data Baseline

- [x] 1.1 Inventory production gateway `fmt.Print*`, `log.Print*`, SIP dump, SDP, ICE/TURN, push, auth, translator, and payload logging sites; commit a reviewed source/field classification table and verify every raw credential/body/dump source has an explicit allow, redact, debug-only, or exclude disposition.
- [x] 1.2 Define the stable resource fields, structured event fields, event-name vocabulary, normalized outcomes/reasons, metric instruments, permitted metric attributes, and span attributes in gateway documentation; verify automated tests can load the catalog and reject duplicate or unbounded names.
- [x] 1.3 Define source-level sanitizers for case-insensitive secret/header keys, SIP identities, addresses, tokens, DSNs, raw SIP/SDP, message/transcript bodies, push identifiers, arbitrary maps, and oversized values; verify table-driven tests contain representative secret variants and prove raw fixtures never appear in output.

## 2. Configuration and Telemetry Runtime

- [x] 2.1 Add the required OpenTelemetry Go API/SDK and OTLP HTTP/gRPC dependencies with versions compatible with the gateway Go toolchain; verify `go mod tidy`, `go build ./...`, and the dependency/license review succeed from `apps/gateway`.
- [x] 2.2 Add a dedicated gateway observability configuration section with disabled-by-default enablement, collector endpoint/protocol, service/environment/resource identity, bounded queue/batch/delay/export/shutdown settings, and trace sampling; verify config tests cover defaults, environment overrides, safe ranges, both protocols, and fail-fast invalid enabled configurations.
- [x] 2.3 Extend the safe public configuration view and startup summary with non-secret observability facts while excluding OTLP headers and credentials; verify public-view and startup-log tests cannot find supplied secret/header fixture values.
- [x] 2.4 Implement a focused telemetry package that installs no-op providers when disabled and bounded meter/tracer providers plus OTLP exporters when enabled; verify unit tests exercise disabled, HTTP, gRPC, initialization failure, and repeated cleanup paths without network calls from the disabled path.
- [x] 2.5 Integrate telemetry initialization after local logging/config validation and before listeners accept traffic, and integrate deadline-bounded cleanup into gateway shutdown; verify lifecycle tests prove initialization order and that a stalled exporter cannot hold shutdown beyond its configured budget.
- [x] 2.6 Implement local-only, throttled SDK/exporter diagnostics that cannot recursively enter the export pipeline; verify repeated exporter failures produce bounded local messages and no recursive telemetry records.

## 3. Non-Blocking Export and Local Health

- [x] 3.1 Implement bounded telemetry acceptance and accounting for accepted, dropped, failed, and successful exports plus last-success/last-failure timestamps and normalized reason codes; verify queue-full concurrency tests demonstrate producer calls return without waiting on exporter network I/O.
- [x] 3.2 Validate the chosen OpenTelemetry processors' queue-full and retry behavior, adding a bounded non-blocking adapter wherever the SDK cannot guarantee it; verify a deliberately blocked fake exporter causes drops rather than blocked SIP/WS/media producer goroutines.
- [x] 3.3 Add a telemetry operational-health provider with cached `disabled`, `connected`, `degraded`, `unavailable`, and `unknown` states, queue utilization/capacity, drop/failure counters, and freshness timestamps; verify health-handler tests make no collector request and expose no endpoint headers, secrets, payloads, or unbounded errors.
- [x] 3.4 Register telemetry health additively without changing existing database, translator, push, listener, or dashboard compatibility meanings; verify existing operational-health tests pass unchanged and focused tests cover telemetry transitions and recovery.

## 4. Centralized Logging and Redaction

- [x] 4.1 Add a structured high-signal logging API that emits stable timestamp, severity, component, event name, resource identity, safe correlation, and typed measurements while retaining the current local/stdout writer; verify golden tests cover text fallback and structured records consumable by the collector.
- [x] 4.2 Apply the central sanitizer before structured serialization and before any newly touched sensitive legacy output; verify tests for Authorization/Proxy-Authorization, JWTs, SIP passwords, TURN credentials, DSNs, push tokens, raw SIP, SDP, IPs, message bodies, and transcripts contain no raw fixture values.
- [x] 4.3 Instrument gateway startup/shutdown and selected HTTP, WebSocket connection/message, call lifecycle, SIP transaction, resume, persistence queue, push, and translator outcomes with stable structured events; verify focused package tests assert event names, safe session correlation, normalized outcomes, and absence of protocol/payload content.
- [x] 4.4 Audit verbose SIP INVITE/MESSAGE, full-SIP payload, TURN/ICE, SDP, and authentication diagnostics so production defaults cannot centralize raw secrets or bodies; verify default-config tests keep all sensitive dump toggles off and explicit debug-mode tests document the residual exposure.
- [x] 4.5 Preserve `GET /api/logs`, `GET /api/logs/current`, and `GET /api/logs/{name}` paths and response schemas while structured lines are introduced; verify existing log API and frontend service contract tests pass without frontend changes.

## 5. Bounded Metrics

- [x] 5.1 Implement the documented gateway and telemetry lifecycle instruments, including uptime/export health/drop/failure observations; verify metric-reader tests assert names, units, monotonicity, and only approved bounded attributes.
- [x] 5.2 Instrument active/started/completed calls, normalized outcomes, setup duration, and call duration at authoritative session state transitions; verify inbound/outbound success, rejection, failure, timeout, and duplicate terminal-action tests produce exactly-once counter/gauge updates.
- [x] 5.3 Instrument WebSocket connections, selected control-message outcomes, and dropped outbound messages without using client/session identifiers as attributes; verify connect/disconnect/reconnect and full-send-buffer tests preserve current behavior and bounded labels.
- [x] 5.4 Instrument SIP transaction count/latency and bounded method/status classification without logging raw requests or identities; verify representative INVITE, REGISTER, MESSAGE, BYE, timeout, and error tests emit only catalogued attributes.
- [x] 5.5 Export LogStore event/stats queue utilization, cumulative drops, batch failures, and batch latency from existing queue health/accounting; verify full-queue and failed-batch tests update metrics without changing drop-newest or batching semantics.
- [x] 5.6 Instrument translator, push, and other configured outbound dependencies with request count, latency, and normalized outcome; verify fake dependency success/timeout/error tests contain no URL query, token, body, transcript, device, or raw error attributes.
- [x] 5.7 Export periodic aggregate media-health and recovery observations from existing session snapshots/counters, never per packet; verify media tests cover loss/jitter/RTT and PLI/FIR/NACK/keyframe/recovery values and assert zero spans or metric records are created per RTP/RTCP packet.
- [x] 5.8 Add an automated metric-cardinality policy test that rejects session IDs, SIP Call-IDs, numbers, usernames, IPs, device IDs, URLs, raw errors, and arbitrary strings as attributes; verify it fails against deliberate forbidden fixtures and passes the full metric catalog.

## 6. Sampled Control-Plane Tracing

- [x] 6.1 Configure parent-based ratio sampling, bounded span processors, and independent trace disablement using validated settings; verify sampler tests cover zero, partial, and full test ratios while production defaults remain conservative.
- [x] 6.2 Add HTTP server spans with bounded route-template names and exclude or heavily sample noisy health and log-tail operations; verify handler tests do not include raw URLs, query strings, authorization values, request bodies, or response bodies.
- [x] 6.3 Add selected WebSocket control and call-lifecycle spans using local safe session correlation without changing WS message types/fields; verify existing WebSocket contract tests pass and sampled/unsampled cases export the expected number of spans.
- [x] 6.4 Add SIP transaction and call setup/resume spans without injecting trace headers into SIP or modifying SDP; verify SIP interoperability and resume tests pass byte/behavior compatibility checks and span fixtures contain no raw SIP/SDP or identities.
- [x] 6.5 Add child spans for bounded persistence, translator, push, and other external dependency operations where context is available; verify errors use normalized status and redacted events rather than raw payloads or credential-bearing messages.
- [x] 6.6 Add a guard test/benchmark around RTP/RTCP forwarding and media recovery loops that fails if packet-level spans or high-cardinality metric attributes are introduced; verify telemetry-enabled packet processing has no material latency/allocation regression against the recorded disabled baseline.

## 7. OpenTelemetry Collector and OpenObserve Deployment Assets

- [x] 7.1 Add a reference Collector configuration with OTLP HTTP/gRPC receivers, memory limiter, resource enrichment, redaction defense, batch processing, file-backed persistent queue, and OTLP export to an environment-supplied existing OpenObserve endpoint; verify `otelcol-contrib validate --config <reference-config>` succeeds with non-secret test placeholders.
- [x] 7.2 Provide separate documented log-source examples for Kubernetes stdout and Docker/host read-only gateway file collection, selecting exactly one source per deployment and never mounting the Docker socket; verify the example record count has no stdout/file duplication.
- [x] 7.3 Add example environment variables and secret placeholders for OpenObserve organization, stream, authorization, TLS, and gateway collector endpoint without committing credentials; verify repository secret scanning and a targeted search find only placeholders.
- [x] 7.4 Document OpenObserve stream suggestions, short environment-specific retention, RBAC/service-account separation, collector self-telemetry, and alerts for refused records, queue growth, drops, stale export, and export failure; verify the operations guide contains a complete enable, verify, failure-diagnose, disable, and rollback procedure.
- [x] 7.5 Update gateway configuration reference and per-app environment example for every supported safe gateway setting and production-off sensitive debug defaults; verify config documentation tests or an environment-key comparison report no undocumented observability setting.

## 8. Failure, Compatibility, and Load Verification

- [x] 8.1 Add fake-collector integration tests for healthy ingest, slow responses, refusal, disconnect, recovery, queue saturation, and shutdown timeout; verify active call/control test flows complete and exporter health/drop counters match each fault.
- [x] 8.2 Run focused gateway package tests for config, logger/telemetry, API health/logs, WebSocket, SIP, session/media, LogStore, push, and translator instrumentation; verify all focused commands pass without updating frontend code or generated route files.
- [x] 8.3 Run `go test ./...`, `go test -race ./...`, and `go build -o webrtc-sip-gateway .` from `apps/gateway`; verify all commands pass and no new data race or panic appears.
- [x] 8.4 Run representative concurrent call/media benchmarks with telemetry disabled, collector healthy, collector unavailable, and queues saturated; verify agreed call setup, RTP loss/jitter, CPU, allocation, memory, and goroutine thresholds show no material media regression or unbounded growth.
- [x] 8.5 Execute an end-to-end staging smoke test from gateway through Collector to the existing OpenObserve deployment; verify logs, metrics, and sampled traces carry service/environment/instance correlation, contain no seeded secrets, and dashboards can detect an induced exporter/call failure.
- [x] 8.6 Disable telemetry and remove/stop the Collector during staging rollback; verify gateway local logs, `/api/logs/*`, PostgreSQL LogStore/session APIs, WebSocket/SIP call control, resume, Opus, H.264, SPS/PPS/keyframe behavior, and RTP/RTCP forwarding remain operational.
