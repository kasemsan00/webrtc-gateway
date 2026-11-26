package session

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	auxCachedIDRMaxPackets = 256
	auxCachedIDRMaxBytes   = 512 * 1024
	auxCachedIDRMaxAge     = 10 * time.Second
	auxReplayTimestampStep = 3000
)

type auxCachedRTPPacket struct {
	payload []byte
	marker  bool
}

type auxCachedIDRSnapshot struct {
	timestamp  uint32
	receivedAt time.Time
	sps        []byte
	pps        []byte
	packets    []auxCachedRTPPacket
	hasFUA     bool
}

// auxUplinkIDRCache retains the most recent complete uplink IDR access unit
// (SPS, PPS and IDR NAL payloads) for the session's auxiliary video track.
// On a re-share (replaceTrack without SDP renegotiation) the browser often
// sends the first IDR of a static screen before stream_update or the SIP
// re-INVITE settles, and will not answer PLI until screen pixels change.
// Replaying this bounded snapshot when the SIP destination or far end becomes
// ready lets static screens render immediately without waiting for motion.
type auxUplinkIDRCache struct {
	mu sync.Mutex

	sps []byte
	pps []byte

	capturing      bool
	capturingTS    uint32
	capturingBytes int
	capturingHasFU bool
	capturingPkts  []auxCachedRTPPacket

	ready *auxCachedIDRSnapshot
}

func (c *auxUplinkIDRCache) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.sps = nil
	c.pps = nil
	c.capturing = false
	c.capturingTS = 0
	c.capturingBytes = 0
	c.capturingHasFU = false
	c.capturingPkts = nil
	c.ready = nil
	c.mu.Unlock()
}

func (c *auxUplinkIDRCache) snapshot(maxAge time.Duration) *auxCachedIDRSnapshot {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	snap := c.ready
	c.mu.Unlock()
	if snap == nil || len(snap.packets) == 0 || len(snap.sps) == 0 || len(snap.pps) == 0 {
		return nil
	}
	if maxAge > 0 && time.Since(snap.receivedAt) > maxAge {
		return nil
	}
	return snap
}

func (c *auxUplinkIDRCache) observe(packet *rtp.Packet) {
	if c == nil || packet == nil || len(packet.Payload) == 0 {
		return
	}
	payload := packet.Payload
	nalType := payload[0] & 0x1F

	c.mu.Lock()
	defer c.mu.Unlock()

	switch nalType {
	case 7:
		if len(payload) > 1 {
			c.sps = append([]byte(nil), payload...)
		}
		return
	case 8:
		if len(payload) > 1 {
			c.pps = append([]byte(nil), payload...)
		}
		return
	case 24:
		nals := splitSTAPA(payload[1:])
		if len(nals) == 0 {
			return
		}
		var idrNALs [][]byte
		for _, nal := range nals {
			if len(nal) == 0 {
				continue
			}
			switch nal[0] & 0x1F {
			case 7:
				if len(nal) > 1 {
					c.sps = append([]byte(nil), nal...)
				}
			case 8:
				if len(nal) > 1 {
					c.pps = append([]byte(nil), nal...)
				}
			case 5:
				idrNALs = append(idrNALs, nal)
			}
		}
		if len(idrNALs) == 0 {
			return
		}
		if !c.capturing || c.capturingTS != packet.Timestamp {
			c.startCaptureLocked(packet.Timestamp)
		}
		for i, nal := range idrNALs {
			marker := packet.Marker && i == len(idrNALs)-1
			if !c.appendCapturedLocked(nal, marker, false) {
				return
			}
		}
		if packet.Marker {
			c.commitCaptureLocked()
		}
		return
	case 5:
		if !c.capturing || c.capturingTS != packet.Timestamp {
			c.startCaptureLocked(packet.Timestamp)
		}
		if !c.appendCapturedLocked(payload, packet.Marker, false) {
			return
		}
		if packet.Marker {
			c.commitCaptureLocked()
		}
		return
	case 28:
		if len(payload) <= 1 {
			c.abortCaptureLocked()
			return
		}
		origType := payload[1] & 0x1F
		if origType != 5 {
			c.abortCaptureLocked()
			return
		}
		isStart := payload[1]&0x80 != 0
		if isStart && (!c.capturing || c.capturingTS != packet.Timestamp) {
			c.startCaptureLocked(packet.Timestamp)
		}
		if !c.capturing || c.capturingTS != packet.Timestamp {
			return
		}
		if !c.appendCapturedLocked(payload, packet.Marker, true) {
			return
		}
		if packet.Marker {
			c.commitCaptureLocked()
		}
		return
	default:
		if nalType == 1 || nalType == 2 || nalType == 3 || nalType == 4 {
			c.abortCaptureLocked()
		}
	}
}

func (c *auxUplinkIDRCache) startCaptureLocked(ts uint32) {
	c.capturing = true
	c.capturingTS = ts
	c.capturingBytes = 0
	c.capturingHasFU = false
	c.capturingPkts = nil
}

func (c *auxUplinkIDRCache) abortCaptureLocked() {
	c.capturing = false
	c.capturingTS = 0
	c.capturingBytes = 0
	c.capturingHasFU = false
	c.capturingPkts = nil
}

func (c *auxUplinkIDRCache) appendCapturedLocked(payload []byte, marker, isFUA bool) bool {
	if len(c.capturingPkts)+1 > auxCachedIDRMaxPackets || c.capturingBytes+len(payload) > auxCachedIDRMaxBytes {
		c.abortCaptureLocked()
		return false
	}
	c.capturingPkts = append(c.capturingPkts, auxCachedRTPPacket{
		payload: append([]byte(nil), payload...),
		marker:  marker,
	})
	c.capturingBytes += len(payload)
	if isFUA {
		c.capturingHasFU = true
	}
	return true
}

func (c *auxUplinkIDRCache) commitCaptureLocked() {
	if !c.capturing || len(c.capturingPkts) == 0 || len(c.sps) == 0 || len(c.pps) == 0 {
		c.abortCaptureLocked()
		return
	}
	c.ready = &auxCachedIDRSnapshot{
		timestamp:  c.capturingTS,
		receivedAt: time.Now(),
		sps:        append([]byte(nil), c.sps...),
		pps:        append([]byte(nil), c.pps...),
		packets:    c.capturingPkts,
		hasFUA:     c.capturingHasFU,
	}
	c.capturing = false
	c.capturingTS = 0
	c.capturingBytes = 0
	c.capturingHasFU = false
	c.capturingPkts = nil
}

func (s *Session) ensureAuxUplinkIDRCache() *auxUplinkIDRCache {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auxUplinkIDRCache == nil {
		s.auxUplinkIDRCache = &auxUplinkIDRCache{}
	}
	return s.auxUplinkIDRCache
}

// auxUplinkState is the WebRTC-to-SIP state of the auxiliary video stream.
// It is separate from the camera's: its own SSRC, sequence numbers and
// parameter sets. Guarded by AuxVideo.mu.
type auxUplinkState struct {
	ssrc uint32
	seq  uint16

	sps []byte
	pps []byte

	// waitIDR holds forwarding until the start of an IDR whose parameter
	// sets are known. It is set at open, whenever the SIP destination or the
	// browser track changes, and after a broken mode-0 frame.
	waitIDR bool

	// paramTS is the RTP timestamp of the access unit that already has SPS
	// and PPS in front of it. An IDR sent as several slices gets one pair.
	paramTS    uint32
	paramTSSet bool

	// SPS/PPS the browser sent in-band and that were forwarded, by timestamp.
	inBandTS    uint32
	inBandTSSet bool
	inBandSPS   bool
	inBandPPS   bool

	lastForwardedTS    uint32
	lastForwardedTSSet bool

	mode0 h264Mode0Reassembler
}

// forwardAuxUplink reads the browser's auxiliary video track and hands every
// packet to the open auxiliary stream. The loop outlives a closed stream: the
// WebRTC transceiver is kept when a share stops, so the same track carries the
// next share and OnTrack does not fire again. Packets read while no stream is
// open are discarded, which also keeps pion's congestion feedback running.
func (s *Session) forwardAuxUplink(reader rtpByteReader, trackSSRC uint32, writeRTCP func([]rtcp.Packet) error) {
	s.forwardAuxUplinkOnPC(nil, reader, trackSSRC, writeRTCP)
}

func (s *Session) forwardAuxUplinkOnPC(pc *webrtc.PeerConnection, reader rtpByteReader, trackSSRC uint32, writeRTCP func([]rtcp.Packet) error) {
	buf := make([]byte, auxBufferSize(s.RTPBufferSize))
	cache := s.ensureAuxUplinkIDRCache()
	var bound *AuxVideo
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if videoReadShouldStop(err, s.GetState() == StateEnded) {
				if bound != nil {
					bound.unbindUplinkTrack(trackSSRC)
				}
				if pc != nil {
					s.clearAuxUplinkBinding(pc, trackSSRC)
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}

		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buf[:n]); err != nil {
			continue
		}

		aux := s.AuxVideo()
		if aux == nil {
			bound = nil
			cache.observe(packet)
			continue
		}
		if aux != bound {
			aux.BindUplinkTrack(trackSSRC, writeRTCP)
			bound = aux
		}
		aux.HandleUplinkRTP(packet)
	}
}

// unbindUplinkTrack clears the browser feedback target if it still names the
// given track.
func (a *AuxVideo) unbindUplinkTrack(ssrc uint32) {
	a.mu.Lock()
	if a.uplinkTrackSSRC == ssrc {
		a.uplinkTrackSSRC = 0
		a.writeRTCP = nil
	}
	a.mu.Unlock()
}

// HandleUplinkRTP forwards one RTP packet of the browser's auxiliary video
// track to the SIP peer. The packet's payload is only read during the call.
func (a *AuxVideo) HandleUplinkRTP(packet *rtp.Packet) {
	if packet == nil {
		return
	}
	if a.uplinkIDRCache != nil {
		a.uplinkIDRCache.observe(packet)
	}
	a.mu.Lock()
	writes, requestPLI := a.processUplinkLocked(packet)
	conn, dest := a.rtpConn, a.remoteRTP
	a.mu.Unlock()

	if conn != nil && dest != nil {
		for _, out := range writes {
			if _, err := conn.WriteToUDP(out, dest); err != nil {
				a.noteUplinkWriteError(err)
			}
		}
	}
	if requestPLI {
		a.RequestKeyframeFromBrowser("uplink-wait-idr")
	}
}

// ReplayCachedIDR writes the latest complete uplink IDR access unit (with SPS
// and PPS) to the SIP peer when the stream is open and has a destination.
// This covers static screens whose initial IDR arrived while the SIP leg or
// far end was still negotiating, and whose encoder ignores PLI until pixels
// change.
func (a *AuxVideo) ReplayCachedIDR(reason string) bool {
	if a == nil || a.uplinkIDRCache == nil {
		return false
	}
	snap := a.uplinkIDRCache.snapshot(auxCachedIDRMaxAge)
	if snap == nil {
		return false
	}

	a.mu.Lock()
	if a.closed || !a.hasUplinkLocked() || a.rtpConn == nil || a.remoteRTP == nil || (a.packetizationMode == 0 && snap.hasFUA) {
		a.mu.Unlock()
		return false
	}
	u := &a.uplink
	ts := snap.timestamp
	if u.lastForwardedTSSet && int32(ts-u.lastForwardedTS) <= 0 {
		ts = u.lastForwardedTS + auxReplayTimestampStep
	}
	u.cacheSPS(snap.sps)
	u.cachePPS(snap.pps)

	writes := make([][]byte, 0, len(snap.packets)+2)
	writes = a.appendUplinkParameterSetsLocked(writes, ts, false, false)
	for _, pkt := range snap.packets {
		if out := a.marshalUplinkLocked(pkt.payload, ts, pkt.marker); out != nil {
			writes = append(writes, out)
		}
	}
	conn, dest := a.rtpConn, a.remoteRTP
	a.mu.Unlock()

	if conn == nil || dest == nil || len(writes) == 0 {
		return false
	}
	for _, out := range writes {
		if _, err := conn.WriteToUDP(out, dest); err != nil {
			a.noteUplinkWriteError(err)
		}
	}
	fmt.Printf("[%s] aux_video replayed cached IDR reason=%s packets=%d\n", a.sessionID, reason, len(writes))
	return true
}

func (a *AuxVideo) noteUplinkWriteError(err error) {
	a.mu.Lock()
	a.stats.UplinkWriteErrs++
	count := a.stats.UplinkWriteErrs
	a.mu.Unlock()
	if count <= 3 || count%500 == 0 {
		fmt.Printf("[%s] aux_video uplink write error (count=%d): %v\n", a.sessionID, count, err)
	}
}

// processUplinkLocked turns one browser RTP packet into the datagrams to send
// to the SIP peer. It reports whether the browser should be asked for a
// keyframe. The caller holds a.mu.
func (a *AuxVideo) processUplinkLocked(packet *rtp.Packet) (writes [][]byte, requestPLI bool) {
	u := &a.uplink
	a.stats.UplinkPackets++
	canWrite := !a.closed && a.hasUplinkLocked() && a.rtpConn != nil && a.remoteRTP != nil
	if len(packet.Payload) == 0 {
		// Padding-only probe from the browser's bandwidth estimator.
		// While waiting for an IDR on an active destination, nudge the browser.
		return nil, canWrite && u.waitIDR
	}

	if a.packetizationMode == 0 {
		decision := u.mode0.push(packet)
		switch decision.result {
		case h264Mode0Buffered:
			return nil, false
		case h264Mode0Dropped:
			if mode0DropArmsRecovery(decision.reason) {
				u.waitIDR = true
				return nil, canWrite
			}
			return nil, false
		}
		packet = decision.packet
		if packet == nil || len(packet.Payload) == 0 {
			return nil, false
		}
	}

	payload := packet.Payload
	nalType := payload[0] & 0x1F
	if nalType == 24 {
		return a.processUplinkSTAPALocked(packet, canWrite)
	}

	isSPS := nalType == 7
	isPPS := nalType == 8
	if isSPS {
		u.cacheSPS(payload)
	}
	if isPPS {
		u.cachePPS(payload)
	}
	isIDRStart := nalType == 5 ||
		(nalType == 28 && len(payload) > 1 && payload[1]&0x80 != 0 && payload[1]&0x1F == 5)

	if !canWrite {
		a.stats.UplinkHeld++
		return nil, false
	}
	if u.waitIDR {
		// Parameter sets are cached and re-sent in front of the IDR, so they
		// are not a reason to ask for another keyframe.
		if !isIDRStart {
			a.stats.UplinkHeld++
			return nil, !isSPS && !isPPS
		}
		if len(u.sps) == 0 || len(u.pps) == 0 {
			a.stats.UplinkHeld++
			return nil, true
		}
		u.waitIDR = false
	}

	if isIDRStart {
		writes = a.appendUplinkParameterSetsLocked(writes, packet.Timestamp, false, false)
	}
	if isSPS || isPPS {
		u.noteInBand(packet.Timestamp, isSPS, isPPS)
	}
	if out := a.marshalUplinkLocked(payload, packet.Timestamp, packet.Marker); out != nil {
		writes = append(writes, out)
	}
	return writes, false
}

// processUplinkSTAPALocked de-aggregates a STAP-A into single NAL unit
// packets, which every SIP H.264 decoder accepts in either packetization
// mode. The caller holds a.mu.
func (a *AuxVideo) processUplinkSTAPALocked(packet *rtp.Packet, canWrite bool) (writes [][]byte, requestPLI bool) {
	u := &a.uplink
	nals := splitSTAPA(packet.Payload[1:])
	containsIDR, containsSPS, containsPPS := false, false, false
	for _, nal := range nals {
		switch nal[0] & 0x1F {
		case 5:
			containsIDR = true
		case 7:
			containsSPS = true
			u.cacheSPS(nal)
		case 8:
			containsPPS = true
			u.cachePPS(nal)
		}
	}
	if len(nals) == 0 {
		return nil, false
	}

	if !canWrite {
		a.stats.UplinkHeld++
		return nil, false
	}
	if u.waitIDR {
		if !containsIDR {
			a.stats.UplinkHeld++
			return nil, !containsSPS && !containsPPS
		}
		if len(u.sps) == 0 || len(u.pps) == 0 {
			a.stats.UplinkHeld++
			return nil, true
		}
		u.waitIDR = false
	}

	if containsIDR {
		writes = a.appendUplinkParameterSetsLocked(writes, packet.Timestamp, containsSPS, containsPPS)
	} else if containsSPS || containsPPS {
		u.noteInBand(packet.Timestamp, containsSPS, containsPPS)
	}
	for i, nal := range nals {
		marker := packet.Marker && i == len(nals)-1
		if out := a.marshalUplinkLocked(nal, packet.Timestamp, marker); out != nil {
			writes = append(writes, out)
		}
	}
	return writes, false
}

// appendUplinkParameterSetsLocked puts the cached SPS and PPS in front of the
// first IDR packet of an access unit. Later slices of the same access unit,
// and an access unit whose parameter sets the browser already sent in-band,
// get none: a repeated pair between slices resets strict decoders. The caller
// holds a.mu.
func (a *AuxVideo) appendUplinkParameterSetsLocked(writes [][]byte, timestamp uint32, hasSPS, hasPPS bool) [][]byte {
	u := &a.uplink
	if u.paramTSSet && u.paramTS == timestamp {
		return writes
	}
	u.paramTS = timestamp
	u.paramTSSet = true
	a.stats.UplinkKeyframes++

	if u.inBandTSSet && u.inBandTS == timestamp {
		hasSPS = hasSPS || u.inBandSPS
		hasPPS = hasPPS || u.inBandPPS
	}
	if (hasSPS && hasPPS) || len(u.sps) == 0 || len(u.pps) == 0 {
		return writes
	}
	for _, nal := range [][]byte{u.sps, u.pps} {
		if out := a.marshalUplinkLocked(nal, timestamp, false); out != nil {
			writes = append(writes, out)
			a.stats.ParameterSetsOut++
		}
	}
	return writes
}

// marshalUplinkLocked builds one outbound RTP packet with the auxiliary
// stream's own SSRC, sequence number and SIP payload type. Header extensions,
// CSRCs and padding from the browser are not carried over. The caller holds
// a.mu.
func (a *AuxVideo) marshalUplinkLocked(payload []byte, timestamp uint32, marker bool) []byte {
	u := &a.uplink
	if u.ssrc == 0 {
		u.ssrc = generateSSRC()
		u.seq = uint16(generateSSRC())
	}
	u.seq++
	out := rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         marker,
			PayloadType:    a.payloadType,
			SequenceNumber: u.seq,
			Timestamp:      timestamp,
			SSRC:           u.ssrc,
		},
		Payload: payload,
	}
	data, err := out.Marshal()
	if err != nil {
		return nil
	}
	u.lastForwardedTS = timestamp
	u.lastForwardedTSSet = true
	a.stats.UplinkForwarded++
	return data
}

func (u *auxUplinkState) cacheSPS(nal []byte) {
	if len(nal) > 1 && !bytes.Equal(u.sps, nal) {
		u.sps = append([]byte(nil), nal...)
	}
}

func (u *auxUplinkState) cachePPS(nal []byte) {
	if len(nal) > 1 && !bytes.Equal(u.pps, nal) {
		u.pps = append([]byte(nil), nal...)
	}
}

func (u *auxUplinkState) noteInBand(timestamp uint32, sps, pps bool) {
	if !u.inBandTSSet || u.inBandTS != timestamp {
		u.inBandTS = timestamp
		u.inBandTSSet = true
		u.inBandSPS = false
		u.inBandPPS = false
	}
	u.inBandSPS = u.inBandSPS || sps
	u.inBandPPS = u.inBandPPS || pps
}

// splitSTAPA returns the NAL units inside a STAP-A payload (without its
// one-byte STAP-A header). A truncated tail is dropped.
func splitSTAPA(payload []byte) [][]byte {
	var nals [][]byte
	offset := 0
	for offset+2 <= len(payload) {
		size := int(payload[offset])<<8 | int(payload[offset+1])
		offset += 2
		if size == 0 || offset+size > len(payload) {
			break
		}
		nals = append(nals, payload[offset:offset+size])
		offset += size
	}
	return nals
}

// RequestKeyframeFromBrowser asks the browser for an IDR on the auxiliary
// track. Requests are rate limited; it reports whether one was sent.
func (a *AuxVideo) RequestKeyframeFromBrowser(reason string) bool {
	return a.requestKeyframeFromBrowser(reason, false)
}

// RequestKeyframeFromBrowserForced asks the browser for an IDR on the auxiliary
// track without applying the 1s rate limit (used when the far end becomes ready).
func (a *AuxVideo) RequestKeyframeFromBrowserForced(reason string) bool {
	return a.requestKeyframeFromBrowser(reason, true)
}

func (a *AuxVideo) requestKeyframeFromBrowser(reason string, force bool) bool {
	now := time.Now()
	a.mu.Lock()
	write := a.writeRTCP
	ssrc := a.uplinkTrackSSRC
	throttled := !force && !a.lastBrowserPLI.IsZero() && now.Sub(a.lastBrowserPLI) < auxKeyframeRequestMinInterval
	canRequest := !a.closed && a.hasUplinkLocked()
	a.mu.Unlock()
	if !canRequest || write == nil || ssrc == 0 || throttled {
		return false
	}

	if err := write([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}}); err != nil {
		// Before DTLS is up the write fails; the next held packet retries, so
		// the rate limit is not consumed here.
		text := err.Error()
		if !strings.Contains(text, "DTLS transport has not started yet") && !strings.Contains(text, "closed pipe") {
			fmt.Printf("[%s] aux_video browser PLI error reason=%s: %v\n", a.sessionID, reason, err)
		}
		return false
	}
	a.mu.Lock()
	a.lastBrowserPLI = now
	a.stats.BrowserPLISent++
	count := a.stats.BrowserPLISent
	a.mu.Unlock()
	if count <= 5 || count%50 == 0 {
		fmt.Printf("[%s] aux_video browser PLI sent reason=%s count=%d\n", a.sessionID, reason, count)
	}
	return true
}

// HandleSIPRTCP takes RTCP received from the SIP peer for the auxiliary
// stream. A PLI or FIR becomes a keyframe request to the browser. NACKs are
// not served: the uplink keeps no history and loss is repaired by keyframes.
func (a *AuxVideo) HandleSIPRTCP(data []byte) {
	packets, err := rtcp.Unmarshal(data)
	if err != nil {
		return
	}
	for _, packet := range packets {
		switch packet.(type) {
		case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
			a.RequestKeyframeFromBrowser("sip-keyframe-request")
			return
		}
	}
}
