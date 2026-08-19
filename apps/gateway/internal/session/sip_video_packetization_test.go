package session

import "testing"

func TestSIPVideoPayloadTypeLocked(t *testing.T) {
	tests := []struct {
		name       string
		negotiated uint8
		want       uint8
	}{
		{name: "TTRS compatibility fallback", negotiated: 0, want: 96},
		{name: "Huawei negotiated PT99", negotiated: 99, want: 99},
		{name: "peer negotiated PT103", negotiated: 103, want: 103},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &Session{SIPVideoPT: tt.negotiated}
			sess.mu.Lock()
			got := sess.sipVideoPayloadTypeLocked()
			sess.mu.Unlock()
			if got != tt.want {
				t.Fatalf("sipVideoPayloadTypeLocked() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetSIPVideoPayloadType(t *testing.T) {
	sess := &Session{}
	if got := sess.GetSIPVideoPayloadType(); got != 96 {
		t.Fatalf("default payload type = %d, want 96", got)
	}

	sess.SetSIPVideoPayloadType(99)
	if got := sess.GetSIPVideoPayloadType(); got != 99 {
		t.Fatalf("negotiated payload type = %d, want 99", got)
	}
}
