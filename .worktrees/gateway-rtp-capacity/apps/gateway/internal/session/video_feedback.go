package session

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/pion/rtcp"

	"k2-gateway/internal/config"
)

const (
	browserNACKDedupeWindow = 120 * time.Millisecond
	nackHandledLogThrottle  = 1 * time.Second
)

type videoFeedbackTarget struct {
	Addr      *net.UDPAddr
	Kind      string // "rtp" | "rtcp"
	Label     string
	IsPrimary bool
}

func (s *Session) GetVideoFeedbackTransport() string {
	s.mu.RLock()
	mode := strings.ToLower(strings.TrimSpace(s.VideoFeedbackTransport))
	s.mu.RUnlock()

	switch mode {
	case config.SIPVideoFeedbackTransportRTP, config.SIPVideoFeedbackTransportRTCP, config.SIPVideoFeedbackTransportDual:
		return mode
	default:
		return config.SIPVideoFeedbackTransportAuto
	}
}

func buildNACKSignature(nacks []rtcp.NackPair) string {
	if len(nacks) == 0 {
		return ""
	}

	clone := make([]rtcp.NackPair, len(nacks))
	copy(clone, nacks)
	sort.Slice(clone, func(i, j int) bool {
		if clone[i].PacketID == clone[j].PacketID {
			return clone[i].LostPackets < clone[j].LostPackets
		}
		return clone[i].PacketID < clone[j].PacketID
	})

	var b strings.Builder
	b.Grow(len(clone) * 12)
	for _, pair := range clone {
		b.WriteString(fmt.Sprintf("%d:%d;", pair.PacketID, pair.LostPackets))
	}
	return b.String()
}

func (s *Session) shouldSuppressBrowserNACK(signature string, now time.Time) bool {
	if signature == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if signature == s.LastBrowserNACKSig && !s.LastBrowserNACKAt.IsZero() && now.Sub(s.LastBrowserNACKAt) < browserNACKDedupeWindow {
		return true
	}

	s.LastBrowserNACKSig = signature
	s.LastBrowserNACKAt = now
	return false
}

func (s *Session) shouldSuppressNACKHandledLog(signature string, retransmit, missing int, now time.Time) bool {
	if retransmit != 0 || missing <= 0 {
		return false
	}

	key := fmt.Sprintf("%s|%d|%d", signature, retransmit, missing)

	s.mu.Lock()
	defer s.mu.Unlock()

	if key == s.LastNACKHandledLogSig && !s.LastNACKHandledLogAt.IsZero() && now.Sub(s.LastNACKHandledLogAt) < nackHandledLogThrottle {
		return true
	}

	s.LastNACKHandledLogSig = key
	s.LastNACKHandledLogAt = now
	return false
}

func (s *Session) getVideoFeedbackTargets(destAddr, learnedAddr *net.UDPAddr, useFallback bool) []videoFeedbackTarget {
	if destAddr == nil || destAddr.IP == nil {
		return nil
	}

	rtpAddr := &net.UDPAddr{IP: destAddr.IP, Port: destAddr.Port}
	rtcpAddr := &net.UDPAddr{IP: destAddr.IP, Port: destAddr.Port + 1}

	primaryRTCP := learnedAddr
	primaryLabel := "learned RTCP addr"
	if primaryRTCP == nil {
		primaryRTCP = rtcpAddr
		primaryLabel = "RTCP port"
	}

	targets := make([]videoFeedbackTarget, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(target videoFeedbackTarget) {
		if target.Addr == nil {
			return
		}
		key := target.Addr.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, target)
	}

	switch s.GetVideoFeedbackTransport() {
	case config.SIPVideoFeedbackTransportRTP:
		add(videoFeedbackTarget{Addr: rtpAddr, Kind: "rtp", Label: "RTP port", IsPrimary: true})
	case config.SIPVideoFeedbackTransportRTCP:
		add(videoFeedbackTarget{Addr: primaryRTCP, Kind: "rtcp", Label: primaryLabel, IsPrimary: true})
	case config.SIPVideoFeedbackTransportDual:
		// RTP first for chan_sip interoperability.
		add(videoFeedbackTarget{Addr: rtpAddr, Kind: "rtp", Label: "RTP port", IsPrimary: true})
		add(videoFeedbackTarget{Addr: primaryRTCP, Kind: "rtcp", Label: primaryLabel, IsPrimary: false})
	default:
		add(videoFeedbackTarget{Addr: primaryRTCP, Kind: "rtcp", Label: primaryLabel, IsPrimary: true})
		if useFallback {
			add(videoFeedbackTarget{Addr: rtcpAddr, Kind: "rtcp", Label: "RTCP port", IsPrimary: false})
			add(videoFeedbackTarget{Addr: rtpAddr, Kind: "rtp", Label: "RTP port", IsPrimary: false})
		}
	}

	return targets
}
