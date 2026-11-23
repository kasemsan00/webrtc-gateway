package sip

import (
	"context"
	"sync"
	"testing"

	"github.com/emiago/sipgo"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

func TestCreateBYERequest_InboundPrefersDialogDomain(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s1"}
	sess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "sip:1100@203.151.21.121:5060", "call-1")
	sess.SetSIPAuthContext("trunk", "", 1, "203.151.21.121", "1100", "secret", 5060)
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.150.245.42:5060>", "198.51.100.20", 5060, 1, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Destination(); got != "198.51.100.20:5060" {
		t.Fatalf("expected destination from dialog domain, got %s", got)
	}
	to := req.To()
	if to == nil || to.Address.Host != "203.150.245.42" {
		t.Fatalf("expected To host to preserve remote host, got %#v", to)
	}
}

func TestCreateBYERequest_InboundFallsBackToAuthContext(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s2"}
	sess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "1100", "call-2")
	sess.SetSIPAuthContext("trunk", "", 1, "203.151.21.121", "1100", "secret", 5060)
	sess.SetSIPDialogState("local-tag", "remote-tag", "", "", 0, 1, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Destination(); got != "203.151.21.121:5060" {
		t.Fatalf("expected destination from auth context, got %s", got)
	}
}

func TestCreateBYERequest_InboundFallsBackToRemoteContact(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s3"}
	sess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "1100", "call-3")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.150.245.42:5060>", "", 0, 1, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	if got := req.Destination(); got != "203.150.245.42:5060" {
		t.Fatalf("expected destination from remote contact, got %s", got)
	}
}

func TestCreateBYERequest_OutboundKeepsCSeqIncrement(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{Domain: "203.151.21.121", Port: 5060},
		publicAddress: "10.10.10.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s4"}
	sess.SetCallInfo("outbound", "sip:1100@203.151.21.121:5060", "sip:00025@203.150.245.42:5060", "call-4")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.150.245.42:5060>", "203.151.21.121", 5060, 2, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	cseq := req.CSeq()
	if cseq == nil || cseq.SeqNo != 3 {
		t.Fatalf("expected outbound BYE CSeq=3, got %#v", cseq)
	}
}

func TestCreateBYERequest_InboundBYEAdvancesPastInDialogCSeq(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{Domain: "sipagent.ttrs.or.th", Port: 5060},
		publicAddress: "203.151.21.121",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "s5"}
	sess.SetCallInfo("inbound", "sip:1429900148716@203.150.245.41", "sip:00025@203.151.21.121:5090", "call-5")
	sess.SetSIPDialogState("local-tag", "as168514b7", "<sip:1429900148716@203.150.245.41:5060;transport=tcp>", "sipagent.ttrs.or.th", 5060, 6, nil)

	req, err := srv.createBYERequest(sess)
	if err != nil {
		t.Fatalf("createBYERequest failed: %v", err)
	}

	cseq := req.CSeq()
	if cseq == nil || cseq.SeqNo != 7 || cseq.MethodName != "BYE" {
		t.Fatalf("expected inbound BYE CSeq=7 BYE after in-dialog CSeq 6, got %#v", cseq)
	}
}

type sipAsyncSnapshotStoreStub struct {
	logstore.LogStore
	mu       sync.Mutex
	sessions []*logstore.SessionRecord
}

func (s *sipAsyncSnapshotStoreStub) LogEvent(_ *logstore.Event) {}
func (s *sipAsyncSnapshotStoreStub) StorePayloadAndLogEventAsync(_ *logstore.PayloadRecord, _ *logstore.Event) {
}
func (s *sipAsyncSnapshotStoreStub) UpsertSessionAsync(sess *logstore.SessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = append(s.sessions, sess)
}
func (s *sipAsyncSnapshotStoreStub) UpsertDialogAsync(_ *logstore.DialogRecord)                 {}
func (s *sipAsyncSnapshotStoreStub) UpsertSessionDirectoryAsync(_, _, _ string, _ int)          {}
func (s *sipAsyncSnapshotStoreStub) UpsertSession(_ context.Context, _ *logstore.SessionRecord) error {
	return nil
}

func TestCancelPendingCall_PreInvitePersistsEndedSnapshot(t *testing.T) {
	store := &sipAsyncSnapshotStoreStub{}
	srv := &Server{logStore: store}
	sess := &session.Session{ID: "pre-invite-cancel"}
	sess.SetCallInfo("outbound", "1000", "2000", "")
	sess.UpdateState(session.StateConnecting)

	if err := srv.CancelPendingCall(sess); err != nil {
		t.Fatalf("CancelPendingCall returned error: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.sessions) != 1 {
		t.Fatalf("expected 1 async session snapshot, got %d", len(store.sessions))
	}
	got := store.sessions[0]
	if got.SessionID != sess.ID || got.FinalState != string(session.StateEnded) || got.EndedAt == nil {
		t.Fatalf("expected ended snapshot with EndedAt != nil, got %+v", got)
	}
}

func TestHangup_PreDialogPersistsEndedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sipClient *sipgo.Client
	}{
		{"nil_sip_client", nil},
		{"empty_sip_call_id", &sipgo.Client{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &sipAsyncSnapshotStoreStub{}
			srv := &Server{logStore: store, sipClient: tc.sipClient}
			sess := &session.Session{ID: "pre-dialog-" + tc.name}
			sess.SetCallInfo("outbound", "1000", "2000", "")
			sess.UpdateState(session.StateConnecting)

			if err := srv.Hangup(sess); err != nil {
				t.Fatalf("Hangup returned error: %v", err)
			}
			if sess.GetState() != session.StateEnded {
				t.Fatalf("expected session state ended, got %s", sess.GetState())
			}

			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.sessions) != 1 {
				t.Fatalf("expected 1 async session snapshot, got %d", len(store.sessions))
			}
			got := store.sessions[0]
			if got.SessionID != sess.ID || got.FinalState != string(session.StateEnded) || got.EndedAt == nil {
				t.Fatalf("expected ended snapshot with EndedAt != nil, got %+v", got)
			}
		})
	}
}
