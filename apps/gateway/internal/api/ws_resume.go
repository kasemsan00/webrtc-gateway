package api

import (
	"context"
	"fmt"
	"log"
	"time"

	"k2-gateway/internal/session"
)

const (
	// Log slow resume requests to help diagnose renegotiation/network bottlenecks.
	resumeSlowLogThreshold = 3 * time.Second
)

type resumeVideoOfferDiagnostics = session.OfferVideoDiagnostics

func analyzeResumeOfferVideoSDP(sdp string) session.OfferVideoDiagnostics {
	return session.AnalyzeOfferVideo(sdp)
}

func hasActiveVideoMedia(sdp string) bool {
	return session.AnalyzeOfferVideo(sdp).HasActiveVideo()
}

// handleWSResume handles WebSocket resume messages for reconnecting after network change
// This allows a client to resume an existing call session after a WebSocket disconnection
// If SDP is provided, it renegotiates the PeerConnection to establish a fresh WebRTC connection
func (s *Server) handleWSResume(client *WSClient, msg WSMessage) {
	resumeStartedAt := time.Now()
	defer func() {
		elapsed := time.Since(resumeStartedAt)
		if elapsed > resumeSlowLogThreshold {
			log.Printf("⚠️ Slow resume request: session=%s elapsed=%s hasSDP=%v", msg.SessionID, elapsed.Round(10*time.Millisecond), msg.SDP != "")
		}
	}()

	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required for resume")
		return
	}

	log.Printf("🔄 Resume request for session: %s (has SDP: %v)", msg.SessionID, msg.SDP != "")
	log.Printf("📊 Resume timing start: session=%s", msg.SessionID)

	localLookupStartedAt := time.Now()
	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	localLookupElapsed := time.Since(localLookupStartedAt).Round(10 * time.Millisecond)
	log.Printf("📊 Resume local session lookup: session=%s found=%v elapsed=%s", msg.SessionID, ok, localLookupElapsed)

	// Local-first: only hit directory when local session is missing.
	if !ok && s.logStore != nil && s.gatewayConfig.InstanceID != "" {
		dirLookupStartedAt := time.Now()
		lookupCtx, cancelLookup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelLookup()

		ownerInstanceID, wsURL, found, err := s.logStore.LookupSessionDirectory(lookupCtx, msg.SessionID)
		dirLookupElapsed := time.Since(dirLookupStartedAt).Round(10 * time.Millisecond)
		if err != nil {
			log.Printf("⚠️ Resume directory lookup failed for %s: %v (elapsed=%s)", msg.SessionID, err, dirLookupElapsed)
		} else if found && ownerInstanceID != s.gatewayConfig.InstanceID {
			log.Printf("🔀 Session %s is owned by instance %s, redirecting to %s (lookup_elapsed=%s)", msg.SessionID, ownerInstanceID, wsURL, dirLookupElapsed)
			response := WSMessage{
				Type:        "resume_redirect",
				SessionID:   msg.SessionID,
				RedirectURL: wsURL,
			}
			s.sendWSMessage(client, response)
			return
		} else {
			log.Printf("📊 Resume directory lookup: session=%s found=%v owner=%s elapsed=%s", msg.SessionID, found, ownerInstanceID, dirLookupElapsed)
		}

		// Re-check local session once after directory lookup in case of race with in-memory restore.
		sess, ok = s.sessionMgr.GetSession(msg.SessionID)
		log.Printf("📊 Resume local session recheck: session=%s found=%v", msg.SessionID, ok)
	}

	if !ok {
		log.Printf("❌ Resume failed: session %s not found", msg.SessionID)
		response := WSMessage{
			Type:      "resume_failed",
			SessionID: msg.SessionID,
			Reason:    "Session not found or expired",
		}
		s.sendWSMessage(client, response)
		return
	}

	mediaStatus := sess.GetMediaEndpointStatus()
	log.Printf("🔍 Resume media status for %s: audioRTP=%v:%d videoRTP=%v:%d audioRTCP=%v:%d videoRTCP=%v:%d hasAsteriskAudio=%v hasAsteriskVideo=%v",
		msg.SessionID,
		mediaStatus.AudioRTPReady,
		mediaStatus.AudioRTPPort,
		mediaStatus.VideoRTPReady,
		mediaStatus.VideoRTPPort,
		mediaStatus.AudioRTCPReady,
		mediaStatus.AudioRTCPPort,
		mediaStatus.VideoRTCPReady,
		mediaStatus.VideoRTCPPort,
		mediaStatus.HasAsteriskAudio,
		mediaStatus.HasAsteriskVideo,
	)

	if (mediaStatus.HasAsteriskAudio && !mediaStatus.AudioRTPReady) ||
		(mediaStatus.HasAsteriskVideo && !mediaStatus.VideoRTPReady) {
		reason := "Session media endpoints expired - cannot resume"
		log.Printf(
			"❌ Resume failed: session %s media endpoints unavailable (reason=%s audioRTP=%v:%d videoRTP=%v:%d audioRTCP=%v:%d videoRTCP=%v:%d hasAsteriskAudio=%v hasAsteriskVideo=%v)",
			msg.SessionID,
			reason,
			mediaStatus.AudioRTPReady,
			mediaStatus.AudioRTPPort,
			mediaStatus.VideoRTPReady,
			mediaStatus.VideoRTPPort,
			mediaStatus.AudioRTCPReady,
			mediaStatus.AudioRTCPPort,
			mediaStatus.VideoRTCPReady,
			mediaStatus.VideoRTCPPort,
			mediaStatus.HasAsteriskAudio,
			mediaStatus.HasAsteriskVideo,
		)
		response := WSMessage{
			Type:      "resume_failed",
			SessionID: msg.SessionID,
			Reason:    reason,
		}
		s.sendWSMessage(client, response)
		return
	}

	// Check if the session is still in an active call state (including reconnecting state)
	state := sess.GetState()
	if state != session.StateActive &&
		state != session.StateConnecting &&
		state != session.StateRinging &&
		state != session.StateReconnecting {
		log.Printf("❌ Resume failed: session %s is in state %s (not resumable)", msg.SessionID, state)
		response := WSMessage{
			Type:      "resume_failed",
			SessionID: msg.SessionID,
			Reason:    fmt.Sprintf("Session is in state '%s', cannot resume", state),
		}
		s.sendWSMessage(client, response)
		return
	}

	// If session was in reconnecting state, transition back to active only after resume succeeds.
	wasReconnecting := state == session.StateReconnecting

	// Remove old client mapping if exists (from previous WebSocket connection)
	s.mu.Lock()
	oldClient, hadOldClient := s.wsClients[msg.SessionID]
	if hadOldClient && oldClient != client {
		// Clear old client's sessionID to prevent its cleanup from deleting the new client
		oldClient.sessionID = ""
		log.Printf("🔄 Replacing old WebSocket client for session %s", msg.SessionID)
	}
	// Associate the new client with the session
	s.wsClients[msg.SessionID] = client
	client.sessionID = msg.SessionID
	s.mu.Unlock()
	s.notifyWSClientChanged("updated", client)
	if hadOldClient && oldClient != client {
		s.notifyWSClientChanged("updated", oldClient)
	}

	// If client provided SDP, renegotiate the PeerConnection
	var answerSDP string
	if msg.SDP != "" {
		renegotiateStartedAt := time.Now()
		log.Printf("🔄 Renegotiating PeerConnection for session %s", msg.SessionID)
		videoDiag := analyzeResumeOfferVideoSDP(msg.SDP)
		log.Printf("🔍 Resume SDP video diagnostics: session=%s hasVideoMLine=%v videoPort=%d videoDirection=%s",
			msg.SessionID,
			videoDiag.HasVideoMLine,
			videoDiag.VideoPort,
			videoDiag.VideoDirection,
		)
		if videoDiag.HasVideoMLine && (videoDiag.VideoPort == 0 || videoDiag.VideoDirection == "recvonly" || videoDiag.VideoDirection == "inactive") {
			log.Printf("⚠️ Resume SDP indicates no client video uplink: session=%s videoPort=%d videoDirection=%s",
				msg.SessionID,
				videoDiag.VideoPort,
				videoDiag.VideoDirection,
			)
		}

		// Renegotiate with the new SDP offer
		if err := sess.RenegotiatePeerConnection(msg.SDP, s.turnConfig, s.config.DebugTURN); err != nil {
			log.Printf("❌ Resume renegotiation failed for session %s: %v (elapsed=%s)", msg.SessionID, err, time.Since(renegotiateStartedAt).Round(10*time.Millisecond))
			response := WSMessage{
				Type:      "resume_failed",
				SessionID: msg.SessionID,
				Reason:    fmt.Sprintf("Failed to renegotiate: %v", err),
			}
			s.sendWSMessage(client, response)
			return
		}
		log.Printf("📊 Resume renegotiation elapsed: session=%s elapsed=%s", msg.SessionID, time.Since(renegotiateStartedAt).Round(10*time.Millisecond))

		// Get the answer SDP to send back to client
		if sess.PeerConnection != nil && sess.PeerConnection.LocalDescription() != nil {
			answerSDP = sess.PeerConnection.LocalDescription().SDP
		}

		log.Printf("✅ Session %s PeerConnection renegotiated successfully", msg.SessionID)
	}

	finalState := sess.GetState()
	if wasReconnecting {
		sess.SetState(session.StateActive)
		finalState = session.StateActive
		log.Printf("✅ Session %s transitioned from reconnecting to active", msg.SessionID)
	}
	sess.StartVideoRecoveryBurst("ws-resume-success")
	direction, _, _, _ := sess.GetCallInfo()
	log.Printf("📊 Resume total elapsed: session=%s elapsed=%s", msg.SessionID, time.Since(resumeStartedAt).Round(10*time.Millisecond))
	log.Printf("✅ Session %s resumed successfully (state: %s, direction: %s, wasReconnecting: %v, hasSDP: %v)", msg.SessionID, finalState, direction, wasReconnecting, answerSDP != "")

	// Send success response with session details (and answer SDP if renegotiated)
	_, from, to, _ := sess.GetCallInfo()
	response := WSMessage{
		Type:      "resumed",
		SessionID: msg.SessionID,
		State:     string(finalState),
		From:      from,
		To:        to,
		SDP:       answerSDP,
	}
	resumeSendStartedAt := time.Now()
	s.sendWSMessage(client, response)
	log.Printf("📊 Resume send elapsed: session=%s elapsed=%s", msg.SessionID, time.Since(resumeSendStartedAt).Round(10*time.Millisecond))
}
