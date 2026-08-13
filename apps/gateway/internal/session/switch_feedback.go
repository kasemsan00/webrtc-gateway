package session

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"k2-gateway/internal/config"
)

type switchFeedbackAuthority struct {
	generation int
	mediaEpoch uint64
}

type sipFeedbackSnapshot struct {
	authorized    bool
	ready         bool
	kind          string
	conn          *net.UDPConn
	rtpConn       *net.UDPConn
	rtcpConn      *net.UDPConn
	targets       []videoFeedbackTarget
	senderSSRC    uint32
	mediaSSRC     uint32
	firSeq        uint8
	count         int
	trigger       string
	force         bool
	learnedSource string
}

type webRTCFeedbackSnapshot struct {
	authorized bool
	ready      bool
	throttled  bool
	kind       string
	pc         *webrtc.PeerConnection
	ssrc       uint32
	firSeq     uint8
	count      int
}

func (s *Session) feedbackAuthorityLocked(authority *switchFeedbackAuthority) bool {
	return authority == nil || (s.MediaEpoch == authority.mediaEpoch && s.SwitchGeneration == authority.generation)
}

func normalizedVideoFeedbackTransport(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case config.SIPVideoFeedbackTransportRTP, config.SIPVideoFeedbackTransportRTCP, config.SIPVideoFeedbackTransportDual:
		return mode
	default:
		return config.SIPVideoFeedbackTransportAuto
	}
}

func (s *Session) prepareSIPFeedbackLocked(kind string, force bool, trigger string, now time.Time, authority *switchFeedbackAuthority) sipFeedbackSnapshot {
	snapshot := sipFeedbackSnapshot{authorized: s.feedbackAuthorityLocked(authority), kind: kind, trigger: trigger, force: force}
	if !snapshot.authorized {
		return snapshot
	}

	destAddr := cloneUDPAddr(s.AsteriskVideoAddr)
	rtpConn := s.VideoRTPConn
	rtcpConn := s.VideoRTCPConn
	if rtcpConn == nil {
		rtcpConn = rtpConn
	}
	if rtpConn == nil {
		rtpConn = rtcpConn
	}
	conn := rtcpConn
	if conn == nil {
		conn = rtpConn
	}
	senderSSRC := s.VideoSSRC
	if senderSSRC == 0 {
		senderSSRC = 0x87654321
	}
	mediaSSRC := s.RemoteVideoSSRC
	snapshot.conn = conn
	snapshot.rtpConn = rtpConn
	snapshot.rtcpConn = rtcpConn
	snapshot.senderSSRC = senderSSRC
	snapshot.mediaSSRC = mediaSSRC

	if kind == "fir" {
		snapshot.firSeq = s.FIRSeq
		s.FIRSeq++
	}
	if destAddr == nil || conn == nil || mediaSSRC == 0 {
		return snapshot
	}

	if kind == "pli" {
		allowBypass := force && trigger == "watchdog-fir"
		if force && trigger == "switch" && s.isSwitchVideoRecoveryActiveLocked(now) && s.SwitchVideoRecoveryOneShotPLI {
			s.SwitchVideoRecoveryOneShotPLI = false
			allowBypass = true
		}
		minInterval := pliMinInterval
		if force {
			minInterval = pliForceMinInterval
		}
		if !allowBypass && ((!s.LastSipPLISent.IsZero() && now.Sub(s.LastSipPLISent) < minInterval) ||
			(!force && !s.LastKeyframe.IsZero() && now.Sub(s.LastKeyframe) <= pliKeyframeGrace)) {
			return snapshot
		}
	}
	if kind == "fir" {
		if !s.LastSipFIRSent.IsZero() && now.Sub(s.LastSipFIRSent) < sipFIRMinInterval {
			return snapshot
		}
	}

	learnedAddr := cloneUDPAddr(s.AsteriskVideoRTCPAddr)
	useFallback := now.Before(s.VideoRTCPFallbackUntil)
	snapshot.learnedSource = s.VideoRTCPSource
	snapshot.targets = buildVideoFeedbackTargets(normalizedVideoFeedbackTransport(s.VideoFeedbackTransport), destAddr, learnedAddr, useFallback)
	if len(snapshot.targets) == 0 {
		return snapshot
	}

	s.PLISent++
	s.LastPLISent = now
	s.LastSipPLISent = now
	if kind == "fir" {
		s.LastSipFIRSent = now
	}
	snapshot.count = s.PLISent
	snapshot.ready = true
	return snapshot
}

func sendPreparedSIPFeedback(id string, snapshot sipFeedbackSnapshot) {
	if !snapshot.ready {
		return
	}
	rr := &rtcp.ReceiverReport{SSRC: snapshot.senderSSRC}
	var feedback rtcp.Packet
	if snapshot.kind == "fir" {
		feedback = &rtcp.FullIntraRequest{
			SenderSSRC: snapshot.senderSSRC, MediaSSRC: snapshot.mediaSSRC,
			FIR: []rtcp.FIREntry{{SSRC: snapshot.mediaSSRC, SequenceNumber: snapshot.firSeq}},
		}
	} else {
		feedback = &rtcp.PictureLossIndication{SenderSSRC: snapshot.senderSSRC, MediaSSRC: snapshot.mediaSSRC}
	}
	out, err := rtcp.Marshal([]rtcp.Packet{rr, feedback})
	if err != nil {
		fmt.Printf("[%s] error marshalling switch %s: %v\n", id, snapshot.kind, err)
		return
	}
	for _, target := range snapshot.targets {
		conn := feedbackConnForTarget(target, snapshot.rtpConn, snapshot.rtcpConn)
		if conn == nil {
			conn = snapshot.conn
		}
		if conn == nil {
			continue
		}
		if _, err := conn.WriteToUDP(out, target.Addr); err != nil {
			fmt.Printf("[%s] error sending %s to %s %s: %v\n", id, snapshot.kind, target.Kind, target.Addr, err)
			continue
		}
		fmt.Printf("[%s] 📡 Sent %s to %s %s (%s)\n", id, strings.ToUpper(snapshot.kind), target.Kind, target.Addr, target.Label)
	}
}

func (s *Session) prepareWebRTCFeedbackLocked(kind string, force bool, authority *switchFeedbackAuthority) webRTCFeedbackSnapshot {
	snapshot := webRTCFeedbackSnapshot{authorized: s.feedbackAuthorityLocked(authority), kind: kind}
	if !snapshot.authorized {
		return snapshot
	}
	pc := s.PeerConnection
	if pc == nil {
		return snapshot
	}
	now := time.Now()
	if !force {
		if kind == "pli" && !s.LastWebRTCPLISent.IsZero() && now.Sub(s.LastWebRTCPLISent) < webrtcPLIMinInterval {
			snapshot.throttled = true
			return snapshot
		}
		if kind == "fir" && !s.LastWebRTCFIRSent.IsZero() && now.Sub(s.LastWebRTCFIRSent) < webrtcFIRMinInterval {
			snapshot.throttled = true
			return snapshot
		}
	}
	for _, receiver := range pc.GetReceivers() {
		track := receiver.Track()
		if track == nil || track.Kind() != webrtc.RTPCodecTypeVideo {
			continue
		}
		snapshot.pc = pc
		snapshot.ssrc = uint32(track.SSRC())
		snapshot.ready = true
		if kind == "pli" {
			s.LastWebRTCPLISent = now
		}
		if kind == "fir" {
			snapshot.firSeq = s.FIRSeq
			s.FIRSeq++
			s.PLISent++
			s.LastPLISent = now
			s.LastWebRTCFIRSent = now
			snapshot.count = s.PLISent
		}
		break
	}
	return snapshot
}

func sendPreparedWebRTCFeedback(id string, snapshot webRTCFeedbackSnapshot) {
	if !snapshot.ready {
		return
	}
	var packet rtcp.Packet
	if snapshot.kind == "fir" {
		packet = &rtcp.FullIntraRequest{
			SenderSSRC: snapshot.ssrc, MediaSSRC: snapshot.ssrc,
			FIR: []rtcp.FIREntry{{SSRC: snapshot.ssrc, SequenceNumber: snapshot.firSeq}},
		}
	} else {
		packet = &rtcp.PictureLossIndication{MediaSSRC: snapshot.ssrc}
	}
	if err := snapshot.pc.WriteRTCP([]rtcp.Packet{packet}); err != nil &&
		!strings.Contains(err.Error(), "DTLS transport has not started yet") &&
		!strings.Contains(err.Error(), "read/write on closed pipe") {
		fmt.Printf("[%s] error sending switch %s to WebRTC: %v\n", id, snapshot.kind, err)
	}
}

func (s *Session) sendSIPFeedback(kind string, force bool, trigger string, authority *switchFeedbackAuthority) bool {
	s.mu.Lock()
	snapshot := s.prepareSIPFeedbackLocked(kind, force, trigger, time.Now(), authority)
	id := s.ID
	s.mu.Unlock()
	if !snapshot.authorized {
		return false
	}
	sendPreparedSIPFeedback(id, snapshot)
	return true
}

func (s *Session) sendWebRTCFeedback(kind string, authority *switchFeedbackAuthority) bool {
	return s.sendWebRTCFeedbackWithForce(kind, false, authority)
}

func (s *Session) sendWebRTCFeedbackWithForce(kind string, force bool, authority *switchFeedbackAuthority) bool {
	s.mu.Lock()
	snapshot := s.prepareWebRTCFeedbackLocked(kind, force, authority)
	id := s.ID
	s.mu.Unlock()
	if !snapshot.authorized {
		return false
	}
	if snapshot.throttled {
		fmt.Printf("[%s] webrtc_%s_throttled force=%v\n", id, kind, force)
		return true
	}
	sendPreparedWebRTCFeedback(id, snapshot)
	return true
}

func (s *Session) SendSwitchFIRToAsterisk(generation int, mediaEpoch uint64) bool {
	return s.sendSIPFeedback("fir", false, "switch", &switchFeedbackAuthority{generation, mediaEpoch})
}

func (s *Session) SendSwitchPLIToAsteriskForced(generation int, mediaEpoch uint64, trigger string) bool {
	return s.sendSIPFeedback("pli", true, trigger, &switchFeedbackAuthority{generation, mediaEpoch})
}

func (s *Session) SendSwitchFIRToWebRTC(generation int, mediaEpoch uint64) bool {
	return s.sendWebRTCFeedbackWithForce("fir", true, &switchFeedbackAuthority{generation, mediaEpoch})
}

func (s *Session) SendSwitchPLIToWebRTC(generation int, mediaEpoch uint64) bool {
	return s.sendWebRTCFeedbackWithForce("pli", true, &switchFeedbackAuthority{generation, mediaEpoch})
}

// FlushPendingBrowserKeyframeRequestForSwitch atomically claims only a pending
// request from the switch token's call epoch, then sends token-bound recovery.
func (s *Session) FlushPendingBrowserKeyframeRequestForSwitch(generation int, mediaEpoch uint64, reason string) bool {
	s.mu.Lock()
	if !s.feedbackAuthorityLocked(&switchFeedbackAuthority{generation, mediaEpoch}) {
		s.mu.Unlock()
		return false
	}
	if !s.PendingBrowserKeyframeRequest || s.PendingBrowserKeyframeRequestEpoch != mediaEpoch {
		s.mu.Unlock()
		return true
	}
	s.clearPendingBrowserKeyframeRequestLocked()
	snapshot := s.prepareSIPFeedbackLocked("pli", true, "ws-request_keyframe", time.Now(), &switchFeedbackAuthority{generation, mediaEpoch})
	id := s.ID
	s.mu.Unlock()

	sendPreparedSIPFeedback(id, snapshot)
	fmt.Printf("[%s] request_keyframe_flushed reason=%s tokenGeneration=%d mediaEpoch=%d\n", id, reason, generation, mediaEpoch)
	return true
}
