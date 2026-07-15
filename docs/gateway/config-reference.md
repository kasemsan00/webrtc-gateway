# Gateway Configuration Reference

From `internal/config/config.go`.

---

## Core SIP/API/RTP

- `SIP_LOCAL_IP` (default `0.0.0.0`)
- `SIP_PUBLIC_IP` (optional, important behind NAT)
- `SIP_PORT` (default `5060`)
- `SIP_LOCAL_PORT` (default `5060`)
- `API_PORT` (default `8080`)
- `API_ENABLE_WS` (default `true`)
- `API_ENABLE_PUBLIC_WS` (default `false`; enables unauthenticated `/ws-public` for public SIP per-call credentials only)
- `API_ENABLE_REST` (default `true`)
- `API_CORS_ORIGINS` (default `*`; for single-domain deploy use e.g. `https://k2-gateway.kasemsan.com` — see [`deploy/README.md`](../../deploy/README.md))
- `SIPCLIENT_AUTH_REGISTER_URL` (optional; when set, user-realm WebSocket auth auto-provisions a mobile SIP trunk)
- `SIPCLIENT_AUTH_TIMEOUT_MS` (default `5000`)
- `RTP_PORT_MIN` (default `10500`)
- `RTP_PORT_MAX` (default `10600`)
- `RTP_BUFFER_SIZE` (default `16384`)

## JWT auth (Keycloak/JWKS)

- `AUTH_ENABLE` (default `false`)
- `AUTH_JWKS_URL` (required when auth enabled)
- `AUTH_JWT_ISSUER` (required when auth enabled)
- `AUTH_JWT_AUDIENCE` (required when auth enabled)
- `AUTH_JWKS_TIMEOUT_MS` (default `5000`)

When `AUTH_ENABLE=true`:

- Startup is fail-fast if required auth env is missing.
- Startup is fail-fast if initial JWKS prefetch fails.
- `/api/*` requires `Authorization: Bearer <jwt>`.
- `/ws` requires `?access_token=<jwt>`.

## Debug and media behavior toggles

- `DEBUG_WEBSOCKET` (default `false`)
- `DEBUG_TURN` (default `false`)
- `DEBUG_SIP_MESSAGE` (default `false`)
- `DEBUG_SIP_INVITE` (default `false`)
- `SWITCH_PLI_DELAY_MS` (default `1000`)
- `SIP_AUDIO_USE_AVPF` (default `false`)
- `SIP_AUDIO_INBOUND_GAIN_ENABLE` (default `false`; requires CGO + libopus in Docker build)
- `SIP_AUDIO_INBOUND_GAIN` (default `1.0`; linear multiplier, clamped to max)
- `SIP_AUDIO_INBOUND_GAIN_MAX` (default `3.0`)
- `SIP_VIDEO_USE_AVPF` (default `false`)
- `SIP_VIDEO_FEEDBACK_TRANSPORT` (default `auto`; `auto|rtp|rtcp|dual`; `dual` can help diagnose RTPengine RTCP routing, but it does not replace complete-IDR validation)
- `SIP_VIDEO_PRESERVE_STAPA` (default `false`)
- `SIP_VIDEO_AU_NORMALIZE_ENABLE` (default `true`; validates complete timestamp-grouped H.264 access units, rewrites outbound RTP continuity, injects fresh SPS/PPS before IDR when absent, and keeps a real `@switch` gated until a decoder-safe IDR is written; set `false` only as a bounded rollback to the legacy raw reordered/packet-level transition path)
- `SIP_SWITCH_VIDEO_TRANSITION_MODE` (default `blackout`; `blackout` drops SIP→WebRTC video until the complete-IDR gate releases; `preserve` is rollback that keeps the last rendered frame visible while holding unsafe packets)
- `SIP_SWITCH_VIDEO_BLACKOUT_ENABLED` (default `true`)
- `SIP_SWITCH_VIDEO_BLACKOUT_MS` (default `300`; minimum blackout before gate may release in `blackout` mode)
- `SIP_SWITCH_VIDEO_BLACKOUT_MAX_WAIT_MS` (default `1200`)
- `SIP_SWITCH_VIDEO_RENEGOTIATE_ENABLE` (default `true`; after `@switch` gate release, emit WebSocket `renegotiate` with `reason=agent_switch` so clients reset the remote video decoder via offer/answer)
- `SIP_VIDEO_KEYFRAME_WATCHDOG` (default `true`)
- `SIP_VIDEO_KEYFRAME_WATCHDOG_INTERVAL_MS` (default `1000`)
- `SIP_VIDEO_KEYFRAME_STALE_MS` (default `2500`)
- `SIP_VIDEO_KEYFRAME_FIR_STALE_MS` (default `6000`)

## TURN

- `TURN_SERVER`, `TURN_USERNAME`, `TURN_PASSWORD`

## Database / LogStore

- `DB_ENABLE` (default `false`)
- `DB_DSN`
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

## Push notifications

- `PUSH_ENABLE` (default `false`)
- `PUSH_TRUNK_PN_APP_ID` (default `th.or.ttrs.video.prod`) controls the SIP Contact `app-id` accepted for trunk PushKit tokens.
- `PUSH_TTRS_API_URL`, `PUSH_TTRS_API_TIMEOUT_MS`
- `PUSH_FIREBASE_CREDENTIALS_FILE`, `PUSH_FIREBASE_PROJECT_ID`
- `PUSH_APNS_ENABLE`, `PUSH_APNS_ENV`, `PUSH_APNS_AUTH_MODE`
- `PUSH_APNS_KEY_FILE`, `PUSH_APNS_KEY_ID`, `PUSH_APNS_TEAM_ID`
- `PUSH_APNS_CERT_FILE`, `PUSH_APNS_CERT_KEY_FILE`
- `PUSH_APNS_BUNDLE_ID`, `PUSH_APNS_TOPIC`

## Mid-call SIP behavior

- In-dialog `re-INVITE` and `UPDATE` with valid Opus SDP can update hold/resume media direction without ending the SIP dialog.
- H.264 video add/remove is accepted only when the SDP remains H.264-compatible. The gateway emits WebSocket `renegotiate` for client-assisted WebRTC changes and tracks a pending `renegotiationId` until `renegotiate_answer` or timeout.
- Glare/pending mid-call renegotiation returns `491`; malformed or unsupported SDP returns `488`; unknown in-dialog requests return `481`.
- `Allow`/`Supported` are intentionally conservative. Do not advertise `PRACK`, `REFER`, `100rel`, or session timers until those flows are implemented as first-class behavior.
- `REFER`, `PRACK`, `Require: 100rel`, and `Session-Expires` currently have explicit reject policies so PBX/trunk behavior is deterministic.

- `SIP_PUBLIC_REGISTER_EXPIRES_SECONDS` (default `3600`)
- `SIP_PUBLIC_REGISTER_TIMEOUT_SECONDS` (default `10`)
- `SIP_PUBLIC_IDLE_TTL_SECONDS` (default `600`)
- `SIP_PUBLIC_CLEANUP_INTERVAL_SECONDS` (default `30`)
- `SIP_PUBLIC_MAX_ACCOUNTS` (default `1000`)

## Trunk / multi-instance

- `SIP_TRUNK_ENABLE` (default follows `DB_ENABLE`)
- `SIP_TRUNK_LEASE_TTL_SECONDS` (default `60`)
- `SIP_TRUNK_LEASE_RENEW_INTERVAL_SECONDS` (default `20`)
- `SIP_TRUNK_REGISTER_TIMEOUT_SECONDS` (default `10`)
- `GATEWAY_INSTANCE_ID` (default hostname/random)
- `GATEWAY_PUBLIC_WS_URL` (for redirects; single-domain example `wss://k2-gateway.kasemsan.com/ws` — see [`deploy/README.md`](../../deploy/README.md))
- `SESSION_DIRECTORY_TTL_SECONDS` (default `7200`)
- `SESSION_DIRECTORY_CLEANUP_INTERVAL_SECONDS` (default `300`)

## Compatibility override (use carefully)

- `SIP_FORCE_AVP` (read in `internal/sip/sdp.go`) can force `RTP/AVP` even when AVPF flags are enabled.
