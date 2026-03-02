package session

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// ExtractH264SpropParameterSets scans SDP text for H.264 "sprop-parameter-sets"
// and returns decoded SPS/PPS NAL units.
//
// Notes:
// - Prefer values found in the "m=video" section, but fall back to the first occurrence.
// - The value format is typically: sprop-parameter-sets=<base64SPS>,<base64PPS>[,...]
func ExtractH264SpropParameterSets(sdp string) (sps []byte, pps []byte, ok bool) {
	const key = "sprop-parameter-sets="

	if sdp == "" {
		return nil, nil, false
	}

	lines := strings.Split(sdp, "\n")

	var firstMatch string
	var videoMatch string

	inVideo := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "m=") {
			inVideo = strings.HasPrefix(line, "m=video ")
		}

		idx := strings.Index(line, key)
		if idx < 0 {
			continue
		}

		val := line[idx+len(key):]
		// Trim at first delimiter after the value.
		for i := 0; i < len(val); i++ {
			switch val[i] {
			case ';', ' ', '\t', '\r':
				val = val[:i]
				i = len(val) // break
			}
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}

		if firstMatch == "" {
			firstMatch = val
		}
		if inVideo && videoMatch == "" {
			videoMatch = val
		}
	}

	match := videoMatch
	if match == "" {
		match = firstMatch
	}
	if match == "" {
		return nil, nil, false
	}

	parts := strings.Split(match, ",")
	if len(parts) < 2 {
		return nil, nil, false
	}

	b64sps := strings.TrimSpace(parts[0])
	b64pps := strings.TrimSpace(parts[1])
	if b64sps == "" || b64pps == "" {
		return nil, nil, false
	}

	decodedSPS, err := base64.StdEncoding.DecodeString(b64sps)
	if err != nil || len(decodedSPS) == 0 {
		return nil, nil, false
	}
	decodedPPS, err := base64.StdEncoding.DecodeString(b64pps)
	if err != nil || len(decodedPPS) == 0 {
		return nil, nil, false
	}

	return decodedSPS, decodedPPS, true
}

// SetCachedSPSPPS stores decoded SPS/PPS NAL units in the session cache.
// This is intended for caching from Offer/SDP as a fallback before RTP starts flowing.
func (s *Session) SetCachedSPSPPS(sps, pps []byte, source string) {
	if len(sps) == 0 || len(pps) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.CachedSPS = make([]byte, len(sps))
	s.CachedPPS = make([]byte, len(pps))
	copy(s.CachedSPS, sps)
	copy(s.CachedPPS, pps)
	s.UpdatedAt = time.Now()

	src := source
	if src == "" {
		src = "unknown"
	}
	fmt.Printf("[Session %s] 💾 Cached SPS/PPS from SDP (%s) (SPS=%d bytes, PPS=%d bytes)\n", s.ID, src, len(sps), len(pps))
}
