# Gateway Collector reference

These examples send gateway logs, metrics, and traces to an **existing**
OpenObserve deployment. The gateway talks only to this Collector over OTLP; it
never receives OpenObserve credentials.

## Choose exactly one log source

Use one configuration per deployment:

| Deployment | Configuration | Only log source | Required read-only mount |
|---|---|---|---|
| Kubernetes | `otel-collector-kubernetes.yaml` | container stdout under `/var/log/pods` | host `/var/log/pods` to the same path, read-only |
| Docker or host process | `otel-collector-host-file.yaml` | gateway-managed log files | gateway log directory at `/var/log/webrtc-sip-gateway`, read-only |

Do not combine the two log pipelines and do not add the OTLP receiver to the
logs pipeline while the gateway also writes the same record to stdout/file.
Doing so duplicates records. Neither example mounts `/var/run/docker.sock`. Gateway observability libraries
are the OpenTelemetry Go API/SDK and OTLP exporters (Apache-2.0); OpenObserve
credentials stay in Collector runtime secrets, never in `apps/gateway`.
Each variant adds `log.source=kubernetes_stdout` or `log.source=host_file`, which
can be used to verify that only one source exists:

```sql
SELECT log_source, COUNT(*)
FROM gateway_logs_dev
WHERE service_name = 'webrtc-sip-gateway'
  AND _timestamp >= NOW() - INTERVAL '5 minutes'
GROUP BY log_source;
```

The result must contain exactly one source row. During a one-instance smoke
test, compare a unique startup event count with the local source; both should be
one. Stop rollout if both source values appear or the count is doubled.

## Configure and validate

1. Copy `.env.example` to a runtime-only environment file outside source
   control. Set the existing OpenObserve OTLP endpoint, organization and three
   stream names. Inject `OPENOBSERVE_AUTHORIZATION` from a secret manager.
2. Keep `OPENOBSERVE_TLS_INSECURE=false`. The endpoint must be HTTPS and trusted
   by system roots. If a private CA is required, mount its PEM read-only and set
   `OPENOBSERVE_TLS_CA_FILE`; do not disable verification in production.
3. Create the persistent storage directory and grant it only to the Collector.
   This directory buffers telemetry when OpenObserve is unavailable and must
   have a disk quota/monitor. For Kubernetes use a PVC or bounded hostPath.
4. Choose one config and validate it with the same pinned contrib image/version
   used in deployment:

   ```powershell
   $env:OPENOBSERVE_AUTHORIZATION = 'Basic_REPLACE_FROM_SECRET_STORE'
   docker run --rm --env-file deploy/observability/.env.example `
     -v "${PWD}/deploy/observability:/conf:ro" `
     otel/opentelemetry-collector-contrib:0.135.0 `
     validate --config /conf/otel-collector-host-file.yaml
   ```

5. Run `pwsh deploy/observability/verify.ps1`. It validates the catalog policy,
   exclusive log-source wiring, environment placeholders, sensitive defaults,
   and (when Docker is available) both Collector configurations.

Environment substitution keeps secrets out of YAML. `OPENOBSERVE_OTLP_ENDPOINT`
is the full OTLP base, normally `https://<host>/api/<organization>`. Confirm the
exact endpoint and headers from the ingestion page of the installed OpenObserve
version. The examples send `Authorization`, `organization`, and `stream-name`
headers. Never expose these variables in gateway or frontend configuration.

## OpenObserve streams, access, and retention

Use separate streams for logs, metrics and traces and separate them further by
environment. Suggested names are `gateway-logs-<env>`,
`gateway-metrics-<env>`, and `gateway-traces-<env>`. A safe starting retention is
3–7 days for development, 7–14 days for staging, and 14–30 days for production;
shorten high-volume legacy-log retention first. Extend retention only after
volume, privacy and incident-response review. PostgreSQL LogStore remains the
authoritative long-lived call evidence store.

Create a least-privilege ingestion service account per environment. It should
write only the selected streams and have no query/admin permission. Give
operators read access only to their environment; reserve stream lifecycle,
retention and service-account management for observability administrators.
Enable OpenObserve audit logs and rotate credentials through the secret store.

## Enable and verify

1. Deploy the Collector with health port `13133`, self-metrics port `8888`, OTLP
   gRPC `4317`, and OTLP HTTP `4318`. Do not expose those ports publicly.
2. Confirm `http://<collector>:13133/` is healthy and scrape Collector
   self-metrics from port 8888.
3. Start one non-production gateway with its documented `OTEL_*` settings and
   `OTEL_ENABLE=true`. The endpoint points to the Collector, never OpenObserve.
4. Generate one HTTP/WS call-control flow. Query all three OpenObserve streams
   for `service.name=webrtc-sip-gateway`, the expected environment and instance.
5. Confirm log records have exactly one `log.source`, metric attributes follow
   the catalog, trace spans are control-plane only, and seeded test secrets do
   not appear. Confirm local logs and `/api/logs/*` still work.
6. Compare call setup/media behavior and Collector queue metrics before adding
   more gateway instances.

## Monitor and alert

Scrape port 8888 and alert on Collector self-metrics by stable suffix because
the exact prefix can vary between Collector releases:

- refused/failed receiver records greater than zero;
- exporter queue utilization above 70% for 10 minutes or above 90% for 2
  minutes;
- file-storage disk usage above 70% and 85%;
- exporter send failures/retries increasing for 5 minutes;
- no successful export/accepted records from an active gateway for 10 minutes;
- gateway-local telemetry drops or failures increasing;
- Collector health unavailable.

Useful Collector series commonly include
`otelcol_receiver_refused_{log_records,metric_points,spans}`,
`otelcol_exporter_queue_size`, `otelcol_exporter_queue_capacity`,
`otelcol_exporter_send_failed_*`, and `otelcol_exporter_sent_*`. Inspect the
deployed version's `/metrics` before encoding names in alert rules.

## Diagnose a failure

1. Check gateway cached telemetry health; it must not probe the Collector.
2. Check Collector health and self-metrics, then local Collector logs. Never
   route Collector SDK/exporter diagnostics back into its gateway log pipeline.
3. Inspect queue utilization, send failures and persistent-storage free space.
4. Verify DNS/TLS from Collector to OpenObserve, stream permissions, endpoint,
   organization and injected authorization. Do not print the authorization
   value.
5. A rejected record can indicate a stream/schema/permission problem; a growing
   queue with retries normally indicates destination/network failure.
6. Keep calls running. Telemetry loss or queue saturation must be handled as a
   bounded observability failure, not a reason to restart call/media workers.

## Disable and roll back

Set `OTEL_ENABLE=false` on gateway instances and restart them one at a time.
Confirm local logs, `/api/logs/*`, PostgreSQL session APIs, WS/SIP control and
media remain healthy. Then stop the Collector only after its bounded drain
window. Preserve the file-storage volume until the rollback decision is final;
deleting it discards queued telemetry. Re-enable by restoring the last validated
config and credential, starting the Collector, then enabling one gateway first.

Collector rollback is independent of gateway and database schemas. OpenObserve
unavailability never changes LogStore, session directory, gateway registry,
trunk coordination, SIP/SDP contracts, Opus/H.264 behavior or media forwarding.

