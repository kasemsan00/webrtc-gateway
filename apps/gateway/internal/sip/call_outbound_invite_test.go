package sip

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/session"
)

// fakeInviteTx is a client transaction that replays scripted responses.
type fakeInviteTx struct {
	responses chan *sip.Response
	done      chan struct{}
	once      sync.Once
}

func newFakeInviteTx(codes ...int) *fakeInviteTx {
	tx := &fakeInviteTx{responses: make(chan *sip.Response, len(codes)), done: make(chan struct{})}
	for _, code := range codes {
		tx.responses <- &sip.Response{StatusCode: code, Reason: "scripted"}
	}
	return tx
}

func (t *fakeInviteTx) Terminate()                             { t.once.Do(func() { close(t.done) }) }
func (t *fakeInviteTx) OnTerminate(sip.FnTxTerminate) bool     { return true }
func (t *fakeInviteTx) Done() <-chan struct{}                  { return t.done }
func (t *fakeInviteTx) Err() error                             { return nil }
func (t *fakeInviteTx) Responses() <-chan *sip.Response        { return t.responses }
func (t *fakeInviteTx) OnRetransmission(sip.FnTxResponse) bool { return true }

type recordingStateNotifier struct {
	states []session.SessionState
}

func (n *recordingStateNotifier) NotifySessionState(_ string, state session.SessionState) {
	n.states = append(n.states, state)
}

func testOutboundInvite() *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "1002", Host: "pbx.example.com", Port: 5060})
	req.AppendHeader(sip.NewHeader("Call-ID", "outbound-call"))
	setRequestCSeq(req, 2, sip.INVITE)
	return req
}

func TestAwaitOutboundInvite_AuthenticatedProvisionalReachesRinging(t *testing.T) {
	notifier := &recordingStateNotifier{}
	srv := &Server{stateNotifier: notifier}
	sess := &session.Session{ID: "auth-ringing", State: session.StateConnecting}

	answered, err := srv.awaitOutboundInvite(context.Background(), newFakeInviteTx(100, 183, 486), testOutboundInvite(), sess, sipAuthParams{}, true)

	if answered || err == nil {
		t.Fatalf("expected failed call, got answered=%v err=%v", answered, err)
	}
	if len(notifier.states) != 2 || notifier.states[0] != session.StateRinging || notifier.states[1] != session.StateEnded {
		t.Fatalf("expected ringing then ended, got %v", notifier.states)
	}
	if got := sess.GetEndReason(); got != endReasonForSIPStatus(486) {
		t.Fatalf("expected end reason for 486, got %q", got)
	}
}

func TestAwaitOutboundInvite_ForwardedAndQueuedAreProvisional(t *testing.T) {
	for _, code := range []int{181, 182, 199} {
		notifier := &recordingStateNotifier{}
		srv := &Server{stateNotifier: notifier}
		sess := &session.Session{ID: "provisional", State: session.StateConnecting}

		_, err := srv.awaitOutboundInvite(context.Background(), newFakeInviteTx(code, 603), testOutboundInvite(), sess, sipAuthParams{}, false)

		if err == nil || !strings.Contains(err.Error(), "603") {
			t.Fatalf("%d: expected the call to end on the final 603, got %v", code, err)
		}
		if len(notifier.states) != 1 || notifier.states[0] != session.StateEnded {
			t.Fatalf("%d: expected only ended, got %v", code, notifier.states)
		}
	}
}

func TestAwaitOutboundInvite_487AfterOwnCancelHasNoEndReason(t *testing.T) {
	srv := &Server{}
	sess := &session.Session{ID: "own-cancel", State: session.StateRinging}
	sess.MarkPendingCancelRequested()

	_, _ = srv.awaitOutboundInvite(context.Background(), newFakeInviteTx(487), testOutboundInvite(), sess, sipAuthParams{}, true)

	if got := sess.GetEndReason(); got != "" {
		t.Fatalf("caller cancel must not be reported as a failure, got %q", got)
	}
}

func TestAwaitOutboundInvite_SecondChallengeRejectsCredentials(t *testing.T) {
	srv := &Server{}
	sess := &session.Session{ID: "auth-rejected", State: session.StateConnecting}

	answered, err := srv.awaitOutboundInvite(context.Background(), newFakeInviteTx(401), testOutboundInvite(), sess, sipAuthParams{}, true)

	if answered || err == nil {
		t.Fatalf("expected auth failure, got answered=%v err=%v", answered, err)
	}
	if got := sess.GetEndReason(); got != endReasonAuthFailed {
		t.Fatalf("expected %q, got %q", endReasonAuthFailed, got)
	}
}

func TestAwaitOutboundInvite_TimeoutEndsAsNoAnswer(t *testing.T) {
	srv := &Server{}
	sess := &session.Session{ID: "timeout", State: session.StateRinging}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	answered, err := srv.awaitOutboundInvite(ctx, newFakeInviteTx(), testOutboundInvite(), sess, sipAuthParams{}, true)

	if answered || err == nil {
		t.Fatalf("expected timeout, got answered=%v err=%v", answered, err)
	}
	if got := sess.GetEndReason(); got != endReasonNoAnswer {
		t.Fatalf("expected %q, got %q", endReasonNoAnswer, got)
	}
}

// sipgo's digest auth increments req.CSeq().SeqNo. The wire value and the
// CANCEL built from the request must follow it.
func TestSetRequestCSeq_IncrementReachesWireAndCancel(t *testing.T) {
	req := testOutboundInvite()
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/TCP 10.0.0.1:5060;branch=z9hG4bK-a"))
	setRequestCSeq(req, 1, sip.INVITE)
	req.CSeq().SeqNo++

	if !strings.Contains(req.String(), "CSeq: 2 INVITE") {
		t.Fatalf("expected wire CSeq 2, got:\n%s", req.String())
	}
	cancelReq, err := createCancelRequestFromInvite(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cancelReq.CSeq().SeqNo; got != 2 {
		t.Fatalf("expected CANCEL CSeq 2, got %d", got)
	}
	if n := len(req.GetHeaders("CSeq")); n != 1 {
		t.Fatalf("expected one CSeq header, got %d", n)
	}
}
