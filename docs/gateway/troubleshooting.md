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

- Look for `request_keyframe_handled ... action=` when the client sends WS `request_keyframe` (underscore). That message recovers the SIP→browser picture (cached IDR replay or SIP-leg keyframe request); `action=webrtc` with session log `request_keyframe_deferred` means the SIP video address/SSRC was not known yet. The hyphenated `request-keyframe` is the opposite direction (browser IDR toward SIP) and logs `switch_keyframe_request received`.
- Confirm switch recovery reaches `h264_au_normalized status=complete-idr` and ends through Gateway RTP stability or its bounded timeout.
- Confirm startup prints `SIP Video H264 AU Normalization: true`. Every 300 SIP video packets, inspect `h264_au_stats` for `emitted`, `dropped_incomplete`, `dropped_overflow`, and `pending_packets`.
- A `h264_au_normalized status=complete-idr` line now means the marker and all FU-A fragments were complete; a bare IDR/FU-A start no longer counts as successful keyframe delivery.
- Accepted `@switch` transitions use **blackout-until-safe-IDR** by default
  (`SIP_SWITCH_VIDEO_TRANSITION_MODE=blackout`): unless skipped by opt-in
  `SIP_SWITCH_VIDEO_RENEGOTIATE_ENABLE=true` (with `SIP_MIDCALL_RENEGOTIATION_ENABLE=true`),
  the transition hold drops **all** SIP→WebRTC video packets (including early
  keyframes) during the minimum `SIP_SWITCH_VIDEO_BLACKOUT_MS` (300ms) window
  and holds the complete-IDR gate until both that window has elapsed and a
  decoder-safe IDR with fresh parameter sets is ready (`switch_video_gate_release`).
  Pion keeps the negotiated WebRTC binding SSRC unchanged on the wire — egress
  packet SSRC rewrite does not reset client decoders.
- `switch_transition_hold_start mode=blackout` plus
  `switch_video_gate_reject reason=blackout-hold` means the minimum blackout
  window has not elapsed yet. `switch_transition_hold_end reason=gate-released`
  means the gate committed the first safe Linphone IDR.
- Phone→SIP keyframe requests stop once an uplink IDR is forwarded. Look for `periodic_pli_stop reason=uplink-idr-forwarded`. `reason=no-uplink-idr` is the 15s safety net. `sip_keyframe_request_suppressed reason=uplink-idr-fresh|rate-limit` means a SIP PLI/FIR was not forwarded to the phone. Startup `webrtc_api twcc=true` means transport-cc feedback is installed.
- After queue→agent, `sip_video_ts_discontinuity` logs each outbound timestamp
  repair: `reason`, `in_prev`/`in` (SIP timestamps), `out_prev`/`out` (WebRTC
  timestamps), and `wall_ms` (real time since the previous forwarded frame).
  `out - out_prev` should match `wall_ms` at 90 kHz (90 ticks per millisecond).
  `reason=same-picture-split ... reused=true` is not a repair: the phone sent
  two access units on one RTP timestamp (Linphone's 10ms ticker), and the
  second keeps the same outbound timestamp (`SIP_VIDEO_SAME_TS_REUSE`). Before
  that flag, the same case logged `reason=source-gap` with `in_prev == in`
  and `wall_ms` 0-2. It runs at a steady ~0.2/s on some Android legs for the
  whole call, and is on the phone→agent path, not the agent→phone path.
  A later `switch_video_gate_activation outcome=satisfied-by-discontinuity-idr`
  means the agent IDR arrived before the `@switch` notice and already started
  the segment. Audio is not rewritten; Opus stays on the SIP timestamp.
- Android Linphone frozen on the agent picture right after answer (decoder errors 0x24/0x4004, then 0x10): check `uplink_video_ts_rebase start browser_ts=... base=5400000` for the call. The first WebRTC→SIP video packet toward Asterisk must carry RTP ts `5400000` (`SIP_VIDEO_TS_BASE`). In a pcap on the gateway→Asterisk video leg, filter `rtp.ssrc == <gateway video SSRC> && rtp.timestamp == 5400000`; it is the first packet of that SSRC. With `SIP_VIDEO_TS_REBASE=off` it is the browser's random timestamp instead.
- If normalization itself is suspected, temporarily set `SIP_VIDEO_AU_NORMALIZE_ENABLE=false` and restart the gateway. This restores the legacy raw reordered path and should be used only as a bounded comparison because incomplete frames can poison strict mobile decoders.

## Queue-to-agent video is blocky or has incorrect colors

With `SIP_VIDEO_AU_NORMALIZE_ENABLE=true`, an accepted `@switch` holds SIP→WebRTC
video until the Gateway writes fresh SPS/PPS and a complete IDR for the new
switch generation. Default `blackout` mode drops **all** packets during the
minimum `SIP_SWITCH_VIDEO_BLACKOUT_MS` window before allowing the complete-IDR
gate to release; `SIP_SWITCH_VIDEO_TRANSITION_MODE=preserve` skips that minimum
blackout window and holds only unsafe non-keyframe packets so the last rendered
frame stays visible until the first keyframe/safe IDR releases immediately.
Audio continues independently.

When opt-in `SIP_SWITCH_VIDEO_RENEGOTIATE_ENABLE=true` (default `false`, requires
`SIP_MIDCALL_RENEGOTIATION_ENABLE=true`), a genuine `@switch` skips the
`SIP_SWITCH_VIDEO_BLACKOUT_MS` transition hold (`switch_transition_hold_skipped reason=renegotiate`)
and starts a make-before-break WebRTC handoff: the live PeerConnection stays
parked until the replacement ICE-connects. The gateway emits
`switch_renegotiate_started` before `switch_renegotiate_pc_replaced` so the
client can `createOffer` in parallel. Gateway answers return after a short ICE
budget (`ice_gathering=ready` or `partial`); they do not wait the resume 3s
gather timeout. Look for `switch_renegotiate_make_before_break_armed`,
`switch_renegotiate_pc_replaced make_before_break=true`, then
`switch_renegotiate_legacy_pc_closed`. ttrs-vri (SIP→WebRTC) should keep the
last frame until the new path is ready. A freeze of **Linphone’s remote video**
(WebRTC→SIP uplink) after `@switch` is a separate path: the replacement
PeerConnection must produce a new camera IDR toward Asterisk; correlate
`OnTrack (renegotiated)` with SIP FIR/PLI after ICE connected.

- `switch_video_gate_activation outcome=active` means the new media generation is gated.
- `switch_video_gate_reject` identifies an unsafe AU; inspect `reason` for a
  stale generation, non-IDR frame, or missing fresh parameter sets.
- `switch_video_gate_release` reports the clean release latency in `waitMs`.
- `switch_video_gate_stalled` reports packet gaps, missing/out-of-order RTP,
  reorder timeouts, rejected AUs, and the active feedback transport.

For RTPengine deployments, capture both media legs—Asterisk to RTPengine and
RTPengine to Gateway. Compare the first fresh SPS/PPS, first complete IDR, RTP
sequence gaps/order/timestamps, and whether Gateway FIR/PLI reaches Asterisk.
Do not bypass RTPengine in production for this diagnosis.

## Phone shows no agent video, or it freezes after answer (WebRTC→SIP)

- Decoder-join kicks ask the browser for a fresh IDR when the SIP far end
  becomes reachable: `uplink_keyframe_kick reason=sip-video-dest-ready|sip-video-rtp-learn|remote-ssrc-learn|sip-rtcp-first|bridged-*`.
  `uplink_keyframe_kick_skipped reason=... skip=idr-fresh` means a complete
  uplink IDR was forwarded in the last 10s; `skip=idr-in-flight` means one is
  still being written.
- `uplink_keyframe_kick reason=remote-ssrc-learn pre_learn_idr=ignored` (or
  `reason=sip-rtcp-first`) means the only recent IDR was forwarded before the
  phone's video SSRC was learned or before its first video RTP/RTCP, so the
  phone never saw it. The gateway sends FIR + PLI anyway, then up to three
  follow-up PLIs 1s apart (`... pre_learn_idr=ignored follow_up=N`) until an
  IDR from after the kick is forwarded. If no later `uplink_keyframe_kick` or
  IDR appears, the browser did not answer the PLI.
- `uplink_idr_fragment_dropped reason=no-start` means FU-A fragments of an IDR
  arrived without their start fragment (start lost, or it came before the SIP
  video address was set). That tail has no SPS/PPS and cannot be decoded, so
  it is dropped instead of being written and counted as a forwarded IDR, and
  the browser gets a PLI (`browser_pli sent reason=idr_without_start`, at most
  once a second). An IDR start that cannot be written no longer opens the
  uplink gate or uses up its access unit's SPS/PPS prefix.
- Lower agent resolution mid-call is expected under low bandwidth (Chrome
  `degradationPreference=balanced`). With the default
  `SIP_UPLINK_PICTURE_SIZE_CHANGE=accept` the gateway does not drop the call's
  video: it logs `uplink_sps_dimension_change accepted WxH was WxH`, holds the
  new size (`uplink_picture_size_hold`) until its first IDR, sends one fresh
  SPS/PPS pair in front of that IDR, and logs `uplink_picture_size_idr released`.
  If the browser changes size without an IDR it gets a PLI at most once per
  second (`browser_pli sent reason=picture_size_change`). Only
  `SIP_UPLINK_PICTURE_SIZE_CHANGE=reject` drops video after a size change
  (`uplink_sps_dimension_change dropped`).

## Client stays black on 9999 / echo while SIP video RTP arrives

- The echo returns only what the gateway sends, and Asterisk `Echo()` answers
  a single picture_fast_update per call. The gateway treats the return path
  as stuck ("return recovery") while the client decoder keeps asking
  (browser PLI/FIR or WS `request_keyframe` within the last 2s) and no
  SIP→WebRTC keyframe has arrived for 1.5s of that asking. A return keyframe
  the client never decoded (`keyframeAge` grows while PLIs continue) counts
  as unanswered too.
- Uplink kick: `uplink_keyframe_kick reason=return-recovery trigger=browser-pli
  attempt=N/10 keyframeAge=<dur|none> unanswered=<dur>`. Sent when no return
  keyframe has arrived this call, or the client has kept asking for 2s after
  the last one. At most one every 2s and ten per recovery episode (5s without
  client requests starts a new episode); skipped while an uplink IDR is in
  flight or was forwarded in the last 2s.
- RTCP escalation: with `SIP_KEYFRAME_REQUEST_MODE=info`, return recovery also
  sends RTCP PLI/FIR toward SIP (`sip_rtcp_fb_escalated ... reason=return-recovery`)
  next to the INFO. The usual RTCP gates still apply (uplink IDR in flight,
  post-IDR guard, `SIP_RTCP_FB_MIN_INTERVAL_MS`, wait for a marker); skips log
  `sip_rtcp_fb_skipped ... escalated=return-recovery`.
- `sip_keyframe_via_info ... recovering=true` means the previous INFO has not
  been answered by a return keyframe, none arrived this call, or return
  recovery is active, so the INFO floor is 500ms instead of 2s.
  `recovering=false` keeps the 2s floor.
- The SIP video keyframe watchdog does not skip as `rtp-flowing` when no
  return keyframe was ever seen or return recovery is active; it then logs
  `keyframe_watchdog_client_waiting action=pli|fir keyframeAge=...`.
- Echo may still ignore every request after the first. The uplink kick is the
  path that works there; if the phone encoder ignores PLI/FIR, set a periodic
  keyframe interval on the Android encoder.

## 401 Unauthorized (and 409 Conflict) on API/WS

Checkpoints:

- Verify `AUTH_ENABLE` and at least one configured realm: user
  (`AUTH_TTRS_USERS_JWKS_URL`, `AUTH_TTRS_USERS_JWT_ISSUER`,
  `AUTH_TTRS_USERS_JWT_AUDIENCE`) or employee
  (`AUTH_TTRS_EMPLOYEE_JWKS_URL`, `AUTH_TTRS_EMPLOYEE_JWT_ISSUER`,
  `AUTH_TTRS_EMPLOYEE_JWT_AUDIENCE`). Issuer and audience checks apply when
  their values are configured.
- For REST operations, remember that a non-empty `FRONTEND_PASSWORD` enforces
  `Authorization: Bearer <FRONTEND_PASSWORD>` (or a valid JWT when
  `AUTH_ENABLE=true`) on protected `/api/*` routes even when `AUTH_ENABLE=false`.
  `FRONTEND_PASSWORD` is never accepted on WebSocket endpoints.
- Verify token `iss` and `aud` match configured values exactly.
- Verify token is not expired (`exp`) and is already valid (`nbf`), accounting
  for the gateway's `30s` clock-skew leeway.
- Verify JWT header `kid` exists in current JWKS (gateway auto-refreshes JWKS once on unknown `kid`).
- Realm hints (`user|users|client` vs `employee|employees|agent`): browser
  cross-origin clients should pass `?auth_type=...` in the URL query rather than
  the `X-Auth-Type` header because `X-Auth-Type` is not listed in CORS
  `Access-Control-Allow-Headers`.
- Endpoint-specific WebSocket upgrade rules:
  - `/ws` requires `?access_token=<jwt>` when `AUTH_ENABLE=true` (ignored when `AUTH_ENABLE=false`). When `SIPCLIENT_AUTH_REGISTER_URL` is set, user-realm `/ws` connections also require `devicePlatform=android|ios`, and a concurrent mobile provisioning or `unregister` operation for the same JWT `sub` returns HTTP `409 Conflict` (`Account operation in progress; retry connection`).
  - `/ws-agent-device` **always** requires `devicePlatform=android|ios` on the connect URL (returns `401` when missing or invalid, even when `AUTH_ENABLE=false`).
  - `/ws-agent` and `/ws-agent-device` treat `access_token` as optional, but passing a non-empty `access_token` while `AUTH_ENABLE=false` is rejected with `401`.

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
