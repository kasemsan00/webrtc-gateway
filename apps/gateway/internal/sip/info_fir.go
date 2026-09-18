package sip

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/telemetry"
)

const (
	pictureFastUpdateContentType = "application/media_control+xml"
	pictureFastUpdateXML         = `<?xml version="1.0" encoding="utf-8"?>
<media_control>
  <vc_primitive>
    <to_encoder>
      <picture_fast_update/>
    </to_encoder>
  </vc_primitive>
</media_control>
`
)

func isPictureFastUpdateBody(body string) bool {
	return strings.Contains(strings.ToLower(body), "picture_fast_update")
}

// createInDialogInfoRequest builds INFO from the same dialog metadata as BYE
// and in-dialog MESSAGE. chan_sip honors RFC 5168 picture fast update on this
// method more reliably than RTCP FIR/PLI.
func (s *Server) createInDialogInfoRequest(
	sess *session.Session,
	body string,
	contentType string,
) (*sip.Request, error) {
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
		contentType = pictureFastUpdateContentType
	}
	req.AppendHeader(sip.NewHeader("Content-Type", contentType))
	req.SetBody([]byte(body))
	return req, nil
}

// SendPictureFastUpdate sends an in-dialog SIP INFO with RFC 5168 media_control
// XML so Asterisk chan_sip can queue AST_CONTROL_VIDUPDATE on the bridged
// encoder (queue → agent switch).
func (s *Server) SendPictureFastUpdate(sess *session.Session) error {
	started := time.Now()
	ctx, span := telemetry.StartSIPSpan(context.Background(), "INFO")
	err := s.sendPictureFastUpdate(sess)
	telemetry.EndSIP(ctx, span, "INFO", started, 0, err)
	return err
}

func (s *Server) sendPictureFastUpdate(sess *session.Session) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}
	if sess == nil {
		return fmt.Errorf("session is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, _, sipCallID := sess.GetCallInfo()
	body := pictureFastUpdateXML
	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "sip_info",
		ContentType: pictureFastUpdateContentType,
		BodyText:    body,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_info_picture_fast_update_send_request",
		PayloadID: payloadID,
		Data:      map[string]interface{}{"call_id": sipCallID},
	})

	req, err := s.createInDialogInfoRequest(sess, body, pictureFastUpdateContentType)
	if err != nil {
		return err
	}

	if s.config.DebugSIPMessage {
		fmt.Printf("\n=== 📸 Sending In-Dialog SIP INFO (picture fast update) ===\n")
		fmt.Printf("Session: %s\n", sess.ID)
		fmt.Printf("Request-URI: %s\n", req.Recipient.String())
		fmt.Printf("Destination: %s\n", req.Destination())
		fmt.Printf("Call-ID: %s\n", sipCallID)
		fmt.Printf("--- Full Request ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("==============================\n\n")
	} else {
		fmt.Printf("[%s] 📸 Sending in-dialog INFO picture_fast_update\n", sess.ID)
	}

	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_info_picture_fast_update_send_failed",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		return fmt.Errorf("failed to send INFO: %w", err)
	}
	defer tx.Terminate()

	select {
	case res := <-tx.Responses():
		if res == nil {
			break
		}
		if s.config.DebugSIPMessage {
			fmt.Printf("[%s] 📸 INFO response: %d %s\n", sess.ID, res.StatusCode, res.Reason)
		}
		s.logEvent(&logstore.Event{
			Timestamp:     time.Now(),
			SessionID:     sess.ID,
			Category:      "sip",
			Name:          "sip_info_picture_fast_update_response",
			SIPStatusCode: res.StatusCode,
			Data:          map[string]interface{}{"reason": res.Reason},
		})
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("INFO failed: %d %s", res.StatusCode, res.Reason)
	case <-tx.Done():
		if err := tx.Err(); err != nil {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_info_picture_fast_update_transaction_error",
				Data:      map[string]interface{}{"error": err.Error()},
			})
			return fmt.Errorf("INFO transaction error: %w", err)
		}
	case <-ctx.Done():
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_info_picture_fast_update_timeout",
		})
		return fmt.Errorf("INFO timed out")
	}

	return nil
}

func (s *Server) sendSwitchPictureFastUpdate(sess *session.Session, generation int, mediaEpoch uint64) {
	if sess == nil || !s.config.SwitchVideoInfoFIREnable {
		return
	}
	if !sess.IsSwitchVideoAuthority(generation, mediaEpoch) {
		return
	}
	s.runSwitchHandlerTestHook("info-fir", session.SwitchTargetDecision{
		Generation: generation,
		MediaEpoch: mediaEpoch,
	})
	if s.switchPictureFastUpdateHook != nil {
		s.switchPictureFastUpdateHook(sess, generation, mediaEpoch)
		return
	}
	if s.sipClient == nil {
		return
	}
	go s.sendSwitchPictureFastUpdateAsync(sess, generation, mediaEpoch)
}

func (s *Server) sendSwitchPictureFastUpdateAsync(sess *session.Session, generation int, mediaEpoch uint64) {
	if sess == nil || sess.GetState() == session.StateEnded {
		return
	}
	if !sess.IsSwitchVideoAuthority(generation, mediaEpoch) {
		fmt.Printf("[%s] switch_picture_fast_update_skipped reason=stale generation=%d mediaEpoch=%d\n",
			sess.ID, generation, mediaEpoch)
		return
	}
	if err := s.SendPictureFastUpdate(sess); err != nil {
		fmt.Printf("[%s] switch_picture_fast_update_failed generation=%d error=%v\n", sess.ID, generation, err)
		return
	}
	fmt.Printf("[%s] switch_picture_fast_update_sent generation=%d mediaEpoch=%d\n", sess.ID, generation, mediaEpoch)
}

func (s *Server) handleINFO(req *sip.Request, tx sip.ServerTransaction) {
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	if err := tx.Respond(res); err != nil {
		fmt.Printf("ERROR responding to INFO: %v\n", err)
	}

	body := string(req.Body())
	if !isPictureFastUpdateBody(body) {
		return
	}

	callIDValue := ""
	if callID := req.CallID(); callID != nil {
		callIDValue = callID.Value()
	}
	if s.sessionMgr == nil || callIDValue == "" {
		return
	}
	sess, ok := s.sessionMgr.GetSessionBySIPCallID(callIDValue)
	if !ok || sess == nil {
		return
	}

	fmt.Printf("[%s] 📸 Inbound INFO picture_fast_update; requesting WebRTC keyframe\n", sess.ID)
	sess.SendFIRToWebRTC()
	sess.SendPLItoWebRTC()
}
