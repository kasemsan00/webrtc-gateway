## Why

Outbound WebRTC clients receive a premature `state: active` when the browser↔gateway ICE path connects—often before Linphone/SIP answers—so UI/SDK treat the call as answered while the far end is still ringing. Real SIP progress (`180`/`183` ringing, `200` answered) is incomplete or filtered on the WebSocket control plane, even though media works once the far end accepts.

## What Changes

- Stop promoting session call state to `active` on WebRTC ICE connected for initial outbound setup; keep ICE-driven recovery for reconnect only.
- Emit reliable outbound call-progress WebSocket signals tied to SIP:
  - `connecting` when outbound INVITE is sent
  - `ringing` on SIP `180`/`183` (and optionally `type: "ringing"` for softphone-kmp-sdk compatibility)
  - `active` only on SIP `200 OK` (answered)
  - `ended` on terminal SIP/failure paths (unchanged intent, ensure it is not shadowed)
- Stop echoing a possibly ICE-promoted `active` snapshot immediately after `call`; send an explicit progress state instead.
- Widen `notifySessionStateChange` so `connecting` and `ringing` reach the client (today only `active`/`ended` are forwarded).
- Add clear gateway logs when call-progress WS messages are sent (today successful `sendWSMessage` is silent).
- Update `docs/gateway/ws-contract.md` to document outbound progress semantics.

## Capabilities

### New Capabilities

- `outbound-call-progress`: WebSocket call-progress signaling for outbound WebRTC→SIP calls, separating media/ICE readiness from SIP answer/ringing semantics.

### Modified Capabilities

- (none — existing specs cover mobile trunk provisioning / ready-to-call WS, not outbound progress)

## Impact

- **Gateway:** `apps/gateway/internal/session/session.go` (ICE state handler), `internal/sip/server.go` (`notifySessionStateChange` filter), `internal/sip/call.go` (180/200 notify), `internal/api/ws_call.go` (post-`call` state echo), `internal/api/ws_notify.go` / `ws_util.go` (logging), tests under `internal/sip` and `internal/api`.
- **Contract:** `docs/gateway/ws-contract.md`; clients that already handle `state` / `ringing` (gateway frontend store, softphone-kmp-sdk) benefit without media-path changes.
- **Not in scope:** RTP/SDP/media forwarding, incoming-call accept/reject, mid-call renegotiation semantics (except ensuring ICE reconnect→`active` behavior stays correct).
