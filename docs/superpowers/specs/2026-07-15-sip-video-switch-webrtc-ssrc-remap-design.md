# SIP Video Switch WebRTC Egress SSRC Remap Design

Date: 2026-07-15
Status: Approved for implementation planning
Scope: `apps/gateway` SIP-to-WebRTC video egress path on accepted `@switch`

## Problem

Production queue → Linphone `@switch` often keeps the same SIP video SSRC
(RTPengine media continuity). Gateway already:

- accepts the switch target and bumps `SwitchGeneration`
- starts complete-IDR gate + transition hold
- resets H.264 AU/normalizer generation
- sends FIR/PLI bursts

RTP after release is clean (`gaps=0 missing=0`), but mobile clients (especially
`ttrs-vri`) can still show stutter/corruption because the WebRTC remote decoder
keeps decoding the same egress SSRC across a new H.264 parameter-set /
encoder generation. Historical client reconnect-on-`@switch` masked this by
recreating the PeerConnection decoder.

## Goals

- On every **accepted** `@switch`, force a new WebRTC-visible video SSRC so
  client decoders treat Linphone video as a new source.
- Keep SIP-side SSRC/feedback (`RemoteVideoSSRC`, FIR/PLI/NACK to Asterisk)
  unchanged.
- Preserve existing complete-IDR gate, AU normalize, SPS/PPS inject, and
  transition hold behavior.
- Keep audio uninterrupted and preserve WS/SIP contracts.
- Emit clear diagnostics for remap old/new SSRC and switch generation.

## Non-Goals

- Client reconnect / resume on `@switch`.
- Remapping audio SSRC.
- Creating a new `TrackLocalStaticRTP` or mid-call renegotiation.
- Changing RTPengine / Asterisk behavior.
- Remapping on ignored duplicate `@switch` targets.

## Decision

**Approach A (selected):** maintain a session-scoped WebRTC video egress SSRC
and rewrite `packet.Header.SSRC` immediately before every SIP→WebRTC video
write. On accepted `@switch`, allocate a new random egress SSRC.

Rejected:

- **New local track per switch** — requires renegotiation / track replace and
  is higher risk.
- **Remap only when SIP SSRC is unchanged** — rejected by product choice;
  always remap on accepted switch for consistent client behavior.
- **Client-only reconnect** — works but is heavy and was removed from ttrs-vri
  for UX reasons.

## Behavior

```text
queue video  --egress_ssrc_0-->  WebRTC decoder
        accepted @switch
        allocate egress_ssrc_1
        gate holds until complete IDR + fresh SPS/PPS
linphone     --egress_ssrc_1-->  WebRTC decoder (source discontinuity)
```

### Session state

Add `WebRTCVideoEgressSSRC uint32` on `Session`.

- Initialized lazily on first SIP→WebRTC video write if zero:
  prefer current packet SSRC when non-zero, else `generateSSRC()`.
- On accepted `@switch` inside `PrepareAndActivateSwitchVideoTarget`:
  set a new `generateSSRC()` value (must differ from previous when previous
  is non-zero; retry once if collision).
- Reset to `0` with other media state in media reset/teardown paths.

### Write path

All SIP→WebRTC video egress writers must apply egress SSRC before write:

1. Normalized AU path (`writeNormalizedVideoAccessUnit`)
2. Legacy raw reorder path (`VideoTrack.Write` without normalizer)
3. Any other direct `VideoTrack.Write` of SIP video RTP in the same handler

Implementation preference: one helper such as
`sess.ApplyWebRTCVideoEgressSSRC(packet *rtp.Packet)` used before marshal/write
so behavior cannot drift across paths.

RTP cache entries used for WebRTC NACK replay must store post-remap bytes so
retransmits match the egress SSRC the client observed.

### What must not change

- `RemoteVideoSSRC` / `SwitchMediaSSRC` remain SIP-learned values for feedback
  and switch-target media-generation identity.
- RTCP PLI/FIR/NACK toward SIP continue to use SIP media SSRC.
- Audio RTP SSRC rewrite behavior unchanged.
- Duplicate `@switch` ignore path must not allocate a new egress SSRC.

### Logging

On remap:

```text
switch_webrtc_ssrc_remap generation=%d mediaEpoch=%d old=%d new=%d sipSsrc=%d reason=accepted-switch
```

Optional first-init log:

```text
webrtc_video_egress_ssrc_init ssrc=%d source=packet|generated
```

## Compatibility

- Browser / RN / KMP clients receive a mid-call SSRC change on the existing
  video track; decoders are expected to reset on SSRC change without signaling
  changes.
- No WS message type changes.
- No SDP renegotiation required.

## Tests

1. Accepted `@switch` allocates a new `WebRTCVideoEgressSSRC` and leaves
   `RemoteVideoSSRC` unchanged.
2. Duplicate ignored `@switch` does not remap egress SSRC.
3. Normalized write path emits packets with egress SSRC, not SIP SSRC, after
   remap.
4. Legacy raw write path also remaps.
5. Cached replay packets use post-remap SSRC.
6. Existing switch gate / duplicate-target tests still pass.

## Rollout / verification

1. Deploy gateway with remap enabled (no feature flag required for v1; behavior
   is always-on for accepted switches).
2. Place ttrs-vri calls through queue → Linphone `@switch` for 4+ consecutive
   calls.
3. Confirm logs show `switch_webrtc_ssrc_remap` and `switch_recovery_end`
   `stable-rtp`.
4. Confirm remote video after agent answer is stable without client reconnect.

## Open risks

- Some WebRTC stacks may briefly freeze until the first post-remap IDR; the
  existing complete-IDR gate is the mitigation.
- If a future path writes SIP video RTP without the helper, remap can be
  skipped; keep writes centralized and cover with tests.
