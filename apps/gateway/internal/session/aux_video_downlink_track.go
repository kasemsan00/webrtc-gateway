package session

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
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
	track       *auxDownlinkLocalTrack
	transceiver *webrtc.RTPTransceiver
}

type auxTrackBinding struct {
	id          string
	ssrc        webrtc.SSRC
	payloadType webrtc.PayloadType
	writeStream webrtc.TrackLocalWriter
}

// auxDownlinkLocalTrack is a pion webrtc.TrackLocal for the auxiliary screen
// stream that fixes two mid-call multi-video hazards:
//
//  1. PayloadType alignment: pion v4 RTPSender.Send passes MediaEngine's
//     PeerConnection-wide negotiatedVideoCodecs to TrackLocal.Bind instead of
//     the transceiver's codec preferences. On a mid-call renegotiation where
//     mid=3 negotiates a different H.264 PT (e.g. 127) than mid=1 (102), a
//     plain TrackLocalStaticRTP binds to 102 and stamps every screen packet
//     with an unregistered PT, causing Chromium/Brave to drop all packets.
//     Pinning the transceiver's chosen codec via SetPreferredCodec keeps wire
//     PT identical to the answer SDP.
//
//  2. SRTP continuity across re-shares: a single send-only transceiver (and
//     WebRTC SSRC) is reused across Share #1 -> Stop -> Share #2, while the SIP
//     peer opens a new SSRC with a fresh random sequence number and timestamp
//     on each share. Normalizing egress sequence numbers and timestamps per
//     WebRTC SSRC prevents SRTP replay/ROC drops and backward timestamp jumps.
type auxDownlinkLocalTrack struct {
	mu             sync.RWMutex
	id             string
	streamID       string
	codec          webrtc.RTPCodecCapability
	preferredCodec *webrtc.RTPCodecParameters
	bindings       []auxTrackBinding

	normMu      sync.Mutex
	hasEgress   bool
	lastInSSRC  uint32
	lastInSeq   uint16
	lastInTS    uint32
	outSeq      uint16
	outTS       uint32
	lastWriteAt time.Time
	nowFn       func() time.Time
}

func newAuxDownlinkLocalTrack(id, streamID string) *auxDownlinkLocalTrack {
	return &auxDownlinkLocalTrack{
		id:       id,
		streamID: streamID,
		codec:    webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		nowFn:    time.Now,
	}
}

func (t *auxDownlinkLocalTrack) SetPreferredCodec(codec webrtc.RTPCodecParameters) {
	t.mu.Lock()
	c := codec
	t.preferredCodec = &c
	for i := range t.bindings {
		t.bindings[i].payloadType = c.PayloadType
	}
	t.mu.Unlock()
}

func (t *auxDownlinkLocalTrack) Bind(ctx webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var chosen webrtc.RTPCodecParameters
	found := false

	if t.preferredCodec != nil {
		for _, c := range ctx.CodecParameters() {
			if c.PayloadType == t.preferredCodec.PayloadType {
				chosen = c
				found = true
				break
			}
		}
		if !found {
			chosen = *t.preferredCodec
			found = true
		}
	}

	if !found {
		for _, c := range ctx.CodecParameters() {
			if strings.EqualFold(c.MimeType, t.codec.MimeType) && (t.codec.ClockRate == 0 || c.ClockRate == t.codec.ClockRate) {
				chosen = c
				found = true
				break
			}
		}
	}

	if !found {
		return webrtc.RTPCodecParameters{}, webrtc.ErrUnsupportedCodec
	}

	t.bindings = append(t.bindings, auxTrackBinding{
		id:          ctx.ID(),
		ssrc:        ctx.SSRC(),
		payloadType: chosen.PayloadType,
		writeStream: ctx.WriteStream(),
	})
	return chosen, nil
}

func (t *auxDownlinkLocalTrack) Unbind(ctx webrtc.TrackLocalContext) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.bindings {
		if t.bindings[i].id == ctx.ID() {
			t.bindings[i] = t.bindings[len(t.bindings)-1]
			t.bindings = t.bindings[:len(t.bindings)-1]
			return nil
		}
	}
	return webrtc.ErrUnbindFailed
}

func (t *auxDownlinkLocalTrack) ID() string              { return t.id }
func (t *auxDownlinkLocalTrack) RID() string             { return "" }
func (t *auxDownlinkLocalTrack) StreamID() string        { return t.streamID }
func (t *auxDownlinkLocalTrack) Kind() webrtc.RTPCodecType { return webrtc.RTPCodecTypeVideo }

// NormalizeDownlink rewrites the RTP sequence number and timestamp on a copy of
// data so the browser's single auxiliary WebRTC SSRC sees monotonic sequence
// numbers and forward timestamps even when a new share starts with a new SIP
// SSRC and random sequence/timestamp baseline.
func (t *auxDownlinkLocalTrack) NormalizeDownlink(data []byte) ([]byte, uint16, uint16) {
	if len(data) < 12 {
		return data, 0, 0
	}
	inSeq := binary.BigEndian.Uint16(data[2:4])
	inTS := binary.BigEndian.Uint32(data[4:8])
	inSSRC := binary.BigEndian.Uint32(data[8:12])
	now := t.nowFn()

	t.normMu.Lock()
	if !t.hasEgress {
		t.hasEgress = true
		t.lastInSSRC = inSSRC
		t.lastInSeq = inSeq
		t.lastInTS = inTS
		t.outSeq = inSeq
		t.outTS = inTS
		t.lastWriteAt = now
	} else if inSSRC == t.lastInSSRC {
		seqDelta := uint16(inSeq - t.lastInSeq)
		if seqDelta >= 1 && seqDelta <= 64 {
			t.outSeq += seqDelta
		} else {
			t.outSeq++
		}
		if inTS != t.lastInTS {
			tsDelta := inTS - t.lastInTS
			if tsDelta < 0x80000000 && tsDelta <= 90000*60 {
				t.outTS += tsDelta
			} else {
				t.outTS += elapsedVideoTicks(now.Sub(t.lastWriteAt))
			}
		}
		t.lastInSeq = inSeq
		t.lastInTS = inTS
		t.lastWriteAt = now
	} else {
		// New SIP SSRC (e.g. a subsequent screen share on the reused transceiver).
		t.outSeq++
		t.outTS += elapsedVideoTicks(now.Sub(t.lastWriteAt))
		t.lastInSSRC = inSSRC
		t.lastInSeq = inSeq
		t.lastInTS = inTS
		t.lastWriteAt = now
	}
	outSeq := t.outSeq
	outTS := t.outTS
	t.normMu.Unlock()

	out := make([]byte, len(data))
	copy(out, data)
	binary.BigEndian.PutUint16(out[2:4], outSeq)
	binary.BigEndian.PutUint32(out[4:8], outTS)
	return out, outSeq, uint16(outSeq - inSeq)
}

func elapsedVideoTicks(d time.Duration) uint32 {
	const (
		minTicks = 3000       // ~33ms at 90kHz
		maxTicks = 90000 * 10 // cap 10s jump so decoder jitter buffer stays calm
	)
	if d <= 0 {
		return minTicks
	}
	ticks := int64(d.Seconds() * 90000)
	if ticks < minTicks {
		return minTicks
	}
	if ticks > maxTicks {
		return maxTicks
	}
	return uint32(ticks)
}

func (t *auxDownlinkLocalTrack) Write(b []byte) (int, error) {
	var packet rtp.Packet
	if err := packet.Unmarshal(b); err != nil {
		return 0, err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	var writeErrs []error
	for _, binding := range t.bindings {
		header := packet.Header
		header.SSRC = uint32(binding.ssrc)
		header.PayloadType = uint8(binding.payloadType)
		if packet.PaddingSize != 0 && header.PaddingSize == 0 {
			header.PaddingSize = packet.PaddingSize
		}
		if _, err := binding.writeStream.WriteRTP(&header, packet.Payload); err != nil {
			writeErrs = append(writeErrs, err)
		}
	}
	return len(b), errors.Join(writeErrs...)
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

	track := newAuxDownlinkLocalTrack("screen", fmt.Sprintf("pion-screen-%s", s.ID))
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

// AuxDownlinkMid returns the negotiated mid of the auxiliary downlink
// transceiver on the live PeerConnection, or "" when none is negotiated.
func (s *Session) AuxDownlinkMid() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.auxDownlink == nil || s.auxDownlink.pc != s.PeerConnection {
		return ""
	}
	return s.auxDownlink.transceiver.Mid()
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
	aux.setLocalWriter(downlink.track)
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
	// Lost is true when any open auxiliary direction could not continue on
	// the replacement PeerConnection.
	Lost bool
	// Sending tells which side was active for single-direction resumes.
	Sending bool
	// LostUplink and LostDownlink distinguish per-direction loss when both
	// local and remote screen shares are active simultaneously.
	LostUplink   bool
	LostDownlink bool
	// hasDownlink records whether the replacement PeerConnection should
	// attach the downlink track after swap.
	hasDownlink bool
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
	hasUplink := aux.HasUplink()
	hasDownlink := aux.HasDownlink()
	outcome.Sending = hasUplink && !hasDownlink

	uplinkMid, downlinkMid, uplinkBeforeDownlink := auxOfferVideoMids(offerSDP, hasUplink, hasDownlink)
	if hasUplink {
		if uplinkMid != "" {
			s.SetAuxUplinkMid(uplinkMid)
		} else {
			outcome.LostUplink = true
			s.SetAuxUplinkMid("")
		}
	} else {
		s.SetAuxUplinkMid("")
	}

	if hasDownlink {
		if downlinkMid == "" {
			outcome.LostDownlink = true
		} else {
			// When both uplink and downlink sections are present and the
			// browser placed its sendonly uplink before its recvonly downlink,
			// add a recvonly placeholder first so pion pairs the sendonly
			// downlink transceiver with the recvonly section.
			if hasUplink && uplinkMid != "" && uplinkBeforeDownlink && pc != nil {
				if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
					webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
					fmt.Printf("[%s] aux_video resume uplink placeholder: %v\n", s.ID, err)
				}
			}
			if err := s.prepareAuxDownlinkTrackOn(pc); err != nil {
				fmt.Printf("[%s] aux_video resume: %v\n", s.ID, err)
				outcome.LostDownlink = true
			} else {
				outcome.hasDownlink = true
			}
		}
	}

	outcome.Lost = outcome.LostUplink || outcome.LostDownlink
	s.setAuxResumeOutcome(outcome)
}

// finishAuxAfterReplacement attaches a received auxiliary stream to the
// replacement PeerConnection once it is the session's live one.
func (s *Session) finishAuxAfterReplacement() {
	s.mu.RLock()
	outcome := s.auxResume
	aux := s.auxVideo
	s.mu.RUnlock()
	if aux == nil || !outcome.hasDownlink || outcome.LostDownlink {
		return
	}
	if err := s.AttachAuxDownlinkTrack(); err != nil {
		fmt.Printf("[%s] aux_video resume: %v\n", s.ID, err)
		outcome.LostDownlink = true
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
