package session

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	pionice "github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

type resolvedGatewayICESettings struct {
	interfaceFilter func(string) bool
	ipFilter        func(net.IP) bool
	rewriteRules    []webrtc.ICEAddressRewriteRule
	networkTypes    []webrtc.NetworkType
	includeLoopback bool
	mdnsMode        pionice.MulticastDNSMode
}

func resolveGatewayICESettings(iceCfg config.WebRTCICEConfig, loopbackOnly bool) (resolvedGatewayICESettings, error) {
	var out resolvedGatewayICESettings

	if len(iceCfg.InterfaceExclude) > 0 {
		out.interfaceFilter = interfaceExcludeFilter(iceCfg.InterfaceExclude)
	}

	if len(iceCfg.IPAllowlist) > 0 {
		filter, err := ipAllowlistFilter(iceCfg.IPAllowlist)
		if err != nil {
			return out, err
		}
		out.ipFilter = filter
	}

	if len(iceCfg.NAT1To1IPs) > 0 {
		out.rewriteRules = nat1To1HostRewriteRules(iceCfg.NAT1To1IPs)
	}

	if len(iceCfg.NetworkTypes) > 0 {
		types := make([]webrtc.NetworkType, 0, len(iceCfg.NetworkTypes))
		for _, raw := range iceCfg.NetworkTypes {
			networkType, err := webrtc.NewNetworkType(raw)
			if err != nil {
				return out, err
			}
			types = append(types, networkType)
		}
		out.networkTypes = types
	}

	if loopbackOnly {
		// Keep `go test` strictly on IPv4 loopback with mDNS disabled so
		// Windows Defender Firewall never prompts on LAN or multicast sockets.
		out.includeLoopback = true
		out.mdnsMode = pionice.MulticastDNSModeDisabled
		if out.ipFilter != nil {
			allowlist := out.ipFilter
			out.ipFilter = func(ip net.IP) bool {
				return ip.IsLoopback() && allowlist(ip)
			}
		} else {
			out.ipFilter = func(ip net.IP) bool {
				return ip.IsLoopback()
			}
		}
		if len(out.networkTypes) == 0 {
			out.networkTypes = []webrtc.NetworkType{webrtc.NetworkTypeUDP4}
		}
	}

	return out, nil
}

// gatewaySettingEngine applies the ICE gathering limits from config. Docker
// and other virtual bridges otherwise add a dozen unreachable host candidates
// that the browser checks before the real address, which delays ICE by
// seconds. Under `go test`, ICE gathering is restricted to loopback with mDNS
// disabled without altering production defaults.
func gatewaySettingEngine(iceCfg config.WebRTCICEConfig) (webrtc.SettingEngine, error) {
	return buildGatewaySettingEngine(iceCfg, testing.Testing())
}

func buildGatewaySettingEngine(iceCfg config.WebRTCICEConfig, loopbackOnly bool) (webrtc.SettingEngine, error) {
	var se webrtc.SettingEngine

	resolved, err := resolveGatewayICESettings(iceCfg, loopbackOnly)
	if err != nil {
		return se, err
	}

	if resolved.interfaceFilter != nil {
		se.SetInterfaceFilter(resolved.interfaceFilter)
	}

	if resolved.ipFilter != nil {
		se.SetIPFilter(resolved.ipFilter)
	}

	if len(resolved.rewriteRules) > 0 {
		if err := se.SetICEAddressRewriteRules(resolved.rewriteRules...); err != nil {
			return se, fmt.Errorf("ICE NAT1To1 rules: %w", err)
		}
	}

	if len(resolved.networkTypes) > 0 {
		se.SetNetworkTypes(resolved.networkTypes)
	}

	if resolved.includeLoopback {
		se.SetIncludeLoopbackCandidate(true)
	}

	if resolved.mdnsMode != 0 {
		se.SetICEMulticastDNSMode(resolved.mdnsMode)
	}

	return se, nil
}

// interfaceExcludeFilter keeps an interface unless its name starts with one of
// the (case-insensitive) prefixes.
func interfaceExcludeFilter(prefixes []string) func(string) bool {
	lowered := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		if prefix = strings.ToLower(strings.TrimSpace(prefix)); prefix != "" {
			lowered = append(lowered, prefix)
		}
	}
	return func(name string) bool {
		name = strings.ToLower(name)
		for _, prefix := range lowered {
			if strings.HasPrefix(name, prefix) {
				return false
			}
		}
		return true
	}
}

// ipAllowlistFilter keeps a local address only when it falls inside one of the
// configured IPs/CIDRs.
func ipAllowlistFilter(entries []string) (func(net.IP) bool, error) {
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		var prefix netip.Prefix
		var err error
		if strings.Contains(entry, "/") {
			prefix, err = netip.ParsePrefix(entry)
		} else {
			var addr netip.Addr
			addr, err = netip.ParseAddr(entry)
			prefix = netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen())
		}
		if err != nil {
			return nil, fmt.Errorf("ICE IP allowlist entry %q: %w", entry, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return func(ip net.IP) bool {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return false
		}
		addr = addr.Unmap()
		for _, prefix := range prefixes {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}, nil
}

// nat1To1HostRewriteRules mirrors Pion's legacy SetNAT1To1IPs(ips, host):
// "public/local" entries map one local IP, bare entries replace every other
// host address.
func nat1To1HostRewriteRules(entries []string) []webrtc.ICEAddressRewriteRule {
	rules := make([]webrtc.ICEAddressRewriteRule, 0, len(entries)+1)
	var catchAll []string
	for _, entry := range entries {
		external, local, hasLocal := strings.Cut(entry, "/")
		if hasLocal {
			rules = append(rules, webrtc.ICEAddressRewriteRule{
				External:        []string{external},
				Local:           local,
				AsCandidateType: webrtc.ICECandidateTypeHost,
				Mode:            webrtc.ICEAddressRewriteReplace,
			})
		}
		catchAll = append(catchAll, external)
	}
	if len(catchAll) > 0 {
		rules = append(rules, webrtc.ICEAddressRewriteRule{
			External:        catchAll,
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		})
	}
	return rules
}

// localCandidateLog collects a PeerConnection's gathered candidates and logs
// them in one line when gathering first completes. An ICE restart on the same
// PeerConnection does not log again.
type localCandidateLog struct {
	sessionID string
	label     string

	mu         sync.Mutex
	candidates []string
	logged     bool
}

func newLocalCandidateLog(sessionID, label string) *localCandidateLog {
	return &localCandidateLog{sessionID: sessionID, label: label}
}

// Observe takes every OnICECandidate callback, including the nil that ends
// gathering.
func (l *localCandidateLog) Observe(candidate *webrtc.ICECandidate) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logged {
		return
	}
	if candidate != nil {
		l.candidates = append(l.candidates, fmt.Sprintf("%s/%s/%s:%d",
			candidate.Typ.String(), candidate.Protocol.String(), candidate.Address, candidate.Port))
		return
	}
	l.logged = true
	fmt.Printf("[%s] ice_local_candidates pc=%s count=%d candidates=%s\n",
		l.sessionID, l.label, len(l.candidates), strings.Join(l.candidates, ","))
}
