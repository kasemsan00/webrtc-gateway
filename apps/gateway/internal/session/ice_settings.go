package session

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

// gatewaySettingEngine applies the ICE gathering limits from config. Docker
// and other virtual bridges otherwise add a dozen unreachable host candidates
// that the browser checks before the real address, which delays ICE by
// seconds.
func gatewaySettingEngine(ice config.WebRTCICEConfig) (webrtc.SettingEngine, error) {
	var se webrtc.SettingEngine

	if len(ice.InterfaceExclude) > 0 {
		se.SetInterfaceFilter(interfaceExcludeFilter(ice.InterfaceExclude))
	}

	if len(ice.IPAllowlist) > 0 {
		filter, err := ipAllowlistFilter(ice.IPAllowlist)
		if err != nil {
			return se, err
		}
		se.SetIPFilter(filter)
	}

	if len(ice.NAT1To1IPs) > 0 {
		if err := se.SetICEAddressRewriteRules(nat1To1HostRewriteRules(ice.NAT1To1IPs)...); err != nil {
			return se, fmt.Errorf("ICE NAT1To1 rules: %w", err)
		}
	}

	if len(ice.NetworkTypes) > 0 {
		types := make([]webrtc.NetworkType, 0, len(ice.NetworkTypes))
		for _, raw := range ice.NetworkTypes {
			networkType, err := webrtc.NewNetworkType(raw)
			if err != nil {
				return se, err
			}
			types = append(types, networkType)
		}
		se.SetNetworkTypes(types)
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
