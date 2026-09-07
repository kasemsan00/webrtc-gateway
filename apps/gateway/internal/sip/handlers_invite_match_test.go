package sip

import (
	"encoding/json"
	"strings"
	"testing"

	gosip "github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

type inviteLogStoreStub struct {
	logstore.LogStore
	events []*logstore.Event
}

func newInviteLogStoreStub() *inviteLogStoreStub {
	return &inviteLogStoreStub{LogStore: logstore.Noop()}
}

func (s *inviteLogStoreStub) LogEvent(event *logstore.Event) {
	if event == nil {
		return
	}
	copied := *event
	if event.Data != nil {
		dataCopy := make(map[string]interface{}, len(event.Data))
		for key, value := range event.Data {
			dataCopy[key] = value
		}
		copied.Data = dataCopy
	}
	s.events = append(s.events, &copied)
}

func eventDataField(t *testing.T, event *logstore.Event, key string) interface{} {
	t.Helper()
	if event == nil || event.Data == nil {
		t.Fatalf("expected event data for %q", key)
	}
	value, ok := event.Data[key]
	if !ok {
		t.Fatalf("expected event field %q in %#v", key, event.Data)
	}
	return value
}

func assertNoSensitiveInviteLogFields(t *testing.T, event *logstore.Event) {
	t.Helper()
	if event == nil {
		return
	}
	raw, err := json.Marshal(event.Data)
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}
	text := strings.ToLower(string(raw))
	for _, forbidden := range []string{"password", "authorization", "pntoken", "pn-tok"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("event data must not include %q: %s", forbidden, text)
		}
	}
}

func findInviteLogEvent(events []*logstore.Event, name string) *logstore.Event {
	for _, event := range events {
		if event != nil && event.Name == name {
			return event
		}
	}
	return nil
}

type inviteNotifierStub struct {
	calls   int
	trunkID int64
}

func (n *inviteNotifierStub) NotifyIncomingCall(_ string, _, _ string, trunkID int64) {
	n.calls++
	n.trunkID = trunkID
}

func (n *inviteNotifierStub) NotifyIncomingCancel(string, int64, string) {}

func TestHandleINVITE_OriginMatchNotifiesIncomingCall(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)
	notifier := &inviteNotifierStub{}
	tx := &cancelServerTxStub{}
	logs := newInviteLogStoreStub()

	trunk := &Trunk{ID: 725, Name: "sipclient-agent-00025@sipagent.ttrs.or.th:5060", Domain: "sipagent.ttrs.or.th", Port: 5060, Username: "00025", Transport: "tcp", Enabled: true}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{725: trunk},
		ownedLeases: map[int64]bool{725: true},
	}
	setRegistrarIdentity(tm, trunk, "203.150.245.41")

	srv := &Server{
		sessionMgr:       mgr,
		sessionCreator:   mgr,
		incomingNotifier: notifier,
		trunkManager:     tm,
		publicAddress:    "203.151.21.121",
		sipPort:          5090,
		logStore:         logs,
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "203.150.245.41", Port: 5060})
	req.AppendHeader(&gosip.FromHeader{DisplayName: "Caller", Address: gosip.Uri{User: "0000178810139", Host: "203.150.245.41"}})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(gosip.NewHeader("Call-ID", "invite-match-1"))
	req.AppendHeader(&gosip.CSeqHeader{SeqNo: 102, MethodName: gosip.INVITE})

	srv.handleINVITE(req, tx)
	if notifier.calls != 1 {
		t.Fatalf("expected incoming notification, got %d status=%d", notifier.calls, tx.statusCode)
	}
	if notifier.trunkID != 725 {
		t.Fatalf("expected trunk 725, got %d", notifier.trunkID)
	}
	selected := findInviteLogEvent(logs.events, "sip_invite_trunk_match_selected")
	if selected == nil {
		t.Fatalf("expected selected trunk match log event, got %#v", logs.events)
	}
	if eventDataField(t, selected, "trunkId") != int64(725) {
		t.Fatalf("expected selected trunk ID in log event")
	}
	if eventDataField(t, selected, "localRequestURI") != true {
		t.Fatalf("expected localRequestURI=true in selected log event")
	}
	if eventDataField(t, selected, "registrarEvidence") != "sipagent.ttrs.or.th:5060" {
		t.Fatalf("expected registrar evidence in selected log event")
	}
	assertNoSensitiveInviteLogFields(t, selected)
}

func TestHandleINVITE_AmbiguousMatchDoesNotCreateSession(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)
	notifier := &inviteNotifierStub{}
	tx := &cancelServerTxStub{}
	logs := newInviteLogStoreStub()

	a := &Trunk{ID: 1, Domain: "a.local", Port: 5060, Username: "00025", Transport: "tcp"}
	b := &Trunk{ID: 2, Domain: "b.local", Port: 5060, Username: "00025", Transport: "tcp"}
	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{1: a, 2: b},
		ownedLeases: map[int64]bool{1: true, 2: true},
	}
	setRegistrarIdentity(tm, a, "203.0.113.9")
	setRegistrarIdentity(tm, b, "203.0.113.9")

	srv := &Server{
		sessionMgr:       mgr,
		sessionCreator:   mgr,
		incomingNotifier: notifier,
		trunkManager:     tm,
		publicAddress:    "203.151.21.121",
		sipPort:          5090,
		logStore:         logs,
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.0.113.9:5060")
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "203.0.113.9", Port: 5060})
	req.AppendHeader(&gosip.FromHeader{Address: gosip.Uri{User: "caller", Host: "203.0.113.9"}})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(gosip.NewHeader("Call-ID", "invite-ambiguous-1"))
	req.AppendHeader(&gosip.CSeqHeader{SeqNo: 102, MethodName: gosip.INVITE})

	srv.handleINVITE(req, tx)
	if notifier.calls != 0 {
		t.Fatalf("ambiguous match must not notify incoming call")
	}
	if tx.statusCode != 503 {
		t.Fatalf("expected 503, got %d", tx.statusCode)
	}
	if _, ok := mgr.GetSessionBySIPCallID("invite-ambiguous-1"); ok {
		t.Fatalf("expected no session to be created")
	}

	rejected := findInviteLogEvent(logs.events, "sip_invite_rejected_ambiguous_trunk_match")
	if rejected == nil {
		t.Fatalf("expected ambiguous rejection log event, got %#v", logs.events)
	}
	if rejected.SIPStatusCode != 503 {
		t.Fatalf("expected status 503 in log event, got %d", rejected.SIPStatusCode)
	}
	if eventDataField(t, rejected, "sipUser") != "00025" {
		t.Fatalf("expected sipUser in ambiguous log event")
	}
	if eventDataField(t, rejected, "localRequestURI") != true {
		t.Fatalf("expected localRequestURI=true in ambiguous log event")
	}
	if origins := eventDataField(t, rejected, "origins"); origins == nil {
		t.Fatalf("expected origins in ambiguous log event")
	}
	assertNoSensitiveInviteLogFields(t, rejected)
}

func TestHandleINVITE_NoEligibleTrunkLogsRejection(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)
	notifier := &inviteNotifierStub{}
	tx := &cancelServerTxStub{}
	logs := newInviteLogStoreStub()

	tm := &TrunkManager{
		publicIP:    "203.151.21.121",
		localPort:   5090,
		trunks:      map[int64]*Trunk{},
		ownedLeases: map[int64]bool{},
	}

	srv := &Server{
		sessionMgr:       mgr,
		sessionCreator:   mgr,
		incomingNotifier: notifier,
		trunkManager:     tm,
		publicAddress:    "203.151.21.121",
		sipPort:          5090,
		logStore:         logs,
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.FromHeader{Address: gosip.Uri{User: "caller", Host: "203.150.245.41"}})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(gosip.NewHeader("Call-ID", "invite-no-trunk-1"))
	req.AppendHeader(&gosip.CSeqHeader{SeqNo: 102, MethodName: gosip.INVITE})

	srv.handleINVITE(req, tx)
	if notifier.calls != 0 {
		t.Fatalf("no eligible trunk must not notify incoming call")
	}
	if tx.statusCode != 403 {
		t.Fatalf("expected 403, got %d", tx.statusCode)
	}

	rejected := findInviteLogEvent(logs.events, "sip_invite_rejected_no_trunk")
	if rejected == nil {
		t.Fatalf("expected no-trunk rejection log event, got %#v", logs.events)
	}
	if rejected.SIPStatusCode != 403 {
		t.Fatalf("expected status 403 in log event, got %d", rejected.SIPStatusCode)
	}
	if eventDataField(t, rejected, "sipUser") != "00025" {
		t.Fatalf("expected sipUser in no-trunk log event")
	}
	if reason, ok := eventDataField(t, rejected, "matchReason").(string); !ok || reason == "" {
		t.Fatalf("expected matchReason in no-trunk log event")
	}
	assertNoSensitiveInviteLogFields(t, rejected)
}
