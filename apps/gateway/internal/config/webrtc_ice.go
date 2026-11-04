package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// DefaultWebRTCICEInterfaceExclude lists interface-name prefixes that are
// skipped when gathering host candidates. They are container, VM, and overlay
// bridges whose addresses a remote client can never reach; advertising them
// only adds dead candidate pairs that the browser checks before the real one.
var DefaultWebRTCICEInterfaceExclude = []string{
	"docker", "br-", "veth", "virbr", "vnet", "vmnet", "vboxnet", "lxcbr", "lxdbr",
	"podman", "cni", "flannel", "cali", "cilium", "weave", "kube-",
}

// DefaultWebRTCICENetworkTypes keeps host and srflx gathering on IPv4 UDP.
// TURN relay candidates are UDP relay endpoints whatever the client-to-TURN
// transport is, so turn:?transport=tcp and turns: keep working. The gateway
// has no ICE-TCP mux, so TCP host candidates were never gathered anyway.
var DefaultWebRTCICENetworkTypes = []string{"udp4"}

var validWebRTCICENetworkTypes = map[string]bool{"udp4": true, "udp6": true, "tcp4": true, "tcp6": true}

// WebRTCICEConfig limits the ICE candidates every gateway PeerConnection
// gathers. Empty slices mean "no restriction" (Pion's own behaviour).
type WebRTCICEConfig struct {
	InterfaceExclude []string // Interface-name prefixes skipped for host candidates (WEBRTC_ICE_INTERFACE_EXCLUDE)
	IPAllowlist      []string // Local IPs/CIDRs kept for host candidates, normalized to CIDR (WEBRTC_ICE_IP_ALLOWLIST)
	NAT1To1IPs       []string // Public IPs advertised in place of host IPs: "public" or "public/local" (WEBRTC_ICE_NAT1TO1_IPS)
	NetworkTypes     []string // udp4|udp6|tcp4|tcp6; empty means all (WEBRTC_ICE_NETWORK_TYPES)
}

// Summary is the one-line form used by Display and session logs.
func (c WebRTCICEConfig) Summary() string {
	list := func(values []string) string {
		if len(values) == 0 {
			return "-"
		}
		return strings.Join(values, ",")
	}
	networks := list(c.NetworkTypes)
	if len(c.NetworkTypes) == 0 {
		networks = "all"
	}
	return fmt.Sprintf("iface_exclude=%s ip_allow=%s nat1to1=%s networks=%s",
		list(c.InterfaceExclude), list(c.IPAllowlist), list(c.NAT1To1IPs), networks)
}

func webrtcICEConfigFromEnv() WebRTCICEConfig {
	return WebRTCICEConfig{
		InterfaceExclude: webrtcICEInterfaceExcludeFromEnv(),
		IPAllowlist:      webrtcICEIPAllowlistFromEnv(),
		NAT1To1IPs:       webrtcICENAT1To1IPsFromEnv(),
		NetworkTypes:     webrtcICENetworkTypesFromEnv(),
	}
}

// splitEnvList splits a comma/space separated value. ok is false when the
// variable is unset or blank so callers can apply their default.
func splitEnvList(key string) (values []string, ok bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, false
	}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if field = strings.TrimSpace(field); field != "" {
			values = append(values, field)
		}
	}
	return values, true
}

func isDisableValue(values []string) bool {
	if len(values) != 1 {
		return false
	}
	switch strings.ToLower(values[0]) {
	case "none", "off", "false", "0":
		return true
	}
	return false
}

func webrtcICEInterfaceExcludeFromEnv() []string {
	values, ok := splitEnvList("WEBRTC_ICE_INTERFACE_EXCLUDE")
	if !ok {
		return append([]string(nil), DefaultWebRTCICEInterfaceExclude...)
	}
	if isDisableValue(values) {
		return nil
	}
	for i := range values {
		values[i] = strings.ToLower(values[i])
	}
	return values
}

// parseIPOrPrefix accepts a bare IP or a CIDR and returns the masked prefix.
func parseIPOrPrefix(value string) (netip.Prefix, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func webrtcICEIPAllowlistFromEnv() []string {
	values, ok := splitEnvList("WEBRTC_ICE_IP_ALLOWLIST")
	if !ok || isDisableValue(values) {
		return nil
	}
	var out []string
	for _, value := range values {
		prefix, err := parseIPOrPrefix(value)
		if err != nil {
			fmt.Printf("Warning: invalid WEBRTC_ICE_IP_ALLOWLIST entry %q ignored: %v\n", value, err)
			continue
		}
		out = append(out, prefix.String())
	}
	if len(out) == 0 {
		fmt.Println("Warning: WEBRTC_ICE_IP_ALLOWLIST has no valid entries, ICE IP filtering disabled")
	}
	return out
}

func webrtcICENAT1To1IPsFromEnv() []string {
	values, ok := splitEnvList("WEBRTC_ICE_NAT1TO1_IPS")
	if !ok || isDisableValue(values) {
		return nil
	}
	var out []string
	for _, value := range values {
		external, local, hasLocal := strings.Cut(value, "/")
		externalAddr, err := netip.ParseAddr(external)
		if err != nil {
			fmt.Printf("Warning: invalid WEBRTC_ICE_NAT1TO1_IPS entry %q ignored: %v\n", value, err)
			continue
		}
		entry := externalAddr.Unmap().String()
		if hasLocal {
			localAddr, err := netip.ParseAddr(local)
			if err != nil {
				fmt.Printf("Warning: invalid WEBRTC_ICE_NAT1TO1_IPS entry %q ignored: %v\n", value, err)
				continue
			}
			entry += "/" + localAddr.Unmap().String()
		}
		out = append(out, entry)
	}
	return out
}

func webrtcICENetworkTypesFromEnv() []string {
	values, ok := splitEnvList("WEBRTC_ICE_NETWORK_TYPES")
	if !ok {
		return append([]string(nil), DefaultWebRTCICENetworkTypes...)
	}
	if len(values) == 1 && strings.EqualFold(values[0], "all") {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(value)
		if !validWebRTCICENetworkTypes[value] {
			fmt.Printf("Warning: invalid WEBRTC_ICE_NETWORK_TYPES entry %q ignored (udp4|udp6|tcp4|tcp6|all)\n", value)
			continue
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		fmt.Printf("Warning: WEBRTC_ICE_NETWORK_TYPES has no valid entries, using %s\n", strings.Join(DefaultWebRTCICENetworkTypes, ","))
		return append([]string(nil), DefaultWebRTCICENetworkTypes...)
	}
	// Relay candidates are published only when a UDP type is configured.
	hasUDP := false
	for _, value := range out {
		if strings.HasPrefix(value, "udp") {
			hasUDP = true
		}
	}
	if !hasUDP {
		fmt.Println("Warning: WEBRTC_ICE_NETWORK_TYPES has no udp4/udp6, so TURN relay candidates will not be gathered")
	}
	return out
}
