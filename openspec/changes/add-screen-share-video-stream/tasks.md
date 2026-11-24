## 1. Asterisk spike (no gateway code)

- [ ] 1.1 Set `max_video_streams = 2` and `direct_media = no` on two test `chan_pjsip` endpoints; capture a SIP trace of a re-INVITE that adds a second `m=video` through a `Dial()` bridge and confirm Asterisk re-INVITEs the other leg
- [ ] 1.2 Repeat through `Queue()`; record the m-line order Asterisk offers to the second leg
- [ ] 1.3 Record what the first leg sees when the second leg answers the new m-line with port 0
- [ ] 1.4 Send RTCP PLI on the second video stream from one leg and record whether it reaches the other leg on the matching stream
- [ ] 1.5 Confirm in-dialog MESSAGE (`@pli` path) is relayed between the two legs in both bridges
- [ ] 1.6 Send the same three-m-line re-INVITE to the deployed `chan_sip` version and record the exact response
- [ ] 1.7 Write the findings into `design.md` Open Questions and adjust decisions 6 and 7 if any assumption fails

## 2. SIP SDP slot table (ships on its own)

- [x] 2.1 Add golden tests that pin today's SDP bytes for outbound offer, inbound answer and hold, for audio-only and audio-plus-video sessions
- [x] 2.2 Add an ordered media-slot model on the session and lay the gateway's SDP out on it; keep the 2.1 golden tests byte-identical
- [x] 2.3 Make `parseSDPMediaEndpoints` and the other primary-stream parsers read the first audio and first video m-lines only
- [x] 2.4 Make `ValidateMidCallSDP` report each video m-line separately so a closed second m-line is not "video removed"
- [x] 2.5 Build INVITE, re-INVITE and UPDATE answers from the offer's m-lines: same count and order, port 0 for unused lines, unknown media types declined in place
- [x] 2.6 Follow a peer offer that has fewer m-lines than the current table
- [x] 2.7 Tests: three-m-line offer does not move the camera destination; answer has three m-lines; shrinking offer is answered with two
- [x] 2.8 Run `go test ./...`
- [ ] 2.9 Run the changed packages under `go test -race` (needs a C compiler; not available on the dev machine used for 2.1–2.8)
- [ ] 2.10 Place one live call to the echo extension on a deployed build, including hold and resume

## 3. Auxiliary video pipeline

- [x] 3.1 Add `internal/session/aux_video.go`: `AuxVideo` struct (sockets, SSRC/seq, SPS/PPS cache, local track, mutex), open/close, and teardown from `CloseMediaTransports`
- [x] 3.2 Add a pair allocator (`bindRTPPortPair` in `internal/sip/rtp_aux_video.go`) that takes one pool slot and binds RTP and RTCP
- [x] 3.3 Uplink forwarder (`aux_video_uplink.go`): header rewrite, extension strip, parameter-set cache, hold until first IDR, SPS/PPS injection, STAP-A de-aggregation, packetization-mode 0 conversion
- [x] 3.4 Downlink (`aux_video_downlink.go`, readers in `internal/sip/rtp_aux_video.go`): remote SSRC learning, reorder buffer, write to the auxiliary track, RTP history for NACK. The peer's address comes from SDP only; it is not learned from RTP
- [x] 3.5 Feedback: browser PLI/FIR/NACK on the auxiliary track to SIP RTCP with the auxiliary SSRC, and SIP PLI/FIR to the browser for the auxiliary track
- [x] 3.6 Route `OnTrack` by transceiver mid so only the auxiliary video track reaches the auxiliary forwarder (first PeerConnection and replacement PeerConnection); detach the auxiliary track before a PeerConnection closes or is replaced
- [x] 3.7 Tests: uplink gate and parameter-set injection, STAP-A, mode 0, camera and auxiliary uplinks keep separate ports, SSRCs and parameter sets, feedback paths, write errors, lifecycle, `OnTrack` routing over a real PeerConnection pair, pair allocator and socket readers
- [ ] 3.8 Run `internal/session` and `internal/sip` under `go test -race` (needs a C compiler; not available on the dev machine used for 3.1–3.7)

## 4. Client-originated add and remove

- [x] 4.1 Add `multiVideo` to `WSMessage` and `WSClient`; accept it on `agent_register`, `device_register` and `client_state`; refuse on `/ws-public`
- [x] 4.2 Add `stream_update` to `ws_dispatch.go` allowlists for `/ws`, `/ws-agent`, `/ws-agent-device` with session access checks (`internal/api/ws_stream.go`)
- [x] 4.3 Add the outbound stream re-INVITE in `internal/sip/aux_stream.go`, sharing one mid-call offer builder with hold, with one 491 retry
- [x] 4.4 Implement add: single-flight claim, port bind, re-INVITE, then in-place `SetRemoteDescription`/`CreateAnswer` (`AnswerClientOfferInPlace`) and `stream_update_result`
- [x] 4.5 Implement rejected and failed outcomes (port 0, non-2xx, answer that does not mirror the offer, transport error, no ports, busy, inactive or held session, audio-only call) with port release
- [x] 4.6 Add the per-account negative cache (10 minutes) for own-leg rejections
- [x] 4.7 Implement remove and SDP-less re-add
- [x] 4.8 Tests with SIP fixtures for accept, port 0, 488, 603, 606, 503, 408, 491-then-accept, 491 twice, transport error; in-place renegotiation over a real PeerConnection pair; handler validation, single flight and endpoint policy
- [x] 4.9 Keep an open stream across an inbound re-INVITE or UPDATE (answer its m-line, follow the peer's address) and end it with `stream_state` when the peer closes or drops the m-line
- [x] 4.10 Document `multiVideo`, `stream_update`, `stream_update_result` and `stream_state` in `docs/gateway/ws-contract.md`
- [ ] 4.11 Live check of the sharing leg on a deployed build: add and remove against the echo extension on `chan_sip` (expect `not_supported`, call unaffected) and on a `chan_pjsip` endpoint with `max_video_streams = 2`

## 5. Peer-originated add and far-end confirmation

- [x] 5.1 Prove in a focused test that pion pairs a pre-added `sendonly` video transceiver with the new m-line of a client offer (`aux_video_downlink_track_test.go`: the client receives the screen as a second remote track)
- [x] 5.2 In `handleActiveReINVITE` and `handleUPDATE`, open the auxiliary slot when the second `m=video` is offered and the bound client opted in; otherwise answer port 0. Answer the m-line with the offer's own transport and payload type
- [x] 5.3 Send `renegotiate` with `reason: stream_added`; apply the client offer from `renegotiate_answer` in place and return the answer in `renegotiate_result` (`internal/api/ws_stream_receive.go`)
- [x] 5.4 Report ready/unsupported from the receiving leg to the sharing leg: directly between paired sessions on one gateway, as a PBX-routed `@screen:` MESSAGE otherwise; consume `@screen:` bodies in `handleMESSAGE` without delivering them as chat (`internal/sip/aux_stream_peer.go`). In-dialog MESSAGE alone was dropped: Asterisk does not relay it between legs
- [x] 5.5 On the sharing side, wait up to 3 seconds for confirmation; emit `stream_state`; close the slot on `unsupported` or timeout
- [x] 5.6 Emit `stream_state active:false` when an inbound re-INVITE closes the auxiliary m-line (done with 4.9)
- [x] 5.7 Request a keyframe once the receiving track is attached: RTCP toward the SIP peer, plus a direct request to the paired sharing session
- [x] 5.8 Tests: both legs on one gateway from share to stop, receiver without opt-in, silent far end, confirmed stream survives the timeout, client decline/no offer/unusable offer/timeout, PeerConnection instance unchanged, control MESSAGE consumed and routed, second share without renegotiation
- [x] 5.9 Refuse `stream_update add` while the far end is sharing (`peer_sharing`), and a second `m=video` on the initial INVITE

## 6. Interaction with existing mid-call features

- [x] 6.1 Hold/unhold: set direction per slot from the slot table; keep the auxiliary m-line (done with 4.3: hold and stream offers use the same builder)
- [x] 6.2 Resume: carry the stream to the replacement PeerConnection when the offer has a second video m-line (routing mid for a sharing session, send-only transceiver for a receiving one); otherwise end it and emit `stream_state` with `reason: resume`
- [x] 6.3 `@switch`: close the stream on acceptance, on either side, and emit `stream_state` with `reason: agent_switch`
- [x] 6.4 Every end-of-call path releases the auxiliary sockets: all of them go through `CloseMediaTransports` (BYE, hangup, cancel, reject, failed setup, ICE end, session delete and cleanup), which closes the stream
- [x] 6.5 Log add, remove, rejection and confirmation as session events with the session ID (`sip_aux_stream_*`, `ws_stream_*`)
- [x] 6.6 Tests for hold-while-sharing, resume outcomes, switch on either side and teardown releasing the ports
- [x] 6.7 End-to-end resume while sharing: the real resume path against a client PeerConnection, for a sharing session, a receiving session, and an offer without the screen section (`aux_video_resume_test.go`)

## 7. Contract and docs

- [x] 7.1 Update `docs/gateway/ws-contract.md`: `multiVideo`, `stream_update`, `stream_update_result`, `stream_state`, `renegotiate` `stream_added`, resume rules
- [x] 7.2 Update `docs/gateway/config-reference.md` with the RTP port capacity note and the Asterisk endpoint settings
- [x] 7.3 Add a troubleshooting entry for `not_supported` and `peer_not_supported`
- [x] 7.4 Update `apps/gateway/AGENTS.md` file map and `CHANGELOG.md`
- [x] 7.5 Run `go test ./...`
- [ ] 7.6 Run `go test -race ./internal/session ./internal/sip ./internal/api` (needs a C compiler; the dev machine has none, and neither WSL nor a local Docker image provides Go with gcc)

## 8. Client follow-on (separate repositories)

- [ ] 8.1 `webrtc-gateway-sdk`: codec for the new messages, `multiVideo` opt-in, sender flow with offer rollback on `rejected`, second remote track event
- [ ] 8.2 Android app: MediaProjection capture with a foreground service, screen track with resolution-preserving degradation and bitrate/frame-rate caps
- [ ] 8.3 Electron agent app: second video view for the screen track, driven by `stream_state`
- [ ] 8.4 End-to-end check on `chan_pjsip` (two tracks visible) and on `chan_sip` (share refused, call unaffected)
