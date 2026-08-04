package sip

import "testing"

func TestTrunkWantsAutoRegister(t *testing.T) {
	t.Parallel()

	if trunkWantsAutoRegister(nil) {
		t.Fatal("nil trunk should not want auto-register")
	}
	if trunkWantsAutoRegister(&Trunk{Enabled: true, SipAutoRegister: true}) != true {
		t.Fatal("enabled trunk with sip_auto_register=true should want auto-register")
	}
	if trunkWantsAutoRegister(&Trunk{Enabled: false, SipAutoRegister: true}) {
		t.Fatal("disabled trunk should not want auto-register")
	}
	if trunkWantsAutoRegister(&Trunk{Enabled: true, SipAutoRegister: false}) {
		t.Fatal("trunk with sip_auto_register=false should not want auto-register")
	}
}
