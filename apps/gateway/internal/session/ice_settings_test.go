package session

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

func TestInterfaceExcludeFilterDropsVirtualBridges(t *testing.T) {
	keep := interfaceExcludeFilter(config.DefaultWebRTCICEInterfaceExclude)
	for _, name := range []string{"docker0", "br-3f9a2c1d", "veth12ab", "virbr0", "vethX", "cni0", "flannel.1", "cali1234"} {
		if keep(name) {
			t.Fatalf("expected %q to be excluded", name)
		}
	}
	for _, name := range []string{"eth0", "ens3", "enp0s31f6", "wlan0", "bond0", "br0", "Ethernet"} {
		if !keep(name) {
			t.Fatalf("expected %q to be kept", name)
		}
	}
}

func TestIPAllowlistFilter(t *testing.T) {
	keep, err := ipAllowlistFilter([]string{"203.150.245.39/32", "10.0.0.0/8", "198.51.100.7"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"203.150.245.39": true,
		"203.150.245.40": false,
		"10.20.30.40":    true,
		"172.17.0.1":     false,
		"192.168.16.1":   false,
		"198.51.100.7":   true,
	}
	for ip, want := range cases {
		if got := keep(net.ParseIP(ip)); got != want {
			t.Fatalf("keep(%s) = %v, want %v", ip, got, want)
		}
	}
	if _, err := ipAllowlistFilter([]string{"not-an-ip"}); err == nil {
		t.Fatal("expected an error for an invalid allowlist entry")
	}
}

func TestNAT1To1HostRewriteRules(t *testing.T) {
	rules := nat1To1HostRewriteRules([]string{"203.0.113.10", "198.51.100.1/10.0.0.5"})
	if len(rules) != 2 {
		t.Fatalf("rules = %+v, want a mapped rule plus a catch-all", rules)
	}
	if rules[0].Local != "10.0.0.5" || rules[0].External[0] != "198.51.100.1" {
		t.Fatalf("mapped rule = %+v", rules[0])
	}
	catchAll := rules[1]
	if catchAll.Local != "" || strings.Join(catchAll.External, ",") != "203.0.113.10,198.51.100.1" {
		t.Fatalf("catch-all rule = %+v", catchAll)
	}
	for _, rule := range rules {
		if rule.AsCandidateType != webrtc.ICECandidateTypeHost || rule.Mode != webrtc.ICEAddressRewriteReplace {
			t.Fatalf("rule %+v is not a host replace rule", rule)
		}
	}
}

func TestGatewaySettingEngineRejectsUnknownNetworkType(t *testing.T) {
	if _, err := gatewaySettingEngine(config.WebRTCICEConfig{NetworkTypes: []string{"sctp"}}); err == nil {
		t.Fatal("expected an error for an unknown network type")
	}
}

// TestGatewayAPIAdvertisesOnlyNAT1To1HostCandidates gathers on the real
// interfaces of the test machine: every host candidate must carry the
// configured public IP and only UDP/IPv4 is gathered.
func TestGatewayAPIAdvertisesOnlyNAT1To1HostCandidates(t *testing.T) {
	ice := config.WebRTCICEConfig{
		InterfaceExclude: config.DefaultWebRTCICEInterfaceExclude,
		NAT1To1IPs:       []string{"203.0.113.10"},
		NetworkTypes:     []string{"udp4"},
	}
	api, err := newGatewayWebRTCAPI(true, "", ice)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-time.After(10 * time.Second):
		t.Fatal("ICE gathering did not complete")
	}

	var candidates []string
	for _, line := range strings.Split(pc.LocalDescription().SDP, "\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			candidates = append(candidates, strings.TrimSpace(line))
		}
	}
	if len(candidates) == 0 {
		t.Skip("no usable IPv4 interface on this machine")
	}
	for _, candidate := range candidates {
		fields := strings.Fields(candidate)
		if len(fields) < 8 {
			t.Fatalf("malformed candidate %q", candidate)
		}
		if !strings.EqualFold(fields[2], "udp") {
			t.Fatalf("non-UDP candidate gathered: %q", candidate)
		}
		if fields[7] == "host" && fields[4] != "203.0.113.10" {
			t.Fatalf("host candidate not rewritten to NAT1To1 IP: %q", candidate)
		}
	}
}

func TestLocalCandidateLogLogsOncePerPeerConnection(t *testing.T) {
	log := newLocalCandidateLog("s1", "initial")
	log.Observe(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost, Protocol: webrtc.ICEProtocolUDP, Address: "203.150.245.39", Port: 50000})
	log.Observe(nil)
	if !log.logged || len(log.candidates) != 1 {
		t.Fatalf("logged=%v candidates=%v", log.logged, log.candidates)
	}
	// An ICE restart gathers again; it must not append or log a second time.
	log.Observe(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeRelay, Protocol: webrtc.ICEProtocolUDP, Address: "203.150.245.38", Port: 3478})
	log.Observe(nil)
	if len(log.candidates) != 1 {
		t.Fatalf("candidates after restart = %v", log.candidates)
	}
	if got := log.candidates[0]; got != "host/udp/203.150.245.39:50000" {
		t.Fatalf("candidate = %q", got)
	}
}
