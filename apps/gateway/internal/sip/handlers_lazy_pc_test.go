package sip

import (
	"testing"
	"time"

	gosip "github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func TestHandleINVITE_CreatesLazyIncomingSessionWithoutPeerConnection(t *testing.T) {
	cfg := &config.Config{}
	mgr := session.NewManager(cfg)
	notifier := &inviteNotifierStub{}
	tx := &cancelServerTxStub{}

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
	}

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "203.150.245.41", Port: 5060})
	req.AppendHeader(&gosip.FromHeader{DisplayName: "Caller", Address: gosip.Uri{User: "0000178810139", Host: "203.150.245.41"}})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(gosip.NewHeader("Call-ID", "invite-lazy-pc-1"))
	req.AppendHeader(&gosip.CSeqHeader{SeqNo: 102, MethodName: gosip.INVITE})

	srv.handleINVITE(req, tx)

	sess, ok := mgr.GetSessionBySIPCallID("invite-lazy-pc-1")
	if !ok || sess == nil {
		t.Fatalf("expected incoming session to be created for Call-ID")
	}
	defer mgr.DeleteSession(sess.ID)

	if sess.GetState() != session.StateIncoming {
		t.Fatalf("expected StateIncoming, got %s", sess.GetState())
	}
	if sess.PeerConnection != nil || sess.HasPeerConnection() {
		t.Fatalf("expected handleINVITE to defer PeerConnection allocation")
	}
	if sess.HasAudioTrack() || sess.HasVideoTrack() {
		t.Fatalf("expected handleINVITE to defer media track allocation")
	}

	// EnsurePeerConnection initializes the PeerConnection on demand.
	if err := sess.EnsurePeerConnection(); err != nil {
		t.Fatalf("EnsurePeerConnection failed: %v", err)
	}
	if !sess.HasPeerConnection() || !sess.HasAudioTrack() || !sess.HasVideoTrack() {
		t.Fatalf("expected PeerConnection and audio/video tracks after EnsurePeerConnection")
	}
}

func TestHandleINVITE_CancelBeforeEnsurePeerConnectionNeverAllocatesPC(t *testing.T) {
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

	req := gosip.NewRequest(gosip.INVITE, gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090})
	req.SetSource("203.150.245.41:5060")
	req.AppendHeader(&gosip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "TCP", Host: "203.150.245.41", Port: 5060})
	req.AppendHeader(&gosip.FromHeader{DisplayName: "Caller", Address: gosip.Uri{User: "0000178977848", Host: "203.150.245.41"}})
	req.AppendHeader(&gosip.ToHeader{Address: gosip.Uri{User: "00025", Host: "203.151.21.121", Port: 5090}})
	req.AppendHeader(gosip.NewHeader("Call-ID", "invite-lazy-cancel-1"))
	req.AppendHeader(&gosip.CSeqHeader{SeqNo: 102, MethodName: gosip.INVITE})

	inviteDone := make(chan struct{})
	go func() {
		srv.handleINVITE(req, inviteTx)
		close(inviteDone)
	}()

	var sess *session.Session
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if found, ok := mgr.GetSessionBySIPCallID("invite-lazy-cancel-1"); ok {
			sess = found
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sess == nil {
		t.Fatal("expected incoming session while INVITE handler is waiting")
	}
	if sess.HasPeerConnection() {
		t.Fatal("expected incoming session to have nil PeerConnection while ringing")
	}

	cancelTx := &cancelServerTxStub{}
	srv.handleCANCEL(newIncomingCancelRequest("invite-lazy-cancel-1"), cancelTx)

	select {
	case <-inviteDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handleINVITE should return after caller CANCEL")
	}
	if sess.HasPeerConnection() {
		t.Fatal("expected canceled incoming session to never allocate PeerConnection")
	}
	if err := sess.EnsurePeerConnection(); err == nil {
		t.Fatal("expected EnsurePeerConnection to fail after incoming session was canceled")
	}
}
