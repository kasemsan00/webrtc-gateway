package session

import "testing"

func TestIsRemoteHoldOffer(t *testing.T) {
	cases := []struct {
		name string
		sdp  string
		want bool
	}{
		{"sendrecv", "c=IN IP4 10.0.0.1\nm=audio 4000 RTP/AVP 111\na=sendrecv\n", false},
		{"no direction means sendrecv", "c=IN IP4 10.0.0.1\nm=audio 4000 RTP/AVP 111\n", false},
		{"audio sendonly", "c=IN IP4 10.0.0.1\nm=audio 4000 RTP/AVP 111\na=sendonly\n", true},
		{"audio inactive with CRLF", "c=IN IP4 10.0.0.1\r\nm=audio 4000 RTP/AVP 111\r\na=inactive\r\n", true},
		{"far end only receiving is not a hold", "m=audio 4000 RTP/AVP 111\na=recvonly\n", false},
		{"session-level sendonly", "a=sendonly\nm=audio 4000 RTP/AVP 111\n", true},
		{"audio direction overrides session", "a=sendonly\nm=audio 4000 RTP/AVP 111\na=sendrecv\n", false},
		{"old style zero address", "c=IN IP4 0.0.0.0\nm=audio 4000 RTP/AVP 111\n", true},
		{"audio connection overrides session", "c=IN IP4 0.0.0.0\nm=audio 4000 RTP/AVP 111\nc=IN IP4 10.0.0.1\n", false},
		{"video direction is ignored", "m=audio 4000 RTP/AVP 111\na=sendrecv\nm=video 4002 RTP/AVP 96\na=inactive\n", false},
	}
	for _, tc := range cases {
		if got := IsRemoteHoldOffer(tc.sdp); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSetRemoteHeldReportsChanges(t *testing.T) {
	s := &Session{}
	if !s.SetRemoteHeld(true) || s.SetRemoteHeld(true) || !s.SetRemoteHeld(false) {
		t.Fatal("SetRemoteHeld must report only real changes")
	}
}

func TestSetEndReasonKeepsTheFirst(t *testing.T) {
	s := &Session{}
	s.SetEndReason("busy")
	s.SetEndReason("failed")
	if got := s.GetEndReason(); got != "busy" {
		t.Fatalf("got %q", got)
	}
}
