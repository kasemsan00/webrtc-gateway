# SIP Video Switch Complete-IDR Gate Design

Date: 2026-07-15
Status: Approved for implementation planning
Scope: `apps/gateway` SIP-to-WebRTC video path

## Problem

When a production call switches from queue video to live Linphone video, all
WebRTC client types can briefly render block corruption or incorrect colors.
The corruption lasts about one to two seconds and then clears. Media follows
this path:

`Asterisk -> Kamailio/RTPengine -> Gateway -> WebRTC client`

The symptom is isolated to the source transition rather than sustained media
delivery, so aggregate bandwidth is not the primary suspected cause. It is
consistent with a decoder receiving predictive frames after an incomplete IDR,
receiving parameter sets from the previous source, or receiving packets from
old and new sources during the same transition.

The gateway already provides RTP reordering, complete H.264 access-unit
normalization, SPS/PPS caching and injection, SIP-directed FIR/PLI recovery,
and a switch transition hold. The remaining weakness is that the current hold
may release when it sees only the start of an IDR or IDR FU-A. A later missing
fragment can cause the normalizer to discard that IDR after the hold has
already opened. Subsequent P-frames may then reach a decoder without a valid
reference frame. The normalizer also preserves cached parameter sets across a
source reset, which is unsafe when queue video and Linphone video use different
H.264 encoder parameters.

## Goals

- Prevent corrupted or incorrectly colored video during queue-to-Linphone
  source switches.
- Release switched video only after a complete, decodable H.264 random-access
  point is available.
- Keep the previously rendered queue frame visible while the new source is
  being validated.
- Preserve H.264 passthrough; do not introduce transcoding.
- Preserve existing WebSocket and SIP contracts.
- Keep audio forwarding independent and uninterrupted.
- Recover when RTPengine preserves the same SSRC across the source switch.
- Produce diagnostics that distinguish missing keyframes, stale parameter
  sets, packet loss, reordering, and RTCP feedback-routing problems.

## Non-Goals

- Removing or bypassing RTPengine in production.
- Changing codecs, resolutions, or client rendering behavior.
- Refactoring unrelated SIP call control or media forwarding.
- Guaranteeing a fixed switch latency when the upstream encoder does not
  produce a valid IDR.
- Adding retransmission protocols or transcoding not already supported by the
  SIP and WebRTC endpoints.

## Considered Approaches

### 1. Gateway complete-IDR transition gate (selected)

Gate the new video generation until the access-unit normalizer confirms a
complete IDR and fresh SPS/PPS. This directly prevents undecodable P-frames
from reaching all client types and builds on existing gateway components.

Trade-off: the previous frame can remain visible for several hundred
milliseconds longer while the gateway waits for a safe release point.

### 2. RTCP and configuration tuning only

Use a dual RTP/RTCP feedback target and tune FIR/PLI timing. This can reduce
time to the first keyframe but cannot guarantee that every fragment of that
keyframe arrives. It is useful as recovery and diagnosis, not as the primary
correctness boundary.

### 3. Upstream-only RTPengine/Asterisk changes

Require an immediate IDR, prevent old/new packet overlap, and verify RTCP
forwarding in infrastructure. These changes may improve the source but depend
on deployment-specific components and endpoints. The gateway still needs to
protect WebRTC decoders from an incomplete access unit.

## Architecture

The complete access unit, not preliminary packet inspection, becomes the
switch release boundary:

`@switch -> reset generation state -> request FIR/PLI -> reorder RTP -> normalize H.264 AU -> validate fresh SPS/PPS + complete IDR -> release to WebRTC`

The gate is session-scoped and active only for a switch generation. Calls that
never receive `@switch` retain their existing media behavior.

The existing switch generation is authoritative even if RTPengine keeps the
same SSRC. An SSRC change remains a useful additional reset signal but is not
required for correct transition isolation.

## State Machine

### `Forwarding`

The gateway forwards normalized video normally. No switch gate is active.

### `AwaitingFreshIDR`

Receiving a non-duplicate `@switch` starts a new generation. The gateway:

1. Resets pending packets in the video reorder buffer.
2. Drops any partial H.264 access unit from the prior generation.
3. Clears generation-scoped SIP SPS/PPS so parameter sets from queue video
   cannot be injected into Linphone video.
4. Continues receiving and analyzing the new RTP stream but emits no video
   access units to the WebRTC track.
5. Sends an immediate FIR followed by a guarded PLI recovery sequence.
6. Leaves audio forwarding unchanged.

Because no new video packets are emitted, clients remain eligible to display
the last decoded queue frame rather than corrupted new frames.

### `CandidateIDR`

An access unit is a release candidate only when all of these conditions hold:

- It belongs to the active switch generation.
- RTP reordering and H.264 normalization have completed the access unit.
- No required FU-A fragment is missing.
- The access unit contains an IDR.
- Fresh SPS and PPS for the active generation are available, either received
  before the candidate or included in it.
- The gateway can rewrite outbound sequence numbers and timestamps onto its
  existing continuous WebRTC timeline.

A bare IDR NAL or FU-A start packet is insufficient to release the gate.

### Release to `Forwarding`

The gateway atomically emits fresh SPS/PPS followed by the complete IDR access
unit, then releases later normalized access units. The release records wait
duration, generation, source SSRC, feedback attempts, and whether parameter
sets were injected.

## Parameter-Set Ownership

SPS/PPS used for a gated release must be generation-scoped. A source reset
caused by `@switch` clears the parameter sets eligible for release. The gateway
may cache new parameter sets as individual NAL units, in STAP-A, or inside the
candidate access unit.

Parameter sets from the previous source may remain available only for
historical diagnostics; they must not be injected into the new generation.
This prevents an IDR from being decoded with incompatible resolution, profile,
level, or picture parameter references.

## RTP and Timeline Handling

- The reorder buffer resets when the switch generation changes, even when SSRC
  remains unchanged.
- Packets buffered for a previous generation are discarded.
- Normalization continues to emit complete timestamp-grouped access units.
- The gateway preserves a continuous outbound RTP sequence-number and timestamp
  space toward WebRTC. It must not expose source discontinuities directly to
  the browser or mobile receiver.
- Existing RTP packet caching for browser-originated NACK remains aligned to
  rewritten outbound sequence numbers.

## Recovery and Failure Policy

Recovery timing is approximate and must reuse the existing throttling and
session cancellation mechanisms:

- Immediately on switch: send FIR and begin the guarded PLI sequence.
- Around 800 ms without a valid candidate: send another throttled PLI.
- Around 1.5 seconds: escalate to FIR plus PLI.
- From 2 to 5 seconds: log a stalled transition with RTP and AU statistics and
  use the configured feedback fallback targets.

The gate must not fail open by forwarding P-frames without a valid IDR. Doing
so recreates the corruption this design prevents. If recovery is delayed, the
gateway keeps audio active, keeps accepting and validating video, and releases
immediately when a valid candidate arrives. Recovery requests remain bounded
and throttled; the gateway must not create an RTCP storm.

Session teardown, a newer switch generation, or call termination cancels the
current gate and its scheduled recovery work. No hot-path error may panic or
end an otherwise healthy audio call.

## Observability

Add high-signal, session-identified events or structured logs:

- `switch_video_gate_start`: generation, source transition reason, previous
  SSRC, current feedback transport.
- `switch_video_au_rejected`: generation and bounded reason such as
  `incomplete-fua`, `missing-fresh-sps`, `missing-fresh-pps`, or
  `stale-generation`.
- `switch_video_gate_release`: wait time, generation, SSRC, IDR packet count,
  parameter-set injection, FIR count, and PLI count.
- `switch_video_gate_stalled`: elapsed time, gaps, missing packets,
  out-of-order packets, reorder timeouts, incomplete-AU count, last keyframe
  age, and feedback targets used.

Repeated rejection logs must be throttled to avoid per-packet log volume. The
existing periodic SIP-to-WebRTC video statistics remain the source for RTP
disorder measurements.

Production diagnosis should compare packet captures on both RTPengine legs:

- Asterisk to RTPengine
- RTPengine to Gateway

The comparison should identify first fresh SPS/PPS time, first complete IDR
time, sequence gaps, out-of-order delivery, timestamp spacing, and whether
Gateway FIR/PLI reaches Asterisk. This diagnosis does not require bypassing
RTPengine.

## Configuration

The correctness rule is enabled with H.264 AU normalization and should not
depend on a new client setting. Existing switch recovery window, stable-window,
keyframe watchdog, and feedback transport settings remain the initial control
surface.

The current deployment uses `SIP_VIDEO_FEEDBACK_TRANSPORT=auto`. Changing it to
`dual` is a deployment experiment only after packet capture or logs show that
feedback sent to the learned RTCP target is not consistently reaching
Asterisk. The design does not require a production RTPengine bypass.

## Testing

Focused gateway tests must cover:

1. A complete IDR with fresh SPS/PPS releases the gate and emits parameter
   sets before IDR packets.
2. An IDR FU-A missing any required fragment does not release the gate.
3. P-frames following a rejected IDR remain gated.
4. SPS/PPS cached from queue video cannot satisfy the Linphone generation.
5. Fresh SPS/PPS arriving before the IDR can satisfy the same generation.
6. A source switch with unchanged SSRC still resets reorder, AU, and
   generation-scoped parameter-set state.
7. Packets and timer callbacks from an older generation cannot release a newer
   gate.
8. Reordered but complete IDR fragments release only after normalization.
9. Recovery escalation is throttled and stops on release, termination, or a
   newer switch.
10. A stalled gate leaves audio forwarding and session state healthy.
11. Calls without `@switch` retain existing normalized video behavior.
12. Browser NACK cache sequence numbers match the rewritten outbound RTP
    sequence numbers after release.

Verification after implementation must include focused package tests followed
by `go test ./...` from `apps/gateway`. A production-like interop test should
switch queue video to Linphone video through Kamailio/RTPengine and confirm
that clients retain the old frame until clean live video appears, with no
block corruption or incorrect colors.

## Rollout and Rollback

Roll out to a controlled environment using the existing RTPengine topology.
Use gate release time, rejection reasons, RTP disorder statistics, and client
diagnostics to compare before and after behavior.

Implementation should keep the existing packet-level transition policy behind
a gateway-only rollback setting or a narrowly isolated code path until the new
gate is verified. Rollback must not require WebSocket, client, database, or SIP
contract changes.

Success means:

- No visible block corruption or incorrect colors during queue-to-Linphone
  transitions in tested browser, Android, and iOS clients.
- Audio remains uninterrupted.
- Clean live video appears as soon as a complete new-generation IDR and fresh
  SPS/PPS are available.
- Delayed recovery is observable without unbounded FIR/PLI traffic.
