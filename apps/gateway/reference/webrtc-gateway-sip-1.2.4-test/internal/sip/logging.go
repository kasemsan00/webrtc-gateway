package sip

import (
	"context"
	"time"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
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

	_ = s.logStore.UpsertSession(ctx, &logstore.SessionRecord{
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
		Meta:          map[string]interface{}{"source": "sip"},
	})
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

	_ = s.logStore.UpsertDialog(ctx, &logstore.DialogRecord{
		SessionID:     sess.ID,
		Timestamp:     time.Now(),
		SIPCallID:     callID,
		FromTag:       fromTag,
		ToTag:         toTag,
		RemoteContact: remoteContact,
		CSeq:          cseq,
		RouteSet:      routeSet,
	})
}
