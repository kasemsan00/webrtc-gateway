package session

import (
	"fmt"
	"time"

	"webrtc-sip-gateway/internal/config"
)

// UplinkPictureSizePLIRetry is the minimum gap between browser PLIs sent
// while a new picture size waits for its first IDR.
const UplinkPictureSizePLIRetry = time.Second

// acceptUplinkPictureSizeChange is false only for
// SIP_UPLINK_PICTURE_SIZE_CHANGE=reject, the old size lock.
func (s *Session) acceptUplinkPictureSizeChange() bool {
	return s.uplinkPictureSizeChange != config.SIPUplinkPictureSizeReject
}

// armUplinkPictureSizeChangeLocked takes a new SPS picture size in accept
// mode. Chrome lowers resolution under degradationPreference=balanced and
// sends the new SPS/PPS with an IDR. Until that IDR, nothing of the new size
// may reach the phone: a P-frame would reference a picture its decoder never
// had. Before the first uplink IDR the startup gate already holds P-frames,
// and the SPS/PPS riding in front of that IDR must keep flowing as they do
// today, so only the size is updated then. The caller holds s.mu.
func (s *Session) armUplinkPictureSizeChangeLocked(width, height int, now time.Time) {
	hold := s.uplinkVideoOpenLocked()
	fmt.Printf("[%s] uplink_sps_dimension_change accepted %dx%d was %dx%d hold_until_idr=%v\n",
		s.ID, width, height, s.uplinkSPSWidth, s.uplinkSPSHeight, hold)
	s.uplinkSPSWidth = width
	s.uplinkSPSHeight = height
	if !hold {
		return
	}
	s.uplinkPictureSizeIDRPending = true
	s.uplinkPictureSizeChangedAt = now
	s.uplinkPictureSizeHeld = 0
	s.uplinkPictureSizePLIs = 0
	s.uplinkPictureSizePLIAt = time.Time{}
}

// holdUplinkForPictureSizeLocked reports whether this packet must wait for
// the first IDR after an accepted size change. In-band SPS/PPS are held too:
// they are already cached, and the once-per-IDR injection puts exactly one
// pair of them in front of the IDR. sendPLI is true when a held packet is a
// picture rather than a parameter set, which means the browser changed size
// without an IDR; it repeats at most once per UplinkPictureSizePLIRetry.
// The caller holds s.mu and sends the PLI off the read loop.
func (s *Session) holdUplinkForPictureSizeLocked(isIDR, paramSetOnly bool, nalType uint8, now time.Time) (hold bool, sendPLI bool) {
	if !s.uplinkPictureSizeIDRPending {
		return false, false
	}
	if isIDR {
		var wait time.Duration
		if !s.uplinkPictureSizeChangedAt.IsZero() {
			wait = now.Sub(s.uplinkPictureSizeChangedAt)
		}
		fmt.Printf("[%s] uplink_picture_size_idr released %dx%d held=%d plis=%d wait_ms=%d\n",
			s.ID, s.uplinkSPSWidth, s.uplinkSPSHeight, s.uplinkPictureSizeHeld, s.uplinkPictureSizePLIs, wait.Milliseconds())
		s.uplinkPictureSizeIDRPending = false
		s.uplinkPictureSizeHeld = 0
		s.uplinkPictureSizePLIAt = time.Time{}
		if s.uplinkStallReason == "picture-size-idr" {
			s.uplinkStallSince = time.Time{}
			s.uplinkStallReason = ""
			s.uplinkStallLogAt = time.Time{}
		}
		return false, false
	}
	s.uplinkPictureSizeHeld++
	if held := s.uplinkPictureSizeHeld; held <= 5 || held%100 == 0 {
		fmt.Printf("[%s] uplink_picture_size_hold held=%d nal=%d\n", s.ID, held, nalType)
	}
	if paramSetOnly {
		return true, false
	}
	if s.uplinkStallReason == "" {
		s.uplinkStallSince = s.uplinkPictureSizeChangedAt
		s.uplinkStallReason = "picture-size-idr"
	}
	if !s.uplinkPictureSizePLIAt.IsZero() && now.Sub(s.uplinkPictureSizePLIAt) < UplinkPictureSizePLIRetry {
		return true, false
	}
	s.uplinkPictureSizePLIAt = now
	s.uplinkPictureSizePLIs++
	s.lastBrowserPLIReason = "picture_size_change"
	return true, true
}
