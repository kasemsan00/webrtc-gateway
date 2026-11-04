package sip

import (
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

func (s *Server) handleINFO(req *sip.Request, tx sip.ServerTransaction) {
	callID := sipHeaderValue(req, "Call-ID")
	from := sipHeaderValue(req, "From")
	to := sipHeaderValue(req, "To")
	userAgent := sipHeaderValue(req, "User-Agent")
	contentType := sipHeaderValue(req, "Content-Type")
	body := string(req.Body())
	source := req.Source()

	sess, dialogOK := s.infoDialogSession(req)
	sessionID := ""
	if sess != nil {
		sessionID = sess.ID
	}
	fmt.Printf("%s\n", formatSIPInfoReceived(sessionID, contentType, body, source, from, to, callID, userAgent))

	if !dialogOK || sess == nil {
		res := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		if err := tx.Respond(res); err != nil {
			fmt.Printf("ERROR responding 481 to INFO: %v\n", err)
		}
		return
	}

	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	if err := tx.Respond(res); err != nil {
		fmt.Printf("[%s] ERROR responding 200 to INFO: %v\n", sess.ID, err)
	}

	if infoRequestsPictureFastUpdate(contentType, body) {
		sess.HandleSIPInfoPictureFastUpdate(time.Now())
		return
	}
	if infoIsDTMF(contentType) {
		digit := parseSIPInfoDTMF(body)
		fmt.Printf("[%s] sip_info_dtmf digit=%s\n", sess.ID, digit)
		if digit != "" && s.dtmfNotifier != nil {
			s.dtmfNotifier.NotifyDTMF(sess.ID, digit)
		}
	}
}

func (s *Server) infoDialogSession(req *sip.Request) (*session.Session, bool) {
	if s == nil || s.sessionMgr == nil {
		return nil, false
	}
	callID := sipHeaderValue(req, "Call-ID")
	if callID == "" {
		return nil, false
	}
	sess, ok := s.sessionMgr.GetSessionBySIPCallID(callID)
	if !ok || sess == nil || !sess.HasDialogState() {
		return sess, false
	}
	localTag, remoteTag, _, _, _, _, _ := sess.GetSIPDialogState()
	fromTag, toTag := sipFromToTags(req)
	if !infoDialogMatches(localTag, remoteTag, fromTag, toTag) {
		return sess, false
	}
	return sess, true
}

func infoDialogMatches(localTag, remoteTag, fromTag, toTag string) bool {
	if localTag == "" || remoteTag == "" || fromTag == "" || toTag == "" {
		return false
	}
	return (fromTag == remoteTag && toTag == localTag) || (fromTag == localTag && toTag == remoteTag)
}

func infoRequestsPictureFastUpdate(contentType, body string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "application/media_control+xml") {
		return true
	}
	return strings.Contains(strings.ToLower(body), "picture_fast_update")
}

func infoIsDTMF(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "application/dtmf-relay") || strings.Contains(ct, "application/dtmf")
}

func parseSIPInfoDTMF(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if len(line) < len("signal=") {
			continue
		}
		if strings.EqualFold(line[:len("signal=")], "signal=") {
			signal := strings.TrimSpace(line[len("signal="):])
			if signal != "" {
				return signal
			}
		}
	}
	trimmed := strings.TrimSpace(body)
	if trimmed != "" && !strings.ContainsAny(trimmed, " \t\r\n") {
		return trimmed
	}
	return ""
}

func formatSIPInfoReceived(sessionID, contentType, body, source, from, to, callID, userAgent string) string {
	return fmt.Sprintf(
		"sip_info received session=%s content_type=%s body=%s source=%s from=%s to=%s call_id=%s user_agent=%s",
		oneLine(sessionID),
		oneLine(contentType),
		singleLinePrefix(body, 200),
		oneLine(source),
		oneLine(from),
		oneLine(to),
		oneLine(callID),
		oneLine(userAgent),
	)
}

func singleLinePrefix(body string, n int) string {
	body = oneLine(body)
	if len(body) > n {
		body = body[:n]
	}
	return body
}

func oneLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return value
}

func sipHeaderValue(req *sip.Request, name string) string {
	if req == nil {
		return ""
	}
	switch strings.ToLower(name) {
	case "from":
		if h := req.From(); h != nil {
			return h.Value()
		}
	case "to":
		if h := req.To(); h != nil {
			return h.Value()
		}
	case "call-id":
		if h := req.CallID(); h != nil {
			return h.Value()
		}
	}
	headers := req.GetHeaders(name)
	if len(headers) == 0 {
		return ""
	}
	return headers[0].Value()
}

func sipFromToTags(req *sip.Request) (fromTag, toTag string) {
	if req == nil {
		return "", ""
	}
	if from := req.From(); from != nil && from.Params != nil {
		fromTag, _ = from.Params.Get("tag")
	}
	if to := req.To(); to != nil && to.Params != nil {
		toTag, _ = to.Params.Get("tag")
	}
	return fromTag, toTag
}
