package sip

import (
	"errors"
	"fmt"

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
	offer, err := s.buildMidCallOffer(sess, held)
	if err != nil {
		return fmt.Errorf("build in-dialog INVITE: %w", err)
	}

	res, err := s.sendMidCallOffer(sess, offer.body)
	if err != nil {
		var statusErr *sipStatusError
		if errors.As(err, &statusErr) {
			if statusErr.Code == 491 {
				return fmt.Errorf("SIP renegotiation glare (491)")
			}
			return fmt.Errorf("SIP hold re-INVITE rejected: %d %s", statusErr.Code, statusErr.Reason)
		}
		return err
	}
	if len(res.Body()) > 0 {
		s.parseAsteriskSDPAndSetEndpoints(res.Body(), sess)
	}
	sess.SetHeld(held)
	return nil
}

func (s *Server) createHoldRequest(sess *session.Session, held bool) (*sip.Request, error) {
	// The offer carries the hold direction on audio and primary video, the
	// auxiliary video section when that stream is open, and port 0 at the
	// positions the gateway refused earlier (aux_stream.go).
	offer, err := s.buildMidCallOffer(sess, held)
	if err != nil {
		return nil, err
	}
	return s.createInDialogInvite(sess, offer.body)
}
