# Task 2 Report — Remap on accepted `@switch` only

## What I implemented

Two files changed per the task brief:

1. **`apps/gateway/internal/session/switch_target.go`** — In `PrepareAndActivateSwitchVideoTarget`, after accepted-switch mutations (`SwitchGeneration++`, target fields, duplicate count reset) and before `clearSIPVideoParameterSetsLocked`, call `s.remapWebRTCVideoEgressSSRCLocked("accepted-switch")` while already holding `s.mu.Lock()`. Duplicate-ignore early return is unchanged.

2. **`apps/gateway/internal/session/switch_target_test.go`** — Added `TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC` verifying:
   - Accepted switch remaps `WebRTCVideoEgressSSRC` away from the pre-switch value
   - `RemoteVideoSSRC` (SIP) stays unchanged
   - Duplicate `@switch` inside debounce does not remap again

No changes to SIP `RemoteVideoSSRC`, audio SSRC, or WS contracts.

## TDD evidence

### RED — Step 2 (egress SSRC not remapped yet)

```
$ go test ./internal/session -run TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC -count=1
--- FAIL: TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC (0.00s)
    switch_target_test.go:173: expected remapped egress SSRC, got 2222
FAIL	k2-gateway/internal/session	1.081s
FAIL
```

### GREEN — Step 4 (all PrepareSwitchVideoTarget tests)

```
$ go test ./internal/session -run "TestPrepareSwitchVideoTarget" -count=1
ok  	k2-gateway/internal/session	1.175s
```

## Commits

- `250f99e` feat(gateway): remap WebRTC egress SSRC on accepted @switch
  - 2 files changed (switch_target.go, switch_target_test.go)

Only the two brief-listed files were staged.

## Self-review

- Remap runs on every accepted `@switch` path (new-target, debounce-expired, media-generation-changed, debounce-disabled) — all share the post-generation-bump block.
- Duplicate-ignore path returns early before remap; test confirms SSRC stability on duplicate.
- Uses `remapWebRTCVideoEgressSSRCLocked` (not `RemapWebRTCVideoEgressSSRC`) to avoid double-lock.
- Placement after generation bump ensures remap log includes correct `SwitchGeneration`.
- `RemoteVideoSSRC` untouched; only WebRTC egress SSRC changes.
- Existing duplicate/debounce/atomic/gate tests still pass.

## Concerns

None.
