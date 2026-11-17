package session

import (
	"fmt"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// Browser RTCP handling shared by the first PeerConnection and every
// replacement built for resume or @switch. A replacement used to run a
// cut-down copy: its PLI/FIR used a trigger the recovery logic did not know
// and its NACKs were discarded, so a resumed call lost packet retransmission
// and keyframe recovery.

// drainSenderRTCP reads and discards RTCP for a sender. Pion interceptors only
// see inbound RTCP while something reads it.
func (s *Session) drainSenderRTCP(sender *webrtc.RTPSender) {
	if sender == nil {
		return
	}
	buf := make([]byte, s.RTPBufferSize)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

// readVideoSenderRTCP handles the browser's feedback about our video track:
// PLI/FIR become a keyframe request toward the SIP side and NACKs are answered
// from the retransmission cache.
func (s *Session) readVideoSenderRTCP(sender *webrtc.RTPSender) {
	if sender == nil {
		return
	}
	buf := make([]byte, s.RTPBufferSize)
	for {
		if s.GetState() == StateEnded {
			return
		}
		n, _, err := sender.Read(buf)
		if err != nil {
			return
		}
		packets, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		for _, p := range packets {
			switch pkt := p.(type) {
			case *rtcp.PictureLossIndication:
				fmt.Printf("[%s] 📸 Received PLI from browser for video (RTPSender)\n", s.ID)
				s.SendBrowserRecoveryToAsterisk("browser-pli")
			case *rtcp.FullIntraRequest:
				fmt.Printf("[%s] 📸 Received FIR from browser for video (RTPSender)\n", s.ID)
				s.SendBrowserRecoveryToAsterisk("browser-fir")
			case *rtcp.TransportLayerNack:
				s.handleBrowserVideoNACK(pkt.Nacks)
			}
		}
	}
}

// handleBrowserVideoNACK resends cached packets for a browser NACK.
func (s *Session) handleBrowserVideoNACK(nacks []rtcp.NackPair) {
	if len(nacks) == 0 {
		return
	}
	now := time.Now()
	sig := buildNACKSignature(nacks)
	if s.shouldSuppressBrowserNACK(sig, now) {
		return
	}
	fmt.Printf("[%s] 🔄 Received NACK from browser for video (RTPSender) - Nacks=%v\n", s.ID, nacks)
	sent, missing := s.RetransmitVideoNACK(nacks)
	if missing > 0 {
		s.forwardBrowserNACKToAsterisk(nacks)
	}
	if (sent > 0 || missing > 0) && !s.shouldSuppressNACKHandledLog(sig, sent, missing, now) {
		fmt.Printf("[%s] 🔁 NACK handled (retransmit=%d, missing=%d)\n", s.ID, sent, missing)
	}
}

// browserNACKMatchesSIPSequence reports whether browser NACK sequence numbers
// are the SIP sender's own. The H.264 AU normalizer renumbers SIP->WebRTC video
// (and inserts SPS/PPS packets), so with it enabled a browser NACK names
// packets Asterisk never sent under those numbers.
func (s *Session) browserNACKMatchesSIPSequence() bool {
	return !s.VideoAUNormalizeEnabled
}

// forwardBrowserNACKToAsterisk asks Asterisk to resend packets the cache no
// longer holds, only when the sequence numbers mean the same thing there.
func (s *Session) forwardBrowserNACKToAsterisk(nacks []rtcp.NackPair) {
	if !s.browserNACKMatchesSIPSequence() {
		return
	}
	s.SendNACKToAsterisk(nacks)
}

// readReceiverRTCP drains RTCP arriving on a receiver (sender reports and any
// feedback Pion routes there) so interceptors keep working.
func (s *Session) readReceiverRTCP(receiver *webrtc.RTPReceiver, kind string) {
	if receiver == nil {
		return
	}
	buf := make([]byte, s.RTPBufferSize)
	for {
		if s.GetState() == StateEnded {
			return
		}
		n, _, err := receiver.Read(buf)
		if err != nil {
			return
		}
		if kind != "video" {
			continue
		}
		packets, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		for _, p := range packets {
			switch pkt := p.(type) {
			case *rtcp.PictureLossIndication:
				fmt.Printf("[%s] 📸 Received PLI from browser for video\n", s.ID)
				s.SendBrowserRecoveryToAsterisk("browser-pli")
			case *rtcp.FullIntraRequest:
				fmt.Printf("[%s] 📸 Received FIR from browser for video\n", s.ID)
				s.SendBrowserRecoveryToAsterisk("browser-fir")
			case *rtcp.TransportLayerNack:
				fmt.Printf("[%s] 🔄 Received NACK from browser for video - Nacks=%v\n", s.ID, pkt.Nacks)
				s.forwardBrowserNACKToAsterisk(pkt.Nacks)
			}
		}
	}
}
