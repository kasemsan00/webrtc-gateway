package sip

import (
	"context"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

func (s *Server) logEvent(event *logstore.Event) {
	if s.logStore == nil || event == nil {
		return
	}
	s.logStore.LogEvent(event)
}

func (s *Server) storePayload(ctx context.Context, payload *logstore.PayloadRecord) *int64 {
	if s.logStore == nil || payload == nil {
		return nil
	}

	payloadID, err := s.logStore.StorePayload(ctx, payload)
	if err != nil {
		return nil
	}
	return &payloadID
}

func (s *Server) storePayloadAndLogEvent(ctx context.Context, payload *logstore.PayloadRecord, event *logstore.Event) {
	if s.logStore == nil {
		return
	}
	if asyncStore, ok := s.logStore.(logstore.AsyncLogStore); ok {
		asyncStore.StorePayloadAndLogEventAsync(payload, event)
		return
	}
	var payloadID *int64
	if payload != nil {
		payloadID = s.storePayload(ctx, payload)
	}
	if event != nil {
		if payloadID != nil {
			event.PayloadID = payloadID
		}
		s.logEvent(event)
	}
}

func (s *Server) storePayloadAsync(ctx context.Context, payload *logstore.PayloadRecord) {
	if s.logStore == nil || payload == nil {
		return
	}
	if asyncStore, ok := s.logStore.(logstore.AsyncLogStore); ok {
		asyncStore.StorePayloadAndLogEventAsync(payload, nil)
		return
	}
	_ = s.storePayload(ctx, payload)
}

func (s *Server) logSessionSnapshot(ctx context.Context, sess *session.Session, endReason string) {
	if s.logStore == nil || sess == nil {
		return
	}

	snap := sess.Snapshot()
	var endedAt *time.Time
	if snap.State == session.StateEnded {
		ended := time.Now()
		endedAt = &ended
	}

	if endReason == "" && snap.State == session.StateEnded {
		endReason = "ended"
	}

	// Extract auth context
	authMode, _, trunkID, _, sipUsername, _, _ := sess.GetSIPAuthContext()
	var trunkIDPtr *int64
	if trunkID > 0 {
		trunkIDPtr = &trunkID
	}
	var trunkName string
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		if trunk, found := s.trunkManager.GetTrunkByID(trunkID); found {
			if t, ok := trunk.(*Trunk); ok {
				trunkName = t.Name
			}
		}
	}

	record := &logstore.SessionRecord{
		SessionID:     snap.ID,
		CreatedAt:     snap.CreatedAt,
		UpdatedAt:     time.Now(),
		EndedAt:       endedAt,
		Direction:     snap.Direction,
		FromURI:       snap.From,
		ToURI:         snap.To,
		SIPCallID:     snap.SIPCallID,
		FinalState:    string(snap.State),
		EndReason:     endReason,
		RTPAudioPort:  snap.RTPPort,
		RTPVideoPort:  snap.VideoRTPPort,
		RTCPAudioPort: snap.AudioRTCPPort,
		RTCPVideoPort: snap.VideoRTCPPort,
		SIPOpusPT:     int(snap.SIPOpusPT),
		AuthMode:      authMode,
		TrunkID:       trunkIDPtr,
		TrunkName:     trunkName,
		SIPUsername:   sipUsername,
		Meta:          buildSessionMeta(sess, "sip"),
	}
	if asyncStore, ok := s.logStore.(logstore.AsyncLogStore); ok {
		asyncStore.UpsertSessionAsync(record)
		return
	}
	_ = s.logStore.UpsertSession(ctx, record)
}

func buildSessionMeta(sess *session.Session, source string) map[string]interface{} {
	meta := map[string]interface{}{"source": source}
	if sess != nil {
		authMode, accountKey, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if authMode != "" {
			meta["authMode"] = authMode
		}
		if accountKey != "" {
			meta["accountKey"] = accountKey
		}
		if trunkID > 0 {
			meta["trunkId"] = trunkID
		}
	}
	return meta
}

func (s *Server) logDialogSnapshot(ctx context.Context, sess *session.Session) {
	if s.logStore == nil || sess == nil {
		return
	}

	fromTag, toTag, remoteContact, routeSet, cseq, _, _ := sess.GetSIPDialogState()
	if fromTag == "" && toTag == "" && remoteContact == "" {
		return
	}

	_, _, _, callID := sess.GetCallInfo()

	record := &logstore.DialogRecord{
		SessionID:     sess.ID,
		Timestamp:     time.Now(),
		SIPCallID:     callID,
		FromTag:       fromTag,
		ToTag:         toTag,
		RemoteContact: remoteContact,
		CSeq:          cseq,
		RouteSet:      routeSet,
	}
	if asyncStore, ok := s.logStore.(logstore.AsyncLogStore); ok {
		asyncStore.UpsertDialogAsync(record)
		return
	}
	_ = s.logStore.UpsertDialog(ctx, record)
}
