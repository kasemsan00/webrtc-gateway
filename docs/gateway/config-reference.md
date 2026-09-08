# Gateway Configuration Reference

From `internal/config/config.go`.

---

## Core SIP/API/RTP

- `SIP_LOCAL_IP` (default `0.0.0.0`)
- `SIP_PUBLIC_IP` (optional, important behind NAT)
- `SIP_PORT` (default `5060`)
- `SIP_LOCAL_PORT` (default `5060`)
- `SIP_LISTEN_TCP` (default `true`)
- `SIP_LISTEN_UDP` (default `false`)
- `API_PORT` (default `8080`)
- `API_ENABLE_WS` (default `true`)
- `API_ENABLE_PUBLIC_WS` (default `false`; enables unauthenticated `/ws-public` for public SIP per-call credentials only)
- `API_ENABLE_AGENT_WS` (default `false`; enables unauthenticated `/ws-agent` for PC agent SIP REGISTER presence with client-provided credentials)
- `API_ENABLE_REST` (default `true`)
- `API_CORS_ORIGINS` (default `*`; for single-domain deploy use e.g. `https://gateway.example.com` — see [`deploy/README.md`](../../deploy/README.md))
- `SIPCLIENT_AUTH_REGISTER_URL` (optional; when set, user-realm WebSocket auth auto-provisions a mobile SIP trunk)
- `SIPCLIENT_AUTH_TIMEOUT_MS` (default `5000`)
- `RTP_PORT_MIN` (default `10500`)
- `RTP_PORT_MAX` (default `10600`)
- `RTP_BUFFER_SIZE` (default `16384`)

## JWT auth (Keycloak/JWKS)

- `AUTH_ENABLE` (default `false`)
- User realm: `AUTH_TTRS_USERS_JWKS_URL`, `AUTH_TTRS_USERS_JWT_ISSUER`,
  `AUTH_TTRS_USERS_JWT_AUDIENCE`
- Employee realm: `AUTH_TTRS_EMPLOYEE_JWKS_URL`,
  `AUTH_TTRS_EMPLOYEE_JWT_ISSUER`, `AUTH_TTRS_EMPLOYEE_JWT_AUDIENCE`
- `AUTH_JWKS_TIMEOUT_MS` (default `5000`)
- `FRONTEND_PASSWORD` (optional on gateway-only processes; required for the admin UI)

When `AUTH_ENABLE=true`:

- Startup is fail-fast unless at least one realm has a JWKS URL. Issuer and
  audience are validated when configured.
- Startup is fail-fast if initial JWKS prefetch fails.
- Protected `/api/*` routes require `Authorization: Bearer <jwt>` or a bearer
  matching `FRONTEND_PASSWORD` when that env is set. All read-only
  `/api/logs*` routes and the four GET `/api/client-diagnostics*` query routes
  are intentionally public.
- `/ws` requires `?access_token=<jwt>`. The admin password is not accepted on `/ws`.

When `FRONTEND_PASSWORD` is set and `AUTH_ENABLE=false`, authenticated `/api/*` routes still require the matching bearer. Set the same `FRONTEND_PASSWORD` on the frontend process. Do not use a `VITE_` prefix; the frontend server reads it from process env and never injects it into the browser bundle.

## Debug and media behavior toggles

- `DEBUG_WEBSOCKET` (default `false`)
- `DEBUG_TURN` (default `false`)
- `DEBUG_SIP_MESSAGE` (default `false`)
- `DEBUG_SIP_INVITE` (default `false`)
- `SWITCH_PLI_DELAY_MS` (default `0`; delayed `@switch` FIR/PLI retries abort if a complete IDR already arrived)
- `SIP_AUDIO_USE_AVPF` (default `false`)
- `SIP_AUDIO_INBOUND_GAIN_ENABLE` (default `false`; requires CGO + libopus in Docker build)
- `SIP_AUDIO_INBOUND_GAIN` (default `1.0`; linear multiplier, clamped to max)
- `SIP_AUDIO_INBOUND_GAIN_MAX` (default `3.0`)
- `SIP_VIDEO_USE_AVPF` (default `true`)
- `SIP_VIDEO_FEEDBACK_TRANSPORT` (default `dual`; `auto|rtp|rtcp|dual`). `dual` always sends PLI/FIR/NACK to both the SIP video RTP port and RTCP (RTP first), which Asterisk `chan_sip` needs. `auto` now keeps the RTP target for the whole call instead of dropping it after the 4s fallback window. AVPF offers advertise `ccm fir`, `nack`, and `nack pli`; chan_sip peers that answer `RTP/AVP` ignore the extra `a=rtcp-fb` lines.
- `SIP_VIDEO_PRESERVE_STAPA` (default `false`)
- `SIP_VIDEO_AU_NORMALIZE_ENABLE` (default `true`; validates complete timestamp-grouped H.264 access units, rewrites outbound RTP continuity, injects fresh SPS/PPS before IDR when absent, and keeps a real `@switch` gated until a decoder-safe IDR is written; set `false` only as a bounded rollback to the legacy raw reordered/packet-level transition path)
- `SIP_SWITCH_VIDEO_TRANSITION_MODE` (default `blackout`; `blackout` drops SIP→WebRTC video until the complete-IDR gate releases; `preserve` is rollback that keeps the last rendered frame visible while holding unsafe packets)
- `SIP_SWITCH_VIDEO_BLACKOUT_ENABLED` (default `true`)
- `SIP_SWITCH_VIDEO_BLACKOUT_MS` (default `300`; minimum blackout before gate may release in `blackout` mode)
- `SIP_SWITCH_VIDEO_BLACKOUT_MAX_WAIT_MS` (default `1200`)
- `SIP_SWITCH_VIDEO_RECOVERY_WINDOW_MS` (default `5000`)
- `SIP_SWITCH_VIDEO_RECOVERY_STABLE_MS` (default `750`)
- `SIP_SWITCH_VIDEO_RTP_STABILITY_ENABLED` (default `true`)
- `SIP_SWITCH_VIDEO_RTP_MIN_PACKET_DELTA` (default `30`)
- `SIP_SWITCH_VIDEO_RTP_MAX_GAP_DELTA` (default `12`)
- `SIP_SWITCH_VIDEO_RTP_MAX_MISSING_DELTA` (default `20`)
- `SIP_SWITCH_VIDEO_RTP_MAX_OOO_DELTA` (default `20`)
- `SIP_SWITCH_VIDEO_RTP_MAX_REORDER_DROP_DELTA` (default `0`)
- `SIP_SWITCH_VIDEO_RTP_MAX_REORDER_TIMEOUT_DELTA` (default `5`)
- `SIP_SWITCH_DUPLICATE_DEBOUNCE_ENABLED` (default `true`)
- `SIP_SWITCH_DUPLICATE_DEBOUNCE_MS` (default `60000`)
- `SIP_MIDCALL_RENEGOTIATION_ENABLE` (default `true`; enables SIP mid-call
  re-INVITE/UPDATE handling and is also required by switch-triggered WebRTC
  renegotiation)
- `SIP_SWITCH_VIDEO_RENEGOTIATE_ENABLE` (default `false`; opt in to
  client-assisted WebRTC renegotiation when an authoritative `@switch`
  MESSAGE is accepted. When enabled, the gateway emits WebSocket
  `renegotiate` with `reason=agent_switch` and a new offer so the existing
  client answers in place **before** the first post-switch IDR is forwarded.
  SIP→WebRTC video stays gated with `renegotiate-pending` until the client
  answers (or the attempt fails/times out). Before `CreateOffer`, the gateway
  restores H.264 codec preferences for the SIP packetization mode so the single
  remote-PT lock does not carry into the mid-call offer. The offer does not
  restart ICE. No switch renegotiation is attempted, and no
  `switch_renegotiate_*` log is emitted, while this flag is `false`.
  Configuration changes require a gateway process restart.)
- `SIP_VIDEO_KEYFRAME_WATCHDOG` (default `true`)
- `SIP_VIDEO_KEYFRAME_WATCHDOG_INTERVAL_MS` (default `2000`)
- `SIP_VIDEO_KEYFRAME_STALE_MS` (default `4000`; must stay above a healthy SIP GOP or watchdog PLI never stops)
- `SIP_VIDEO_KEYFRAME_FIR_STALE_MS` (default `8000`)
- `SIP_VIDEO_RECOVERY_BURST_ENABLED` (default `true`)
- `SIP_VIDEO_RECOVERY_BURST_WINDOW_MS` (default `8000`)
- `SIP_VIDEO_RECOVERY_BURST_INTERVAL_MS` (default `1000`)
- `SIP_VIDEO_RECOVERY_BURST_STALE_MS` (default `4000`)
- `SIP_VIDEO_RECOVERY_BURST_FIR_STALE_MS` (default `7000`)
- `SIP_VIDEO_RTP_DISORDER_MONITOR_ENABLED` (default `true`)
- `SIP_VIDEO_RTP_DISORDER_MIN_PACKET_DELTA` (default `300`)
- `SIP_VIDEO_RTP_DISORDER_MAX_GAP_DELTA` (default `45`)
- `SIP_VIDEO_RTP_DISORDER_MAX_MISSING_DELTA` (default `80`)
- `SIP_VIDEO_RTP_DISORDER_MAX_OOO_DELTA` (default `80`)
- `SIP_VIDEO_RTP_DISORDER_MAX_REORDER_TIMEOUT_DELTA` (default `20`)
- `SIP_VIDEO_RTP_DISORDER_CONSECUTIVE_WINDOWS` (default `3`)
- `SIP_VIDEO_RTP_DISORDER_LOG_INTERVAL_MS` (default `5000`)
- `SIP_VIDEO_RTP_DISORDER_CONTAINMENT_ENABLED` (default `false`)
- `SIP_VIDEO_RTP_DISORDER_CONTAINMENT_MS` (default `10000`)

`DEBUG_TURN`, `DEBUG_SIP_MESSAGE`, `DEBUG_SIP_INVITE`, `DB_LOG_FULL_SIP`, and
`PUSH_DEBUG_TTRS_TOKEN` must remain `false` in production. They can expose addresses, identities,
authorization headers, SIP/SDP or message bodies in local logs or LogStore.
Enabling a local diagnostic does not authorize exporting its raw content.

## OpenTelemetry Collector export

Telemetry is disabled by default. When enabled, the gateway exports only to an
OpenTelemetry Collector; OpenObserve endpoint, organization, stream and
authorization settings belong to the Collector deployment, not the gateway or
frontend. See [`deploy/observability/README.md`](../../deploy/observability/README.md).

- `OTEL_ENABLE` (default `false`)
- `OTEL_EXPORTER_OTLP_ENDPOINT` (default `http://localhost:4318`; Collector
  endpoint only, no OpenObserve URL)
- `OTEL_EXPORTER_OTLP_PROTOCOL` (default `http/protobuf`; supported values
  `http/protobuf` and `grpc`)
- `OTEL_EXPORTER_OTLP_HEADERS` (default empty; optional Collector headers,
  treated as secret and redacted from safe config/startup output)
- `OTEL_SERVICE_NAME` (default `webrtc-sip-gateway`)
- `OTEL_DEPLOYMENT_ENVIRONMENT` (default `unspecified`; set explicitly per
  environment)
- `OTEL_RESOURCE_ATTRIBUTES` (default empty; comma-separated standard resource
  attributes, treated as potentially sensitive and redacted from safe config
  output; never place credentials, tokens, identity, URLs with query strings,
  or arbitrary user data here)
- `OTEL_METRIC_EXPORT_INTERVAL_MS` (default `10000`)
- `OTEL_TRACES_ENABLE` (default `true`; independently disables trace creation
  while allowing configured metrics/log collection to continue)
- `OTEL_TRACES_SAMPLER_ARG` (default `0.05`; parent-based trace ratio in
  inclusive range `0`–`1`)
- `OTEL_BSP_MAX_QUEUE_SIZE` (default `2048`; bounded span queue)
- `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` (default `512`; must not exceed queue size)
- `OTEL_BSP_SCHEDULE_DELAY_MS` (default `5000`)
- `OTEL_EXPORTER_OTLP_TIMEOUT_MS` (default `3000`)
- `OTEL_SHUTDOWN_TIMEOUT_MS` (default `5000`; hard cleanup budget)

When `OTEL_ENABLE=false`, no exporter is initialized and the gateway does not
contact a Collector. When enabled, invalid protocol, endpoint, sampling ratio,
timeout, queue, or batch values fail startup before listeners accept traffic.
Both OTLP protocols are supported: use port 4318 for `http/protobuf` and normally
4317 for `grpc`. Network export runs outside call/media producer paths; a full
queue or unavailable Collector drops telemetry and degrades cached telemetry
health instead of interrupting calls.

## TURN

- `TURN_SERVER`, `TURN_USERNAME`, `TURN_PASSWORD`

## Database / LogStore

- `DB_ENABLE` (default `false`)
- `DB_DSN`
- `DB_BOOTSTRAP_ON_START` (default `false`; runs the serialized bootstrap/migration operation before this gateway process starts. Use only for local or single-instance deployment.)
- `DB_BOOTSTRAP_LOCK_TIMEOUT` (default `30s`; maximum time to wait for the database-scoped bootstrap advisory lock)
- `DB_BOOTSTRAP_MIGRATION_TIMEOUT` (default `5m`; maximum duration for Goose migration execution)
- `DB_BOOTSTRAP_BASELINE` (default `./schema/bootstrap-baseline.sql`; normally set only by tests or custom image layouts)
- `DB_BOOTSTRAP_MIGRATIONS_DIR` (default `./migrations`; normally set only by tests or custom image layouts)
- `DB_BOOTSTRAP_GOOSE_BINARY` (default `./goose`; normally set only by tests or custom image layouts)
- `DB_AUTO_MIGRATE` (**deprecated**; when `DB_BOOTSTRAP_ON_START` is unset, `true` maps to startup bootstrap and prints a warning)
- `DB_STATS_INTERVAL_MS` (default `5000`)
- `DB_LOG_FULL_SIP` (default `false`)
- `DB_BATCH_SIZE` (default `100`)
- `DB_BATCH_INTERVAL_MS` (default `1000`)
- `DB_PARTITION_LOOKAHEAD_DAYS` (default `7`)
- `DB_RETENTION_PAYLOADS_DAYS` (default `730`)
- `DB_RETENTION_EVENTS_DAYS` (default `730`)
- `DB_RETENTION_STATS_DAYS` (default `730`)
- `DB_RETENTION_SESSIONS_DAYS` (default `730`)

## SIP public mode

- `SIP_PUBLIC_REGISTER_EXPIRES_SECONDS` (default `3600`)
- `SIP_PUBLIC_REGISTER_TIMEOUT_SECONDS` (default `10`)
- `SIP_PUBLIC_IDLE_TTL_SECONDS` (default `600`)
- `SIP_PUBLIC_CLEANUP_INTERVAL_SECONDS` (default `30`)
- `SIP_PUBLIC_MAX_ACCOUNTS` (default `1000`)

## Push notifications

- `PUSH_ENABLE` (default `false`)
- `PUSH_TRUNK_PN_APP_ID` (default `th.or.ttrs.video.prod`) controls the SIP Contact `app-id` accepted for trunk PushKit tokens.
- `PUSH_TTRS_API_URL`, `PUSH_TTRS_API_TIMEOUT_MS`
- `PUSH_DEBUG_TTRS_TOKEN` (default `false`; extremely sensitive local-only
  troubleshooting switch that prints the TTRS OAuth bearer token and request
  URL; never enable in production and never export these records)
- `PUSH_FIREBASE_CREDENTIALS_FILE`, `PUSH_FIREBASE_PROJECT_ID`
- `PUSH_APNS_ENABLE`, `PUSH_APNS_ENV`, `PUSH_APNS_AUTH_MODE`
- `PUSH_APNS_KEY_FILE`, `PUSH_APNS_KEY_ID`, `PUSH_APNS_TEAM_ID`
- `PUSH_APNS_CERT_FILE`, `PUSH_APNS_CERT_KEY_FILE`
- `PUSH_APNS_BUNDLE_ID`, `PUSH_APNS_TOPIC`
- Employee OAuth used by the TTRS notification API:
  `AUTH_TTRS_EMPLOYEE_TOKEN_URL`, `AUTH_TTRS_EMPLOYEE_TOKEN_GRANT_TYPE`
  (default `client_credentials`), `AUTH_TTRS_EMPLOYEE_CLIENT_ID`, and
  `AUTH_TTRS_EMPLOYEE_CLIENT_SECRET`

## Incoming-call admission

- `SIP_INCOMING_RING_TIMEOUT_SECONDS` (default `30`)
- `SIP_INCOMING_OFFLINE_POLICY` (default `push_then_480`)

## Mid-call SIP behavior

- In-dialog `re-INVITE` and `UPDATE` with valid Opus SDP can update hold/resume media direction without ending the SIP dialog.
- H.264 video add/remove is accepted only when the SDP remains H.264-compatible. The gateway emits WebSocket `renegotiate` for client-assisted WebRTC changes and tracks a pending `renegotiationId` until `renegotiate_answer` or timeout.
- Glare/pending mid-call renegotiation returns `491`; malformed or unsupported SDP returns `488`; unknown in-dialog requests return `481`.
- `Allow`/`Supported` are intentionally conservative. Do not advertise `PRACK`, `REFER`, `100rel`, or session timers until those flows are implemented as first-class behavior.
- `REFER`, `PRACK`, `Require: 100rel`, and `Session-Expires` currently have explicit reject policies so PBX/trunk behavior is deterministic.

## Trunk / multi-instance

- `SIP_TRUNK_ENABLE` (default follows `DB_ENABLE`)
- `SIP_TRUNK_LEASE_TTL_SECONDS` (default `60`)
- `SIP_TRUNK_LEASE_RENEW_INTERVAL_SECONDS` (default `20`)
- `SIP_TRUNK_REGISTER_TIMEOUT_SECONDS` (default `10`)
- `GATEWAY_INSTANCE_ID` (default hostname/random)
- `GATEWAY_PUBLIC_WS_URL` (for redirects; single-domain example `wss://gateway.example.com/ws` — see [`deploy/README.md`](../../deploy/README.md))
- `SESSION_DIRECTORY_TTL_SECONDS` (default `7200`)
- `SESSION_DIRECTORY_CLEANUP_INTERVAL_SECONDS` (default `300`)

## Translator

- `TRANSLATOR_ENABLE` (default `false`)
- `TRANSLATOR_ADDR` (default `localhost:5000`)
- `TRANSLATOR_SOURCE_LANG` (default `en-US`)
- `TRANSLATOR_TARGET_LANG` (default `th`)
- `TRANSLATOR_TTS_VOICE` (default `th-TH-PremwadeeNeural`)
- `TRANSLATOR_OPUS_BITRATE` (default `24000`)

## Compatibility override (use carefully)

- `SIP_FORCE_AVP` (read in `internal/sip/sdp.go`) can force `RTP/AVP` even when AVPF flags are enabled.
