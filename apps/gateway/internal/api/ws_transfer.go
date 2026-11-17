package api

import (
	"errors"
	"log"
	"strings"

	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

// sipTransferController is implemented by the SIP server (REFER, RFC 3515).
type sipTransferController interface {
	TransferCall(sess *session.Session, destination string, target *session.Session) error
}

// Transfer states sent to the client in transfer_progress.
const (
	transferStateAccepted  = "accepted"  // the PBX took the REFER (202)
	transferStateTrying    = "trying"    // NOTIFY 1xx from the PBX
	transferStateCompleted = "completed" // NOTIFY 2xx: the far end is connected to the target
	transferStateFailed    = "failed"    // REFER rejected or NOTIFY >= 300
)

// handleWSTransfer transfers the far end of msg.SessionID.
//
//   - Blind: {"type":"transfer","sessionId":A,"destination":"1002"}
//   - Attended: {"type":"transfer","sessionId":A,"targetSessionId":B}
//     connects the far end of A to the far end of B (both calls of this
//     client, typically B answered while A is on hold).
//
// The REFER goes out asynchronously; the client hears back through
// transfer_progress messages. On success the PBX ends the call with BYE.
func (s *Server) handleWSTransfer(client *WSClient, msg WSMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	targetID := strings.TrimSpace(msg.TargetSessionID)
	destination := strings.TrimSpace(msg.Destination)
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}
	if (destination == "") == (targetID == "") {
		s.sendTransferProgress(client, sessionID, transferStateFailed, 0, "exactly one of destination or targetSessionId is required")
		return
	}
	if !s.clientOwnsSession(client, sessionID) {
		s.sendTransferProgress(client, sessionID, transferStateFailed, 0, "call not owned by this connection")
		return
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		s.sendTransferProgress(client, sessionID, transferStateFailed, 0, "call not found")
		return
	}
	var target *session.Session
	if targetID != "" {
		if !s.clientOwnsSession(client, targetID) {
			s.sendTransferProgress(client, sessionID, transferStateFailed, 0, "target call not owned by this connection")
			return
		}
		target, ok = s.sessionMgr.GetSession(targetID)
		if !ok || target == nil {
			s.sendTransferProgress(client, sessionID, transferStateFailed, 0, "target call not found")
			return
		}
	}
	controller, ok := s.sipMaker.(sipTransferController)
	if !ok || controller == nil {
		s.sendTransferProgress(client, sessionID, transferStateFailed, 0, "transfer is not available")
		return
	}

	go func() {
		err := controller.TransferCall(sess, destination, target)
		if err == nil {
			s.sendTransferProgress(client, sessionID, transferStateAccepted, 202, "")
			return
		}
		code := 0
		var rejected *sip.TransferRejectedError
		if errors.As(err, &rejected) {
			code = rejected.StatusCode
		}
		log.Printf("[%s] transfer failed: %v", sessionID, err)
		s.sendTransferProgress(client, sessionID, transferStateFailed, code, err.Error())
	}()
}

// NotifyTransferProgress relays the PBX's NOTIFY sipfrag status of a
// transfer to the client that owns the call.
func (s *Server) NotifyTransferProgress(sessionID string, statusCode int, reason string, final bool) {
	state := transferStateTrying
	switch {
	case statusCode >= 200 && statusCode < 300:
		state = transferStateCompleted
	case statusCode >= 300:
		state = transferStateFailed
	case final:
		// The subscription ended on a provisional status: the PBX gives no
		// outcome. Report it as accepted rather than guess.
		state = transferStateAccepted
	}
	targets, _, _ := s.selectSIPMessageTargets(sessionID)
	for _, client := range targets {
		s.sendTransferProgress(client, sessionID, state, statusCode, reason)
	}
}

func (s *Server) sendTransferProgress(client *WSClient, sessionID, state string, statusCode int, reason string) {
	s.sendWSMessage(client, WSMessage{
		Type:       "transfer_progress",
		SessionID:  sessionID,
		State:      state,
		StatusCode: statusCode,
		Reason:     reason,
	})
}
