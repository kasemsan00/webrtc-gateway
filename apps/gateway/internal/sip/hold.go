package sip

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

// SetHold performs a client-originated in-dialog re-INVITE. Held calls use
// inactive media because the gateway does not generate music-on-hold; resume
// restores sendrecv without rebuilding RTP transports or the WebRTC PC.
func (s *Server) SetHold(sess *session.Session, held bool) error {
	if sess == nil || sess.GetState() != session.StateActive || !sess.HasDialogState() {
		return fmt.Errorf("active SIP dialog required")
	}
	if sess.IsHeld() == held {
		return nil
	}
	req, err := s.createHoldRequest(sess, held)
	if err != nil {
		return fmt.Errorf("build in-dialog INVITE: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("send in-dialog INVITE: %w", err)
	}
	defer tx.Terminate()

	for {
		select {
		case res := <-tx.Responses():
			if res == nil || res.StatusCode < 200 {
				continue
			}
			if res.StatusCode == 491 {
				return fmt.Errorf("SIP renegotiation glare (491)")
			}
			if res.StatusCode < 200 || res.StatusCode >= 300 {
				return fmt.Errorf("SIP hold re-INVITE rejected: %d %s", res.StatusCode, res.Reason)
			}
			s.sendAckForInvite(req, res)
			if len(res.Body()) > 0 {
				s.parseAsteriskSDPAndSetEndpoints(res.Body(), sess)
			}
			sess.SetHeld(held)
			return nil
		case <-tx.Done():
			return fmt.Errorf("SIP hold re-INVITE transaction ended without final response")
		case <-ctx.Done():
			return fmt.Errorf("SIP hold re-INVITE timed out: %w", ctx.Err())
		}
	}
}

func (s *Server) createHoldRequest(sess *session.Session, held bool) (*sip.Request, error) {
	status := sess.GetMediaEndpointStatus()
	if status.AudioRTPPort <= 0 {
		return nil, fmt.Errorf("audio RTP endpoint is not ready")
	}

	body := s.createSDPOffer(status.AudioRTPPort, sess)
	direction := "sendrecv"
	if held {
		direction = "inactive"
	}
	body = []byte(strings.ReplaceAll(string(body), "a=sendrecv", "a="+direction))

	req, err := s.createBYERequest(sess)
	if err != nil {
		return nil, err
	}
	req.Method = sip.INVITE
	// createBYERequest currently adds CSeq as a generic header. req.CSeq()
	// lazily parses that value into a detached typed header, so mutating the
	// returned pointer changes the transaction key but not the serialized wire
	// header. Replace the stored header to keep sipgo transaction matching and
	// the actual SIP request on the same INVITE method/CSeq.
	req.ReplaceHeader(&sip.CSeqHeader{
		SeqNo:      uint32(sess.NextSIPCSeq()),
		MethodName: sip.INVITE,
	})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(sip.NewHeader("Allow", sipAllowHeaderValue()))
	req.SetBody(body)
	return req, nil
}
