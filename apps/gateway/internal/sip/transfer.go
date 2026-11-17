package sip

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

// TransferProgressNotifier is told how a transfer this gateway requested is
// going: the REFER answer, then each NOTIFY sipfrag status from the PBX.
// statusCode is the SIP status (202 for REFER accepted, then 100/180/200...).
// final is true for the last report of the transfer.
type TransferProgressNotifier interface {
	NotifyTransferProgress(sessionID string, statusCode int, reason string, final bool)
}

const transferRequestTimeout = 10 * time.Second

// TransferCall asks the SIP peer (Asterisk) to transfer the far end of sess
// with an in-dialog REFER (RFC 3515).
//
//   - Blind: destination is the number or extension to transfer to.
//   - Attended: target is another established call of this client; the far
//     end of sess is connected to the far end of target (Refer-To with
//     Replaces, RFC 3891). Both calls must be dialogs with the same PBX.
//
// It returns once the PBX accepts the REFER (202). Progress then arrives as
// NOTIFY sipfrag (see handleNOTIFY); on success the PBX ends this dialog
// with BYE.
func (s *Server) TransferCall(sess *session.Session, destination string, target *session.Session) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}
	if sess == nil || !sess.HasDialogState() {
		return fmt.Errorf("established SIP dialog required")
	}
	if state := sess.GetState(); state != session.StateActive && state != session.StateReconnecting {
		return fmt.Errorf("call is %s, not active", state)
	}

	referTo, err := s.buildReferTo(sess, destination, target)
	if err != nil {
		return err
	}

	req, err := s.createBYERequest(sess)
	if err != nil {
		return fmt.Errorf("build REFER: %w", err)
	}
	req.Method = sip.REFER
	req.ReplaceHeader(&sip.CSeqHeader{SeqNo: uint32(sess.NextSIPCSeq()), MethodName: sip.REFER})
	req.AppendHeader(sip.NewHeader("Refer-To", referTo))
	if from := req.From(); from != nil {
		req.AppendHeader(sip.NewHeader("Referred-By", "<"+from.Address.String()+">"))
	}

	kind := "blind"
	if target != nil {
		kind = "attended"
	}
	fmt.Printf("[%s] sip_transfer_request kind=%s refer_to=%s\n", sess.ID, kind, referTo)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_transfer_requested",
		SIPMethod: string(sip.REFER),
		Data:      map[string]interface{}{"kind": kind, "referTo": referTo},
	})

	ctx, cancel := context.WithTimeout(context.Background(), transferRequestTimeout)
	defer cancel()
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("send REFER: %w", err)
	}
	defer tx.Terminate()

	for {
		select {
		case res := <-tx.Responses():
			if res == nil || res.IsProvisional() {
				continue
			}
			fmt.Printf("[%s] sip_transfer_response status=%d %s\n", sess.ID, res.StatusCode, res.Reason)
			if res.IsSuccess() {
				return nil
			}
			return &TransferRejectedError{StatusCode: res.StatusCode, Reason: res.Reason}
		case <-tx.Done():
			if err := tx.Err(); err != nil {
				return fmt.Errorf("REFER transaction failed: %w", err)
			}
			return fmt.Errorf("REFER transaction ended without a final response")
		case <-ctx.Done():
			return fmt.Errorf("REFER timed out")
		}
	}
}

// TransferRejectedError is a REFER the PBX answered with a failure status.
type TransferRejectedError struct {
	StatusCode int
	Reason     string
}

func (e *TransferRejectedError) Error() string {
	return fmt.Sprintf("transfer rejected: %d %s", e.StatusCode, e.Reason)
}

// buildReferTo returns the Refer-To header value.
func (s *Server) buildReferTo(sess *session.Session, destination string, target *session.Session) (string, error) {
	domain := s.transferDomain(sess)
	if domain == "" {
		return "", fmt.Errorf("no SIP domain for transfer")
	}
	if target == nil {
		user := normalizeSIPUser(destination)
		if err := validateSIPUserPart(user); err != nil {
			return "", fmt.Errorf("invalid transfer destination: %w", err)
		}
		return fmt.Sprintf("<sip:%s@%s>", user, domain), nil
	}

	if target == sess || target.ID == sess.ID {
		return "", fmt.Errorf("cannot transfer a call to itself")
	}
	if !target.HasDialogState() {
		return "", fmt.Errorf("transfer target call has no established dialog")
	}
	direction, from, to, callID := target.GetCallInfo()
	localTag, remoteTag, _, _, _, _, _ := target.GetSIPDialogState()
	if callID == "" || localTag == "" || remoteTag == "" {
		return "", fmt.Errorf("transfer target call is missing dialog identifiers")
	}
	remote := to
	if direction == "inbound" {
		remote = from
	}
	user := normalizeSIPUser(remote)
	if err := validateSIPUserPart(user); err != nil {
		return "", fmt.Errorf("transfer target party: %w", err)
	}
	return fmt.Sprintf("<sip:%s@%s?Replaces=%s>", user, domain, encodeReplaces(callID, remoteTag, localTag)), nil
}

// encodeReplaces builds the escaped Replaces value of a Refer-To URI. to-tag
// is the PBX's tag of the replaced dialog (its local tag) and from-tag is
// ours, which is how Asterisk looks the dialog up.
func encodeReplaces(callID, toTag, fromTag string) string {
	return url.QueryEscape(callID + ";to-tag=" + toTag + ";from-tag=" + fromTag)
}

// transferDomain is the PBX domain transfer targets are addressed in.
func (s *Server) transferDomain(sess *session.Session) string {
	if params, err := s.resolveSessionSIPParams(sess); err == nil && params.Domain != "" {
		return params.Domain
	}
	domain, _ := s.resolveDialogDomainPort(sess)
	return domain
}

// handleNOTIFY answers in-dialog NOTIFY. For the refer event package the
// sipfrag body reports the progress of a transfer this gateway requested.
func (s *Server) handleNOTIFY(req *sip.Request, tx sip.ServerTransaction) {
	callID := ""
	if h := req.CallID(); h != nil {
		callID = h.Value()
	}
	var sess *session.Session
	if s.sessionMgr != nil && callID != "" {
		sess, _ = s.sessionMgr.GetSessionBySIPCallID(callID)
	}
	if sess == nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil))
		return
	}
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))

	event := ""
	if h := req.GetHeader("Event"); h != nil {
		event = strings.ToLower(strings.TrimSpace(h.Value()))
	}
	if !strings.HasPrefix(event, "refer") {
		return
	}
	code, reason, ok := parseSipfragStatus(req.Body())
	if !ok {
		return
	}
	final := code >= 200 || subscriptionTerminated(req)
	fmt.Printf("[%s] sip_transfer_progress status=%d %s final=%v\n", sess.ID, code, reason, final)
	s.logEvent(&logstore.Event{
		Timestamp:     time.Now(),
		SessionID:     sess.ID,
		Category:      "sip",
		Name:          "sip_transfer_progress",
		SIPMethod:     string(sip.NOTIFY),
		SIPStatusCode: code,
		Data:          map[string]interface{}{"reason": reason, "final": final},
	})
	if notifier, ok := s.midCallNotifier.(TransferProgressNotifier); ok {
		notifier.NotifyTransferProgress(sess.ID, code, reason, final)
	}
}

// parseSipfragStatus reads the status line of a message/sipfrag body, for
// example "SIP/2.0 200 OK".
func parseSipfragStatus(body []byte) (int, string, bool) {
	line := strings.TrimSpace(strings.SplitN(string(body), "\n", 2)[0])
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(strings.ToUpper(parts[0]), "SIP/") {
		return 0, "", false
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil || code < 100 || code > 699 {
		return 0, "", false
	}
	reason := ""
	if len(parts) == 3 {
		reason = strings.TrimSpace(parts[2])
	}
	return code, reason, true
}

func subscriptionTerminated(req *sip.Request) bool {
	h := req.GetHeader("Subscription-State")
	return h != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(h.Value())), "terminated")
}
