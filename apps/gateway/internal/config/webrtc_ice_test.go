package config

import (
	"reflect"
	"testing"
)

func TestWebRTCICEConfigDefaults(t *testing.T) {
	for _, key := range []string{"WEBRTC_ICE_INTERFACE_EXCLUDE", "WEBRTC_ICE_IP_ALLOWLIST", "WEBRTC_ICE_NAT1TO1_IPS", "WEBRTC_ICE_NETWORK_TYPES"} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ice := cfg.SIP.WebRTCICE
	if !reflect.DeepEqual(ice.InterfaceExclude, DefaultWebRTCICEInterfaceExclude) {
		t.Fatalf("InterfaceExclude = %v, want defaults", ice.InterfaceExclude)
	}
	if ice.IPAllowlist != nil || ice.NAT1To1IPs != nil {
		t.Fatalf("expected no IP allowlist or NAT1To1 by default, got %v / %v", ice.IPAllowlist, ice.NAT1To1IPs)
	}
	if !reflect.DeepEqual(ice.NetworkTypes, []string{"udp4"}) {
		t.Fatalf("NetworkTypes = %v, want [udp4]", ice.NetworkTypes)
	}
}

func TestWebRTCICEConfigOverrides(t *testing.T) {
	t.Setenv("WEBRTC_ICE_INTERFACE_EXCLUDE", "Docker, tailscale")
	t.Setenv("WEBRTC_ICE_IP_ALLOWLIST", "203.150.245.39, 10.0.0.0/8 ,bogus,10.1.2.3/16")
	t.Setenv("WEBRTC_ICE_NAT1TO1_IPS", "203.0.113.10,198.51.100.1/10.0.0.5,nope")
	t.Setenv("WEBRTC_ICE_NETWORK_TYPES", "udp4,TCP4,udp4,sctp")

	ice := webrtcICEConfigFromEnv()
	if want := []string{"docker", "tailscale"}; !reflect.DeepEqual(ice.InterfaceExclude, want) {
		t.Fatalf("InterfaceExclude = %v, want %v", ice.InterfaceExclude, want)
	}
	if want := []string{"203.150.245.39/32", "10.0.0.0/8", "10.1.0.0/16"}; !reflect.DeepEqual(ice.IPAllowlist, want) {
		t.Fatalf("IPAllowlist = %v, want %v", ice.IPAllowlist, want)
	}
	if want := []string{"203.0.113.10", "198.51.100.1/10.0.0.5"}; !reflect.DeepEqual(ice.NAT1To1IPs, want) {
		t.Fatalf("NAT1To1IPs = %v, want %v", ice.NAT1To1IPs, want)
	}
	if want := []string{"udp4", "tcp4"}; !reflect.DeepEqual(ice.NetworkTypes, want) {
		t.Fatalf("NetworkTypes = %v, want %v", ice.NetworkTypes, want)
	}
}

func TestWebRTCICEConfigDisableValues(t *testing.T) {
	t.Setenv("WEBRTC_ICE_INTERFACE_EXCLUDE", "none")
	t.Setenv("WEBRTC_ICE_NETWORK_TYPES", "all")

	ice := webrtcICEConfigFromEnv()
	if ice.InterfaceExclude != nil {
		t.Fatalf("InterfaceExclude = %v, want nil for none", ice.InterfaceExclude)
	}
	if ice.NetworkTypes != nil {
		t.Fatalf("NetworkTypes = %v, want nil (Pion default) for all", ice.NetworkTypes)
	}
	if got := ice.Summary(); got != "iface_exclude=- ip_allow=- nat1to1=- networks=all" {
		t.Fatalf("Summary() = %q", got)
	}
}

func TestWebRTCICENetworkTypesAllInvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("WEBRTC_ICE_NETWORK_TYPES", "sctp")

	if got := webrtcICENetworkTypesFromEnv(); !reflect.DeepEqual(got, DefaultWebRTCICENetworkTypes) {
		t.Fatalf("NetworkTypes = %v, want default %v", got, DefaultWebRTCICENetworkTypes)
	}
}
