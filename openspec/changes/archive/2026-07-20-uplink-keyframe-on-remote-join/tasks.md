## 1. Session dedupe helper

- [x] 1.1 Add a per-session flag (e.g. `uplinkKeyframeKickOnRemoteJoinDone`) with a thread-safe claim helper that returns true only on the first remote-join kick
- [x] 1.2 Unit-test claim-once vs subsequent claims for the same session

## 2. SSRC-learn uplink kick

- [x] 2.1 In `apps/gateway/internal/sip/rtp.go` SSRC-learn recovery goroutine, after claiming the first-join kick, send `SendFIRToWebRTC` plus a short bounded `SendPLItoWebRTC` burst (do not block the RTP read loop)
- [x] 2.2 Preserve existing Asterisk-directed FIR/PLI recovery on SSRC learn unchanged
- [x] 2.3 Log `uplink_keyframe_kick reason=remote-ssrc-learn` (or equivalent) when the kick runs

## 3. Regression coverage

- [x] 3.1 Add a focused test that first remote SSRC learn triggers browser FIR/PLI kick exactly once
- [x] 3.2 Add a focused test that a later SSRC change does not re-arm the first-join kick
- [x] 3.3 Run `go test` for affected packages (`./internal/sip/...`, `./internal/session/...` as needed)

## 4. Manual verification

- [x] 4.1 Reproduce late Linphone answer (>15s): confirm kick log near remote SSRC learn and subsequent uplink `KEYFRAME (Forwarded)` after the periodic PLI window
- [x] 4.2 Confirm fast answer (<10s) still shows video on Linphone and no RTCP storm beyond the short burst
