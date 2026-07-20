## 1. Session notify plumbing

- [x] 1.1 Add per-session dedupe flags for remote video/audio media-ready notifies
- [x] 1.2 Add API `NotifyRemoteMedia(sessionID, kind, direction, state)` that sends WS `type: media` and logs the emit
- [x] 1.3 Wire session→API callback (same pattern as state / mid-call notifiers) so RTP path can notify without importing api

## 2. Emit on decode-ready remote video

- [x] 2.1 Hook SIP→WebRTC video path at first complete IDR (with SPS/PPS available) to call remote video ready once
- [x] 2.2 Ensure SSRC-learn alone does not emit media ready
- [x] 2.3 Keep media path non-blocking if WS client missing or send buffer full

## 3. Optional remote audio ready

- [x] 3.1 Emit `kind: audio` `direction: remote` `state: receiving` once on first accepted SIP audio RTP toward WebRTC
- [x] 3.2 Dedupe audio notify per session

## 4. Contract and tests

- [x] 4.1 Update `docs/gateway/ws-contract.md` with `media` message and note that `active` ≠ remote video receiving
- [x] 4.2 Add unit/API tests: first IDR emits once; duplicate IDR does not re-emit; no client is non-fatal
- [x] 4.3 Run focused `go test` for `internal/session` and `internal/api`

## 5. Client follow-on (coordinated)

- [x] 5.1 softphone-kmp-sdk: decode `type: media` and expose event (e.g. remote media receiving)
- [x] 5.2 test-call / RN / gateway UI: use event for “remote video available” UX without changing SIP `active` meaning
- [x] 5.3 Manual verify: Asterisk auto-answer → `active` early, then `media` video receiving when Linphone video arrives
