## Why

Gateway runtime logs are local, unstructured, restart-file-scoped, and difficult to correlate across instances, while synchronous observability work in this real-time media service could disrupt calls. The gateway needs a vendor-neutral, bounded telemetry path to the operator's existing OpenObserve deployment through an OpenTelemetry Collector without changing frontend, WebSocket, SIP, SDP, media, or PostgreSQL LogStore behavior.

## What Changes

- Add optional gateway-side OpenTelemetry initialization and lifecycle management for exporting telemetry to a separately operated OpenTelemetry Collector over OTLP.
- Add structured, redacted gateway log records for selected high-signal control-plane and lifecycle events while preserving legacy local/stdout logging during migration.
- Add bounded operational metrics for gateway, call, WebSocket, SIP, persistence-queue, translator, and aggregate media-health behavior.
- Add sampled traces for HTTP, WebSocket control, SIP signaling, call lifecycle, and external dependency operations; explicitly exclude per-RTP/RTCP-packet spans.
- Add environment-driven service identity, OTLP transport, timeout, queue, retry, sampling, and resource metadata configuration with telemetry disabled by default.
- Define failure isolation so collector or OpenObserve latency/unavailability cannot block SIP, WebSocket, RTP/RTCP, media, or shutdown hot paths; expose local exporter health and drop/failure counters.
- Add source-level sensitive-data classification and redaction requirements, with collector-side filtering treated as defense in depth.
- Add a reference OpenTelemetry Collector configuration that exports to an already provisioned OpenObserve instance, without embedding OpenObserve credentials in the gateway or repository.
- Preserve PostgreSQL LogStore as the source of truth for sessions, events, payloads, dialogs, stats, session directory, instance registry, and trunk coordination.
- Preserve the existing `/api/logs/*` endpoints and gateway log files as a bounded break-glass fallback for this change; no frontend work is included.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `gateway-operations-observability`: Extend gateway operations observability with safe OTLP export, structured/redacted telemetry, bounded metrics, sampled control-plane traces, exporter health, and OpenObserve Collector integration while preserving existing LogStore and protocol behavior.

## Impact

- Affected gateway areas include startup/shutdown, configuration and public config views, centralized logging, HTTP/WS/SIP control-plane instrumentation, session lifecycle, LogStore queue health, translator calls, aggregate media statistics, deployment examples, environment documentation, and focused tests.
- New Go dependencies may include the OpenTelemetry API/SDK, OTLP exporters, and instrumentation helpers, but no alternative WebRTC or SIP stack.
- Deployment gains an optional OpenTelemetry Collector configuration and secret placeholders for the existing OpenObserve endpoint; OpenObserve itself is not deployed or managed by this repository.
- Existing REST and WebSocket contracts, `/api/logs/*`, frontend behavior, PostgreSQL schemas and retention, SIP signaling semantics, SDP, Opus/H.264 handling, and RTP/RTCP forwarding remain compatible.
