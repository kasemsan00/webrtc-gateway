# Gateway Troubleshooting Quick Guide

---

## 488 Not Acceptable Here

Most common causes:

1. malformed SDP `o=` username
2. codec mismatch (must support Opus + H.264)
3. video not enabled on SIP endpoint

Checkpoints:

- confirm SDP origin username fallback logic in `internal/sip/sdp.go`
- confirm peer config has `allow=opus` and `allow=h264`
- confirm `videosupport=yes`/equivalent endpoint settings

## Video rejected (`m=video ... 0`)

- inspect full SDP answer logs through `https://k2-gateway.kasemsan.com/api/logs/current?tail=500`
- validate AVPF compatibility (`SIP_VIDEO_USE_AVPF` and endpoint support)
- temporarily force AVP with `SIP_FORCE_AVP=true` for interoperability testing

## Auth/register timeouts

- transport consistency matters; requests explicitly set transport to avoid digest retry switching transports.

## Remote video is black on mobile web after queue-to-agent switch

Correlate browser inbound RTP diagnostics with Gateway logs by `sessionId`:

- Look for `request_keyframe_handled` when the client sends a legacy keyframe request.
- Confirm switch recovery reaches `h264_au_normalized status=complete-idr` and ends through Gateway RTP stability or its bounded timeout.
- Confirm startup prints `SIP Video H264 AU Normalization: true`. Every 300 SIP video packets, inspect `h264_au_stats` for `emitted`, `dropped_incomplete`, `dropped_overflow`, and `pending_packets`.
- A `h264_au_normalized status=complete-idr` line now means the marker and all FU-A fragments were complete; a bare IDR/FU-A start no longer counts as successful keyframe delivery.
- If normalization itself is suspected, temporarily set `SIP_VIDEO_AU_NORMALIZE_ENABLE=false` and restart the gateway. This restores the legacy raw reordered path and should be used only as a bounded comparison because incomplete frames can poison strict mobile decoders.

## Queue-to-agent video is blocky or has incorrect colors

With `SIP_VIDEO_AU_NORMALIZE_ENABLE=true`, an accepted `@switch` keeps the
previous decoded frame visible until the Gateway writes fresh SPS/PPS and a
complete IDR for the new switch generation. Audio continues independently.

- `switch_video_gate_start` means the new media generation is gated.
- `switch_video_au_rejected` identifies an unsafe AU; inspect `reason` for a
  stale generation, non-IDR frame, or missing fresh parameter sets.
- `switch_video_gate_release` reports the clean release latency in `wait_ms`.
- `switch_video_gate_stalled` reports packet gaps, missing/out-of-order RTP,
  reorder timeouts, rejected AUs, and the active feedback transport.

For RTPengine deployments, capture both media legs—Asterisk to RTPengine and
RTPengine to Gateway. Compare the first fresh SPS/PPS, first complete IDR, RTP
sequence gaps/order/timestamps, and whether Gateway FIR/PLI reaches Asterisk.
Do not bypass RTPengine in production for this diagnosis.

## 401 Unauthorized on API/WS

Checkpoints:

- Verify `AUTH_ENABLE`, `AUTH_JWKS_URL`, `AUTH_JWT_ISSUER`, `AUTH_JWT_AUDIENCE` are set correctly.
- Verify token `iss` and `aud` match configured values exactly.
- Verify token is not expired (`exp`) and is already valid (`nbf`).
- Verify JWT header `kid` exists in current JWKS (gateway auto-refreshes JWKS once on unknown `kid`).
- For WebSocket, verify client sends `access_token` query param on the connect URL.
