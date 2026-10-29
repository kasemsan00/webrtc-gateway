package session

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
	pkg_webrtc "webrtc-sip-gateway/internal/pkg/webrtc"
)

// Prepared incoming PeerConnections (WS offer with prepare=true).
//
// While an incoming call rings, each presented agent may negotiate its own
// PeerConnection (ICE+DTLS) against the incoming session without claiming
// the call. A prepared PC is wired to the session callbacks but stays inert:
// its tracks are not the session tracks, so SIP media never reaches it, and
// its OnTrack readers discard agent media. AcceptPreparedIncoming claims the
// call and installs the winner's PC as the session PC under one s.mu critical
// section. Every other prepared PC is closed by the gateway on the first
// transition out of StateIncoming.

var (
	// ErrPrepareNotIncoming means the session is no longer a ringing incoming call.
	ErrPrepareNotIncoming = errors.New("incoming call is no longer ringing")
	// ErrPrepareAnsweredElsewhere means another client already claimed the call.
	ErrPrepareAnsweredElsewhere = errors.New("incoming call answered elsewhere")
	// ErrPreparedNotFound means the client holds no live prepared PeerConnection.
	ErrPreparedNotFound = errors.New("no prepared PeerConnection for this client")
)

// Reasons passed to PreparedCloseFunc. The first three match the WS `cancel`
// reasons; PreparedCloseEnded covers reject and SIP failure.
const (
	PreparedCloseAnsweredElsewhere = "answered_elsewhere"
	PreparedCloseCallerCancelled   = "caller_cancelled"
	PreparedCloseNoAnswer          = "no_answer"
	PreparedCloseEnded             = "ended"
)

// History reasons for prepared PCs that were closed without notifying the owner.
const (
	preparedClosedExpired    = "expired"
	preparedClosedDropped    = "dropped"
	preparedClosedNegotiate  = "negotiation_failed"
	preparedGatherTimeout    = 10 * time.Second
	preparedTrackReadMinSize = 1500
)

// PreparedCloseFunc is invoked (outside session locks) when the gateway closes
// a prepared PC because the incoming call left StateIncoming.
type PreparedCloseFunc func(reason string)

type preparedPeerConnection struct {
	owner      string
	pc         *webrtc.PeerConnection
	audioTrack *webrtc.TrackLocalStaticRTP
	videoTrack *webrtc.TrackLocalStaticRTP
	offerSDP   string
	onClosed   PreparedCloseFunc
	// pendingICE and timer are guarded by Session.mu.
	pendingICE []webrtc.ICECandidateInit
	timer      *time.Timer
	promoted   atomic.Bool
	closed     chan struct{}
	closeOnce  sync.Once
	// heldPackets counts agent RTP discarded before accept.
	heldPackets atomic.Int64
}

// isLive reports whether media and state callbacks for this PC may act. The
// session's own PeerConnection passes a nil entry and is always live.
func (p *preparedPeerConnection) isLive() bool {
	return p == nil || p.promoted.Load()
}

func (p *preparedPeerConnection) markClosed() {
	p.closeOnce.Do(func() { close(p.closed) })
}

// PrepareIncomingAnswer builds a prepared PeerConnection for owner, applies
// the client offer, and returns the gateway answer (ICE gathering complete).
// The call is not claimed. A repeated prepare from the same owner replaces
// that owner's previous prepared PC. ttl <= 0 disables expiry.
func (s *Session) PrepareIncomingAnswer(owner, offerSDP string, turnConfig config.TURNConfig, debugTURN bool, ttl time.Duration, onClosed PreparedCloseFunc) (string, error) {
	if owner == "" {
		return "", fmt.Errorf("prepare owner required")
	}
	s.mu.RLock()
	err := s.incomingPrepareAllowedLocked(owner)
	s.mu.RUnlock()
	if err != nil {
		return "", err
	}

	entry, audioSender, videoSender, err := s.newPreparedPeerConnection(owner, turnConfig)
	if err != nil {
		return "", err
	}
	entry.offerSDP = offerSDP
	entry.onClosed = onClosed
	s.wirePeerConnection(entry.pc, audioSender, videoSender, debugTURN, entry)

	if err := s.registerPreparedPeerConnection(entry); err != nil {
		return "", err
	}

	fail := func(err error) (string, error) {
		s.dropPrepared(entry, preparedClosedNegotiate)
		return "", err
	}
	if err := entry.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return fail(fmt.Errorf("failed to set offer: %w", err))
	}
	// The SIP answer is not built until accept. Derive its mode from the
	// pending INVITE instead of depending on earlier session initialization.
	videoPacketizationMode := s.GetSIPVideoPacketizationMode()
	modeSource := "session"
	if _, _, invite, _, _ := s.GetIncomingInvite(); len(invite) > 0 {
		videoPacketizationMode = SIPVideoPacketizationMode(invite)
		modeSource = "pending-invite"
	}
	if err := PreferWebRTCH264PacketizationMode(entry.pc, offerSDP, videoPacketizationMode); err != nil {
		return fail(fmt.Errorf("failed to select H264 packetization mode: %w", err))
	}
	fmt.Printf("[%s] prepared_pc_h264 packetization_mode=%d source=%s owner=%s\n", s.ID, videoPacketizationMode, modeSource, owner)
	answer, err := entry.pc.CreateAnswer(nil)
	if err != nil {
		return fail(fmt.Errorf("failed to create answer: %w", err))
	}
	gatherComplete := webrtc.GatheringCompletePromise(entry.pc)
	if err := entry.pc.SetLocalDescription(answer); err != nil {
		return fail(fmt.Errorf("failed to set local description: %w", err))
	}
	s.flushPreparedICE(entry)

	select {
	case <-gatherComplete:
	case <-entry.closed:
	case <-time.After(preparedGatherTimeout):
		fmt.Printf("[%s] ⚠️ prepared_pc ICE gathering timeout after %s owner=%s - answering with partial candidates\n", s.ID, preparedGatherTimeout, owner)
	}

	s.mu.RLock()
	live := s.preparedPCs[owner] == entry || entry.promoted.Load()
	stateErr := s.incomingPrepareAllowedLocked(owner)
	s.mu.RUnlock()
	if !live {
		if stateErr == nil {
			stateErr = ErrPreparedNotFound
		}
		return "", stateErr
	}
	local := entry.pc.LocalDescription()
	if local == nil {
		return fail(fmt.Errorf("prepared PeerConnection has no local description"))
	}
	s.armPreparedTTL(entry, ttl)
	return local.SDP, nil
}

// registerPreparedPeerConnection stores entry if the call still rings,
// replacing (and closing) the owner's previous prepared PC. On failure the
// entry's PC is closed.
func (s *Session) registerPreparedPeerConnection(entry *preparedPeerConnection) error {
	owner := entry.owner
	s.mu.Lock()
	if err := s.incomingPrepareAllowedLocked(owner); err != nil {
		s.mu.Unlock()
		entry.markClosed()
		ClosePeerConnectionAsync(entry.pc, s.ID)
		return err
	}
	if s.preparedPCs == nil {
		s.preparedPCs = make(map[string]*preparedPeerConnection)
	}
	replaced := s.preparedPCs[owner]
	s.preparedPCs[owner] = entry
	delete(s.preparedClosed, owner)
	if replaced != nil {
		stopPreparedTimerLocked(replaced)
	}
	count := len(s.preparedPCs)
	s.mu.Unlock()
	if replaced != nil {
		fmt.Printf("[%s] 🧊 prepared_pc_replaced owner=%s\n", s.ID, owner)
		replaced.markClosed()
		ClosePeerConnectionAsync(replaced.pc, s.ID)
	}
	fmt.Printf("[%s] 🧊 prepared_pc_created owner=%s prepared=%d\n", s.ID, owner, count)
	return nil
}

// armPreparedTTL starts the unclaimed-PC expiry once the answer is ready, so
// a short TTL cannot close the PC in the middle of negotiation.
func (s *Session) armPreparedTTL(entry *preparedPeerConnection, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preparedPCs[entry.owner] != entry || entry.timer != nil {
		return
	}
	entry.timer = time.AfterFunc(ttl, func() { s.expirePrepared(entry, ttl) })
}

func (s *Session) newPreparedPeerConnection(owner string, turnConfig config.TURNConfig) (*preparedPeerConnection, *webrtc.RTPSender, *webrtc.RTPSender, error) {
	api, err := newGatewayWebRTCAPI(s.WebRTCTWCCEnabled, s.ID, s.WebRTCICE)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create WebRTC API: %w", err)
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: pkg_webrtc.BuildICEServers(turnConfig)})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create prepared PeerConnection: %w", err)
	}
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		fmt.Sprintf("pion-audio-%s-prepared", s.ID),
	)
	if err != nil {
		pc.Close()
		return nil, nil, nil, fmt.Errorf("failed to create audio track: %w", err)
	}
	audioSender, err := pc.AddTrack(audioTrack)
	if err != nil {
		pc.Close()
		return nil, nil, nil, fmt.Errorf("failed to add audio track: %w", err)
	}
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000},
		"video",
		fmt.Sprintf("pion-video-%s-prepared", s.ID),
	)
	if err != nil {
		pc.Close()
		return nil, nil, nil, fmt.Errorf("failed to create video track: %w", err)
	}
	videoSender, err := pc.AddTrack(videoTrack)
	if err != nil {
		pc.Close()
		return nil, nil, nil, fmt.Errorf("failed to add video track: %w", err)
	}
	return &preparedPeerConnection{
		owner:      owner,
		pc:         pc,
		audioTrack: audioTrack,
		videoTrack: videoTrack,
		closed:     make(chan struct{}),
	}, audioSender, videoSender, nil
}

// incomingPrepareAllowedLocked requires s.mu (read or write).
func (s *Session) incomingPrepareAllowedLocked(owner string) error {
	if s.IncomingClaimed && s.IncomingClaimedBy != owner {
		return ErrPrepareAnsweredElsewhere
	}
	if s.TerminalAction == "accept" {
		return ErrPrepareAnsweredElsewhere
	}
	if s.State != StateIncoming || s.TerminalAction != "" {
		return ErrPrepareNotIncoming
	}
	return nil
}

// AddPreparedICECandidate applies a trickled candidate to owner's prepared PC,
// queueing it until the offer is applied. handled is false when owner has no
// prepared PC on this session.
func (s *Session) AddPreparedICECandidate(owner string, candidate webrtc.ICECandidateInit) (handled, queued bool, err error) {
	s.mu.Lock()
	entry := s.preparedPCs[owner]
	if entry == nil {
		s.mu.Unlock()
		return false, false, nil
	}
	if entry.pc.RemoteDescription() == nil {
		if len(entry.pendingICE) >= maxPendingRemoteICE {
			s.mu.Unlock()
			return true, true, fmt.Errorf("pending ICE candidate queue full")
		}
		entry.pendingICE = append(entry.pendingICE, candidate)
		s.mu.Unlock()
		return true, true, nil
	}
	pc := entry.pc
	s.mu.Unlock()
	return true, false, pc.AddICECandidate(candidate)
}

func (s *Session) flushPreparedICE(entry *preparedPeerConnection) {
	s.mu.Lock()
	queued := entry.pendingICE
	entry.pendingICE = nil
	s.mu.Unlock()
	for _, candidate := range queued {
		if err := entry.pc.AddICECandidate(candidate); err != nil {
			fmt.Printf("[%s] ⚠️ prepared_pc queued ICE candidate rejected owner=%s: %v\n", s.ID, entry.owner, err)
		}
	}
}

// AcceptPreparedIncoming is the single authority for a prepared accept. Under
// one s.mu critical section it checks that the call still rings and is not
// claimed by another client, takes the "accept" terminal action, claims the
// call for owner, installs owner's prepared PC as the session PC, and takes
// every other prepared PC. Losers are closed and notified with
// answered_elsewhere; their owners are returned.
func (s *Session) AcceptPreparedIncoming(owner string) ([]string, error) {
	s.mu.Lock()
	if err := s.incomingPrepareAllowedLocked(owner); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	entry := s.preparedPCs[owner]
	if entry == nil {
		s.mu.Unlock()
		return nil, ErrPreparedNotFound
	}
	now := time.Now()
	s.TerminalAction = "accept"
	s.TerminalActionAt = now
	s.PendingMidCallRenegotiation = nil
	s.IncomingClaimed = true
	s.IncomingClaimedBy = owner
	delete(s.preparedPCs, owner)
	stopPreparedTimerLocked(entry)
	entry.promoted.Store(true)
	oldPC := s.PeerConnection
	s.PeerConnection = entry.pc
	s.AudioTrack = entry.audioTrack
	s.VideoTrack = entry.videoTrack
	s.pendingRemoteICE = nil
	s.UpdatedAt = now
	losers := s.takePreparedLocked(PreparedCloseAnsweredElsewhere)
	s.mu.Unlock()

	if oldPC != nil && oldPC != entry.pc {
		ClosePeerConnectionAsync(oldPC, s.ID)
	}
	if sps, pps, ok := ExtractH264SpropParameterSets(entry.offerSDP); ok {
		s.SetCachedSPSPPS(sps, pps, "ws-offer-prepared")
	}
	fmt.Printf("[%s] ✅ prepared_pc_promoted owner=%s losers=%d\n", s.ID, owner, len(losers))
	return s.closePreparedEntries(losers, PreparedCloseAnsweredElsewhere, true), nil
}

// ClosePreparedIncoming closes every prepared PC on this session, notifies
// each owner with reason, and returns the owners.
func (s *Session) ClosePreparedIncoming(reason string) []string {
	s.mu.Lock()
	entries := s.takePreparedLocked(reason)
	s.mu.Unlock()
	return s.closePreparedEntries(entries, reason, true)
}

// DropPreparedPeerConnection closes owner's prepared PC without notifying it
// (for example when its WebSocket disconnected). Returns true when one existed.
func (s *Session) DropPreparedPeerConnection(owner string) bool {
	s.mu.Lock()
	entry := s.preparedPCs[owner]
	s.mu.Unlock()
	if entry == nil {
		return false
	}
	return s.dropPrepared(entry, preparedClosedDropped)
}

func (s *Session) dropPrepared(entry *preparedPeerConnection, why string) bool {
	s.mu.Lock()
	if s.preparedPCs[entry.owner] != entry {
		s.mu.Unlock()
		return false
	}
	delete(s.preparedPCs, entry.owner)
	stopPreparedTimerLocked(entry)
	s.rememberPreparedClosedLocked(entry.owner, why)
	s.mu.Unlock()
	fmt.Printf("[%s] 🧊 prepared_pc_closed owner=%s reason=%s\n", s.ID, entry.owner, why)
	s.closePreparedEntries([]*preparedPeerConnection{entry}, why, false)
	return true
}

func (s *Session) expirePrepared(entry *preparedPeerConnection, ttl time.Duration) {
	if s.dropPrepared(entry, preparedClosedExpired) {
		fmt.Printf("[%s] ⏱️ prepared_pc_expired owner=%s ttl=%s\n", s.ID, entry.owner, ttl)
	}
}

// takePreparedLocked removes all prepared PCs; requires s.mu held for writing.
func (s *Session) takePreparedLocked(reason string) []*preparedPeerConnection {
	if len(s.preparedPCs) == 0 {
		return nil
	}
	entries := make([]*preparedPeerConnection, 0, len(s.preparedPCs))
	for owner, entry := range s.preparedPCs {
		stopPreparedTimerLocked(entry)
		s.rememberPreparedClosedLocked(owner, reason)
		entries = append(entries, entry)
	}
	s.preparedPCs = nil
	return entries
}

func (s *Session) rememberPreparedClosedLocked(owner, reason string) {
	if s.preparedClosed == nil {
		s.preparedClosed = make(map[string]string)
	}
	s.preparedClosed[owner] = reason
}

func stopPreparedTimerLocked(entry *preparedPeerConnection) {
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
}

// closePreparedEntries must run without s.mu held.
func (s *Session) closePreparedEntries(entries []*preparedPeerConnection, reason string, notify bool) []string {
	owners := make([]string, 0, len(entries))
	for _, entry := range entries {
		owners = append(owners, entry.owner)
		entry.markClosed()
		ClosePeerConnectionAsync(entry.pc, s.ID)
		if notify {
			fmt.Printf("[%s] 🧊 prepared_pc_closed owner=%s reason=%s\n", s.ID, entry.owner, reason)
			if entry.onClosed != nil {
				entry.onClosed(reason)
			}
		}
	}
	return owners
}

// preparedCloseReasonLocked maps why the call left StateIncoming to the WS
// cancel reason for the remaining prepared owners. Requires s.mu.
func (s *Session) preparedCloseReasonLocked() string {
	if s.IncomingClaimed || s.TerminalAction == "accept" {
		return PreparedCloseAnsweredElsewhere
	}
	switch s.TerminalAction {
	case "caller_cancel":
		return PreparedCloseCallerCancelled
	case "timeout":
		return PreparedCloseNoAnswer
	}
	return PreparedCloseEnded
}

// HasPreparedPeerConnection reports whether owner holds a live prepared PC.
func (s *Session) HasPreparedPeerConnection(owner string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preparedPCs[owner] != nil
}

// PreparedClosedReason reports why owner's last prepared PC on this session
// was closed, if it ever had one.
func (s *Session) PreparedClosedReason(owner string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reason, ok := s.preparedClosed[owner]
	return reason, ok
}

// PreparedPeerConnectionCount returns the number of live prepared PCs.
func (s *Session) PreparedPeerConnectionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.preparedPCs)
}

// holdPreparedTrack discards agent media on a prepared PC until it is
// promoted, then hands the track to the normal forwarding path. The first
// packet read after promotion is dropped; the post-accept keyframe kick
// requests a fresh IDR anyway.
func (s *Session) holdPreparedTrack(prepared *preparedPeerConnection, track *webrtc.TrackRemote, release func()) {
	kind := track.Kind().String()
	fmt.Printf("[%s] 🎬 prepared_pc OnTrack kind=%s ssrc=%d owner=%s: media held until accept\n", s.ID, kind, track.SSRC(), prepared.owner)
	size := s.RTPBufferSize
	if size < preparedTrackReadMinSize {
		size = preparedTrackReadMinSize
	}
	buf := make([]byte, size)
	for {
		if prepared.promoted.Load() {
			fmt.Printf("[%s] 🎬 prepared_pc track released kind=%s held_packets=%d\n", s.ID, kind, prepared.heldPackets.Load())
			release()
			return
		}
		if _, _, err := track.Read(buf); err != nil {
			return
		}
		if !prepared.promoted.Load() {
			prepared.heldPackets.Add(1)
		}
	}
}

// KickMediaAfterPreparedAccept requests keyframes both ways once a prepared
// PC carries the call. Its ICE-connected startup recovery ran while the PC was
// inert, so nothing has asked either encoder for an IDR yet.
func (s *Session) KickMediaAfterPreparedAccept() {
	const reason = "prepared-accept"
	s.RequestSIPVideoIDRReplay(reason)
	s.SendFIRToAsterisk()
	s.SendPLIToAsteriskForced(reason)
	s.KickUplinkKeyframeForSIPDecoder(reason)
}
