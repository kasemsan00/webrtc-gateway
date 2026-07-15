# Final whole-branch review fixes

## Changes

- Added `SnapshotWebRTCVideoEgressSSRC` with Ensure semantics and changed normalized access-unit writes to use one snapshotted egress SSRC for every packet in the AU.
- Added `ClearVideoRTPHistory` and call it from the locked egress SSRC remap path so accepted switches invalidate cached packets from the prior SSRC.
- Added regression coverage for an in-flight multi-packet AU remap, retransmission-history invalidation, and a second accepted switch after the debounce window.
- Preserved SIP `RemoteVideoSSRC`, audio state, WebSocket behavior, and the complete-IDR gate flow.

## Tests

- RED: `go test ./internal/sip ./internal/session -run "TestWriteNormalizedVideoAccessUnitUsesSingleWebRTCEgressSSRC|TestRemapWebRTCVideoEgressSSRCClearsRTPHistory"` — failed on mixed AU SSRC and retained cached packet as expected.
- PASS: `go test ./internal/sip ./internal/session -run "TestWriteNormalizedVideoAccessUnitUsesSingleWebRTCEgressSSRC|TestRemapWebRTCVideoEgressSSRCClearsRTPHistory|TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC"` — passed.
- PASS: `go test ./...` from `apps/gateway` — all packages passed.
- PASS: IDE lint diagnostics for the six changed Go files — no errors.
- NOT RUN: focused `go test -race` — Windows Go reported `-race requires cgo; enable cgo by setting CGO_ENABLED=1`.

## Commits

- `2bfcb5f fix(gateway): prevent stale packets across video SSRC remaps`

## Remaining Important finding follow-up

### Changes

- Made `CacheVideoRTPPacket` parse the outbound RTP SSRC and hold the session read lock through the history insert, so a remap cannot clear history and then be followed by an old-SSRC insert.
- Made cached-packet lookup reject malformed RTP and packets whose SSRC differs from the current `WebRTCVideoEgressSSRC`; `RetransmitVideoNACK` revalidates immediately before writing.
- Extended the in-flight multi-packet AU regression to inspect history after the first-write remap, and added stale-cache/stale-retransmit coverage.
- Preserved SIP `RemoteVideoSSRC` semantics.

### Tests

- RED: focused regression run failed because the in-flight AU repopulated sequence 100 with SSRC 7777 and stale cache/retrieval tests returned SSRC 1111.
- PASS: `go test ./internal/sip ./internal/session -run "TestWriteNormalizedVideoAccessUnitUsesSingleWebRTCEgressSSRC|TestVideoRTPHistoryRejectsPacketFromStaleEgressSSRC|TestGetCachedVideoRTPPacketRejectsHistoryFromStaleEgressSSRC|TestRemapWebRTCVideoEgressSSRCClearsRTPHistory"`.
- PASS: `go test ./...` from `apps/gateway`.
- PASS: IDE lint diagnostics for the three changed Go files.

### NACK retransmit TOCTOU follow-up

#### Changes

- Changed `videoRTPHistoryMu` to `sync.RWMutex`; `RetransmitVideoNACK` now holds `mu.RLock` + `videoRTPHistoryMu.RLock` from cache lookup and egress SSRC validation through `VideoTrack.Write`.
- `ClearVideoRTPHistory` keeps exclusive `Lock`, so remap blocks until an in-flight retransmit finishes and cannot clear history mid-write.
- Documented lock ordering in `RetransmitVideoNACK`; `getCachedVideoRTPPacket` uses `RLock` for read-only lookups.

#### Tests

- PASS: `go test ./internal/session -run "TestRetransmitVideoNACK|TestGetCachedVideoRTPPacket|TestRemapWebRTCVideoEgressSSRCClearsRTPHistory|TestVideoRTPHistory"`.
- PASS: `go test ./...` from `apps/gateway`.
- Added `TestRetransmitVideoNACKRejectsStaleSSRCAfterRemap` for sequential stale-history-after-remap coverage.

#### Commits

- `6916153` fix(gateway): serialize NACK retransmit against SSRC remap
