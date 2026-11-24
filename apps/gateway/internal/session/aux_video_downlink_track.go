package session

import (
	"fmt"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// MidCallRenegotiationSourceStreamAdded marks the client renegotiation that
// follows a peer opening the auxiliary video stream toward this session. The
// client answers with an offer that adds a receive-only video section, and
// that offer is applied on the live PeerConnection.
const MidCallRenegotiationSourceStreamAdded = "sip_stream_added"

// auxDownlinkTrack is the WebRTC side of a received auxiliary video stream:
// a send-only transceiver on one PeerConnection. It outlives a closed stream,
// because the client keeps the transceiver between shares, and is dropped
// when its PeerConnection is replaced or closed.
type auxDownlinkTrack struct {
	pc          *webrtc.PeerConnection
	track       *webrtc.TrackLocalStaticRTP
	transceiver *webrtc.RTPTransceiver
}

// PrepareAuxDownlinkTrack makes sure the live PeerConnection has a send-only
// video transceiver for the auxiliary stream. It must run before the client
// offer that adds the matching receive-only section is applied: pion pairs a
// new remote section with an existing local transceiver of the same kind that
// has no mid yet.
func (s *Session) PrepareAuxDownlinkTrack() error {
	s.mu.RLock()
	pc := s.PeerConnection
	s.mu.RUnlock()
	return s.prepareAuxDownlinkTrackOn(pc)
}

func (s *Session) prepareAuxDownlinkTrackOn(pc *webrtc.PeerConnection) error {
	if pc == nil {
		return fmt.Errorf("peer connection not available")
	}
	s.mu.RLock()
	existing := s.auxDownlink
	s.mu.RUnlock()
	if existing != nil && existing.pc == pc {
		return nil
	}

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"screen",
		fmt.Sprintf("pion-screen-%s", s.ID),
	)
	if err != nil {
		return fmt.Errorf("create auxiliary video track: %w", err)
	}
	transceiver, err := pc.AddTransceiverFromTrack(track,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		return fmt.Errorf("add auxiliary video transceiver: %w", err)
	}

	s.mu.Lock()
	s.auxDownlink = &auxDownlinkTrack{pc: pc, track: track, transceiver: transceiver}
	s.mu.Unlock()
	go s.readAuxSenderRTCP(transceiver.Sender())
	return nil
}

// HasNegotiatedAuxDownlink reports whether the live PeerConnection already
// carries a negotiated auxiliary video section from an earlier share, so a
// new share needs no WebRTC renegotiation.
func (s *Session) HasNegotiatedAuxDownlink() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auxDownlink != nil && s.auxDownlink.pc == s.PeerConnection && s.auxDownlink.transceiver.Mid() != ""
}

// AttachAuxDownlinkTrack starts delivering the open auxiliary stream on the
// prepared track. It fails when the client's offer did not add a matching
// receive-only video section, or when no stream is open.
func (s *Session) AttachAuxDownlinkTrack() error {
	s.mu.RLock()
	downlink := s.auxDownlink
	pc := s.PeerConnection
	aux := s.auxVideo
	s.mu.RUnlock()
	if aux == nil {
		return fmt.Errorf("auxiliary video stream is not open")
	}
	if downlink == nil || downlink.pc != pc {
		return fmt.Errorf("auxiliary video track is not prepared")
	}
	if downlink.transceiver.Mid() == "" {
		return fmt.Errorf("client offer has no receive-only video section for the auxiliary stream")
	}
	aux.SetLocalTrack(downlink.track)
	fmt.Printf("[%s] aux_video_downlink_attached mid=%s\n", s.ID, downlink.transceiver.Mid())
	return nil
}

// dropAuxDownlinkTrack forgets the auxiliary track of a PeerConnection that
// is being closed or replaced.
func (s *Session) dropAuxDownlinkTrack() {
	s.mu.Lock()
	s.auxDownlink = nil
	s.mu.Unlock()
}

// readAuxSenderRTCP handles the browser's feedback about the auxiliary track
// for as long as its sender lives, across shares: the stream it reports on is
// looked up per packet.
func (s *Session) readAuxSenderRTCP(sender *webrtc.RTPSender) {
	if sender == nil {
		return
	}
	buf := make([]byte, auxBufferSize(s.RTPBufferSize))
	for {
		n, _, err := sender.Read(buf)
		if err != nil {
			return
		}
		aux := s.AuxVideo()
		if aux == nil {
			continue
		}
		packets, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		aux.HandleBrowserRTCP(packets)
	}
}

// AuxResumeOutcome says what a resume did with an open auxiliary stream.
type AuxResumeOutcome struct {
	// Lost is true when the stream was open but the client's resume offer
	// has no second video section, so it cannot continue on the new
	// PeerConnection.
	Lost bool
	// Sending tells which side this session is on: true when it sends the
	// stream to the SIP peer, false when it receives it.
	Sending bool
}

// prepareAuxForReplacement carries an open auxiliary stream over to a
// replacement PeerConnection built for a resume. It runs after the new
// PeerConnection exists and before the client's offer is applied on it.
func (s *Session) prepareAuxForReplacement(pc *webrtc.PeerConnection, offerSDP string) {
	aux := s.AuxVideo()
	outcome := AuxResumeOutcome{}
	if aux == nil {
		// No stream, so no auxiliary section is expected on the new
		// PeerConnection either.
		s.SetAuxUplinkMid("")
		s.setAuxResumeOutcome(outcome)
		return
	}
	outcome.Sending = aux.SIPDirection() == "sendonly"
	mid, ok := AuxVideoMidFromOffer(offerSDP)
	switch {
	case !ok:
		outcome.Lost = true
		s.SetAuxUplinkMid("")
	case outcome.Sending:
		// The new PeerConnection's OnTrack must route the screen track.
		s.SetAuxUplinkMid(mid)
	default:
		s.SetAuxUplinkMid("")
		if err := s.prepareAuxDownlinkTrackOn(pc); err != nil {
			fmt.Printf("[%s] aux_video resume: %v\n", s.ID, err)
			outcome.Lost = true
		}
	}
	s.setAuxResumeOutcome(outcome)
}

// finishAuxAfterReplacement attaches a received auxiliary stream to the
// replacement PeerConnection once it is the session's live one.
func (s *Session) finishAuxAfterReplacement() {
	s.mu.RLock()
	outcome := s.auxResume
	aux := s.auxVideo
	s.mu.RUnlock()
	if aux == nil || outcome.Lost || outcome.Sending {
		return
	}
	if err := s.AttachAuxDownlinkTrack(); err != nil {
		fmt.Printf("[%s] aux_video resume: %v\n", s.ID, err)
		outcome.Lost = true
		s.setAuxResumeOutcome(outcome)
	}
}

func (s *Session) setAuxResumeOutcome(outcome AuxResumeOutcome) {
	s.mu.Lock()
	s.auxResume = outcome
	s.mu.Unlock()
}

// TakeAuxResumeOutcome returns what the last resume did with the auxiliary
// stream and clears it, so the caller acts on a lost stream once.
func (s *Session) TakeAuxResumeOutcome() AuxResumeOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome := s.auxResume
	s.auxResume = AuxResumeOutcome{}
	return outcome
}

// SetAuxResumeOutcomeForTest sets the outcome TakeAuxResumeOutcome returns.
// It exists for tests of callers outside this package; production code sets
// the outcome only through a resume.
func (s *Session) SetAuxResumeOutcomeForTest(outcome AuxResumeOutcome) {
	s.setAuxResumeOutcome(outcome)
}
