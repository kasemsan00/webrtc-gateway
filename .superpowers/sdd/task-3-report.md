# Task 3 Report — Apply egress SSRC on SIP→WebRTC write paths

## What I implemented

Two files changed per the task brief:

1. **`apps/gateway/internal/sip/rtp.go`**
   - **`writeNormalizedVideoAccessUnit`**: Call `sess.ApplyWebRTCVideoEgressSSRC(packet)` before `Marshal()` in the packet loop so normalized AU writes and cache entries use egress SSRC.
   - **Legacy reorder callback** (`auNormalizer == nil`): Unmarshal → apply egress SSRC → marshal → cache post-remap bytes → `VideoTrack.Write(out)`.
   - **Main loop pre-reorder path** (`auNormalizer == nil`): Apply egress SSRC to parsed packet, cache marshaled bytes, push remapped `egressData` into reorder buffer (satisfies global "cache stores post-remap bytes" constraint).
   - **Malformed-bypass branch** (`auNormalizer == nil`, unmarshal failed in main path): Same unmarshal/apply/marshal/cache/write pattern; skips write if RTP cannot be parsed.

2. **`apps/gateway/internal/sip/rtp_video_gate_test.go`**
   - Added `TestWriteNormalizedVideoAccessUnitUsesWebRTCEgressSSRC` verifying written RTP carries pre-set `WebRTCVideoEgressSSRC=7777` instead of fixture SSRC 4242.

No changes to `RemoteVideoSSRC`, RTCP feedback targeting, or gate evaluation logic.

## TDD evidence

### RED — Step 2

```
$ go test ./internal/sip -run TestWriteNormalizedVideoAccessUnitUsesWebRTCEgressSSRC -count=1
--- FAIL: TestWriteNormalizedVideoAccessUnitUsesWebRTCEgressSSRC (0.00s)
    rtp_video_gate_test.go:105: expected egress SSRC 7777, got 4242
FAIL	k2-gateway/internal/sip	1.227s
```

### GREEN — Step 4

```
$ go test ./internal/sip -run "TestWriteNormalizedVideoAccessUnit" -count=1
ok  	k2-gateway/internal/sip	1.210s

$ go test ./internal/session -run "TestPrepareSwitchVideoTarget|TestEnsureWebRTCVideoEgressSSRC|TestRemapWebRTCVideoEgressSSRC|TestApplyWebRTCVideoEgressSSRC" -count=1
ok  	k2-gateway/internal/session	1.063s

$ go test ./internal/sip -run "Switch|switch" -count=1
ok  	k2-gateway/internal/sip	2.466s
```

## Commits

- `d0b6389` feat(gateway): rewrite SIP-to-WebRTC video SSRC on egress writes
  - 2 files changed (rtp.go, rtp_video_gate_test.go)

Only the two brief-listed files were staged.

## Self-review

- `ApplyWebRTCVideoEgressSSRC` called only from unlocked RTP write paths (acquires `s.mu` internally) — no deadlock risk.
- All SIP→WebRTC video egress branches covered: normalized AU loop, legacy reorder flush, main-loop cache/reorder push, malformed bypass.
- `CacheVideoRTPPacket` stores post-remap marshaled bytes on all legacy and normalized paths.
- `RemoteVideoSSRC` learning and RTCP feedback paths untouched.
- Complete-IDR gate behavior preserved: `ApplyWebRTCVideoEgressSSRC` runs only after `EvaluateSwitchVideoAccessUnit` returns `Emit=true`.
- Malformed bypass no longer writes raw unparsed bytes; only writes when unmarshal succeeds (stricter than prior raw passthrough).

## Concerns

- Main-loop `egressData` remap (pre-reorder cache + push) was added beyond the brief's explicit snippets to satisfy the global "cache stores post-remap bytes" constraint; reorder callback still applies idempotent remap on flush.
- Malformed-bypass branch silently drops packets that fail RTP unmarshal (previously wrote raw bytes); acceptable for legacy rollback path but behavior change for corrupt RTP.
