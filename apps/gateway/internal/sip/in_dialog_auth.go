package sip

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"

	"webrtc-sip-gateway/internal/session"
)

// authorizeInDialogInvite builds an authenticated retry of an in-dialog INVITE
// after a 401 Unauthorized or 407 Proxy Authentication Required challenge. It
// keeps the gateway's Via host/port and dialog transport, advances the dialog
// CSeq so subsequent BYE/re-INVITE stay monotonic, and signs the request using
// the session's resolved SIP credentials.
func (s *Server) authorizeInDialogInvite(sess *session.Session, originalReq *sip.Request, challenge *sip.Response) (*sip.Request, error) {
	if sess == nil || originalReq == nil || challenge == nil {
		return nil, fmt.Errorf("missing request or challenge for in-dialog auth")
	}

	var challengeHeaderName, authHeaderName string
	switch challenge.StatusCode {
	case 401:
		challengeHeaderName = "WWW-Authenticate"
		authHeaderName = "Authorization"
	case 407:
		challengeHeaderName = "Proxy-Authenticate"
		authHeaderName = "Proxy-Authorization"
	default:
		return nil, fmt.Errorf("unsupported auth challenge status %d", challenge.StatusCode)
	}

	challengeHeaders := challenge.GetHeaders(challengeHeaderName)
	if len(challengeHeaders) == 0 {
		return nil, fmt.Errorf("missing %s header in %d response", challengeHeaderName, challenge.StatusCode)
	}

	params, err := s.resolveSessionSIPParams(sess)
	if err != nil {
		return nil, fmt.Errorf("resolve SIP credentials: %w", err)
	}
	if params.Username == "" || params.Password == "" {
		return nil, fmt.Errorf("authentication required but no SIP password configured")
	}

	chal, err := digest.ParseChallenge(challengeHeaders[0].Value())
	if err != nil {
		return nil, fmt.Errorf("parse %s challenge: %w", challengeHeaderName, err)
	}
	if chal.Algorithm != "" {
		chal.Algorithm = strings.ToUpper(strings.TrimSpace(chal.Algorithm))
	}

	opts := digest.Options{
		Method:   originalReq.Method.String(),
		URI:      originalReq.Recipient.Addr(),
		Username: params.Username,
		Password: params.Password,
	}
	if body := originalReq.Body(); len(body) > 0 {
		opts.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}

	cred, err := digest.Digest(chal, opts)
	if err != nil {
		return nil, fmt.Errorf("compute digest credentials: %w", err)
	}

	authReq := originalReq.Clone()
	transportUpper := strings.ToUpper(strings.TrimSpace(originalReq.Transport()))
	if transportUpper == "" {
		if origVia := originalReq.Via(); origVia != nil && origVia.Transport != "" {
			transportUpper = strings.ToUpper(strings.TrimSpace(origVia.Transport))
		} else {
			transportUpper, _ = s.dialogTransport(sess)
		}
	}
	for authReq.RemoveHeader("Via") {
	}
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       transportUpper,
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaParams.Add("rport", "")
	viaHop.Params = viaParams
	authReq.PrependHeader(viaHop)

	authReq.ReplaceHeader(&sip.CSeqHeader{
		SeqNo:      uint32(sess.NextSIPCSeq()),
		MethodName: originalReq.Method,
	})
	for authReq.RemoveHeader(authHeaderName) {
	}
	authReq.AppendHeader(sip.NewHeader(authHeaderName, cred.String()))
	authReq.SetTransport(transportUpper)
	if dest := originalReq.Destination(); dest != "" {
		authReq.SetDestination(dest)
	}
	return authReq, nil
}
