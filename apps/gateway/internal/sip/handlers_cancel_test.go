package sip

import (
	"testing"

	"github.com/emiago/sipgo/sip"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

type cancelNotifierStub struct {
	sessionID string
	trunkID   int64
	reason    string
	calls     int
}

func (n *cancelNotifierStub) NotifyIncomingCall(sessionID, from, to string, trunkID int64) {}

func (n *cancelNotifierStub) NotifyIncomingCancel(sessionID string, trunkID int64, reason string) {
	n.calls++
	n.sessionID = sessionID
	n.trunkID = trunkID
	n.reason = reason
}

type cancelServerTxStub struct {
	responded  bool
	statusCode int
	onCancel   sip.FnTxCancel
}

func (tx *cancelServerTxStub) Respond(res *sip.Response) error {
	tx.responded = true
	if res != nil {
		tx.statusCode = res.StatusCode
	}
	return nil
}
func (tx *cancelServerTxStub) Terminate() {}
func (tx *cancelServerTxStub) OnTerminate(_ sip.FnTxTerminate) bool {
	return false
}
func (tx *cancelServerTxStub) Done() <-chan struct{} { return make(chan struct{}) }
func (tx *cancelServerTxStub) Err() error            { return nil }
func (tx *cancelServerTxStub) Acks() <-chan *sip.Request {
	return make(chan *sip.Request)
}
func (tx *cancelServerTxStub) OnCancel(fn sip.FnTxCancel) bool {
	tx.onCancel = fn
	return true
}

func TestIncomingInviteOnCancel_NotifiesIncomingCancelAndCleansUpIncomingSession(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetCallInfo("inbound", "sip:1001@example.com", "sip:1002@example.com", "call-cancel-1")
	sess.SetSIPAuthContext("trunk", "", 2, "sip.example.com", "1002", "secret", 5060)
	sess.SetState(session.StateIncoming)

	notifier := &cancelNotifierStub{}
	tx := &cancelServerTxStub{}

	srv := &Server{
		sessionMgr:       mgr,
		incomingNotifier: notifier,
	}

	srv.registerIncomingCancelHandler(sess, tx)
	if tx.onCancel == nil {
		t.Fatalf("expected OnCancel callback to be registered")
	}
	req := sip.NewRequest(sip.CANCEL, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-cancel-1"))
	tx.onCancel(req)

	if notifier.calls != 1 {
		t.Fatalf("expected NotifyIncomingCancel once, got %d", notifier.calls)
	}
	if notifier.sessionID != sess.ID || notifier.trunkID != 2 || notifier.reason != "caller_cancelled" {
		t.Fatalf("unexpected cancel notification payload: sessionID=%s trunkID=%d reason=%s", notifier.sessionID, notifier.trunkID, notifier.reason)
	}
	if _, ok := mgr.GetSession(sess.ID); ok {
		t.Fatalf("expected incoming session %s to be deleted after CANCEL", sess.ID)
	}
}

func TestHandleCANCEL_UnmatchedResponds481(t *testing.T) {
	req := sip.NewRequest(sip.CANCEL, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "call-cancel-unmatched"))

	tx := &cancelServerTxStub{}
	srv := &Server{}

	srv.handleCANCEL(req, tx)

	if !tx.responded {
		t.Fatalf("expected unmatched CANCEL to be responded")
	}
	if tx.statusCode != 481 {
		t.Fatalf("expected 481 for unmatched CANCEL, got %d", tx.statusCode)
	}
}
