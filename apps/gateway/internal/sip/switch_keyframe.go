package sip

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

const switchVideoPictureFastUpdate = `<?xml version="1.0" encoding="utf-8" ?>
<media_control>
<vc_primitive>
<to_encoder>
<picture_fast_update/>
</to_encoder>
</vc_primitive>
</media_control>
`

func (s *Server) relayBridgedKeyframe(origin *session.Session, reason string) {
	if s == nil || s.sessionMgr == nil || origin == nil {
		return
	}
	_, _ = session.RelayBridgedKeyframe(s.sessionMgr.ListSessions(), origin, reason, time.Now())
}

func (s *Server) maybeSendSwitchVideoInfoFIR(sess *session.Session, reason string) {
	if s == nil || sess == nil || !s.config.SwitchVideoInfoFIREnable {
		return
	}
	if !sess.AllowSwitchVideoInfoFIR(time.Now()) {
		fmt.Printf("[%s] switch_video_info_fir_skipped reason=rate-limit trigger=%s\n", sess.ID, reason)
		return
	}
	go s.sendSwitchVideoInfoFIR(sess, reason)
}

func (s *Server) sendSwitchVideoInfoFIR(sess *session.Session, reason string) {
	err := s.SendInfoToSession(sess, switchVideoPictureFastUpdate, "application/media_control+xml")
	if err != nil {
		fmt.Printf("[%s] switch_video_info_fir_failed trigger=%s err=%v\n", sess.ID, reason, err)
		return
	}
	fmt.Printf("[%s] switch_video_info_fir trigger=%s\n", sess.ID, reason)
}

// SendInfoToSession sends an in-dialog INFO on the session's SIP dialog.
func (s *Server) SendInfoToSession(sess *session.Session, body, contentType string) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}
	req, err := s.createInDialogInfoRequest(sess, body, contentType)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send INFO: %w", err)
	}
	defer tx.Terminate()
	select {
	case res := <-tx.Responses():
		if res == nil {
			return fmt.Errorf("INFO response missing")
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("INFO failed: %d %s", res.StatusCode, res.Reason)
	case <-tx.Done():
		if err := tx.Err(); err != nil {
			return fmt.Errorf("INFO transaction error: %w", err)
		}
		return fmt.Errorf("INFO transaction closed")
	case <-ctx.Done():
		return fmt.Errorf("INFO timeout")
	}
}

func (s *Server) createInDialogInfoRequest(sess *session.Session, body, contentType string) (*sip.Request, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	if !sess.HasDialogState() {
		return nil, fmt.Errorf("session has no SIP dialog state")
	}
	_, _, remoteContact, _, _, _, _ := sess.GetSIPDialogState()
	if strings.TrimSpace(remoteContact) == "" {
		return nil, fmt.Errorf("session has no remote contact address")
	}
	_, _, _, sipCallID := sess.GetCallInfo()
	if strings.TrimSpace(sipCallID) == "" {
		return nil, fmt.Errorf("session has no SIP Call-ID")
	}
	req, err := s.createBYERequest(sess)
	if err != nil {
		return nil, fmt.Errorf("build in-dialog INFO: %w", err)
	}
	req.Method = sip.INFO
	req.ReplaceHeader(&sip.CSeqHeader{
		SeqNo:      uint32(sess.NextSIPCSeq()),
		MethodName: sip.INFO,
	})
	if contentType == "" {
		contentType = "application/media_control+xml"
	}
	req.AppendHeader(sip.NewHeader("Content-Type", contentType))
	req.SetBody([]byte(body))
	return req, nil
}
