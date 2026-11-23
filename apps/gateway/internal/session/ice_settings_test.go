package session

import (
	"net"
	"strings"
	"testing"
	"time"

	pionice "github.com/pion/ice/v4"
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

func TestResolveGatewayICESettings_TestModeVsProduction(t *testing.T) {
	t.Run("test mode forces loopback udp4 and disables mDNS", func(t *testing.T) {
		got, err := resolveGatewayICESettings(config.WebRTCICEConfig{}, true)
		if err != nil {
			t.Fatalf("resolveGatewayICESettings(test): %v", err)
		}
		if !got.includeLoopback {
			t.Fatal("expected includeLoopback=true in test mode")
		}
		if got.mdnsMode != pionice.MulticastDNSModeDisabled {
			t.Fatalf("mdnsMode = %v, want MulticastDNSModeDisabled", got.mdnsMode)
		}
		if got.ipFilter == nil {
			t.Fatal("expected non-nil ipFilter in test mode")
		}
		if !got.ipFilter(net.ParseIP("127.0.0.1")) {
			t.Fatal("expected test ipFilter to keep 127.0.0.1")
		}
		if got.ipFilter(net.ParseIP("192.168.1.10")) || got.ipFilter(net.ParseIP("203.0.113.10")) {
			t.Fatal("expected test ipFilter to reject non-loopback IPs")
		}
		if len(got.networkTypes) != 1 || got.networkTypes[0] != webrtc.NetworkTypeUDP4 {
			t.Fatalf("networkTypes = %v, want [udp4]", got.networkTypes)
		}
	})

	t.Run("test mode intersects explicit IP allowlist with loopback and validates input", func(t *testing.T) {
		got, err := resolveGatewayICESettings(config.WebRTCICEConfig{
			IPAllowlist: []string{"127.0.0.1/32", "10.0.0.0/8"},
		}, true)
		if err != nil {
			t.Fatalf("resolveGatewayICESettings(test+allowlist): %v", err)
		}
		if !got.ipFilter(net.ParseIP("127.0.0.1")) {
			t.Fatal("expected intersected filter to keep 127.0.0.1")
		}
		if got.ipFilter(net.ParseIP("10.0.0.5")) {
			t.Fatal("expected intersected filter to reject non-loopback 10.0.0.5 in test mode")
		}
		if _, err := resolveGatewayICESettings(config.WebRTCICEConfig{IPAllowlist: []string{"not-an-ip"}}, true); err == nil {
			t.Fatal("expected invalid allowlist entry to return error in test mode")
		}
	})

	t.Run("production mode preserves normal non-loopback defaults", func(t *testing.T) {
		empty, err := resolveGatewayICESettings(config.WebRTCICEConfig{}, false)
		if err != nil {
			t.Fatalf("resolveGatewayICESettings(prod empty): %v", err)
		}
		if empty.includeLoopback {
			t.Fatal("expected includeLoopback=false in production mode")
		}
		if empty.mdnsMode != 0 {
			t.Fatalf("expected production mdnsMode to stay unset (0), got %v", empty.mdnsMode)
		}
		if empty.ipFilter != nil || empty.interfaceFilter != nil || len(empty.networkTypes) != 0 {
			t.Fatalf("expected empty production config to leave filters/networks unset, got %+v", empty)
		}

		prod, err := resolveGatewayICESettings(config.WebRTCICEConfig{
			InterfaceExclude: config.DefaultWebRTCICEInterfaceExclude,
			IPAllowlist:      []string{"203.150.245.39/32"},
			NetworkTypes:     []string{"udp4"},
		}, false)
		if err != nil {
			t.Fatalf("resolveGatewayICESettings(prod configured): %v", err)
		}
		if prod.includeLoopback || prod.mdnsMode != 0 {
			t.Fatalf("unexpected test-only overrides in production mode: includeLoopback=%v mdnsMode=%v", prod.includeLoopback, prod.mdnsMode)
		}
		if !prod.ipFilter(net.ParseIP("203.150.245.39")) || prod.ipFilter(net.ParseIP("127.0.0.1")) {
			t.Fatal("production ipFilter did not match configured allowlist")
		}
		if prod.interfaceFilter("docker0") || !prod.interfaceFilter("eth0") {
			t.Fatal("production interfaceFilter did not match configured exclusions")
		}
		if len(prod.networkTypes) != 1 || prod.networkTypes[0] != webrtc.NetworkTypeUDP4 {
			t.Fatalf("production networkTypes = %v, want [udp4]", prod.networkTypes)
		}
	})
}

func TestGatewayAPIInTestModeGathersOnlyLoopbackUDP4(t *testing.T) {
	api, err := newGatewayWebRTCAPI(true, "", config.WebRTCICEConfig{})
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
	case <-time.After(5 * time.Second):
		t.Fatal("ICE gathering did not complete")
	}

	var candidates []string
	for _, line := range strings.Split(pc.LocalDescription().SDP, "\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			candidates = append(candidates, strings.TrimSpace(line))
		}
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one loopback host candidate in test mode")
	}
	for _, candidate := range candidates {
		fields := strings.Fields(candidate)
		if len(fields) < 8 {
			t.Fatalf("malformed candidate %q", candidate)
		}
		if !strings.EqualFold(fields[2], "udp") {
			t.Fatalf("non-UDP candidate gathered in test mode: %q", candidate)
		}
		if strings.HasSuffix(strings.ToLower(fields[4]), ".local") {
			t.Fatalf("mDNS candidate gathered in test mode: %q", candidate)
		}
		ip := net.ParseIP(fields[4])
		if ip == nil || ip.To4() == nil || !ip.IsLoopback() {
			t.Fatalf("non-IPv4-loopback candidate gathered in test mode: %q", candidate)
		}
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
