package session

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const auxRTPHistorySize = 512

// auxRTPHistory keeps recent SIP-to-WebRTC packets of the auxiliary stream so
// a browser NACK can be answered without asking the SIP side. Guarded by
// AuxVideo.mu.
type auxRTPHistory struct {
	packets [auxRTPHistorySize][]byte
	seqs    [auxRTPHistorySize]uint16
}

func (h *auxRTPHistory) store(seq uint16, data []byte) {
	index := int(seq) % auxRTPHistorySize
	h.packets[index] = append(h.packets[index][:0], data...)
	h.seqs[index] = seq
}

func (h *auxRTPHistory) get(seq uint16) []byte {
	index := int(seq) % auxRTPHistorySize
	if h.seqs[index] != seq || len(h.packets[index]) == 0 {
		return nil
	}
	return append([]byte(nil), h.packets[index]...)
}

// HandleSIPRTP takes one RTP datagram received from the SIP peer on the
// auxiliary port and delivers it, in sequence order, on the auxiliary WebRTC
// track. Packets are passed through unchanged; the track rewrites SSRC and
// payload type for the browser. data is copied, so the caller may reuse it.
func (a *AuxVideo) HandleSIPRTP(data []byte) {
	var header rtp.Header
	headerLen, err := header.Unmarshal(data)
	if err != nil || headerLen > len(data) {
		return
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.stats.DownlinkPackets++
	sourceChanged := a.remoteSSRC != header.SSRC
	if sourceChanged {
		a.remoteSSRC = header.SSRC
	}
	hasTrack := a.track != nil
	wantKeyframe := sourceChanged && (hasTrack || a.pendingSIPPLI)
	reorder := a.reorder
	if !hasTrack {
		a.stats.DownlinkDropped++
	}
	a.mu.Unlock()

	if sourceChanged {
		// A new SIP source starts its own sequence space.
		reorder.Reset()
		fmt.Printf("[%s] aux_video remote_ssrc=%d\n", a.sessionID, header.SSRC)
	}
	if wantKeyframe {
		a.RequestKeyframeFromSIP("remote-ssrc-learned")
	}
	if !hasTrack {
		// Nothing can show the picture yet. Attaching the track asks for a
		// keyframe, so nothing is kept from this packet.
		return
	}

	reorder.Push(header.SequenceNumber, append([]byte(nil), data...), isH264KeyframeStart(data[headerLen:]))
}

// isH264KeyframeStart reports whether an RTP payload starts an IDR picture.
func isH264KeyframeStart(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	switch payload[0] & 0x1F {
	case 5:
		return true
	case 24:
		for _, nal := range splitSTAPA(payload[1:]) {
			if nal[0]&0x1F == 5 {
				return true
			}
		}
	case 28:
		return len(payload) > 1 && payload[1]&0x80 != 0 && payload[1]&0x1F == 5
	}
	return false
}

type auxRTPWriter interface {
	Write([]byte) (int, error)
}

type auxDownlinkNormalizer interface {
	NormalizeDownlink(data []byte) (out []byte, outSeq uint16, seqOffset uint16)
}

// writeDownlink is the reorder buffer's sink: one in-order RTP packet for the
// browser.
func (a *AuxVideo) writeDownlink(data []byte, _ bool) {
	if len(data) < 12 {
		return
	}
	a.egressMu.Lock()
	defer a.egressMu.Unlock()

	a.mu.Lock()
	track := a.track
	if a.closed || track == nil {
		a.stats.DownlinkDropped++
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	out := data
	outSeq := binary.BigEndian.Uint16(data[2:4])
	var seqOffset uint16
	if normalizer, ok := track.(auxDownlinkNormalizer); ok {
		out, outSeq, seqOffset = normalizer.NormalizeDownlink(data)
	}

	a.mu.Lock()
	if a.closed || a.track != track {
		a.stats.DownlinkDropped++
		a.mu.Unlock()
		return
	}
	a.egressSeqOffset = seqOffset
	a.history.store(outSeq, out)
	a.mu.Unlock()

	a.writeTrack(track, out)
}

// writeTrack writes to the WebRTC track. The caller holds egressMu.
func (a *AuxVideo) writeTrack(track auxRTPWriter, data []byte) bool {
	_, err := track.Write(data)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.stats.DownlinkErrs++
		if count := a.stats.DownlinkErrs; count <= 3 || count%500 == 0 {
			fmt.Printf("[%s] aux_video downlink write error (count=%d): %v\n", a.sessionID, count, err)
		}
		return false
	}
	a.stats.DownlinkWritten++
	return true
}

// ReadSenderRTCP handles the browser's feedback about the auxiliary track
// until the sender closes. It must run for as long as the track is attached:
// pion's interceptors only see inbound RTCP while something reads it.
func (a *AuxVideo) ReadSenderRTCP(sender *webrtc.RTPSender, bufferSize int) {
	if sender == nil {
		return
	}
	buf := make([]byte, auxBufferSize(bufferSize))
	for {
		n, _, err := sender.Read(buf)
		if err != nil {
			return
		}
		packets, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		a.HandleBrowserRTCP(packets)
	}
}

// HandleBrowserRTCP acts on the browser's feedback about the auxiliary track:
// PLI and FIR become a keyframe request to the SIP side, NACKs are answered
// from the history and, for packets it no longer holds, passed to the SIP
// side. Sequence numbers are the SIP sender's own, so that NACK is valid there.
func (a *AuxVideo) HandleBrowserRTCP(packets []rtcp.Packet) {
	for _, packet := range packets {
		switch p := packet.(type) {
		case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
			a.RequestKeyframeFromSIP("browser-keyframe-request")
		case *rtcp.TransportLayerNack:
			a.handleBrowserNACK(p.Nacks)
		}
	}
}

func (a *AuxVideo) handleBrowserNACK(nacks []rtcp.NackPair) {
	var missing []uint16
	for _, pair := range nacks {
		for _, seq := range pair.PacketList() {
			a.mu.Lock()
			data := a.history.get(seq)
			a.mu.Unlock()
			if data == nil || !a.retransmit(data) {
				missing = append(missing, seq)
			}
		}
	}

	a.mu.Lock()
	a.stats.NACKMisses += len(missing)
	seqOffset := a.egressSeqOffset
	a.mu.Unlock()
	if len(missing) > 0 {
		sipMissing := make([]uint16, len(missing))
		for i, seq := range missing {
			sipMissing[i] = seq - seqOffset
		}
		a.sendSIPRTCP(func(senderSSRC, mediaSSRC uint32) rtcp.Packet {
			return &rtcp.TransportLayerNack{
				SenderSSRC: senderSSRC,
				MediaSSRC:  mediaSSRC,
				Nacks:      rtcp.NackPairsFromSequenceNumbers(sipMissing),
			}
		})
	}
}

func (a *AuxVideo) retransmit(data []byte) bool {
	a.egressMu.Lock()
	defer a.egressMu.Unlock()
	a.mu.Lock()
	track := a.track
	closed := a.closed
	a.mu.Unlock()
	if closed || track == nil {
		return false
	}
	if !a.writeTrack(track, data) {
		return false
	}
	a.mu.Lock()
	a.stats.NACKRetransmits++
	a.mu.Unlock()
	return true
}

// RequestKeyframeFromSIP asks the SIP peer for an IDR on the auxiliary
// stream. Before the peer's SSRC is known the request is remembered and sent
// with the first packet. Requests are rate limited; it reports whether one
// was sent now.
func (a *AuxVideo) RequestKeyframeFromSIP(reason string) bool {
	now := time.Now()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false
	}
	if a.remoteSSRC == 0 {
		a.pendingSIPPLI = true
		relay := a.keyframeRelay
		a.mu.Unlock()
		if relay != nil {
			relay()
		}
		return false
	}
	if !a.lastSIPPLI.IsZero() && now.Sub(a.lastSIPPLI) < auxKeyframeRequestMinInterval {
		a.mu.Unlock()
		return false
	}
	relay := a.keyframeRelay
	a.mu.Unlock()

	sent := a.sendSIPRTCP(func(senderSSRC, mediaSSRC uint32) rtcp.Packet {
		return &rtcp.PictureLossIndication{SenderSSRC: senderSSRC, MediaSSRC: mediaSSRC}
	})
	if !sent && relay == nil {
		return false
	}
	a.mu.Lock()
	a.lastSIPPLI = now
	a.pendingSIPPLI = false
	count := a.stats.SIPPLISent
	if sent {
		a.stats.SIPPLISent++
		count = a.stats.SIPPLISent
	}
	a.mu.Unlock()
	// The relay does not depend on the RTCP reaching anyone: it is the path
	// that still works when the SIP peer drops feedback for this stream.
	if relay != nil {
		relay()
	}
	if sent && (count <= 5 || count%50 == 0) {
		fmt.Printf("[%s] aux_video SIP PLI sent reason=%s count=%d\n", a.sessionID, reason, count)
	}
	return sent
}

// sendSIPRTCP sends one feedback packet to the SIP peer as compound RTCP
// (receiver report first, RFC 3550). The sender SSRC is the auxiliary uplink
// SSRC and the media SSRC is the peer's auxiliary stream, so the request can
// never be read as one for the camera.
func (a *AuxVideo) sendSIPRTCP(build func(senderSSRC, mediaSSRC uint32) rtcp.Packet) bool {
	a.mu.Lock()
	if a.uplink.ssrc == 0 {
		a.uplink.ssrc = generateSSRC()
		a.uplink.seq = uint16(generateSSRC())
	}
	senderSSRC := a.uplink.ssrc
	mediaSSRC := a.remoteSSRC
	conn, dest := a.sipRTCPTargetLocked()
	a.mu.Unlock()
	if conn == nil || dest == nil || mediaSSRC == 0 {
		return false
	}

	out, err := rtcp.Marshal([]rtcp.Packet{&rtcp.ReceiverReport{SSRC: senderSSRC}, build(senderSSRC, mediaSSRC)})
	if err != nil {
		return false
	}
	if _, err := conn.WriteToUDP(out, dest); err != nil {
		fmt.Printf("[%s] aux_video SIP RTCP write error: %v\n", a.sessionID, err)
		return false
	}
	return true
}

// sipRTCPTargetLocked picks the socket and address for RTCP to the SIP peer:
// the RTP pair when the peer multiplexes RTCP or has no RTCP address, the
// dedicated RTCP pair otherwise. The caller holds a.mu.
func (a *AuxVideo) sipRTCPTargetLocked() (*net.UDPConn, *net.UDPAddr) {
	if a.rtcpMux || a.remoteRTCP == nil {
		return a.rtpConn, a.remoteRTP
	}
	if a.rtcpConn != nil {
		return a.rtcpConn, a.remoteRTCP
	}
	return a.rtpConn, a.remoteRTCP
}
