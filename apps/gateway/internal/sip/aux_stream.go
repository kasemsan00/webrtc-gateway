package sip

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

// Mid-call negotiation of a session's auxiliary (screen) video stream with
// the SIP peer. The stream is the second m=video of the dialog. Whether the
// peer supports it is decided per call from its answer: a port, port 0, or a
// refusal of the whole re-INVITE. There is no configuration switch.

// ScreenStreamOutcome is the result of asking the SIP peer to open the
// auxiliary video stream.
type ScreenStreamOutcome struct {
	// Status is "ok", "rejected" (the peer does not take a second video
	// stream) or "failed" (the request could not be completed).
	Status string
	Reason string
}

// AuxVideoStateNotifier is told when the auxiliary video stream of a session
// starts or stops for a reason other than the client's own request.
// direction is "local" (this client's shared screen) or "remote" (peer's screen).
type AuxVideoStateNotifier interface {
	NotifyAuxVideoState(sessionID string, active bool, reason string, direction string)
}

const (
	screenStreamOK       = "ok"
	screenStreamRejected = "rejected"
	screenStreamFailed   = "failed"

	// A refusal depends on the SIP account's endpoint configuration, so it
	// is remembered and later requests on the account are answered without
	// another re-INVITE.
	auxRejectCacheTTL = 10 * time.Minute

	inDialogInviteTimeout = 10 * time.Second
)

// inDialogInviteFunc sends a re-INVITE and returns its final response.
type inDialogInviteFunc func(req *sip.Request) (*sip.Response, error)

// sipStatusError is a final non-2xx response to an in-dialog request.
type sipStatusError struct {
	Code   int
	Reason string
}

func (e *sipStatusError) Error() string {
	return fmt.Sprintf("SIP %d %s", e.Code, e.Reason)
}

// auxRejectCache remembers SIP accounts whose peer refused a second video
// stream.
type auxRejectCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func (c *auxRejectCache) remember(key string, now time.Time) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]time.Time)
	}
	for k, until := range c.entries {
		if !now.Before(until) {
			delete(c.entries, k)
		}
	}
	c.entries[key] = now.Add(auxRejectCacheTTL)
}

func (c *auxRejectCache) rejected(key string, now time.Time) bool {
	if key == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.entries[key]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(c.entries, key)
		return false
	}
	return true
}

// auxRejectKey identifies the SIP account a session registered or dialed
// with. Sessions without one are not cached.
func auxRejectKey(sess *session.Session) string {
	_, accountKey, trunkID, domain, username, _, _ := sess.GetSIPAuthContext()
	switch {
	case trunkID > 0:
		return "trunk:" + strconv.FormatInt(trunkID, 10)
	case accountKey != "":
		return "account:" + accountKey
	case username != "" && domain != "":
		return "user:" + username + "@" + domain
	}
	return ""
}

// auxRejectIsCacheable reports whether a final response refuses the offered
// media, as opposed to a transient or dialog-level failure.
func auxRejectIsCacheable(code int) bool {
	switch code {
	case 415, 488, 501, 603, 606:
		return true
	}
	return false
}

// midCallOffer is a gateway-originated in-dialog SDP offer and the m-line
// layout it uses.
type midCallOffer struct {
	body     []byte
	layout   []session.SIPMediaSlot
	auxIndex int // position of the auxiliary m-line, -1 when the offer has none
}

// buildMidCallOffer builds the SDP for a gateway-originated re-INVITE: the
// primary audio and video with the hold direction, the auxiliary video
// section when that stream is open, and port 0 at every position the dialog
// carries but the gateway does not use.
func (s *Server) buildMidCallOffer(sess *session.Session, held bool) (midCallOffer, error) {
	status := sess.GetMediaEndpointStatus()
	if status.AudioRTPPort <= 0 {
		return midCallOffer{}, fmt.Errorf("audio RTP endpoint is not ready")
	}

	body := s.createSDPOffer(status.AudioRTPPort, sess)
	if negotiated, ok := sess.GetSIPNegotiatedMedia(); ok {
		body = []byte(constrainSDPToNegotiatedDialog(string(body), status.AudioRTPPort, negotiated))
	}
	direction := "sendrecv"
	if held {
		direction = "inactive"
	}
	body = []byte(strings.ReplaceAll(string(body), "a=sendrecv", "a="+direction))

	offer := midCallOffer{layout: sess.SIPMediaLayout(), auxIndex: -1}
	var auxSection []string
	if aux := sess.AuxVideo(); aux != nil {
		parts := splitGatewaySDP(body)
		rtpPort, rtcpPort := aux.Ports()
		auxDirection := aux.SIPDirection()
		if held {
			auxDirection = "inactive"
		}
		auxSection = buildAuxSDPSection(parts.video, rtpPort, rtcpPort, auxDirection)
		if auxSection == nil {
			return midCallOffer{}, errAuxNeedsPrimaryVideo
		}
		if len(offer.layout) == 0 {
			offer.layout = plainSIPMediaLayout(parts)
		}
		offer.layout, offer.auxIndex = ensureAuxSlot(offer.layout, declinedSDPMediaLine(auxSection[0]))
	}

	// Keep m-lines the peer offered earlier and the gateway refused: an offer
	// must not have fewer m-lines than the dialog's previous SDP.
	offer.body = renderSDPOfferForLayout(body, offer.layout, auxSection)
	return offer, nil
}

var errAuxNeedsPrimaryVideo = errors.New("auxiliary video needs a primary video m-line")

// createInDialogInvite builds a re-INVITE carrying an SDP offer on the
// session's dialog.
func (s *Server) createInDialogInvite(sess *session.Session, body []byte) (*sip.Request, error) {
	req, err := s.createBYERequest(sess)
	if err != nil {
		return nil, err
	}
	req.Method = sip.INVITE
	// createBYERequest currently adds CSeq as a generic header. req.CSeq()
	// lazily parses that value into a detached typed header, so mutating the
	// returned pointer changes the transaction key but not the serialized wire
	// header. Replace the stored header to keep sipgo transaction matching and
	// the actual SIP request on the same INVITE method/CSeq.
	req.ReplaceHeader(&sip.CSeqHeader{
		SeqNo:      uint32(sess.NextSIPCSeq()),
		MethodName: sip.INVITE,
	})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(sip.NewHeader("Allow", sipAllowHeaderValue()))
	req.SetBody(body)
	return req, nil
}

// sendInDialogInvite sends a re-INVITE and returns its final response. A 2xx
// is acknowledged here.
func (s *Server) sendInDialogInvite(req *sip.Request) (*sip.Response, error) {
	if s.inDialogInvite != nil {
		return s.inDialogInvite(req)
	}
	if s.sipClient == nil {
		return nil, fmt.Errorf("SIP client not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), inDialogInviteTimeout)
	defer cancel()
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send in-dialog INVITE: %w", err)
	}
	defer tx.Terminate()

	for {
		select {
		case res := <-tx.Responses():
			if res == nil || res.StatusCode < 200 {
				continue
			}
			if res.StatusCode < 300 {
				s.sendAckForInvite(req, res)
			}
			return res, nil
		case <-tx.Done():
			return nil, fmt.Errorf("in-dialog INVITE transaction ended without final response")
		case <-ctx.Done():
			return nil, fmt.Errorf("in-dialog INVITE timed out: %w", ctx.Err())
		}
	}
}

// reinviteGlareBackoff is the wait before retrying a re-INVITE that met 491
// (RFC 3261 section 14.1): 2.1 to 4 seconds for the owner of the Call-ID, up
// to 2 seconds otherwise.
func (s *Server) reinviteGlareBackoff(sess *session.Session) time.Duration {
	if s.glareBackoff != nil {
		return s.glareBackoff(sess)
	}
	if direction, _, _, _ := sess.GetCallInfo(); direction == "outbound" {
		return 2100*time.Millisecond + time.Duration(rand.Int63n(int64(1900*time.Millisecond)))
	}
	return time.Duration(rand.Int63n(int64(2 * time.Second)))
}

// sendMidCallOffer sends an SDP offer in a re-INVITE and returns the 2xx
// response. A 401/407 challenge is answered once using the session's Digest
// credentials, and a 491 glare is retried once after the glare back-off. Any
// other final non-2xx response is returned as a *sipStatusError.
func (s *Server) sendMidCallOffer(sess *session.Session, body []byte) (*sip.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := s.createInDialogInvite(sess, body)
		if err != nil {
			return nil, fmt.Errorf("build in-dialog INVITE: %w", err)
		}
		res, err := s.sendInDialogInvite(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == 401 || res.StatusCode == 407 {
			authReq, authErr := s.authorizeInDialogInvite(sess, req, res)
			if authErr != nil {
				fmt.Printf("[%s] sip_reinvite auth (%d) skipped: %v\n", sess.ID, res.StatusCode, authErr)
				return nil, &sipStatusError{Code: res.StatusCode, Reason: res.Reason}
			}
			res, err = s.sendInDialogInvite(authReq)
			if err != nil {
				return nil, err
			}
		}
		if res.StatusCode == 491 && attempt == 0 {
			wait := s.reinviteGlareBackoff(sess)
			fmt.Printf("[%s] sip_reinvite glare (491), retrying in %s\n", sess.ID, wait)
			time.Sleep(wait)
			continue
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, &sipStatusError{Code: res.StatusCode, Reason: res.Reason}
		}
		return res, nil
	}
}

// OpenScreenStream asks the SIP peer to open the session's auxiliary video
// stream as a send-only second m=video, or upgrades an existing recvonly
// stream to sendrecv when the far end is already sharing so both parties can
// share simultaneously. On "ok" the stream forwards the client's auxiliary
// track once attached. On any other status any newly opened stream is closed
// (or an existing receiving stream is restored to recvonly) and the call's
// audio, primary video, and any peer screen share remain untouched.
func (s *Server) OpenScreenStream(sess *session.Session) ScreenStreamOutcome {
	if sess == nil || sess.GetState() != session.StateActive || !sess.HasDialogState() {
		return ScreenStreamOutcome{Status: screenStreamFailed, Reason: "not_active"}
	}
	if sess.IsHeld() {
		return ScreenStreamOutcome{Status: screenStreamFailed, Reason: "held"}
	}
	if aux := sess.AuxVideo(); aux != nil && aux.HasUplink() && aux.RemoteRTPAddr() != nil {
		return ScreenStreamOutcome{Status: screenStreamOK}
	}
	accountKey := auxRejectKey(sess)
	if s.auxRejects.rejected(accountKey, time.Now()) {
		fmt.Printf("[%s] aux_stream_add skipped reason=cached_rejection account=%s\n", sess.ID, accountKey)
		return ScreenStreamOutcome{Status: screenStreamRejected, Reason: "not_supported"}
	}

	previousLayout := sess.SIPMediaLayout()
	aux := sess.AuxVideo()
	upgradedFromDownlink := false
	if aux == nil {
		opened, err := s.openAuxVideoForSession(sess)
		if err != nil {
			fmt.Printf("[%s] aux_stream_add failed reason=no_rtp_ports: %v\n", sess.ID, err)
			return ScreenStreamOutcome{Status: screenStreamFailed, Reason: "no_rtp_ports"}
		}
		aux = opened
		aux.SetSIPDirection("sendonly")
	} else if aux.HasDownlink() {
		upgradedFromDownlink = true
		aux.PrepareUplinkStart()
		aux.SetSIPDirection("sendrecv")
	} else {
		aux.SetSIPDirection("sendonly")
	}

	abort := func(outcome ScreenStreamOutcome, layout []session.SIPMediaSlot, cache bool) ScreenStreamOutcome {
		if upgradedFromDownlink {
			aux.StopUplink()
		} else {
			sess.CloseAuxVideo("add-" + outcome.Reason)
			sess.SetSIPMediaLayout(layout)
		}
		if cache {
			s.auxRejects.remember(accountKey, time.Now())
		}
		fmt.Printf("[%s] aux_stream_add %s reason=%s cached=%v dual=%v\n", sess.ID, outcome.Status, outcome.Reason, cache, upgradedFromDownlink)
		s.logAuxStreamEvent(sess, "sip_aux_stream_add_"+outcome.Status, map[string]interface{}{"reason": outcome.Reason, "cached": cache})
		return outcome
	}

	offer, err := s.buildMidCallOffer(sess, false)
	if err != nil {
		reason := "sip_error"
		if errors.Is(err, errAuxNeedsPrimaryVideo) {
			reason = "video_call_required"
		}
		return abort(ScreenStreamOutcome{Status: screenStreamFailed, Reason: reason}, previousLayout, false)
	}

	res, err := s.sendMidCallOffer(sess, offer.body)
	if err != nil {
		var statusErr *sipStatusError
		if !errors.As(err, &statusErr) {
			fmt.Printf("[%s] aux_stream_add re-INVITE error: %v\n", sess.ID, err)
			return abort(ScreenStreamOutcome{Status: screenStreamFailed, Reason: "sip_error"}, previousLayout, false)
		}
		if statusErr.Code == 491 {
			return abort(ScreenStreamOutcome{Status: screenStreamFailed, Reason: "busy"}, previousLayout, false)
		}
		if statusErr.Code == 401 || statusErr.Code == 407 {
			return abort(ScreenStreamOutcome{Status: screenStreamFailed, Reason: "sip_error"}, previousLayout, false)
		}
		// A failed re-INVITE leaves the dialog's SDP as it was.
		return abort(ScreenStreamOutcome{Status: screenStreamRejected, Reason: "not_supported"},
			previousLayout, auxRejectIsCacheable(statusErr.Code))
	}

	sections := describeSDPMediaSections(res.Body())
	if offer.auxIndex < 0 || offer.auxIndex >= len(sections) {
		// The answer does not mirror the offer's m-lines. Nothing in it can
		// be attributed to the auxiliary stream.
		return abort(ScreenStreamOutcome{Status: screenStreamRejected, Reason: "not_supported"}, previousLayout, true)
	}
	answer := sections[offer.auxIndex]
	if answer.kind != "video" || answer.port <= 0 {
		// Refused in place. The m-line is now part of the dialog and stays
		// in later SDP with port 0.
		return abort(ScreenStreamOutcome{Status: screenStreamRejected, Reason: "not_supported"}, offer.layout, !upgradedFromDownlink)
	}
	if upgradedFromDownlink && (answer.direction == "sendonly" || answer.direction == "inactive") {
		// The peer kept its own screen flowing toward us (sendonly) but refused
		// to receive our simultaneous screen stream.
		return abort(ScreenStreamOutcome{Status: screenStreamRejected, Reason: "not_supported"}, previousLayout, false)
	}

	s.applyAuxRemoteDescription(sess, aux, answer)
	sess.SetSIPMediaLayout(offer.layout)
	s.updateAsteriskEndpointsFromMidCallOffer(res.Body(), sess)
	fmt.Printf("[%s] aux_stream_add ok direction=%s remote=%s pt=%d pmode=%d rtcp_mux=%v\n",
		sess.ID, aux.SIPDirection(), aux.RemoteRTPAddr(), answer.h264PT, answer.packetizationMode, answer.rtcpMux)
	s.logAuxStreamEvent(sess, "sip_aux_stream_add_ok", map[string]interface{}{"remote": aux.RemoteRTPAddr().String(), "direction": aux.SIPDirection()})
	// The SIP peer accepted on this leg only. The far end still has to
	// confirm that it shows the stream (aux_stream_peer.go).
	s.armScreenStreamConfirmation(sess, aux)
	return ScreenStreamOutcome{Status: screenStreamOK}
}

// CloseScreenStream closes the session's local auxiliary uplink and tells the
// SIP peer with a re-INVITE: if the far end is still sharing toward us
// ("sendrecv"), the m-line is downgraded to "recvonly" so the peer's screen
// keeps flowing; otherwise the stream is closed and its m-line is set to
// port 0. It reports whether a local uplink was open.
func (s *Server) CloseScreenStream(sess *session.Session, reason string) bool {
	if sess == nil {
		return false
	}
	aux := sess.AuxVideo()
	if aux == nil || !aux.HasUplink() {
		return false
	}
	if aux.HasDownlink() {
		aux.StopUplink()
		fmt.Printf("[%s] aux_video_uplink_stopped reason=%s remaining=recvonly\n", sess.ID, reason)
	} else if !sess.CloseAuxVideo(reason) {
		return false
	}
	if sess.GetState() != session.StateActive || !sess.HasDialogState() {
		return true
	}
	offer, err := s.buildMidCallOffer(sess, sess.IsHeld())
	if err != nil {
		fmt.Printf("[%s] aux_stream_remove offer error: %v\n", sess.ID, err)
		return true
	}
	if _, err := s.sendMidCallOffer(sess, offer.body); err != nil {
		// The peer still lists the m-line as open but receives nothing on
		// it. The next mid-call offer from either side updates it.
		fmt.Printf("[%s] aux_stream_remove re-INVITE error: %v\n", sess.ID, err)
	}
	return true
}

// applyAuxRemoteDescription points the auxiliary stream at the peer's
// m-section: address, RTCP handling, payload type and packetization mode.
func (s *Server) applyAuxRemoteDescription(sess *session.Session, aux *session.AuxVideo, desc sdpMediaDescription) {
	ip := net.ParseIP(strings.TrimSpace(desc.ip))
	if ip == nil || ip.IsUnspecified() {
		// No usable address on the m-line: the stream goes to the same host
		// as the call's audio.
		if _, audio, _ := sess.GetAudioRTPInfo(); audio != nil {
			ip = audio.IP
		}
	}
	if ip == nil {
		return
	}
	aux.SetSIPFormat(desc.h264PT, desc.packetizationMode)
	var rtcpAddr *net.UDPAddr
	if !desc.rtcpMux && desc.rtcpPort > 0 {
		rtcpAddr = &net.UDPAddr{IP: ip, Port: desc.rtcpPort}
	}
	aux.SetRemoteEndpoints(&net.UDPAddr{IP: ip, Port: desc.port}, rtcpAddr, desc.rtcpMux)
}

// answerSDPForInboundOffer lays the gateway's answer out on the m-lines of an
// offer from the peer and records that layout. It also decides what the
// offer's second m=video means for the auxiliary stream:
//
//   - a stream is open and the offer still carries the m-line: it is answered
//     with the stream's own section and the stream follows the peer's address
//   - a stream is open and the offer closes or drops the m-line: the stream ends
//   - no stream is open, the offer opens the m-line, allowOpen is set and the
//     session's client can show a second video: a receiving stream is opened
//   - otherwise the m-line is refused with port 0
//
// opened reports the third case. The caller starts the client renegotiation
// after its SIP answer went out.
func (s *Server) answerSDPForInboundOffer(sess *session.Session, base, offer []byte, allowOpen bool) (answer []byte, opened bool) {
	layout := sipMediaLayoutFromOffer(offer)
	var auxSection []string

	index := auxSlotIndex(layout)
	var desc sdpMediaDescription
	offered := false
	if index >= 0 {
		if sections := describeSDPMediaSections(offer); index < len(sections) {
			desc = sections[index]
			offered = desc.kind == "video" && desc.port > 0
		}
	}

	aux := sess.AuxVideo()
	switch {
	case aux != nil && offered:
		currentDir := aux.SIPDirection()
		answerDir := currentDir
		closeAll := false

		switch desc.direction {
		case "inactive":
			answerDir = "inactive"
		case "recvonly":
			switch currentDir {
			case "sendrecv":
				// Peer stopped sending its screen while continuing to receive ours.
				aux.StopDownlink()
				answerDir = "sendonly"
				fmt.Printf("[%s] aux_stream remote downlink closed by peer offer (keeping local uplink)\n", sess.ID)
				s.logAuxStreamEvent(sess, "sip_aux_stream_closed", map[string]interface{}{"reason": "peer_closed", "direction": "remote"})
				s.notifyAuxVideoState(sess.ID, false, "peer_closed", "remote")
			case "recvonly":
				// Peer stopped sending and we are not sending either.
				closeAll = true
			}
		case "sendonly":
			if currentDir == "sendrecv" {
				// Peer stopped receiving our screen while still sending its own.
				aux.StopUplink()
				answerDir = "recvonly"
				fmt.Printf("[%s] aux_stream local uplink closed by peer offer (keeping remote downlink)\n", sess.ID)
				s.logAuxStreamEvent(sess, "sip_aux_stream_closed", map[string]interface{}{"reason": "peer_closed", "direction": "local"})
				s.notifyAuxVideoState(sess.ID, false, "peer_closed", "local")
			}
		case "sendrecv", "":
			if currentDir == "sendonly" {
				// Peer started sharing its own screen while we are already sharing ours.
				wanted := allowOpen && desc.h264PT != 0 && s.canReceiveAuxVideo(sess)
				if wanted {
					aux.SetSIPDirection("sendrecv")
					aux.SetKeyframeRelay(func() { s.relayAuxKeyframeToBridgedPeer(sess) })
					answerDir = "sendrecv"
					opened = true
					fmt.Printf("[%s] aux_stream upgraded to sendrecv (simultaneous peer screen share)\n", sess.ID)
					s.logAuxStreamEvent(sess, "sip_aux_stream_receive_opened", map[string]interface{}{"dual": true})
				} else if allowOpen {
					s.logAuxStreamEvent(sess, "sip_aux_stream_receive_refused", map[string]interface{}{"dual": true})
					go s.ReportScreenStreamToPeer(sess, false)
				}
			}
		}

		if !closeAll {
			rtpPort, rtcpPort := aux.Ports()
			if auxSection = buildAuxSDPAnswerSection(desc, rtpPort, rtcpPort, answerDir); auxSection != nil {
				s.applyAuxRemoteDescription(sess, aux, desc)
			} else if opened {
				aux.StopDownlink()
				opened = false
			}
		}
		if auxSection == nil {
			hadUplink := aux.HasUplink()
			hadDownlink := aux.HasDownlink()
			sess.CloseAuxVideo("peer-closed")
			fmt.Printf("[%s] aux_stream closed by peer offer\n", sess.ID)
			s.logAuxStreamEvent(sess, "sip_aux_stream_closed", map[string]interface{}{"reason": "peer_closed"})
			if hadUplink {
				s.notifyAuxVideoState(sess.ID, false, "peer_closed", "local")
			}
			if hadDownlink {
				s.notifyAuxVideoState(sess.ID, false, "peer_closed", "remote")
			}
		}
	case aux != nil:
		hadUplink := aux.HasUplink()
		hadDownlink := aux.HasDownlink()
		sess.CloseAuxVideo("peer-closed")
		fmt.Printf("[%s] aux_stream closed by peer offer\n", sess.ID)
		s.logAuxStreamEvent(sess, "sip_aux_stream_closed", map[string]interface{}{"reason": "peer_closed"})
		if hadUplink {
			s.notifyAuxVideoState(sess.ID, false, "peer_closed", "local")
		}
		if hadDownlink {
			s.notifyAuxVideoState(sess.ID, false, "peer_closed", "remote")
		}
	case offered:
		// The far end offers its screen. A stream it marks recvonly or
		// inactive carries nothing toward this session.
		wanted := allowOpen && desc.h264PT != 0 && desc.direction != "recvonly" && desc.direction != "inactive"
		if wanted && s.canReceiveAuxVideo(sess) {
			if received, err := s.openAuxVideoForSession(sess); err != nil {
				fmt.Printf("[%s] aux_stream receive refused: %v\n", sess.ID, err)
			} else {
				received.SetSIPDirection("recvonly")
				received.SetKeyframeRelay(func() { s.relayAuxKeyframeToBridgedPeer(sess) })
				rtpPort, rtcpPort := received.Ports()
				auxSection = buildAuxSDPAnswerSection(desc, rtpPort, rtcpPort, "recvonly")
				s.applyAuxRemoteDescription(sess, received, desc)
				opened = true
				fmt.Printf("[%s] aux_stream receive opened rtp_port=%d remote=%s pt=%d\n",
					sess.ID, rtpPort, received.RemoteRTPAddr(), desc.h264PT)
				s.logAuxStreamEvent(sess, "sip_aux_stream_receive_opened", nil)
			}
		}
		if !opened && allowOpen {
			// Tell the sharing leg now instead of letting it wait.
			s.logAuxStreamEvent(sess, "sip_aux_stream_receive_refused", nil)
			go s.ReportScreenStreamToPeer(sess, false)
		}
	}

	sess.SetSIPMediaLayout(layout)
	return renderSDPAnswerForLayout(base, layout, auxSection), opened
}

func (s *Server) notifyAuxVideoState(sessionID string, active bool, reason, direction string) {
	if notifier, ok := s.midCallNotifier.(AuxVideoStateNotifier); ok {
		notifier.NotifyAuxVideoState(sessionID, active, reason, direction)
	}
}
