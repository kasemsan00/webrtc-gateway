## 1. Stop false ICE→active promotion

- [x] 1.1 In `session.go` ICE connected handler, do not set `StateActive` when current state is `connecting` (initial outbound/inbound setup before SIP answer)
- [x] 1.2 Keep ICE connected → `StateActive` only when recovering from `reconnecting`
- [x] 1.3 Preserve existing FIR/PLI startup recovery on ICE connected without tying it to call-progress `active`
- [x] 1.4 Add/adjust unit test covering ICE connected while `connecting`/`ringing` does not become `active`

## 2. Forward full SIP progress to WebSocket

- [x] 2.1 Widen `notifySessionStateChange` in `sip/server.go` to forward `connecting` and `ringing` (in addition to `active`/`ended`)
- [x] 2.2 Deduplicate consecutive identical state notifies per session
- [x] 2.3 On SIP 180/183 in `sip/call.go` (and auth path), ensure `StateRinging` + notify reaches the client
- [x] 2.4 On SIP 200 OK, keep `StateActive` + notify as the sole answer signal for outbound
- [x] 2.5 Optionally emit `type: "ringing"` once per transition into ringing (`ws_notify.go` / SIP notify path)

## 3. Fix post-`call` state acknowledgment

- [x] 3.1 In `ws_call.go`, after accepting `call`, send explicit `state: connecting` (or current SIP progress if already `ringing`/`active`), never a raw ICE-promoted `active` snapshot
- [x] 3.2 Ensure MakeCall still sets/notifies `connecting` when INVITE is sent without racing a false early `active` to the client

## 4. Observability and contract docs

- [x] 4.1 Log session-scoped line when sending call-progress WS messages (state/type + trigger reason)
- [x] 4.2 Update `docs/gateway/ws-contract.md` with outbound progress semantics (`connecting`/`ringing`/`active`/`ended`, optional `ringing` type, ICE ≠ answered)

## 5. Verification

- [x] 5.1 Add API/SIP tests: call after ICE connected does not emit premature `active`; 180 emits `ringing`; 200 emits `active`
- [x] 5.2 Run focused `go test` for affected packages (`internal/session`, `internal/sip`, `internal/api`)
- [x] 5.3 Manual check: outbound to Linphone shows client `connecting` → `ringing` while ringing, `active` only after Accept; video still works
