## ADDED Requirements

### Requirement: Client opt-in for a second video stream

The gateway MUST treat a WebSocket connection as able to send or receive an auxiliary video stream only after that connection sent `multiVideo: true` in `agent_register`, `device_register` or `client_state`. Connections that omit the field or send `false` MUST keep single-video behaviour. `/ws-public` connections MUST NOT be able to opt in.

#### Scenario: Opted-in client

- **WHEN** a `/ws`, `/ws-agent` or `/ws-agent-device` client sends `client_state` with `multiVideo: true`
- **THEN** the gateway MUST record the capability for that connection

#### Scenario: Legacy client is unaffected

- **WHEN** a client never sends `multiVideo`
- **THEN** the gateway MUST NOT send it `renegotiate` with `reason: stream_added` or any `stream_state` message
- **AND** a `stream_update` from that client MUST be refused with `status: "failed"`

### Requirement: Client-originated screen stream add

On `stream_update` with `role: "screen"` and `action: "add"` for an active session of an opted-in connection, the gateway MUST offer a second `m=video` to the SIP peer in an in-dialog re-INVITE before changing the PeerConnection. Once the peer accepts it, the gateway MUST apply the client's SDP offer on the live PeerConnection and reply `stream_update_result` with `status: "ok"`. Camera and audio MUST keep flowing throughout.

#### Scenario: Peer accepts the second video stream

- **WHEN** an opted-in client sends `stream_update` `add` with an SDP offer that adds a second video m-line, and the SIP peer answers the re-INVITE with a non-zero port for the second `m=video`
- **THEN** the client MUST receive `stream_update_result` with `status: "ok"` and an SDP answer
- **AND** RTP from the client's second video track MUST be sent to the peer's second video endpoint with its own SSRC and sequence numbers
- **AND** the primary video SSRC, sequence numbers and destination MUST be unchanged

#### Scenario: Request on a session that is not active

- **WHEN** a client sends `stream_update` `add` for a session that is not in the active state
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and MUST NOT send a re-INVITE

#### Scenario: Another renegotiation is pending

- **WHEN** a client sends `stream_update` while a mid-call renegotiation is pending on the session
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and `reason: "busy"`

#### Scenario: Call is on hold

- **WHEN** a client sends `stream_update` `add` for a session it has put on hold
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and `reason: "held"` and MUST NOT send a re-INVITE

#### Scenario: Call without camera video

- **WHEN** a client sends `stream_update` `add` for a session whose SIP dialog has no primary video m-line
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and `reason: "video_call_required"` and MUST NOT send a re-INVITE

#### Scenario: Offer without a second video section

- **WHEN** a client sends `stream_update` `add` with an SDP offer that does not contain exactly two video m-lines
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and `reason: "invalid_offer"` and MUST NOT send a re-INVITE

#### Scenario: No RTP port available

- **WHEN** the gateway cannot bind an RTP/RTCP port pair for the auxiliary stream
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and `reason: "no_rtp_ports"`
- **AND** the gateway MUST NOT send a re-INVITE

### Requirement: Peers that reject the second video stream

When the SIP peer does not accept the second `m=video`, the gateway MUST leave the PeerConnection and the existing call media untouched, release the auxiliary ports, and reply `stream_update_result` with `status: "rejected"` and `reason: "not_supported"`. The gateway MUST NOT substitute the screen for the camera on the primary video stream.

#### Scenario: Second m-line answered with port 0

- **WHEN** the SIP peer answers the re-INVITE with 200 OK and port 0 for the second `m=video`
- **THEN** the client MUST receive `stream_update_result` with `status: "rejected"` and `reason: "not_supported"`
- **AND** audio and primary video MUST continue without interruption

#### Scenario: Re-INVITE refused with a final error

- **WHEN** the SIP peer answers the re-INVITE with a 4xx, 5xx or 6xx response other than 491
- **THEN** the client MUST receive `stream_update_result` with `status: "rejected"` and `reason: "not_supported"`
- **AND** the SIP dialog and the call MUST remain established

#### Scenario: Glare

- **WHEN** the SIP peer answers the re-INVITE with 491
- **THEN** the gateway MUST retry the re-INVITE once after a randomized back-off
- **AND** if the retry is also answered with 491 the client MUST receive `stream_update_result` with `status: "failed"` and `reason: "busy"`

### Requirement: Rejections are cached per SIP account

After the gateway's own SIP leg refuses the second `m=video`, the gateway MUST answer further `stream_update` `add` requests for the same SIP account with `status: "rejected"` and `reason: "not_supported"` without sending a re-INVITE, for 10 minutes. A refusal is port 0 for the m-line, an answer that does not mirror the offer's m-lines, or a 415, 488, 501, 603 or 606 response. Other final errors and a refusal reported by the far end MUST NOT populate this cache.

#### Scenario: Second attempt on a rejecting account

- **WHEN** a `stream_update` `add` was rejected by the SIP answer for an account, and another `add` arrives for a session of the same account within 10 minutes
- **THEN** the gateway MUST reply `rejected` with `reason: "not_supported"`
- **AND** the gateway MUST NOT send a re-INVITE

#### Scenario: Transient SIP error is not remembered

- **WHEN** the re-INVITE is answered with 408 or 503, and another `add` arrives for the same account
- **THEN** the gateway MUST offer the second `m=video` to the SIP peer again

#### Scenario: Cache expiry

- **WHEN** more than 10 minutes have passed since the rejection
- **THEN** the next `stream_update` `add` MUST be offered to the SIP peer again

### Requirement: Peer-originated screen stream add

When an in-dialog re-INVITE opens a second `m=video` and the bound client has opted in, the gateway MUST accept that m-line in its SIP answer without waiting for the client, and MUST send the client `renegotiate` with `reason: "stream_added"`, `role: "screen"` and an empty `sdp`. The gateway MUST apply the offer from `renegotiate_answer` on the live PeerConnection and MUST NOT create a replacement PeerConnection.

#### Scenario: Agent receives the caller's screen

- **WHEN** an opted-in client's session receives a re-INVITE that opens a second `m=video`, and the client answers `renegotiate` with an offer that adds a receive-only video m-line
- **THEN** the client MUST receive `renegotiate_result` with `status: "ok"` and an SDP answer
- **AND** RTP received on the auxiliary SIP port MUST be delivered on the client's second video track
- **AND** the PeerConnection instance, ICE transport and the primary audio and video tracks MUST be the same as before the renegotiation

#### Scenario: Second share on the same call

- **WHEN** the far end shares again after an earlier share on the same call, and the client's PeerConnection still has the screen section
- **THEN** the gateway MUST attach the stream without sending `renegotiate`
- **AND** the client MUST receive `stream_state` with `role: "screen"` and `active: true`

#### Scenario: Second video on the initial INVITE

- **WHEN** the INVITE that sets up a call already offers a second `m=video`
- **THEN** the gateway MUST answer that m-line with port 0

#### Scenario: Offer uses its own payload type for the screen

- **WHEN** the offered second `m=video` uses a transport profile or H.264 payload type different from the camera's
- **THEN** the gateway's answer for that m-line MUST use the offered transport profile and payload type

#### Scenario: Far end is already sharing

- **WHEN** a client sends `stream_update` `add` on a session that is receiving the far end's screen
- **THEN** the gateway MUST reply `stream_update_result` with `status: "failed"` and `reason: "peer_sharing"` and MUST NOT send a re-INVITE

#### Scenario: Client has not opted in

- **WHEN** the session's bound client has not sent `multiVideo: true`, or no client is bound
- **THEN** the gateway MUST answer the second `m=video` with port 0
- **AND** the gateway MUST NOT send `renegotiate` for it

#### Scenario: Client does not answer

- **WHEN** the client does not send `renegotiate_answer` within the mid-call renegotiation timeout
- **THEN** the client MUST receive `renegotiate_result` with `status: "timeout"`
- **AND** audio and primary video MUST continue on the existing PeerConnection

### Requirement: Far-end confirmation of the screen stream

The receiving session MUST report to the sharing session whether it shows the screen. The sharing session MUST wait up to 3 seconds for that report after its own re-INVITE is accepted, MUST send its client `stream_state` `active: true` on a ready report, and MUST close the second `m=video` and send `stream_state` `active: false` with `reason: "peer_not_supported"` otherwise.

#### Scenario: Both legs on the same gateway

- **WHEN** the sharing and receiving sessions are the two legs of one call on the same gateway instance, and the receiving client completes the `stream_added` renegotiation
- **THEN** the sharing client MUST receive `stream_state` with `role: "screen"` and `active: true`
- **AND** the report MUST NOT depend on the SIP peer relaying a message between the two legs

#### Scenario: Receiving client did not opt in

- **WHEN** the receiving session refuses the second `m=video` because its client did not opt in
- **THEN** the sharing session MUST close the second `m=video` with a re-INVITE without waiting for the timeout
- **AND** the sharing client MUST receive `stream_state` with `active: false` and `reason: "peer_not_supported"`

#### Scenario: Far end stays silent

- **WHEN** the sharing session's re-INVITE is accepted but no report arrives within 3 seconds
- **THEN** the gateway MUST send a re-INVITE with port 0 for the second `m=video`
- **AND** the sharing client MUST receive `stream_state` with `active: false` and `reason: "peer_not_supported"`

#### Scenario: Confirmed stream is kept

- **WHEN** a ready report arrived before the 3 seconds elapsed
- **THEN** the gateway MUST NOT close the stream when the 3 seconds elapse
- **AND** a repeated ready report MUST NOT produce a second `stream_state`

#### Scenario: Legs on different gateway instances

- **WHEN** the receiving session cannot find the sharing session on its own gateway instance
- **THEN** it MUST send the report as a PBX-routed SIP MESSAGE with body `@screen:ready` or `@screen:unsupported`
- **AND** the gateway that receives it MUST apply it to the sharing session it names by Call-ID, or to the single sharing session of the addressed user that is still waiting

#### Scenario: Control message is not shown as chat

- **WHEN** a SIP MESSAGE whose body begins with `@screen:` reaches the gateway
- **THEN** the gateway MUST answer it 200 and MUST NOT deliver a `message` event for it to any WebSocket client

#### Scenario: Keyframe at the start of a confirmed share

- **WHEN** the far end reports ready
- **THEN** the sharing session MUST ask its client for a keyframe on the screen track

### Requirement: Screen stream removal

On `stream_update` with `action: "remove"`, the gateway MUST close the second `m=video` with an in-dialog re-INVITE (port 0), release the auxiliary ports, and reply `stream_update_result` with `status: "ok"`. A session whose peer closes the second `m=video` MUST send its client `stream_state` with `role: "screen"` and `active: false`. Removal MUST NOT require a WebRTC SDP exchange.

#### Scenario: Sharer stops sharing

- **WHEN** the sharing client sends `stream_update` `remove`
- **THEN** the SIP peer MUST receive a re-INVITE whose second `m=video` has port 0
- **AND** the sharing client MUST receive `stream_update_result` with `status: "ok"`
- **AND** the receiving client MUST receive `stream_state` with `active: false`

#### Scenario: Re-add on the same call

- **WHEN** the sharing client sends `stream_update` `add` without `sdp` after a previous add and remove on the same session
- **THEN** the gateway MUST reopen the second `m=video` over SIP and reuse the existing WebRTC video transceiver
- **AND** the client MUST receive `stream_update_result` with `status: "ok"` and no `sdp`

### Requirement: The auxiliary stream is isolated from the primary video stream

The auxiliary stream MUST use its own RTP and RTCP sockets, SSRC, sequence numbers and H.264 parameter-set cache in both directions. Keyframe requests for one stream MUST NOT be sent with the other stream's SSRC. A failure in the auxiliary path MUST NOT stop, reset or renegotiate the primary audio or video path.

#### Scenario: Two uplink video tracks

- **WHEN** the client is sending camera and screen video at the same time
- **THEN** camera RTP and screen RTP MUST leave the gateway from different local ports with different SSRCs
- **AND** an SPS or picture-size change on the screen track MUST NOT change the camera's cached parameter sets

#### Scenario: Keyframe request for the screen

- **WHEN** the receiving client sends PLI for its screen video track
- **THEN** the gateway MUST send the RTCP feedback toward the SIP peer on the auxiliary RTCP path with the auxiliary media SSRC

#### Scenario: Auxiliary write error

- **WHEN** writing auxiliary RTP to the SIP peer or to the WebRTC track fails
- **THEN** the gateway MUST log and continue
- **AND** primary video and audio forwarding MUST be unaffected

### Requirement: Screen stream lifecycle with other mid-call features

The gateway MUST close an open auxiliary stream and send `stream_state` with `active: false` and a `reason` when the session accepts an `@switch`, when a resume offer does not contain a second video m-line, and when the peer shrinks the SDP. The gateway MUST release the auxiliary sockets and stop its goroutines when the session ends, whatever the cause. Hold and unhold MUST preserve the auxiliary m-line in the SDP.

#### Scenario: Agent switch

- **WHEN** a session with an open auxiliary stream accepts an `@switch`
- **THEN** the second `m=video` MUST be closed over SIP
- **AND** the client MUST receive `stream_state` with `active: false` and `reason: "agent_switch"`

#### Scenario: Resume with the screen still shared

- **WHEN** a client resumes a session with an open auxiliary stream using an offer that contains a second video m-line
- **THEN** the replacement PeerConnection MUST carry the auxiliary track
- **AND** the second `m=video` on the SIP dialog MUST stay open

#### Scenario: Resume without the screen section

- **WHEN** a client resumes a session with an open auxiliary stream using an offer that has no second video m-line
- **THEN** the client MUST receive `stream_state` with `active: false` and `reason: "resume"`
- **AND** a sharing session MUST close the second `m=video` over SIP, and a receiving session MUST report unsupported to the sharing session

#### Scenario: Call ends while sharing

- **WHEN** a session with an open auxiliary stream ends by BYE, hangup, ICE failure or timeout
- **THEN** the auxiliary RTP and RTCP sockets MUST be closed and their ports returned to the pool

### Requirement: Contract documentation

`docs/gateway/ws-contract.md` MUST document `multiVideo`, `stream_update`, `stream_update_result`, `stream_state`, and the `stream_added` reason of `renegotiate`, including which side creates the SDP offer and that the PeerConnection is not replaced. `docs/gateway/config-reference.md` MUST state the RTP port capacity cost of a shared screen.

#### Scenario: Integrator reads the contract

- **WHEN** an integrator reads the WebSocket contract
- **THEN** they MUST be able to find each new message with its fields, the allowed endpoints, and the `status` and `reason` values
