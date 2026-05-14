package sip

import (
	"strings"
	"testing"
)

func strPtr(v string) *string {
	return &v
}

func TestTrunkContactHeaderAddsPushParamsAfterURI(t *testing.T) {
	tm := &TrunkManager{
		publicIP:  "192.168.1.100",
		localPort: 5060,
	}
	trunk := &Trunk{
		Username: "user123",
		PNAppID:  strPtr("th.or.ttrs.video.prod"),
		PNType:   strPtr("apple"),
		PNToken:  strPtr("D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"),
	}

	got := tm.trunkContactHeader(trunk).String()
	want := "Contact: <sip:user123@192.168.1.100:5060>;app-id=th.or.ttrs.video.prod;pn-type=apple;pn-tok=D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	if got != want {
		t.Fatalf("unexpected Contact header:\nwant %s\n got %s", want, got)
	}
	if strings.Contains(got, "5060;app-id") {
		t.Fatalf("push params must be Contact header params, not URI params: %s", got)
	}
}
