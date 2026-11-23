package session

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	// DefaultOfferICEGatherTimeout bounds initial offer->answer ICE gathering
	// when the caller does not specify an explicit positive timeout.
	DefaultOfferICEGatherTimeout = 1500 * time.Millisecond
	// offerICEGatherPollInterval polls LocalDescription().SDP alongside
	// GatheringCompletePromise without overwriting pc.OnICECandidate.
	offerICEGatherPollInterval = 25 * time.Millisecond
	// offerICENAT1To1TURNGatherGrace gives TURN a brief window to land a relay
	// candidate even when 1:1 NAT already provides a directly reachable host IP.
	offerICENAT1To1TURNGatherGrace = 150 * time.Millisecond
	// offerICEDefaultGatherGrace gives secondary candidates (e.g. srflx) a
	// brief window after the first usable candidate when neither 1:1 NAT nor
	// TURN is configured.
	offerICEDefaultGatherGrace = 150 * time.Millisecond
)

type offerICECandidateSummary struct {
	hasNonLoopbackHost bool
	hasSrflx           bool
	hasRelay           bool
	usableCount        int
}

func summarizeOfferICECandidates(sdp string) offerICECandidateSummary {
	var summary offerICECandidateSummary
	if sdp == "" {
		return summary
	}
	for _, rawLine := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "a=candidate:"))
		if len(fields) < 8 {
			continue
		}
		candType := ""
		for i := 6; i < len(fields)-1; i++ {
			if fields[i] == "typ" {
				candType = fields[i+1]
				break
			}
		}
		ipStr := fields[4]
		ip := net.ParseIP(ipStr)
		switch candType {
		case "host":
			if ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
				continue
			}
			summary.hasNonLoopbackHost = true
			summary.usableCount++
		case "srflx":
			summary.hasSrflx = true
			summary.usableCount++
		case "relay":
			summary.hasRelay = true
			summary.usableCount++
		}
	}
	return summary
}

// WaitForOfferICEGatheringComplete bounds initial WebRTC offer->answer ICE
// gathering using a deployment-aware stopping rule without overwriting
// pc.OnICECandidate:
//
//   - timeout <= 0 defaults to DefaultOfferICEGatherTimeout (1500ms).
//   - hasNAT1To1 && !hasTURN: returns immediately once a non-loopback host,
//     srflx, or relay candidate is present in LocalDescription.
//   - hasNAT1To1 && hasTURN: returns immediately on relay, or waits up to a
//     short 150ms grace after the first non-loopback host/srflx candidate.
//   - !hasNAT1To1 && hasTURN: waits for a typ relay candidate (or gathering
//     completion) up to timeout so CGNAT/TURN-only mobile clients never lose
//     relay candidates.
//   - !hasNAT1To1 && !hasTURN: waits for gathering completion or at least one
//     usable candidate plus a short 150ms grace up to timeout.
//
// Returns true if gathering completed or the deployment stopping rule was
// satisfied before timeout/ctx cancellation, and false on timeout/cancellation.
func WaitForOfferICEGatheringComplete(
	ctx context.Context,
	pc *webrtc.PeerConnection,
	hasNAT1To1 bool,
	hasTURN bool,
	timeout time.Duration,
) bool {
	if pc == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	readLocalSDP := func() string {
		if ld := pc.LocalDescription(); ld != nil {
			return ld.SDP
		}
		return ""
	}
	return waitForOfferICEGathering(
		ctx,
		gatherDone,
		readLocalSDP,
		hasNAT1To1,
		hasTURN,
		timeout,
		offerICEGatherPollInterval,
		offerICENAT1To1TURNGatherGrace,
		offerICEDefaultGatherGrace,
	)
}

func waitForOfferICEGathering(
	ctx context.Context,
	gatherDone <-chan struct{},
	readLocalSDP func() string,
	hasNAT1To1 bool,
	hasTURN bool,
	timeout time.Duration,
	pollInterval time.Duration,
	nat1To1TurnGrace time.Duration,
	defaultGrace time.Duration,
) bool {
	if readLocalSDP == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = DefaultOfferICEGatherTimeout
	}
	if pollInterval <= 0 {
		pollInterval = offerICEGatherPollInterval
	}

	var graceTimer *time.Timer
	var graceCh <-chan time.Time
	armGrace := func(d time.Duration) bool {
		if d <= 0 {
			return true
		}
		if graceTimer == nil {
			graceTimer = time.NewTimer(d)
			graceCh = graceTimer.C
		}
		return false
	}
	defer func() {
		if graceTimer != nil {
			graceTimer.Stop()
		}
	}()

	evaluateSDP := func() bool {
		summary := summarizeOfferICECandidates(readLocalSDP())
		switch {
		case hasNAT1To1:
			if summary.hasRelay {
				return true
			}
			if summary.hasNonLoopbackHost || summary.hasSrflx {
				if !hasTURN {
					return true
				}
				return armGrace(nat1To1TurnGrace)
			}
			return false
		case hasTURN:
			return summary.hasRelay
		default:
			if summary.usableCount >= 1 {
				return armGrace(defaultGrace)
			}
			return false
		}
	}

	// Fast-path check before allocating timers.
	select {
	case <-gatherDone:
		return true
	case <-ctx.Done():
		return false
	default:
	}
	if evaluateSDP() {
		return true
	}

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gatherDone:
			return true
		case <-ctx.Done():
			return false
		case <-graceCh:
			return true
		case <-ticker.C:
			if evaluateSDP() {
				return true
			}
		case <-timeoutTimer.C:
			return false
		}
	}
}
