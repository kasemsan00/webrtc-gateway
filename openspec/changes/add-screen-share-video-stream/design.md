## Context

The gateway bridges one WebRTC PeerConnection to one SIP dialog per session. Both parties of a call are separate sessions, each with its own dialog to Asterisk:

```text
mobile ⇄ gateway session A ⇄ Asterisk (Dial/Queue bridge) ⇄ gateway session B ⇄ agent (web/Electron)
```

Findings in the current code that shape this design:

- **Video state is single-stream throughout.** `VideoTrack`, `VideoRTPConn`, `VideoSSRC` and related fields are referenced about 150 times across 15 files, and `Session` carries well over a hundred fields of switch-gate, recovery and blackout state for that one stream.
- **`OnTrack` sends every video track to the same forwarder** (`forwardRTPToAsterisk`), which writes with the one `VideoSSRC`/`VideoSeq`.
- **SDP parsing is "last `m=video` wins".** `parseSDPMediaEndpoints` (`sip/sdp.go`) and `ValidateMidCallSDP` (`session/midcall.go`) both overwrite the video result on each `m=video`. A second `m=video` would move the camera's RTP destination to the screen port.
- **An inbound re-INVITE is answered with the remembered SDP** (`handleActiveReINVITE`, "sdp=unchanged"). An offer with three m-lines would get an answer with two, which violates RFC 3264.
- **An outbound in-dialog re-INVITE already exists** for hold (`sip/hold.go` `SetHold`), including 491 detection and ACK handling.
- **Mid-call WebRTC changes always replace the PeerConnection** (resume, `@switch`). The client is always the offerer. No flow renegotiates on the live PeerConnection.
- **RTP ports are bound as a quad** (audio RTP/RTCP, video RTP/RTCP) from a slot pool with a step of four.
- **In-dialog SIP MESSAGE control bodies already exist** (`@switch`, `@fir`, `@pli`) and are consumed by `handleMESSAGE`.

Constraints from `apps/gateway/AGENTS.md`: no media regressions, no panics in hot paths, WebSocket and SIP changes are compatibility changes, new files stay under about 600 lines.

## Goals / Non-Goals

**Goals:**

- A caller can add and remove a screen video stream mid-call while the camera and audio keep running.
- The far end receives the screen as a second, separate video track.
- Works with `Dial()` and `Queue()` two-party bridges on `chan_pjsip` with `max_video_streams = 2`.
- Calls on `chan_sip`, or on endpoints that do not allow a second video stream, behave exactly as today; a share request on such a call is refused cleanly.
- No configuration flag: support is detected per call.
- The camera pipeline is not restructured.

**Non-Goals:**

- More than one auxiliary stream, or any role other than `screen`.
- A single-track fallback (replacing the camera picture with the screen) for peers that reject the second stream.
- ConfBridge/SFU rooms, simulcast, bitrate adaptation beyond what the WebRTC leg already does.
- Sharing before the far end has answered (see Decision 7).
- Client SDK and app work (listed as follow-on tasks only).

## Decisions

### 1. A separate auxiliary pipeline, not a generalized `[]VideoStream`

Add `AuxVideo` as a self-contained struct on `Session` with its own sockets, SSRC and sequence state, parameter-set cache, local and remote track references and its own mutex. The camera fields and their code paths are left alone.

The auxiliary pipeline is deliberately simple:

- **Uplink (WebRTC→SIP):** rewrite SSRC, sequence and payload type; strip header extensions; cache SPS/PPS; hold until the first IDR; inject SPS/PPS ahead of an IDR; convert FU-A for packetization-mode 0 peers using the existing `h264Mode0Reassembler`.
- **Downlink (SIP→WebRTC):** learn the remote SSRC and source address; small reorder buffer; write to the auxiliary local track; keep an RTP cache to serve browser NACKs.
- **Feedback:** browser PLI/FIR on the auxiliary track becomes RTCP toward Asterisk with the auxiliary SSRC, and the reverse.
- No switch gate, blackout, access-unit normalizer, proactive PLI scheduler or timestamp rebase.
- Keyframe requests in both directions are limited to one per second, since a screen IDR is large.
- The uplink read loop belongs to the WebRTC track, not to one stream: it outlives a closed stream and feeds the next one opened on the same session, because the transceiver is kept between shares and `OnTrack` does not fire again (Decision 8). Packets read while no stream is open are discarded.
- A NACK from the SIP side is not served; the uplink keeps no history and loss is repaired by keyframes, as on the camera path.

**Alternative:** refactor all video state into a slice of streams. Rejected: it touches every recovery and `@switch` path for a feature that needs two streams, and the regression surface is the whole product.

### 2. SIP SDP is built and parsed from a per-session slot table

Keep `createSDPOffer` as the source of the audio and primary video sections, and lay its output out on an ordered slot list kept on the session: `[audio, video-main, video-aux]`. The layout step (`sip/sdp_layout.go`) places each section at its slot and writes port 0 for slots the gateway does not use. Leaving the template in place is what keeps existing calls byte-identical; the auxiliary section is added to the same layout step later.

- Position is the contract: the first `m=video` is the camera, the second is the screen. `a=content:slides` is added to the screen m-line as a hint but nothing depends on it surviving Asterisk.
- The auxiliary slot is absent until first negotiated. After that the gateway keeps it in every SDP it generates for the dialog (hold, answers), with port 0 when closed.
- The table follows the most recent offer/answer exchange. If the peer later sends an offer with fewer m-lines (some stacks do), the gateway answers with that count and drops the slot.
- Parsers return per-position results. The main audio and video endpoints are read only from the first audio and first video m-lines.
- An offered m-line the gateway does not use is answered with port 0, so the answer always has as many m-lines as the offer.
- The screen m-line uses the same H.264 payload type and packetization mode as the camera m-line, RTP/AVPF when the camera uses it, and direction `sendonly` from the sharing side and `recvonly` in the receiving side's answer.

The initial INVITE and its answer are unchanged: audio plus one video.

### 3. Client-originated add: SIP first, then WebRTC

```text
client → gateway   stream_update {sessionId, role:"screen", action:"add", sdp}
gateway            begin mid-call renegotiation (single flight), bind aux RTP/RTCP pair
gateway → Asterisk re-INVITE with three m-lines
Asterisk → gateway 200 OK
  aux m-line port > 0 → set aux endpoints, apply the client offer in place, create answer
                        → stream_update_result {status:"ok", sdp}
  aux m-line port 0, or a non-2xx final response
                      → release the aux sockets
                        → stream_update_result {status:"rejected", reason:"not_supported"}
```

SIP goes first so that a refusal leaves the PeerConnection untouched; the client rolls back its local offer. A 491 is retried once after a random back-off within the RFC 3261 §14.1 ranges. A timeout or transport error yields `status:"failed"`.

The request is validated before anything is sent: the offer must contain exactly two video m-lines, the session must be active and not on hold, and the dialog must already carry a primary video m-line. The last rule follows from "the first `m=video` is the camera": a screen on an audio-only call would take the camera's position. It is reported as `video_call_required`.

The SIP round trip runs off the WebSocket read loop, guarded by the single-flight claim, so a slow peer or a glare back-off does not stall the connection's other messages.

**Alternative:** apply the WebRTC offer first and tear it down on SIP refusal. Rejected: it needs a second renegotiation on a mobile network to undo a change the peer never accepted.

### 4. Peer-originated add: answer SIP immediately, then ask the client to offer

When an inbound re-INVITE or UPDATE opens a second `m=video` and the bound client has opted in with `multiVideo`:

1. Bind the auxiliary RTP/RTCP pair, answer 200 with the auxiliary m-line `recvonly`, start the SIP-side readers.
2. Put a send-only video transceiver with a new local track on the live PeerConnection. It has no mid yet.
3. Send `renegotiate {reason:"stream_added", role:"screen", sdp:""}`. The client adds a receive-only video transceiver, creates an offer and sends it in `renegotiate_answer`. pion pairs the new remote section with the waiting local transceiver; the gateway answers on the live PeerConnection and returns the answer in `renegotiate_result.sdp`.
4. Attach the track to the stream, which asks the SIP side for a keyframe, and report `ready` to the sharing leg (Decision 6).

SIP is answered without waiting for the client because Asterisk times a re-INVITE out. Screen RTP that arrives before the track is attached is dropped; the keyframe request in step 4 recovers the picture.

The answer's auxiliary section uses the **offer's** transport and H.264 payload type for that m-line, which need not be the camera's. The same builder answers the m-line on the sharing leg when the peer re-offers it.

The stream is refused with port 0, and `unsupported` is reported, when the client has not opted in, no client is bound, the offered m-line has no H.264 or is marked `recvonly`/`inactive`, no RTP port is free, or the same offer already needs a client renegotiation for the primary video. A second `m=video` on the **initial** INVITE is always refused: a share starts after the call is answered (Decision 7).

A later share on the same call needs no WebRTC renegotiation: the section negotiated in step 3 is still on the PeerConnection, so the stream is attached at once and the client gets `stream_state {active:true}`.

A session carries one auxiliary stream. While the far end is sharing, `stream_update add` from this session's own client is answered `peer_sharing`.

The client stays the offerer in every flow, as in resume and `@switch`, so there is no WebRTC glare.

### 5. In-place renegotiation on the live PeerConnection

`renegotiate_answer` for a `stream_added` renegotiation, and `stream_update` with an SDP, call `SetRemoteDescription`/`CreateAnswer` on the current PeerConnection. They must not go through `createReplacementPeerConnection`.

`OnTrack` routes by transceiver: a video track on the transceiver created for the auxiliary m-line goes to the auxiliary uplink forwarder; every other track keeps today's path.

pion cannot roll back a remote offer (its signaling state machine has no transition out of have-remote-offer except an answer). Everything that can be checked is therefore checked before `SetRemoteDescription`, including that the offer has an H.264 codec for the SIP leg's packetization mode. A failure after that point leaves the PeerConnection mid-negotiation until a resume replaces it; the SIP slot is closed again and the client gets `webrtc_renegotiation_failed`.

A client that ignores `stream_added` only misses the screen. The pending renegotiation times out after the existing 10 seconds and the camera is unaffected, unlike `@switch`.

### 6. Far-end confirmation between the two gateway sessions

Asterisk answers the sharing leg on its own, before and regardless of what the other leg does. In a mixed call (sharing side on a prepared `chan_pjsip` endpoint, far side on `chan_sip` or an endpoint limited to one video stream) the sharing side would see success while nobody receives the stream. The receiving leg therefore reports back, and the sharing leg closes a stream that is not confirmed.

The first draft sent the report as an in-dialog SIP MESSAGE. The existing chat code already records why that does not work: Asterisk is a B2BUA and can accept an in-dialog MESSAGE on one leg without forwarding it to the other (`internal/sip/message.go`, `ws_notify.go`). The report uses the two routes chat uses instead:

- **Same gateway instance:** the other leg is found with `session.FindBridgedGatewayPeer`, the pairing chat relay and `@switch` keyframe relay already rely on, and the report is a direct call from the receiving session to the sharing session. No SIP is involved.
- **Otherwise:** a PBX-routed (out-of-dialog) MESSAGE with body `@screen:ready` or `@screen:unsupported`, addressed like session chat. The receiving gateway matches it to the one sharing session of the addressed user that is still waiting for confirmation; with none or several it is dropped. An in-dialog MESSAGE is also accepted and matched by Call-ID.

The receiving leg reports `ready` when its client finished the in-place renegotiation (or at once on a later share that reuses the section), and `unsupported` when it refused the m-line, its client did not opt in, declined, sent no usable offer, or did not answer in time.

The sharing leg waits 3 seconds after its own 200 OK. On `ready` it emits `stream_state {role:"screen", active:true}` and asks its client for a keyframe, since the far end has just attached its track. On `unsupported` or on timeout it closes the m-line with a re-INVITE (port 0) and emits `stream_state {active:false, reason:"peer_not_supported"}`. That closing re-INVITE takes the session's single-flight claim, retrying briefly when another negotiation holds it.

`@screen:` bodies are consumed by the gateway and never delivered to a client as a chat `message`.

A far end that is not a gateway session (a SIP softphone able to show two streams) cannot confirm, so a share toward it is closed after 3 seconds. That is accepted for now: both ends of the target deployment are gateway clients, and failing closed is what makes `peer_not_supported` trustworthy.

The pairing is refused when it is ambiguous (one caller with several calls). The report then falls back to the MESSAGE route.

**Keyframes.** Whether Asterisk forwards RTCP PLI/FIR for a second video stream to the right stream on the other leg is not known. The receiving leg sends that RTCP and, when the pairing is available, also asks the sharing session's client directly. Both paths are limited to one request per second.

**Alternative:** infer delivery from RTCP receiver reports. Rejected: Asterisk generates reports for its own leg whether or not it forwards the stream.

### 7. Sharing requires an answered far end

`stream_update add` is accepted only on an active session. While a caller is still waiting in a queue there is no far-end gateway to confirm, so the confirmation times out and the share is reported as `peer_not_supported`; the user starts sharing after the agent answers. This keeps one rule for every case in v1.

### 8. Remove and re-add without WebRTC renegotiation

`stream_update {action:"remove"}` carries no SDP. The gateway sends a re-INVITE with the auxiliary m-line at port 0, closes the auxiliary sockets and emits `stream_state {active:false}` on both legs (the receiving leg learns it from the inbound re-INVITE). The WebRTC transceivers stay in place; the client detaches its capture track.

A later `add` on the same call may omit the SDP when the transceiver already exists: only the SIP leg is renegotiated.

### 9. Support is decided per call, with a short negative cache

There is no enable flag. Two things gate the feature: the client opt-in (`multiVideo: true`) and the explicit `stream_update` request.

The gateway cannot tell `chan_sip` from `chan_pjsip` from SIP headers, so the first share request on a call probes with the three-m-line re-INVITE. A refusal on the gateway's own leg depends on that SIP account's endpoint configuration, so it is cached per SIP account for 10 minutes; requests on that account during the window are answered `not_supported` without a re-INVITE. Cached refusals are port 0, an answer that does not mirror the offer, and the responses that reject the offered media (415, 488, 501, 603, 606). Any other final error is still reported as `not_supported` for that request but is not remembered, because a 408 or 503 says nothing about the endpoint's capabilities. A far-end refusal is not cached.

**Alternative:** an environment flag or a per-trunk setting. Rejected by the product owner; the opt-in already serves as the kill switch, since a client that stops sending `multiVideo` disables the feature.

### 10. Auxiliary RTP ports

The auxiliary stream binds one RTP/RTCP pair by taking one slot from the existing pool and using its first two ports. This reuses the round-robin allocator and keeps even/odd pairing. A session with a shared screen holds two slots.

### 11. Interaction with existing mid-call features

- **Single flight:** stream add/remove uses `TryBeginMidCallRenegotiation`. A request that arrives while another renegotiation is pending is refused with `status:"failed", reason:"busy"`; an inbound re-INVITE in that window gets 491 as today.
- **Hold/unhold:** hold and stream offers come from one builder (`buildMidCallOffer`). It sets the hold direction on audio and primary video, writes the auxiliary section when the stream is open (`inactive` while held, its own direction otherwise), and port 0 at every other position of the dialog.
- **Inbound offers while sharing:** a re-INVITE or UPDATE from the peer that still carries the second `m=video` with a port is answered with the stream's own section, and the stream follows the address in the offer. An offer that sets that m-line to port 0, or no longer has it, closes the stream and emits `stream_state {active:false, reason:"peer_closed"}`.
- **Resume:** the stream moves to the replacement PeerConnection when the client's resume offer keeps the second video m-line. A sharing session points the track routing at the offer's new mid before the offer is applied; a receiving session puts its send-only transceiver on the new PeerConnection first and attaches the stream after the answer. When the offer has no second video m-line the stream cannot continue: a sharing session closes its SIP m-line, a receiving session reports `unsupported` to the sharing leg, and both emit `stream_state {active:false, reason:"resume"}`.
- **`@switch`:** the stream is closed when a switch is accepted, on whichever side the session is, and `stream_state {active:false, reason:"agent_switch"}` is emitted. The closing re-INVITE runs off the switch handler so it cannot delay video recovery. The user shares again with the new agent.
- **Teardown:** `CloseMediaTransports` and every end-of-call path close the auxiliary sockets and stop its goroutines.
- **Diagnostics:** add, refusal, receive-open, receive-refusal, confirmation and close are stored as session events (`sip_aux_stream_*`, `ws_stream_*`) and logged with the session ID under the `aux_stream` / `aux_video` prefixes.

### 12. WebSocket contract

All additions are additive.

| Direction | Message | Fields |
|---|---|---|
| client → gateway | `agent_register`, `device_register`, `client_state` | optional `multiVideo: true` |
| client → gateway | `stream_update` | `sessionId`, `role:"screen"`, `action:"add"\|"remove"`, optional `sdp` |
| gateway → client | `stream_update_result` | `sessionId`, `role`, `action`, `status:"ok"\|"rejected"\|"failed"`, `reason?`, `sdp?` |
| gateway → client | `renegotiate` | existing shape, `reason:"stream_added"`, `role:"screen"`, empty `sdp`, `requiresAnswer:true` |
| gateway → client | `stream_state` | `sessionId`, `role`, `active`, `reason?` |

`stream_update` is allowed on `/ws`, `/ws-agent` and `/ws-agent-device`, requires session access, and is refused on `/ws-public`.

## Risks / Trade-offs

- **Asterisk does not forward the added stream in a `Queue()` bridge, or does so late** (ASTERISK-29512 reported late re-INVITEs) → verified in the spike before any gateway work; the far-end confirmation turns a silent failure into a reported one.
- **A `chan_sip` version mishandles the probe re-INVITE** (older reports mention 603 instead of 488, and relaying a second stream) → every non-2xx is treated as a refusal, the negative cache limits how often an account is probed, and the behaviour is captured in a SIP trace against the deployed version before release. Residual risk: a version that answers 200 with a malformed body; the answer parser must reject it without touching the main streams.
- **RTCP keyframe requests for the second stream are not forwarded per stream by Asterisk** → the sharing client uses a short keyframe interval for the screen encoder; `@pli`-style MESSAGE requests can be extended with a stream qualifier if the spike shows it is needed.
- **In-dialog MESSAGE is not relayed between the two legs in some dialplans** → the share fails closed after 3 seconds; the spike confirms relay on the target PBX, which `@pli` and `@switch` already rely on.
- **pion does not pair a pre-added `sendonly` transceiver with the new m-line of the client's offer** → proven with a focused test before the receiving flow is built; fallback is to add the track after `SetRemoteDescription` on the transceiver pion created.
- **Port pool exhaustion** → documented capacity note; allocation failure yields `status:"failed", reason:"no_rtp_ports"` and leaves the call untouched.
- **Mobile uplink carries two encodes** → client-side bitrate and frame-rate caps for the screen track; outside the gateway.
- **The SDP hardening runs for every call** → the builder is covered by golden tests that require byte-identical output for the audio-plus-one-video case.

## Migration Plan

1. Ship the SDP hardening (slot table, per-position parsing, m-line count in answers) on its own. Behaviour for existing calls is unchanged; verify with the golden tests and a live call to the echo extension.
2. Ship the auxiliary pipeline and both flows. Nothing activates until a client sends `multiVideo` and `stream_update`.
3. Set `max_video_streams = 2` and `direct_media = no` on the `chan_pjsip` endpoints that should support screen share; widen the RTP port range.
4. Release SDK, Android and Electron clients.

Rollback: stop sending `multiVideo` from clients (immediate), or redeploy the previous gateway build. Step 1 has no client-visible surface to roll back.

## Open Questions

These need the Asterisk spike (tasks group 1). The code handles each outcome, but the answers decide whether the feature works end to end on the target PBX.

- Does Asterisk propagate the stream add in both `Dial()` and `Queue()` bridges on the target version, and in what m-line order does it offer the second leg? The gateway relies on the second `m=video` staying second.
- What exactly does the deployed `chan_sip` version answer to a re-INVITE with two `m=video` lines? Port 0 and any non-2xx are handled; a 200 with a malformed body is treated as a refusal.
- Are RTCP PLI/FIR for the second video stream forwarded to the right stream on the other leg? The direct request between paired sessions covers the single-instance case either way.
- With the two legs on different gateway instances, does the PBX route the `@screen:` MESSAGE between the two users as it does for chat?

Resolved while implementing:

- In-dialog MESSAGE is not a usable path between the two legs; see Decision 6.
- pion pairs a send-only transceiver that was added before the client's offer with that offer's new receive-only section, and delivers the track to the client (covered by a test over a real PeerConnection pair).
- pion cannot roll back a remote offer; see Decision 5.
