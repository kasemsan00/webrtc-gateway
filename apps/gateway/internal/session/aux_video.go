package session

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// AuxVideo is a session's auxiliary video stream: a second, independent H.264
// stream (a shared screen) carried next to the camera. It owns its RTP and
// RTCP sockets, its SSRC and sequence space, its parameter-set cache and its
// tracks, and takes none of the camera pipeline's locks. A failure here must
// never stop, reset or renegotiate the primary audio or video path.
//
// The pipeline is deliberately small: no switch gate, blackout, access-unit
// normalizer or proactive keyframe schedule. Loss is repaired by NACK from a
// short history and by keyframe requests.
//
// Lock order: egressMu, then mu. Neither is held across socket or track I/O
// except egressMu around the WebRTC track write.
type AuxVideo struct {
	sessionID string

	mu     sync.Mutex
	closed bool

	// SIP side: the gateway's own sockets and the peer's endpoints.
	rtpConn    *net.UDPConn
	rtcpConn   *net.UDPConn
	rtpPort    int
	rtcpPort   int
	remoteRTP  *net.UDPAddr
	remoteRTCP *net.UDPAddr
	rtcpMux    bool
	// SIP payload type and RFC 6184 packetization mode of the auxiliary m-line.
	payloadType       uint8
	packetizationMode uint8
	sipDirection      string
	// peerConfirmed is set once the far end reported it shows the stream.
	peerConfirmed bool
	// keyframeRelay, when set, is an extra path for a keyframe request to
	// the sending side that does not depend on the SIP peer forwarding RTCP.
	keyframeRelay func()

	uplink         auxUplinkState
	uplinkIDRCache *auxUplinkIDRCache

	// Browser feedback target for the uplink track.
	uplinkTrackSSRC uint32
	writeRTCP       func([]rtcp.Packet) error
	lastBrowserPLI  time.Time

	// Downlink (SIP to WebRTC).
	track           auxRTPWriter
	remoteSSRC      uint32
	egressSeqOffset uint16
	lastSIPPLI      time.Time
	pendingSIPPLI   bool
	reorder         *VideoReorderBuffer
	history         auxRTPHistory

	// egressMu fences WebRTC track writes against detaching the track before
	// its PeerConnection closes. Writing a TrackLocalStaticRTP concurrently
	// with Close can deadlock pion (see video_egress.go).
	egressMu sync.Mutex

	stats AuxVideoStats
}

// AuxVideoStats counts what the auxiliary pipeline did. Read with Stats.
type AuxVideoStats struct {
	UplinkPackets    int
	UplinkForwarded  int
	UplinkHeld       int
	UplinkKeyframes  int
	UplinkWriteErrs  int
	DownlinkPackets  int
	DownlinkWritten  int
	DownlinkDropped  int
	DownlinkErrs     int
	BrowserPLISent   int
	SIPPLISent       int
	NACKRetransmits  int
	NACKMisses       int
	ParameterSetsOut int
}

const (
	auxDefaultPayloadType = 96
	// A screen IDR is large. Keyframe requests are spaced further apart than
	// the camera's so a lossy uplink is not flooded with intra frames.
	auxKeyframeRequestMinInterval = time.Second
)

var (
	errAuxVideoOpen   = errors.New("auxiliary video already open")
	errAuxVideoClosed = errors.New("auxiliary video closed")
)

// OpenAuxVideo attaches an auxiliary video stream to the session using the
// given SIP-side sockets. It fails when one is already open or the session is
// closed; the caller keeps ownership of the sockets in that case.
func (s *Session) OpenAuxVideo(rtpConn, rtcpConn *net.UDPConn, rtpPort, rtcpPort int) (*AuxVideo, error) {
	if s == nil {
		return nil, errAuxVideoClosed
	}
	aux := &AuxVideo{
		sessionID:         s.ID,
		rtpConn:           rtpConn,
		rtcpConn:          rtcpConn,
		rtpPort:           rtpPort,
		rtcpPort:          rtcpPort,
		payloadType:       auxDefaultPayloadType,
		packetizationMode: 1,
	}
	aux.uplink.waitIDR = true
	aux.reorder = NewVideoReorderBuffer(s.ID+"/aux", aux.writeDownlink)
	aux.reorder.SetSkipHandler(func(skipped int, fromSeq, toSeq uint16) {
		aux.RequestKeyframeFromSIP("reorder-gap")
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosedLocked() {
		return nil, errAuxVideoClosed
	}
	if s.auxVideo != nil {
		return nil, errAuxVideoOpen
	}
	if s.auxUplinkIDRCache == nil {
		s.auxUplinkIDRCache = &auxUplinkIDRCache{}
	}
	aux.uplinkIDRCache = s.auxUplinkIDRCache
	if s.auxUplinkSSRC != 0 && s.auxUplinkWriteRTCP != nil && s.auxUplinkPC != nil && s.auxUplinkPC == s.PeerConnection {
		aux.uplinkTrackSSRC = s.auxUplinkSSRC
		aux.writeRTCP = s.auxUplinkWriteRTCP
		fmt.Printf("[%s] aux_video_uplink_rebound ssrc=%d\n", s.ID, s.auxUplinkSSRC)
	}
	s.auxVideo = aux
	s.UpdatedAt = time.Now()
	fmt.Printf("[%s] aux_video_opened rtp_port=%d rtcp_port=%d\n", s.ID, rtpPort, rtcpPort)
	return aux, nil
}

// AuxVideo returns the session's open auxiliary video stream, or nil.
func (s *Session) AuxVideo() *AuxVideo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auxVideo
}

// CloseAuxVideo detaches and closes the auxiliary video stream. It reports
// whether one was open. Safe to call more than once and from any teardown
// path; it never touches the primary media transports.
func (s *Session) CloseAuxVideo(reason string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	aux := s.auxVideo
	s.auxVideo = nil
	cache := s.auxUplinkIDRCache
	if aux != nil {
		s.UpdatedAt = time.Now()
	}
	s.mu.Unlock()
	if aux == nil {
		return false
	}
	if cache != nil {
		cache.reset()
	}
	aux.close()
	fmt.Printf("[%s] aux_video_closed reason=%s stats=%+v\n", s.ID, reason, aux.Stats())
	return true
}

// detachAuxVideoTrack stops auxiliary WebRTC writes before a PeerConnection
// that carries the auxiliary track is closed or replaced. The SIP side stays
// open; a new track can be attached with SetLocalTrack.
func (s *Session) detachAuxVideoTrack() {
	if aux := s.AuxVideo(); aux != nil {
		aux.SetLocalTrack(nil)
		aux.BindUplinkTrack(0, nil)
	}
	s.mu.Lock()
	s.auxUplinkPC = nil
	s.auxUplinkSSRC = 0
	s.auxUplinkWriteRTCP = nil
	cache := s.auxUplinkIDRCache
	s.mu.Unlock()
	if cache != nil {
		cache.reset()
	}
	s.dropAuxDownlinkTrack()
}

func (s *Session) clearAuxUplinkBinding(pc *webrtc.PeerConnection, ssrc uint32) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.auxUplinkPC == pc && s.auxUplinkSSRC == ssrc {
		s.auxUplinkPC = nil
		s.auxUplinkSSRC = 0
		s.auxUplinkWriteRTCP = nil
	}
	s.mu.Unlock()
}

// SetAuxUplinkMid names the WebRTC media section (a=mid) whose incoming track
// is the auxiliary video. It must be set before the client offer that adds
// that section is applied, so OnTrack can route the track away from the
// camera's forwarder. It outlives a closed stream because the transceiver is
// kept for the next share. An empty mid clears it.
func (s *Session) SetAuxUplinkMid(mid string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.auxUplinkMid = mid
	s.mu.Unlock()
}

// AuxUplinkMid returns the mid set by SetAuxUplinkMid.
func (s *Session) AuxUplinkMid() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auxUplinkMid
}

// isAuxUplinkMid reports whether a track received on the given mid is the
// auxiliary video. It does not depend on a stream being open: a track on that
// media section must never reach the camera's forwarder.
func (s *Session) isAuxUplinkMid(mid string) bool {
	if mid == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auxUplinkMid == mid
}

// routeAuxUplinkTrack takes a remote video track that arrived on the
// auxiliary media section and forwards it on the auxiliary pipeline. It
// reports false for every other track, which then follows the primary path.
func (s *Session) routeAuxUplinkTrack(pc *webrtc.PeerConnection, track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) bool {
	if pc == nil || track == nil || track.Kind() != webrtc.RTPCodecTypeVideo {
		return false
	}
	mid := transceiverMidForReceiver(pc, receiver)
	if !s.isAuxUplinkMid(mid) {
		return false
	}
	ssrc := uint32(track.SSRC())
	s.mu.Lock()
	s.auxUplinkPC = pc
	s.auxUplinkSSRC = ssrc
	s.auxUplinkWriteRTCP = pc.WriteRTCP
	aux := s.auxVideo
	s.mu.Unlock()
	fmt.Printf("[%s] aux_video_uplink_track mid=%s ssrc=%d pt=%d\n", s.ID, mid, track.SSRC(), track.PayloadType())
	if aux != nil {
		aux.BindUplinkTrack(ssrc, pc.WriteRTCP)
		if aux.RemoteRTPAddr() != nil {
			aux.RequestKeyframeFromBrowser("uplink-track-routed")
		}
	}
	go s.drainReceiverRTCP(receiver)
	go s.forwardAuxUplinkOnPC(pc, trackRTPReader{track: track}, ssrc, pc.WriteRTCP)
	return true
}

// drainReceiverRTCP reads and discards RTCP on a receiver so pion's
// interceptors keep seeing it.
func (s *Session) drainReceiverRTCP(receiver *webrtc.RTPReceiver) {
	if receiver == nil {
		return
	}
	buf := make([]byte, auxBufferSize(s.RTPBufferSize))
	for {
		if _, _, err := receiver.Read(buf); err != nil {
			return
		}
	}
}

func transceiverMidForReceiver(pc *webrtc.PeerConnection, receiver *webrtc.RTPReceiver) string {
	if pc == nil || receiver == nil {
		return ""
	}
	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Receiver() == receiver {
			return transceiver.Mid()
		}
	}
	return ""
}

func auxBufferSize(size int) int {
	if size < 1500 {
		return 1500
	}
	return size
}

// Ports returns the gateway's local RTP and RTCP ports for the stream.
func (a *AuxVideo) Ports() (rtpPort, rtcpPort int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rtpPort, a.rtcpPort
}

// Stats returns a snapshot of the pipeline counters.
func (a *AuxVideo) Stats() AuxVideoStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stats
}

// SetRemoteEndpoints sets where auxiliary RTP and RTCP are sent on the SIP
// side. rtcpMux means the peer takes RTCP on its RTP port; otherwise a nil
// rtcp address defaults to the RTP port plus one. When the RTP destination
// changes, uplink forwarding restarts at the next keyframe, which is requested
// here; repeating the current destination (a session refresh) changes nothing.
func (a *AuxVideo) SetRemoteEndpoints(rtp, rtcpAddr *net.UDPAddr, rtcpMux bool) {
	a.mu.Lock()
	moved := !sameUDPAddr(a.remoteRTP, rtp)
	a.remoteRTP = cloneUDPAddr(rtp)
	a.rtcpMux = rtcpMux
	switch {
	case rtcpAddr != nil:
		a.remoteRTCP = cloneUDPAddr(rtcpAddr)
	case rtp != nil && !rtcpMux:
		a.remoteRTCP = &net.UDPAddr{IP: append(net.IP(nil), rtp.IP...), Port: rtp.Port + 1, Zone: rtp.Zone}
	default:
		a.remoteRTCP = nil
	}
	if moved {
		a.uplink.waitIDR = true
	}
	a.mu.Unlock()
	if moved {
		a.RequestKeyframeFromBrowser("sip-endpoint-set")
	}
}

// RemoteRTPAddr returns the SIP peer's RTP address for the stream, or nil.
func (a *AuxVideo) RemoteRTPAddr() *net.UDPAddr {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneUDPAddr(a.remoteRTP)
}

// RemoteRTCPAddr returns the SIP peer's RTCP address for the stream, or nil.
func (a *AuxVideo) RemoteRTCPAddr() *net.UDPAddr {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneUDPAddr(a.remoteRTCP)
}

// LearnSymmetricRTP updates the peer's auxiliary RTP destination port when
// symmetric RTP packets arrive from the same peer IP on a different UDP port.
func (a *AuxVideo) LearnSymmetricRTP(remoteAddr *net.UDPAddr) {
	if a == nil || remoteAddr == nil || remoteAddr.IP == nil || remoteAddr.Port <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	if a.remoteRTP == nil || a.remoteRTP.IP.IsUnspecified() ||
		(a.remoteRTP.IP.Equal(remoteAddr.IP) && a.remoteRTP.Port != remoteAddr.Port) {
		oldPort := 0
		if a.remoteRTP != nil {
			oldPort = a.remoteRTP.Port
		}
		a.remoteRTP = cloneUDPAddr(remoteAddr)
		if !a.rtcpMux && a.remoteRTCP != nil && a.remoteRTCP.IP.Equal(remoteAddr.IP) && a.remoteRTCP.Port == oldPort+1 {
			a.remoteRTCP.Port = remoteAddr.Port + 1
		}
		fmt.Printf("[%s] aux_video symmetric RTP learned remote=%s\n", a.sessionID, remoteAddr)
	}
}

// LearnSymmetricRTCP updates the peer's auxiliary RTCP destination port when
// non-muxed RTCP packets arrive from the same peer IP on a different UDP port.
func (a *AuxVideo) LearnSymmetricRTCP(remoteAddr *net.UDPAddr) {
	if a == nil || remoteAddr == nil || remoteAddr.IP == nil || remoteAddr.Port <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.rtcpMux {
		return
	}
	if a.remoteRTCP == nil || a.remoteRTCP.IP.IsUnspecified() ||
		(a.remoteRTCP.IP.Equal(remoteAddr.IP) && a.remoteRTCP.Port != remoteAddr.Port) {
		a.remoteRTCP = cloneUDPAddr(remoteAddr)
		fmt.Printf("[%s] aux_video symmetric RTCP learned remote=%s\n", a.sessionID, remoteAddr)
	}
}

// SetSIPDirection records the gateway's SDP direction for the auxiliary
// m-line: "sendonly" on a sharing-only leg (the default), "recvonly" on a
// receiving-only leg, or "sendrecv" when both legs share simultaneously.
func (a *AuxVideo) SetSIPDirection(direction string) {
	a.mu.Lock()
	a.sipDirection = direction
	a.mu.Unlock()
}

// HasUplink reports whether this auxiliary stream currently sends local
// screen media toward the SIP peer ("sendonly" or "sendrecv").
func (a *AuxVideo) HasUplink() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasUplinkLocked()
}

func (a *AuxVideo) hasUplinkLocked() bool {
	if a.closed {
		return false
	}
	return a.sipDirection == "" || a.sipDirection == "sendonly" || a.sipDirection == "sendrecv"
}

// HasDownlink reports whether this auxiliary stream currently receives remote
// screen media from the SIP peer ("recvonly" or "sendrecv").
func (a *AuxVideo) HasDownlink() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasDownlinkLocked()
}

func (a *AuxVideo) hasDownlinkLocked() bool {
	if a.closed {
		return false
	}
	return a.sipDirection == "recvonly" || a.sipDirection == "sendrecv"
}

// PrepareUplinkStart resets uplink framing and peer confirmation before
// activating the local screen direction on an existing receiving stream.
func (a *AuxVideo) PrepareUplinkStart() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.peerConfirmed = false
	a.uplink.waitIDR = true
	a.uplink.paramTSSet = false
	a.uplink.inBandTSSet = false
	a.mu.Unlock()
}

// StopUplink disables the local screen uplink while keeping an active
// downlink open on the same SIP m-line ("sendrecv" -> "recvonly").
func (a *AuxVideo) StopUplink() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.closed && a.sipDirection == "sendrecv" {
		a.sipDirection = "recvonly"
	}
	a.peerConfirmed = false
	a.uplink.waitIDR = true
	a.uplink.paramTSSet = false
	a.uplink.inBandTSSet = false
	cache := a.uplinkIDRCache
	a.mu.Unlock()
	if cache != nil {
		cache.reset()
	}
}

// StopDownlink disables the remote screen downlink while keeping an active
// local uplink open on the same SIP m-line ("sendrecv" -> "sendonly").
func (a *AuxVideo) StopDownlink() {
	if a == nil {
		return
	}
	a.egressMu.Lock()
	a.mu.Lock()
	if !a.closed && a.sipDirection == "sendrecv" {
		a.sipDirection = "sendonly"
	}
	a.track = nil
	a.remoteSSRC = 0
	a.pendingSIPPLI = false
	reorder := a.reorder
	a.mu.Unlock()
	a.egressMu.Unlock()
	if reorder != nil {
		reorder.Reset()
	}
}

// MarkPeerConfirmed records that the far end shows the stream. It reports
// true the first time only.
func (a *AuxVideo) MarkPeerConfirmed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.peerConfirmed {
		return false
	}
	a.peerConfirmed = true
	return true
}

// ResetPeerConfirmed clears peer confirmation so a new local share on a
// reused stream must be confirmed again.
func (a *AuxVideo) ResetPeerConfirmed() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.peerConfirmed = false
	a.mu.Unlock()
}

// PeerConfirmed reports whether MarkPeerConfirmed was called.
func (a *AuxVideo) PeerConfirmed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.peerConfirmed
}

// SetKeyframeRelay sets a function called, without locks held, whenever a
// keyframe is requested from the SIP side. The SIP peer may not forward RTCP
// feedback for a second video stream, so the receiving leg also asks the
// sending leg directly when both are on this gateway.
func (a *AuxVideo) SetKeyframeRelay(relay func()) {
	a.mu.Lock()
	a.keyframeRelay = relay
	a.mu.Unlock()
}

// SIPDirection returns the direction set by SetSIPDirection.
func (a *AuxVideo) SIPDirection() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sipDirection == "" {
		return "sendonly"
	}
	return a.sipDirection
}

// SetSIPFormat sets the H.264 payload type and packetization mode negotiated
// for the auxiliary m-line. A zero payload type keeps the default.
func (a *AuxVideo) SetSIPFormat(payloadType, packetizationMode uint8) {
	a.mu.Lock()
	if payloadType != 0 {
		a.payloadType = payloadType
	}
	if packetizationMode != 0 {
		packetizationMode = 1
	}
	a.packetizationMode = packetizationMode
	a.mu.Unlock()
}

// BindUplinkTrack records the browser's SSRC for the auxiliary track and how
// to send it RTCP, so keyframes can be requested. A nil writer unbinds.
func (a *AuxVideo) BindUplinkTrack(ssrc uint32, writeRTCP func([]rtcp.Packet) error) {
	a.mu.Lock()
	sameBinding := ssrc != 0 && writeRTCP != nil && a.uplinkTrackSSRC == ssrc && a.writeRTCP != nil
	a.uplinkTrackSSRC = ssrc
	a.writeRTCP = writeRTCP
	if !sameBinding {
		a.uplink.waitIDR = true
	}
	a.mu.Unlock()
}

// SetLocalTrack attaches the WebRTC track that carries the auxiliary stream
// to the client, or detaches it with nil. It waits for an in-flight write.
// Attaching asks the SIP side for a keyframe so the new track starts clean.
func (a *AuxVideo) SetLocalTrack(track *webrtc.TrackLocalStaticRTP) {
	var writer auxRTPWriter
	if track != nil {
		writer = track
	}
	a.setLocalWriter(writer)
}

func (a *AuxVideo) setLocalWriter(writer auxRTPWriter) {
	a.egressMu.Lock()
	a.mu.Lock()
	a.track = writer
	a.mu.Unlock()
	a.egressMu.Unlock()
	if writer != nil {
		a.RequestKeyframeFromSIP("track-attached")
	}
}

func (a *AuxVideo) close() {
	a.egressMu.Lock()
	a.mu.Lock()
	a.closed = true
	a.track = nil
	a.writeRTCP = nil
	rtpConn, rtcpConn := a.rtpConn, a.rtcpConn
	a.rtpConn, a.rtcpConn = nil, nil
	reorder := a.reorder
	a.mu.Unlock()
	a.egressMu.Unlock()

	if reorder != nil {
		reorder.Reset()
	}
	if rtpConn != nil {
		if err := rtpConn.Close(); err != nil {
			fmt.Printf("[%s] aux_video close RTP conn error: %v\n", a.sessionID, err)
		}
	}
	if rtcpConn != nil {
		if err := rtcpConn.Close(); err != nil {
			fmt.Printf("[%s] aux_video close RTCP conn error: %v\n", a.sessionID, err)
		}
	}
}
