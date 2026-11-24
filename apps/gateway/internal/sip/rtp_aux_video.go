package sip

import (
	"errors"
	"fmt"
	"net"
	"time"

	"webrtc-sip-gateway/internal/session"
)

// SIP-side transport of a session's auxiliary (screen) video stream. It has
// its own RTP/RTCP port pair and its own read loops, and hands every datagram
// to session.AuxVideo. Nothing here touches the camera's sockets or handlers.

// bindRTPPortPair takes one slot from the RTP port pool and binds its first
// two ports as an RTP/RTCP pair. It shares the round-robin cursor with
// bindRTPPortQuad; a slot held by a pair is skipped there because its first
// port is taken.
func (s *Server) bindRTPPortPair() (port int, rtpConn, rtcpConn *net.UDPConn, err error) {
	slotCount := rtpPortSlotCount(s.rtpConfig.PortMin, s.rtpConfig.PortMax)
	if slotCount <= 0 {
		return 0, nil, nil, fmt.Errorf("failed to find available RTP port in range %d-%d", s.rtpConfig.PortMin, s.rtpConfig.PortMax)
	}

	bindIP := rtpBindIP()
	seq := s.rtpPortCursor.Add(1) - 1
	startSlot := int(seq % uint64(slotCount))

	for offset := 0; offset < slotCount; offset++ {
		slot := (startSlot + offset) % slotCount
		candidatePort := s.rtpConfig.PortMin + slot*rtpPortSlotStep

		rtp, err := net.ListenUDP("udp", &net.UDPAddr{Port: candidatePort, IP: bindIP})
		if err != nil {
			continue
		}
		rtcp, err := net.ListenUDP("udp", &net.UDPAddr{Port: candidatePort + 1, IP: bindIP})
		if err != nil {
			rtp.Close()
			continue
		}

		if offset > 0 {
			s.rtpPortCursor.CompareAndSwap(seq+1, seq+uint64(offset)+1)
		}
		return candidatePort, rtp, rtcp, nil
	}

	return 0, nil, nil, fmt.Errorf("failed to find available RTP port in range %d-%d", s.rtpConfig.PortMin, s.rtpConfig.PortMax)
}

// openAuxVideoForSession binds an RTP/RTCP pair for the session's auxiliary
// video stream and starts its SIP-side readers. The stream sends nothing
// until its remote endpoints are set, and the readers stop when it is closed
// (session.CloseAuxVideo, or the session's media teardown).
func (s *Server) openAuxVideoForSession(sess *session.Session) (*session.AuxVideo, error) {
	if sess == nil {
		return nil, fmt.Errorf("session required")
	}
	port, rtpConn, rtcpConn, err := s.bindRTPPortPair()
	if err != nil {
		return nil, err
	}
	aux, err := sess.OpenAuxVideo(rtpConn, rtcpConn, port, port+1)
	if err != nil {
		rtpConn.Close()
		rtcpConn.Close()
		return nil, err
	}

	go s.readAuxVideoSocket(rtpConn, aux, true)
	go s.readAuxVideoSocket(rtcpConn, aux, false)
	return aux, nil
}

// auxSocketMaxConsecutiveReadErrors ends a read loop whose socket keeps
// failing without having been closed.
const auxSocketMaxConsecutiveReadErrors = 50

// readAuxVideoSocket reads one auxiliary socket until it is closed. Packets
// are classified by content, not by port: a peer using rtcp-mux sends RTCP to
// the RTP port, and some peers deliver RTP to the RTCP port.
func (s *Server) readAuxVideoSocket(conn *net.UDPConn, aux *session.AuxVideo, isRTPSocket bool) {
	bufferSize := s.rtpConfig.BufferSize
	if bufferSize < 1500 {
		bufferSize = 1500
	}
	buffer := make([]byte, bufferSize)
	readErrors := 0
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// Closing the stream closes the socket; that is the normal exit.
			// Anything else (an ICMP error surfacing on the socket) is
			// transient and must not end the stream.
			readErrors++
			if errors.Is(err, net.ErrClosed) || readErrors > auxSocketMaxConsecutiveReadErrors {
				return
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		readErrors = 0
		data := buffer[:n]
		switch {
		case isRTCPPacketCheck(data):
			if !isRTPSocket {
				aux.LearnSymmetricRTCP(remoteAddr)
			}
			aux.HandleSIPRTCP(data)
		case isLikelyRTPPacket(data):
			if isRTPSocket {
				aux.LearnSymmetricRTP(remoteAddr)
			}
			aux.HandleSIPRTP(data)
		}
	}
}
