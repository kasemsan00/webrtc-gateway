## Verification evidence

Date: 2026-08-31

### Automated verification

- `go test -count=1 ./internal/sip -run "TestMatchTrunkFromInvite|TestRegistrar|TestHandleLeaseRenewResult|TestInviteMatching|TestHandleINVITE"` passed.
- `go test -count=1 ./...` passed from `apps/gateway`.
- `go vet ./internal/sip` passed.
- `git diff --check` passed.
- `openspec validate "fix-inbound-trunk-origin-matching" --strict` passed after the implementation follow-up.
- `BenchmarkMatchTrunkFromInviteDetailed` with 200 trunks measured 100829-183141 ns/op, 137546 B/op, and 1251 allocs/op across three Windows runs. Allocation count changed by one from the earlier implementation measurement; timings varied with host load.

### Regression scenarios covered

- Gateway Contact Request-URI and To do not act as registrar-domain evidence.
- Domain/port fallback does not select a different SIP username when the INVITE user is known.
- Expired, disabled, unowned, and unregistered trunks are excluded from eligibility.
- Direct non-local Request-URI matching requires an enabled, owned, current registration identity.
- A unique transport-source match wins over conflicting Via or Contact evidence, including source-port relaxation.
- Two indistinguishable current registrations return `503` and do not create a session.
- Every REGISTER operation advances a token; stale completion cannot replace a newer identity.
- Failed initial REGISTER publishes no identity.
- Failed refresh preserves the previous successful identity only while it remains current.
- Lease loss and reconnect invalidate older operation generations.
- INVITE matching does not invoke the registrar resolver.

### Race detector

The local Windows run could not start the Go race detector because CGO requires a C compiler and `gcc` is not installed:

```text
cgo: C compiler "gcc" not found
```

Docker Desktop was not running, so a Linux container fallback was unavailable. Run the following on Linux CI before production rollout:

```bash
go test -race -count=1 ./internal/sip -run "TestMatchTrunkFromInvite|TestRegistrarIdentityConcurrent|TestRegistrarOperation"
```

### Cross-repository call-flow verifier

The verifier ran SDK JVM tests, protocol coverage, and Gateway tests successfully. The overall command still fails on the existing push-expiry contract because push expiration is hard-coded independently from `SIP_INCOMING_RING_TIMEOUT_SECONDS`. That failure is outside this trunk matcher change, but task 7.6 remains incomplete until the verifier is green or the exception is explicitly approved.

### Pending live evidence

The following checks require a deployed Gateway and real Asterisk/media path:

- Capture REGISTER Contact `sip:00025@203.151.21.121:5090` and INVITE source/Via `203.150.245.41:5060` in one trace.
- Confirm the selected trunk is `00025@sipagent.ttrs.or.th:5060` with duplicate stale/unowned rows present.
- Confirm decline and busy return `486`, no-answer returns `480`, remote CANCEL produces `200` to CANCEL and `487` to INVITE, and established hangup sends BYE.
- Complete one accepted call and verify Opus passthrough, H.264-only video, SPS/PPS behavior, RTP/RTCP forwarding, and keyframe recovery.

### Rollout and rollback

1. Deploy to a non-production Gateway instance and inspect the new match diagnostics.
2. Deploy one production instance and confirm `localRURI`, `localTo`, `origins`, `eligible`, `evidence`, and `reason` before broad rollout.
3. Soft-disable obsolete duplicate trunks with `enabled=false`; do not delete rows.
4. Roll back by reverting the matcher and registrar-identity code. No database migration or schema rollback is required.

## Automated re-verification

Date: 2026-09-01

- Re-inspection found and closed the metadata/identity race window: mobile/agent upsert, API update, and direct register cache replacement now invalidate changed registrar identity in the same trunk-manager critical section.
- `loadTrunks` now compares registrar identity fields for retained trunk IDs and atomically invalidates a same-ID snapshot when username, domain, port, or transport changes; unchanged same-ID snapshots remain current.
- REGISTER now copies trunk metadata and advances its operation generation under one lock, so an identity-changing cache update cannot interleave between metadata capture and generation allocation.
- Selected trunk matches now emit the same safe structured logstore diagnostics as ambiguous and rejected matches.
- Added regression tests for atomic cache replacement, same-ID database reload changes, unchanged reload preservation, and selected-match observability.
- `go test -count=1 ./internal/sip` passed.
- `go test -count=1 ./...` passed from `apps/gateway`.
- `go vet ./internal/sip` passed.
- Focused verbose matcher, handler, registrar lifecycle, cache replacement, and reload tests passed.
- `git diff --check` passed.
- `openspec validate "fix-inbound-trunk-origin-matching" --strict` passed.
- `BenchmarkMatchTrunkFromInviteDetailed` with 200 trunks and 100 iterations measured 142409 ns/op, 137596 B/op, and 1251 allocs/op on Windows.
- The race detector remains unavailable on this Windows host: `CGO_ENABLED=0` rejects `-race`, while `CGO_ENABLED=1` fails because `gcc` is not installed. The concurrent lifecycle tests pass without instrumentation; Linux CI must still run the documented `go test -race` command before production rollout.
- The cross-repository call-flow verifier passed SDK JVM tests, protocol coverage, and all Gateway tests, then failed only its pre-existing push-expiry policy check because push expiration is independently hard-coded from `SIP_INCOMING_RING_TIMEOUT_SECONDS`. This is outside the trunk-origin matcher change and leaves the verifier/production portion of task 7.6 open.
- Automated, non-live scope remains 32/32 complete. Tasks 7.1, 7.3, 7.4, and the production/verifier completion in 7.6 remain excluded pending deployed SIP/media evidence or explicit acceptance of the unrelated verifier exception.

### Live accepted-call evidence

Date: 2026-09-01 (Gateway log timestamps are UTC; console timestamps are Asia/Bangkok)

Session: `PjExByCxgm0q`
SIP Call-ID: `26346f8a2ff2065378f897aa0ffba29c@203.150.245.41:5060`
Gateway log: `webrtc-sip-gateway-2026-09-01_03-09-48.log`

#### REGISTER and inbound trunk selection

- At `03:41:14.704Z`, trunk `170` registered to `sipagent.ttrs.or.th:5060` over TCP and advertised Contact `sip:00025@203.151.21.121:5090`.
- At `03:41:40.544Z`, an INVITE arrived with transport source `203.150.245.41:5060`, top Via `203.150.245.41:5060`, Contact `203.150.245.41:5060`, and local Request-URI/To `sip:00025@203.151.21.121:5090`.
- The matcher selected trunk `170`, `sipclient-agent-00025@sipagent.ttrs.or.th:5060`, with `owned=true`, `localRURI=true`, `localTo=true`, normalized source/Via/Contact origins, registrar evidence `sipagent.ttrs.or.th:5060`, owned candidates `[170 725 726]`, and eligible candidates `[170]`.
- The selected rule was `username_only_online`, which is expected because eligibility filtering removed the stale/non-current duplicates before origin disambiguation was needed.
- The Gateway delivered one incoming notification for trunk `170`, sent `100 Trying` and `180 Ringing`, received client accept, sent `200 OK`, received ACK, and entered active state.

This completes task 7.1 and provides live confirmation of task 7.2 behavior.

#### Accepted-call signaling and media

- The SIP offer advertised Opus PT `107` and H.264 PT `96` with `packetization-mode=1` alongside unsupported alternatives. The SIP answer retained only Opus PT `107`, telephone-event PT `101`, and H.264 PT `96` with profile `42E01F` and `packetization-mode=1`.
- SIP-to-WebRTC audio packets arrived from `203.150.245.41:16052` and were payload-type rewritten `107 -> 111`; WebRTC-to-SIP audio arrived as Opus PT `111` and was rewritten `111 -> 107`. No audio codec transcode was observed.
- WebRTC video negotiated H.264 `packetization-mode=1`. The Gateway detected STAP-A, cached SPS/PPS, de-aggregated parameter sets, reinjected SPS/PPS before keyframes, and forwarded H.264 to `203.150.245.41:18206`.
- SIP-to-WebRTC video reached at least 900 packets with `gaps=0`, `missing=0`, `ooo=0`, and `dup=0`. The client received remote video state `receiving`.
- Keyframe recovery learned the remote video SSRC, sent guarded FIR/PLI, received complete IDR responses, and recorded PLI responses. Dedicated audio and video RTCP Sender Reports arrived on ports `16053` and `18207`.
- At `03:41:57.357Z`, the client requested hangup. The Gateway built and sent in-dialog BYE to the stored remote Contact, received `200 OK`, moved the session to ended, and completed cleanup.

This completes task 7.4. The accepted-call and established-BYE portions of task 7.3 passed, but task 7.3 remains open because decline (`486`), busy (`486`), no-answer (`480`), and remote CANCEL (`200`/`487`) were not exercised in this call.

### Remaining live/production work after accepted call

- Task 7.3: run decline, busy, no-answer, and remote-CANCEL cases. Accepted-call and established-BYE behavior already passed.
- Task 7.6: resolve or explicitly accept the unrelated push-expiry verifier failure and record final rollout/rollback approval.
