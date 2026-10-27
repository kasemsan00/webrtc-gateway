package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

func TestListSessionsAnsweredAtIsNullUntilActive(t *testing.T) {
	trunk := makeTestTrunk(56, "56565656-5656-4565-8565-565656565656")
	tm := &apiHandlerTrunkManagerStub{byID: map[int64]*sip.Trunk{56: trunk}}
	srv, mgr := newAPIHandlerTestServer(t, tm, nil, nil)

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.SetCallInfo("outbound", "1001", "0812345678", "call-ans")
	sess.SetSIPAuthContext("trunk", "", 56, "sip.example.com", "1001", "secret", 5060)
	sess.SetState(session.StateRinging)

	rr := doRequest(t, srv.handleListSessions, http.MethodGet, "/sessions", "", nil)
	if body := rr.Body.String(); !strings.Contains(body, `"answeredAt":null`) {
		t.Fatalf("ringing session must expose answeredAt:null, got %s", body)
	}

	sess.SetState(session.StateActive)
	rr = doRequest(t, srv.handleListSessions, http.MethodGet, "/sessions", "", nil)
	items := assertJSONDecode[[]SessionResponse](t, rr, http.StatusOK)
	if len(items) != 1 || items[0].AnsweredAt == nil {
		t.Fatalf("active session must expose answeredAt, got %+v", items)
	}
	parsed, err := time.Parse(time.RFC3339, *items[0].AnsweredAt)
	if err != nil {
		t.Fatalf("answeredAt %q is not RFC3339: %v", *items[0].AnsweredAt, err)
	}
	if want := sess.AnsweredAt().Truncate(time.Second); !parsed.Equal(want) {
		t.Fatalf("answeredAt=%v want %v", parsed, want)
	}

	rr = doRequest(t, srv.handleGetSession, http.MethodGet, "/sessions/x", "", map[string]string{"sessionId": sess.ID})
	out := assertJSONDecode[SessionResponse](t, rr, http.StatusOK)
	if out.AnsweredAt == nil || *out.AnsweredAt != *items[0].AnsweredAt {
		t.Fatalf("get session answeredAt=%v want %s", out.AnsweredAt, *items[0].AnsweredAt)
	}
}
