package sip

import (
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
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
	done       chan struct{}
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
func (tx *cancelServerTxStub) Done() <-chan struct{} {
	if tx.done != nil {
		return tx.done
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (tx *cancelServerTxStub) Err() error { return nil }
func (tx *cancelServerTxStub) Acks() <-chan *sip.Request {
	return make(chan *sip.Request)
}
func (tx *cancelServerTxStub) OnCancel(fn sip.FnTxCancel) bool {
	tx.onCancel = fn
	return true
}

func newIncomingCancelRequest(callID string) *sip.Request {
	req := sip.NewRequest(sip.CANCEL, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	return req
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
	req := newIncomingCancelRequest("call-cancel-1")
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
	req := newIncomingCancelRequest("call-cancel-unmatched")

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

func TestHandleCANCEL_CallIDFallbackNotifiesIncomingAndResponds200(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetCallInfo("inbound", "sip:1001@example.com", "sip:1002@example.com", "call-cancel-fallback")
	sess.SetSIPAuthContext("trunk", "", 2, "sip.example.com", "1002", "secret", 5060)
	sess.SetState(session.StateIncoming)

	inviteReq := sip.NewRequest(sip.INVITE, sip.Uri{User: "1002", Host: "sip.example.com", Port: 5060})
	inviteReq.AppendHeader(sip.NewHeader("Call-ID", "call-cancel-fallback"))
	inviteTx := &cancelServerTxStub{}
	sess.SetIncomingInvite(inviteTx, inviteReq, nil, "sip:1001@example.com", "sip:1002@example.com")

	notifier := &cancelNotifierStub{}
	cancelTx := &cancelServerTxStub{}
	srv := &Server{
		sessionMgr:       mgr,
		incomingNotifier: notifier,
	}

	srv.handleCANCEL(newIncomingCancelRequest("call-cancel-fallback"), cancelTx)

	if cancelTx.statusCode != 200 {
		t.Fatalf("expected 200 for Call-ID matched CANCEL, got %d", cancelTx.statusCode)
	}
	if inviteTx.statusCode != 487 {
		t.Fatalf("expected 487 on stored INVITE tx, got %d", inviteTx.statusCode)
	}
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

func TestHandleCANCEL_ActiveSessionResponds200WithoutCancelNotify(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetCallInfo("inbound", "sip:1001@example.com", "sip:1002@example.com", "call-cancel-active")
	sess.SetSIPAuthContext("trunk", "", 2, "sip.example.com", "1002", "secret", 5060)
	sess.SetState(session.StateActive)

	notifier := &cancelNotifierStub{}
	cancelTx := &cancelServerTxStub{}
	srv := &Server{
		sessionMgr:       mgr,
		incomingNotifier: notifier,
	}

	srv.handleCANCEL(newIncomingCancelRequest("call-cancel-active"), cancelTx)

	if cancelTx.statusCode != 200 {
		t.Fatalf("expected 200 for CANCEL against an established call, got %d", cancelTx.statusCode)
	}
	if notifier.calls != 0 {
		t.Fatalf("expected no incoming cancel notify for active session, got %d", notifier.calls)
	}
	if _, ok := mgr.GetSession(sess.ID); !ok {
		t.Fatalf("expected active session %s to remain after CANCEL", sess.ID)
	}
}

func TestWaitForIncomingInviteTransaction_ReturnsWhenSessionEnds(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetState(session.StateIncoming)

	tx := &cancelServerTxStub{done: make(chan struct{})}
	srv := &Server{}

	done := make(chan struct{})
	go func() {
		srv.waitForIncomingInviteTransaction(sess, tx)
		close(done)
	}()

	sess.SetState(session.StateEnded)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected waitForIncomingInviteTransaction to return after session ended")
	}
}

func TestHandleINVITE_CallerCancelByCallIDUnblocksRingingAndNotifiesAgent(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)
	notifier := &cancelNotifierStub{}
	inviteTx := &cancelServerTxStub{done: make(chan struct{})}

	trunk := &Trunk{ID: 170, Name: "sipclient-agent-00025@sipagent.ttrs.or.th:5060", Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{170: trunk},
		ownedLeases: map[int64]bool{170: true},
	}
	setRegistrarIdentity(tm, trunk, "203.150.245.41")

	srv := &Server{
		sessionMgr:       mgr,
		sessionCreator:   mgr,
		incomingNotifier: notifier,
		trunkManager:     tm,
		publicAddress:    "203.151.21.121",
		sipPort:          5090,
	}

	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "203.150.245.41", Port: 5060})
	req.AppendHeader(&sip.FromHeader{DisplayName: "Caller", Address: sip.Uri{User: "0000178977848", Host: "203.150.245.41"}})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(sip.NewHeader("Call-ID", "invite-cancel-live"))
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 102, MethodName: sip.INVITE})

	inviteDone := make(chan struct{})
	go func() {
		srv.handleINVITE(req, inviteTx)
		close(inviteDone)
	}()

	var sess *session.Session
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if found, ok := mgr.GetSessionBySIPCallID("invite-cancel-live"); ok {
			sess = found
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sess == nil {
		t.Fatal("expected incoming session while INVITE handler is waiting")
	}

	cancelTx := &cancelServerTxStub{}
	srv.handleCANCEL(newIncomingCancelRequest("invite-cancel-live"), cancelTx)

	select {
	case <-inviteDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handleINVITE should return after caller CANCEL matched by Call-ID")
	}
	if cancelTx.statusCode != 200 {
		t.Fatalf("expected 200 for Call-ID matched CANCEL, got %d", cancelTx.statusCode)
	}
	if notifier.calls != 1 || notifier.reason != "caller_cancelled" || notifier.sessionID != sess.ID {
		t.Fatalf("expected agent cancel notify, got calls=%d reason=%s sessionID=%s", notifier.calls, notifier.reason, notifier.sessionID)
	}
	if _, ok := mgr.GetSession(sess.ID); ok {
		t.Fatalf("expected incoming session %s to be deleted after CANCEL", sess.ID)
	}
}
