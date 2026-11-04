package api

import (
	"strings"
	"sync"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func TestNormalizeBridgedChatBody(t *testing.T) {
	if got := normalizeBridgedChatBody("  สวัสดีครับ\r\n"); got != "สวัสดีครับ" {
		t.Fatalf("normalize Thai greeting = %q", got)
	}
	if got := normalizeBridgedChatBody("@open_chat"); got != "@open_chat" {
		t.Fatalf("normalize command = %q", got)
	}
	if got := normalizeBridgedChatBody("  "); got != "" {
		t.Fatalf("blank body = %q", got)
	}
}

func TestBridgedChatEchoGuardExpiresAndEvictsOldest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	g := newBridgedChatEchoGuard()
	g.maxSize = 2
	g.now = func() time.Time { return now }

	g.remember("sess", "a")
	g.remember("sess", "b")
	g.remember("sess", "a") // refresh a so b is the oldest
	g.remember("sess", "c")
	if g.suppress("sess", "b") {
		t.Fatal("oldest body should be evicted once the cap is exceeded")
	}
	if !g.suppress("sess", "a") || !g.suppress("sess", "c") {
		t.Fatal("refreshed and newest bodies should stay inside the window")
	}

	now = now.Add(bridgedChatEchoTTL)
	if g.suppress("sess", "a") || g.suppress("sess", "c") {
		t.Fatal("bodies should deliver again once the window has elapsed")
	}
	if len(g.entries) != 0 {
		t.Fatalf("expired entries = %d, want 0", len(g.entries))
	}
}

func TestBridgedChatEchoGuardConcurrent(t *testing.T) {
	g := newBridgedChatEchoGuard()
	g.maxSize = 8
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := "สวัสดี"
			if i%2 == 0 {
				body = "@open_chat"
			}
			gen := g.remember("sess", body)
			_ = g.suppress("sess", body+"\r\n")
			if i%5 == 0 {
				g.forget("sess", body, gen)
			}
		}(i)
	}
	wg.Wait()
}

type bridgedChatFixture struct {
	srv     *Server
	maker   *agentMessageSIPMaker
	public  *session.Session
	agent   *session.Session
	pubConn *WSClient
	agtConn *WSClient
	now     *time.Time
}

func newBridgedChatFixture(t *testing.T) *bridgedChatFixture {
	t.Helper()
	mgr := newTestSessionManager()
	publicSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create public session: %v", err)
	}
	publicSess.SetCallInfo("outbound", "sip:0000178995763@sipclient.ttrs.or.th", "sip:14131@sipclient.ttrs.or.th", "public-dialog")
	publicSess.SetSIPAuthContext("public", "", 0, "sipclient.ttrs.or.th", "0000178995763", "secret", 5060)
	publicSess.SetState(session.StateActive)

	agentSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create agent session: %v", err)
	}
	agentSess.SetCallInfo("inbound", "sip:0000178995763@sipclient.ttrs.or.th", "sip:00025@sipagent.ttrs.or.th", "agent-dialog")
	agentSess.SetSIPAuthContext("trunk", "", 1, "sipagent.ttrs.or.th", "00025", "secret", 5060)
	agentSess.SetState(session.StateActive)

	maker := &agentMessageSIPMaker{}
	srv := NewServer(config.APIConfig{DebugWebSocket: true}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, maker, nil, nil, nil)
	now := time.Unix(1_700_000_000, 0).UTC()
	srv.bridgedChatEcho.now = func() time.Time { return now }
	publicClient := &WSClient{sessionID: publicSess.ID, publicOnly: true, send: make(chan []byte, 8), ConnectedAt: now}
	agentClient := &WSClient{sessionID: agentSess.ID, agentOnly: true, send: make(chan []byte, 8), ConnectedAt: now}
	srv.mu.Lock()
	srv.wsClients[publicSess.ID] = publicClient
	srv.wsClients[agentSess.ID] = agentClient
	srv.wsConnections[publicClient] = struct{}{}
	srv.wsConnections[agentClient] = struct{}{}
	srv.mu.Unlock()

	return &bridgedChatFixture{
		srv:     srv,
		maker:   maker,
		public:  publicSess,
		agent:   agentSess,
		pubConn: publicClient,
		agtConn: agentClient,
		now:     &now,
	}
}

func chatBodies(msgs []WSMessage) []string {
	var bodies []string
	for _, msg := range msgs {
		if msg.Type == "message" {
			bodies = append(bodies, msg.Body)
		}
	}
	return bodies
}

func TestAgentBridgedChatDropsPBXCopyWithinWindow(t *testing.T) {
	fx := newBridgedChatFixture(t)
	body := "สวัสดีครับ"
	logs := captureStandardLogs(t, func() {
		fx.srv.handleWSSendMessage(fx.agtConn, WSMessage{
			Type:      "send_message",
			SessionID: fx.agent.ID,
			Body:      body,
		})
		*fx.now = fx.now.Add(4 * time.Millisecond)
		fx.srv.NotifySIPMessageOnDialog("public-dialog", "sip:0000178995763@sipclient.ttrs.or.th", "00025", body+"\r\n", "text/plain")
	})

	if fx.maker.inDialogCount == 0 {
		t.Fatal("agent chat should still SIP MESSAGE toward the PBX")
	}
	bodies := chatBodies(readWSMessages(t, fx.pubConn.send))
	if len(bodies) != 1 || bodies[0] != body {
		t.Fatalf("public session bodies = %#v, want one relayed copy", bodies)
	}
	if !strings.Contains(logs, "Dropped duplicate PBX chat copy") {
		t.Fatalf("expected debug drop log, got %q", logs)
	}
}

func TestAgentBridgedChatDeliversPBXCopyAfterWindow(t *testing.T) {
	fx := newBridgedChatFixture(t)
	body := "@open_chat"
	fx.srv.handleWSSendMessage(fx.agtConn, WSMessage{
		Type:      "send_message",
		SessionID: fx.agent.ID,
		Body:      body,
	})
	*fx.now = fx.now.Add(bridgedChatEchoTTL)
	fx.srv.NotifySIPMessageOnDialog("public-dialog", "sip:0000178995763@sipclient.ttrs.or.th", "00025", body, "text/plain")

	bodies := chatBodies(readWSMessages(t, fx.pubConn.send))
	if len(bodies) != 2 || bodies[0] != body || bodies[1] != body {
		t.Fatalf("public session bodies = %#v, want relay plus PBX copy after the window", bodies)
	}
}

func TestAgentBridgedChatDeliversDifferentPBXBody(t *testing.T) {
	fx := newBridgedChatFixture(t)
	fx.srv.handleWSSendMessage(fx.agtConn, WSMessage{
		Type:      "send_message",
		SessionID: fx.agent.ID,
		Body:      "@open_chat",
	})
	*fx.now = fx.now.Add(4 * time.Millisecond)
	fx.srv.NotifySIPMessageOnDialog("public-dialog", "sip:0000178995763@sipclient.ttrs.or.th", "00025", "สวัสดีครับ", "text/plain")

	bodies := chatBodies(readWSMessages(t, fx.pubConn.send))
	if len(bodies) != 2 || bodies[0] != "@open_chat" || bodies[1] != "สวัสดีครับ" {
		t.Fatalf("public session bodies = %#v, want relayed command and a different PBX body", bodies)
	}
}

func TestPhoneBridgedChatDropsPBXCopyWithinWindow(t *testing.T) {
	fx := newBridgedChatFixture(t)
	body := "สวัสดีครับ"
	logs := captureStandardLogs(t, func() {
		fx.srv.handleWSSendMessage(fx.pubConn, WSMessage{
			Type:      "send_message",
			SessionID: fx.public.ID,
			Body:      body,
		})
		*fx.now = fx.now.Add(4 * time.Millisecond)
		fx.srv.NotifySIPMessageOnDialog("agent-dialog", "sip:00025@sipagent.ttrs.or.th", "0000178995763", "  "+body+"\n", "text/plain")
	})

	if fx.maker.inDialogCount != 0 || fx.maker.outOfDialogCount != 0 {
		t.Fatalf("bridged phone chat must not also SIP MESSAGE, sip calls=%d/%d", fx.maker.inDialogCount, fx.maker.outOfDialogCount)
	}
	bodies := chatBodies(readWSMessages(t, fx.agtConn.send))
	if len(bodies) != 1 || bodies[0] != body {
		t.Fatalf("agent session bodies = %#v, want one relayed copy", bodies)
	}
	if !strings.Contains(logs, "Dropped duplicate PBX chat copy") {
		t.Fatalf("expected debug drop log for phone→agent echo, got %q", logs)
	}
}

func TestAgentBridgedChatSIPFailureDoesNotSuppressLaterPBX(t *testing.T) {
	fx := newBridgedChatFixture(t)
	fx.maker.inDialogErr = errBridgedChatSIP
	fx.srv.handleWSSendMessage(fx.agtConn, WSMessage{
		Type:      "send_message",
		SessionID: fx.agent.ID,
		Body:      "@open_chat",
	})
	if got := chatBodies(readWSMessages(t, fx.pubConn.send)); len(got) != 0 {
		t.Fatalf("failed SIP send must not relay, got %#v", got)
	}
	fx.srv.NotifySIPMessageOnDialog("public-dialog", "sip:0000178995763@sipclient.ttrs.or.th", "00025", "@open_chat", "text/plain")
	bodies := chatBodies(readWSMessages(t, fx.pubConn.send))
	if len(bodies) != 1 || bodies[0] != "@open_chat" {
		t.Fatalf("PBX copy after a failed send = %#v, want delivery", bodies)
	}
}

var errBridgedChatSIP = bridgedChatSIPError{}

type bridgedChatSIPError struct{}

func (bridgedChatSIPError) Error() string { return "sip failed" }
