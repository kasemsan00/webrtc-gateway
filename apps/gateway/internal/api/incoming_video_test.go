package api

import (
	"testing"

	"k2-gateway/internal/config"
	"k2-gateway/internal/sip"
)

func TestHasActiveVideoMedia(t *testing.T) {
	tests := []struct {
		name string
		sdp  string
		want bool
	}{
		{
			name: "active video",
			sdp:  "v=0\r\nm=audio 4000 RTP/AVP 111\r\nm=video 4002 RTP/AVP 96\r\na=sendrecv\r\n",
			want: true,
		},
		{
			name: "rejected video port",
			sdp:  "v=0\r\nm=video 0 RTP/AVP 96\r\na=sendrecv\r\n",
			want: false,
		},
		{
			name: "inactive video",
			sdp:  "v=0\r\nm=video 4002 RTP/AVP 96\r\na=inactive\r\n",
			want: false,
		},
		{
			name: "audio only",
			sdp:  "v=0\r\nm=audio 4000 RTP/AVP 111\r\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasActiveVideoMedia(tt.sdp); got != tt.want {
				t.Fatalf("hasActiveVideoMedia() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrunkHasApplePushKitTarget(t *testing.T) {
	appID := config.DefaultTrunkPNAppID
	pnType := "apple"
	token := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunk := &sip.Trunk{
		PNAppID: &appID,
		PNType:  &pnType,
		PNToken: &token,
	}

	if !trunkHasApplePushKitTarget(trunk, config.DefaultTrunkPNAppID) {
		t.Fatal("expected valid Apple PushKit target")
	}

	pnType = "firebase"
	if trunkHasApplePushKitTarget(trunk, config.DefaultTrunkPNAppID) {
		t.Fatal("did not expect non-apple PN type to be a PushKit target")
	}
}
