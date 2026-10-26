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
- `API_ENABLE_AGENT_WS` (default `false`; enables `/ws-agent` for PC agent SIP REGISTER presence with client-provided credentials; `access_token` is optional and verified when present)
- `API_ENABLE_AGENT_DEVICE_WS` (default `false`; enables JWT `/ws-agent-device` for sticky agent-device SIP REGISTER, stored FCM, and logout unregister)
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
  `/api/logs*` routes, the four GET `/api/client-diagnostics*` query routes,
  `POST /api/chat-images`, and `GET /api/chat-images/{id}` are intentionally
  public. Chat-image POST still requires a live call `sessionId`.
- `/ws` requires `?access_token=<jwt>`. The admin password is not accepted on `/ws`.
- `/ws-agent` still accepts a connection with no token. When `access_token` is present it is verified and stored on the connection. A call may then use the trunk from `agent_register`, or omit the token and send `sipDomain`, `sipUsername`, and `sipPassword` on `agent_register` or on `call`. Trunk calls on `/ws` and `/ws-agent-device` still require claims while this verifier is enabled.

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
- `SIP_SWITCH_VIDEO_GATE_MIN_IDR_PACKETS` (default `1`; smallest complete post-switch IDR the gate will release before the 2s stall timeout. Completeness is SPS/PPS + IDR with a closed FU-A/marker. Raise toward `24` only if a specific encoder's tiny flush IDRs still poison the decoder.)
- `SIP_SWITCH_VIDEO_GATE_FAIL_OPEN_MS` (default `3000`; release a stalled gate when RTP is still healthy and the SSRC and source match the stream the gate already opened)
- `SIP_SWITCH_VIDEO_GATE_FAIL_OPEN_CAP_MS` (default `5000`; release any stalled gate after this long)
- `SIP_SWITCH_VIDEO_INFO_FIR_ENABLE` (default `false`; extra INFO on `@switch`/stall when that flag is on. The default keyframe path is `SIP_KEYFRAME_REQUEST_MODE=info`, which does not depend on this knob.)
- `SIP_KEYFRAME_REQUEST_MODE` (default `info`; `info|rtcp|both`). How the gateway asks the SIP/Asterisk video leg for a keyframe. `info` sends SIP INFO `picture_fast_update` only. Asterisk stamps `marker=1` on the next packet it forwards after each RTCP PLI/FIR, which splits Linphone packetization-mode=0 IDRs, so RTCP feedback toward SIP is off by default. `rtcp` is the legacy PLI/FIR path (still skipped while an uplink IDR is in flight, for `SIP_RTCP_FB_GUARD_MS` after it, at most once per `SIP_RTCP_FB_MIN_INTERVAL_MS`, and only right after a forwarded marker bit). `both` sends INFO plus that gated RTCP.
- `SIP_RTCP_FB_MIN_INTERVAL_MS` (default `2000`; clamped `500..10000`). Minimum gap between SIP-leg RTCP PLI/FIR.
- `SIP_RTCP_FB_GUARD_MS` (default `1500`; `0` disables; clamped `0..5000`). Skip SIP-leg RTCP PLI/FIR this long after a complete uplink IDR.
- `SIP_UPLINK_PICTURE_SIZE_CHANGE` (default `accept`; `accept|reject`, invalid values fall back to `accept`). What the WebRTC→SIP video path does when the browser's H.264 SPS changes picture size mid-call, e.g. Chrome `degradationPreference=balanced` scaling down under low bandwidth. `accept` forwards the new size: once an uplink IDR has been forwarded, every non-IDR packet of the new size (including its in-band SPS/PPS) is held until the first IDR, and that IDR gets exactly one pair of the new SPS/PPS in front of it. Same SSRC, contiguous sequence numbers, and the browser's RTP timestamps are kept. If the new size arrives without an IDR, the browser (not Asterisk) gets a PLI, at most once per second. Before the first uplink IDR the normal startup gate applies. `reject` is the old lock: the first size is kept, and all uplink video after a change is dropped for the rest of the call unless an `@switch` keyframe request allows one change. It protects a TTRS build whose GL uploader overflowed on a mid-call resolution change. Log lines: `uplink_sps_dimension_change accepted|dropped`, `uplink_picture_size_hold`, `uplink_picture_size_idr released`, `browser_pli sent reason=picture_size_change`.
- `SIP_LOSSY_KEYFRAME_ASSIST` (default `on`; `on|off`, also `true|false|1|0|yes|no`; invalid values use `on`). Extra browser keyframes for lossy SIP video legs (Android Linphone at 5-20% loss rarely gets a 25-30 packet IDR through whole, then re-asks only every ~5s). Loss is measured on the SIP video RTP the gateway receives from the phone leg: expected minus received sequence numbers over the last ~4 complete seconds, so reordered packets are not loss, and a sequence jump over 3000 is a new stream. Below the threshold, keyframe behaviour is unchanged. While lossy: (1) within `SIP_LOSSY_EARLY_WINDOW_MS` of answer, each answered SIP INFO `picture_fast_update` schedules one extra browser PLI `SIP_LOSSY_EXTRA_IDR_DELAY_MS` later. A second VFU while one is scheduled is coalesced. The extra is dropped if a VFU retry or an unanswered PLI is still pending, waits while an IDR is in flight (up to ~1s), and waits out `BrowserIDRRequestMinGap` (500ms) after the last request or IDR. (2) The proactive safety-net interval drops from `SIP_PROACTIVE_BROWSER_PLI_SAFETY_MS` to `SIP_LOSSY_SAFETY_IDR_INTERVAL_MS`, and an answered VFU no longer suppresses it. It returns to normal once loss falls below the threshold. `off` disables loss tracking and both behaviours. No change to resolution, SPS/PPS injection, or picture-size handling. Log lines: `lossy_keyframe_assist lossy_detected|recovered loss_pct=`, `lossy_keyframe_assist safety_interval switched from= to= reason=lossy|recovered`, `lossy_extra_idr scheduled|sent|coalesced|skipped`.
- `SIP_LOSSY_LOSS_THRESHOLD_PCT` (default `5`; valid `1..50`, else default). Loss % that marks the SIP video leg lossy.
- `SIP_LOSSY_EXTRA_IDR_DELAY_MS` (default `1200`; valid `500..5000`, else default). Delay of the extra browser IDR request after an answered VFU.
- `SIP_LOSSY_EARLY_WINDOW_MS` (default `10000`; valid `1000..60000`, else default). Extra IDRs are only scheduled for VFUs this soon after the call is answered (session becomes active).
- `SIP_LOSSY_SAFETY_IDR_INTERVAL_MS` (default `3000`; valid `1000..10000`, else default). Safety-net browser IDR interval while the leg is lossy.
- `SIP_VIDEO_SAME_TS_REUSE` (default `on`; `on|off`, also `true|false|1|0|yes|no`; invalid values use `on`). SIP→WebRTC video (phone → agent browser), with `SIP_VIDEO_AU_NORMALIZE_ENABLE=true`. Linphone stamps video on a 10ms ticker, so the phone sometimes sends two marker-terminated access units with the same RTP timestamp. `on`: the second one keeps the first one's outbound timestamp, as the source sent it. That is not a discontinuity and does not count as a discontinuity IDR. It is logged as `sip_video_ts_discontinuity reason=same-picture-split in= out= reused=true first_mb= packets= prev_packets= count= wall_ms=` (first 5 per stream, then every 50th). `first_mb=0` means a separate picture sharing the timestamp, `>0` means the tail of one picture split by a mid-picture marker. Backward timestamps, jumps over 10s, stream switches, and the first AU after a cached-IDR replay still step by wall clock (`reason=source-gap`, `switch`, ...). `off`: the old behaviour, where a repeated timestamp steps by wall clock (+1..+186 ticks) and logs `reason=source-gap`. This flag does not affect the agent→phone (WebRTC→SIP) video.
- `SIP_VIDEO_REORDER_PACING_MS` (default `1`; sleep between consecutive packets released from a filled reorder gap so a recovered burst does not arrive as one UDP clump. `0` disables. Clamped to `0..10`.)
- `SIP_VIDEO_TIMESTAMP_JUMP_PLI` (default `true`; send a forced SIP PLI when SIP video RTP timestamps jump by more than 10s, e.g. Asterisk pre-answer stills at ts≈2700 then the endpoint clock at ~1e9). The same jump, and an SSRC change, is treated as a stream switch: the normalizer resets, the keyframe gate re-arms, and the outbound timestamp steps by wall-clock time since the previous forwarded frame instead of a fixed +33ms or +1s.
- `SIP_VIDEO_FORWARD_EARLY_MEDIA` (default `true`). Set `false` to drop SIP video until the call is answered (`state=active`). That avoids locking the phone's jitter buffer onto Asterisk early-media stills, at the cost of no queue/ringback picture — the caller stays on black video until the answered stream. Leave it `true` when the queue picture should stay visible; timestamp repair still covers the handoff.
- `WEBRTC_TWCC_ENABLE` (default `true`). Installs Pion's transport-cc sender (header extension plus feedback) on both the initial and replacement PeerConnection so the phone's `availableOutgoingBitrate` can move. Set `false` to omit `transport-cc` and the sender interceptor. `goog-remb` is not advertised. NACK retransmission stays on the gateway cache.
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
  MESSAGE is accepted. When enabled, the gateway parks the live PeerConnection
  (make-before-break), emits WebSocket `renegotiate` with `reason=agent_switch`
  immediately (the client may createOffer while the gateway still prepares the
  replacement PeerConnection), empty SDP, and `requiresAnswer=true`. The client
  creates a fresh offer on a parallel PeerConnection; the gateway answers that
  offer and returns the answer SDP in `renegotiate_result.sdp` as soon as a
  local ICE candidate exists (it does not wait the resume 3s gather timeout).
  SIP audio keeps flowing on the parked PC until the replacement ICE-connects;
  SIP→WebRTC video is gated with `renegotiate-pending` until the client offer
  is applied (or the attempt fails/times out). The 300ms switch blackout hold
  is skipped while this flag is true. The parked PC is closed after ICE
  connected. No switch renegotiation is attempted, and no `switch_renegotiate_*`
  log is emitted, while this flag is `false`. Configuration changes require a
  gateway process restart.)
- `SIP_PROACTIVE_BROWSER_PLI_INITIAL_MS` (default `2000`; browser PLI interval until the first WebRTC→SIP IDR is forwarded)
- `SIP_PROACTIVE_BROWSER_PLI_SAFETY_MS` (default `10000`; after the first IDR, send a browser PLI only if none has been forwarded in this window. SIP INFO FIR and RTCP FIR/PLI handle recovery in between.)
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
- H.264 video add/remove is accepted only when the SDP remains H.264-compatible. The gateway emits WebSocket `renegotiate` for client-assisted WebRTC changes and tracks a pending `renegotiationId` until `renegotiate_answer` or timeout. `@switch` uses a separate client-offer / gateway-answer contract (`reason=agent_switch`, empty offer SDP) and does not send a SIP re-INVITE.
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

## Chat images

In-call clients upload a file, receive an HTTPS URL, and send that URL as the whole SIP MESSAGE body. Receivers that allowlist the gateway `/api/chat-images/{uuid}` path render it as an image; unknown clients show the URL as text. Legacy `<image>URL</image>` bodies are still accepted. Binary images are not carried over WebSocket `send_message` (16KB frame limit).

- `CHAT_IMAGE_ENABLE` (default `true`)
- `CHAT_IMAGE_DIR` (default `chat-images`)
- `CHAT_IMAGE_PUBLIC_BASE_URL` (optional absolute origin such as `https://k2-gateway.kasemsan.com`; if empty, the upload response URL uses `X-Forwarded-Proto`/`X-Forwarded-Host` or the request host)
- `CHAT_IMAGE_MAX_BYTES` (default `10485760`)
- `CHAT_IMAGE_MAX_PER_SESSION` (default `20`)
- `CHAT_IMAGE_TTL_SECONDS` (default `86400`)
- `CHAT_IMAGE_CLEANUP_INTERVAL_SECONDS` (default `900`)

`POST /api/chat-images` is unauthenticated REST (VRI public calls have no JWT) and requires multipart field `file` plus `sessionId` of a live, non-ended session (form field or `X-Session-Id`). Allowed magic types: JPEG, PNG, WebP, GIF. `GET /api/chat-images/{id}` is public; ids are UUIDs with no directory listing.

Nginx in front of `/api/` must allow bodies larger than `CHAT_IMAGE_MAX_BYTES` (`client_max_body_size 12m`).

## Translator

- `TRANSLATOR_ENABLE` (default `false`)
- `TRANSLATOR_ADDR` (default `localhost:5000`)
- `TRANSLATOR_SOURCE_LANG` (default `en-US`)
- `TRANSLATOR_TARGET_LANG` (default `th`)
- `TRANSLATOR_TTS_VOICE` (default `th-TH-PremwadeeNeural`)
- `TRANSLATOR_OPUS_BITRATE` (default `24000`)

## Compatibility override (use carefully)

- `SIP_FORCE_AVP` (read in `internal/sip/sdp.go`) can force `RTP/AVP` even when AVPF flags are enabled.
