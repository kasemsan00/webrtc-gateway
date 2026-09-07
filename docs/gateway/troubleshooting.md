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

- inspect full SDP answer logs through `https://gateway.example.com/api/logs/current?tail=500`
- validate AVPF compatibility (`SIP_VIDEO_USE_AVPF` and endpoint support)
- temporarily force AVP with `SIP_FORCE_AVP=true` for interoperability testing

## Linphone closes or crashes when an agent answers

- Inspect the inbound SIP offer and Gateway answer for `packetization-mode`.
  RFC 6184 treats an omitted value as mode 0; the Gateway answers mode 0 and
  converts WebRTC FU-A into Single NAL Unit packets for that SIP leg.
- Look for `h264_mode0_reassembled` to confirm FU-A conversion. Repeated
  `h264_mode0_drop` means a fragmented NAL was incomplete, discontinuous, or
  too large for one UDP/RTP packet; correlate it with the Linphone tombstone
  and the first SPS/PPS/IDR sequence.
- A SIP offer that explicitly advertises `packetization-mode=1` keeps the
  existing non-interleaved forwarding behavior.

## Auth/register timeouts

- transport consistency matters; requests explicitly set transport to avoid digest retry switching transports.

## Remote video is black on mobile web after queue-to-agent switch

Correlate browser inbound RTP diagnostics with Gateway logs by `sessionId`:

- Look for `request_keyframe_handled` when the client sends a legacy keyframe request.
- Confirm switch recovery reaches `h264_au_normalized status=complete-idr` and ends through Gateway RTP stability or its bounded timeout.
- Confirm startup prints `SIP Video H264 AU Normalization: true`. Every 300 SIP video packets, inspect `h264_au_stats` for `emitted`, `dropped_incomplete`, `dropped_overflow`, and `pending_packets`.
- A `h264_au_normalized status=complete-idr` line now means the marker and all FU-A fragments were complete; a bare IDR/FU-A start no longer counts as successful keyframe delivery.
- Accepted `@switch` transitions use **blackout-until-safe-IDR** by default
  (`SIP_SWITCH_VIDEO_TRANSITION_MODE=blackout`): the complete-IDR gate drops all
  SIP→WebRTC video until a decoder-safe IDR with fresh parameter sets is ready,
  then `switch_video_gate_release` opens the path. Pion keeps the negotiated
  WebRTC binding SSRC unchanged on the wire — egress packet SSRC rewrite does
  not reset client decoders.
- `switch_transition_hold_start mode=blackout` plus
  `switch_video_gate_reject reason=blackout-hold` means the minimum blackout
  window has not elapsed yet. `switch_transition_hold_end reason=gate-released`
  means the gate committed the first safe Linphone IDR.
- If normalization itself is suspected, temporarily set `SIP_VIDEO_AU_NORMALIZE_ENABLE=false` and restart the gateway. This restores the legacy raw reordered path and should be used only as a bounded comparison because incomplete frames can poison strict mobile decoders.

## Queue-to-agent video is blocky or has incorrect colors

With `SIP_VIDEO_AU_NORMALIZE_ENABLE=true`, an accepted `@switch` holds SIP→WebRTC
video until the Gateway writes fresh SPS/PPS and a complete IDR for the new
switch generation. Default `blackout` mode intentionally freezes the last frame
until release; set `SIP_SWITCH_VIDEO_TRANSITION_MODE=preserve` only as a bounded
rollback if you need the queue still visible during the unsafe window. Audio
continues independently.

- `switch_video_gate_activation outcome=active` means the new media generation is gated.
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

## Incoming INVITE rejected as ambiguous trunk match

Asterisk and similar PBXs rewrite inbound INVITE Request-URI and To to the Gateway REGISTER Contact, for example `sip:00025@203.151.21.121:5090`. That address is the Gateway itself, not the SIP registrar identity.

The matcher therefore:

1. Classifies a Request-URI/To that equals the Gateway public SIP address as a local Contact route.
2. Restricts candidates to enabled trunks owned by this instance with a current successful REGISTER identity.
3. Compares INVITE origin evidence in order: transport peer, top Via, Contact.
4. Matches hostname-configured trunks using registrar IPs captured during REGISTER, not DNS during INVITE handling.

Useful log fields: `rule`, `sipUser`, `localRURI`, `localTo`, `origins`, `ownedCandidates`, `eligible`, `evidence`, `reason`.

If two owned current registrations share the same username and the same origin, Gateway returns `503 Service Unavailable`. Do not pick by newest `last_registered_at` or trunk ID.

To inspect and soft-disable obsolete duplicates:

```sql
SELECT id, name, username, domain, port, enabled, sip_auto_register, lease_owner, last_registered_at
FROM sip_trunks
WHERE username = '00025'
ORDER BY id;
```

Soft-delete stale rows with `enabled=false`. Never hard-delete `sip_trunks`.
