package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/sip"
)

func TestWSClientSIPAccountRESTAndSSE(t *testing.T) {
	for _, tc := range []struct {
		name       string
		trunk      bool
		session    bool
		resolvedID int64
		wantUser   string
		wantDomain string
		wantPort   int
	}{
		{name: "idle resolved trunk", trunk: true, resolvedID: 42, wantUser: "1001", wantDomain: "sip.example.com", wantPort: 5060},
		{name: "resolved trunk precedes session", trunk: true, session: true, resolvedID: 42, wantUser: "1001", wantDomain: "sip.example.com", wantPort: 5060},
		{name: "public session", session: true, wantUser: "public-user", wantDomain: "public.example.com", wantPort: 5070},
		{name: "missing trunk falls back to session", session: true, resolvedID: 99, wantUser: "public-user", wantDomain: "public.example.com", wantPort: 5070},
		{name: "unresolved connection"},
		{name: "missing trunk without session", resolvedID: 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trunks := &apiHandlerTrunkManagerStub{byID: map[int64]*sip.Trunk{}}
			if tc.trunk {
				trunks.byID[42] = makeTestTrunk(42, "trunk-public-id")
			}
			srv, mgr := newAPIHandlerTestServer(t, trunks, nil, nil)
			client := &WSClient{
				clientID: "client-1", ConnectedAt: time.Now(),
				resolvedTrunkID: tc.resolvedID, trunkResolved: tc.resolvedID > 0,
				publicOnly: tc.session && tc.resolvedID == 0,
			}
			if tc.session {
				sess, err := mgr.CreateSession(config.TURNConfig{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
				sess.SetSIPAuthContext("public", "", 0, "public.example.com", "public-user", "session-password", 5070)
				client.sessionID = sess.ID
			}
			srv.wsConnections[client] = struct{}{}
			rr := doRequest(t, srv.handleListWSClients, http.MethodGet, "/api/ws-clients", "", nil)
			responses := assertJSONDecode[[]WSClientResponse](t, rr, http.StatusOK)
			if len(responses) != 1 {
				t.Fatalf("expected one connection: %+v", responses)
			}
			assertAccount := func(response WSClientResponse, payload []byte) {
				t.Helper()
				if response.SIPUsername != tc.wantUser || response.SIPDomain != tc.wantDomain || response.SIPPort != tc.wantPort {
					t.Fatalf("unexpected SIP account: %+v", response)
				}
				if strings.Contains(string(payload), "password") || strings.Contains(string(payload), "secret") {
					t.Fatalf("credentials leaked in connection diagnostics: %s", payload)
				}
				if tc.wantUser == "" && (strings.Contains(string(payload), "sipUsername") || strings.Contains(string(payload), "sipDomain") || strings.Contains(string(payload), "sipPort")) {
					t.Fatalf("unknown SIP account should be omitted: %s", payload)
				}
			}
			assertAccount(responses[0], rr.Body.Bytes())

			streamID, events := srv.subscribeWSClientStream()
			defer srv.unsubscribeWSClientStream(streamID)
			srv.notifyWSClientChanged("updated", client)
			select {
			case payload := <-events:
				var event WSClientStreamEvent
				if err := json.Unmarshal(payload, &event); err != nil {
					t.Fatal(err)
				}
				if event.Client == nil {
					t.Fatal("SSE event missing client")
				}
				assertAccount(*event.Client, payload)
			case <-time.After(time.Second):
				t.Fatal("SSE client update missing")
			}
		})
	}
}
