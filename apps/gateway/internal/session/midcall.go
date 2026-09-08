package session

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MidCallRenegotiationRequest describes a pending in-dialog SIP/WebRTC
// negotiation. It is intentionally metadata-only; network I/O lives in SIP/API.
type MidCallRenegotiationRequest struct {
	ID        string
	Source    string
	Method    string
	OfferSDP  string
	AnswerSDP string
	Reason    string
	StartedAt time.Time
	Timeout   time.Duration
}

// MidCallRenegotiationSnapshot is a thread-safe copy of pending negotiation state.
type MidCallRenegotiationSnapshot struct {
	ID         string
	Source     string
	Method     string
	OfferSDP   string
	AnswerSDP  string
	Reason     string
	StartedAt  time.Time
	Timeout    time.Duration
	Status     string
	StatusCode int
}

type midCallRenegotiationState struct {
	MidCallRenegotiationSnapshot
}

// MidCallMedia describes one parsed SDP m-line relevant to mid-call policy.
type MidCallMedia struct {
	Present   bool
	Port      int
	Direction string
	Codecs    map[int]string
}

// MidCallSDPValidation is the policy decision for a SIP mid-call SDP offer.
type MidCallSDPValidation struct {
	Accept                      bool
	StatusCode                  int
	Reason                      string
	Audio                       MidCallMedia
	Video                       MidCallMedia
	HasOpus                     bool
	HasH264                     bool
	HasActiveVideo              bool
	RequiresClientRenegotiation bool
}

// MidCallMediaState is the session-visible result of the last accepted mid-call SDP.
type MidCallMediaState struct {
	AudioDirection string
	VideoDirection string
	HasActiveVideo bool
}

// TryBeginMidCallRenegotiation starts a single pending mid-call operation.
func (s *Session) TryBeginMidCallRenegotiation(req MidCallRenegotiationRequest) (MidCallRenegotiationSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.TerminalAction != "" || s.PendingMidCallRenegotiation != nil {
		return MidCallRenegotiationSnapshot{}, false
	}

	startedAt := req.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("reneg-%d", startedAt.UnixNano())
	}

	snap := MidCallRenegotiationSnapshot{
		ID:        id,
		Source:    strings.TrimSpace(req.Source),
		Method:    strings.ToUpper(strings.TrimSpace(req.Method)),
		OfferSDP:  req.OfferSDP,
		AnswerSDP: req.AnswerSDP,
		Reason:    strings.TrimSpace(req.Reason),
		StartedAt: startedAt,
		Timeout:   req.Timeout,
		Status:    "pending",
	}
	s.PendingMidCallRenegotiation = &midCallRenegotiationState{MidCallRenegotiationSnapshot: snap}
	s.UpdatedAt = startedAt
	return snap, true
}

// GetPendingMidCallRenegotiation returns a copy of pending negotiation state.
func (s *Session) GetPendingMidCallRenegotiation() (MidCallRenegotiationSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.PendingMidCallRenegotiation == nil {
		return MidCallRenegotiationSnapshot{}, false
	}
	return s.PendingMidCallRenegotiation.MidCallRenegotiationSnapshot, true
}

// CompleteMidCallRenegotiation clears a matching pending negotiation as successful.
func (s *Session) CompleteMidCallRenegotiation(id, answerSDP string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingMidCallRenegotiation == nil || s.PendingMidCallRenegotiation.ID != id {
		return false
	}
	clearSwitchVideoRenegotiateHoldLocked(s, s.PendingMidCallRenegotiation.Source)
	s.PendingMidCallRenegotiation.AnswerSDP = answerSDP
	s.PendingMidCallRenegotiation.Status = "completed"
	s.PendingMidCallRenegotiation = nil
	s.UpdatedAt = time.Now()
	return true
}

// FailMidCallRenegotiation clears a matching pending negotiation as failed.
func (s *Session) FailMidCallRenegotiation(id string, statusCode int, reason string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PendingMidCallRenegotiation == nil || s.PendingMidCallRenegotiation.ID != id {
		return false
	}
	clearSwitchVideoRenegotiateHoldLocked(s, s.PendingMidCallRenegotiation.Source)
	s.PendingMidCallRenegotiation.Status = "failed"
	s.PendingMidCallRenegotiation.StatusCode = statusCode
	s.PendingMidCallRenegotiation.Reason = reason
	s.PendingMidCallRenegotiation = nil
	s.UpdatedAt = time.Now()
	return true
}

// ApplyMidCallSDPValidation stores accepted mid-call media state on the session.
func (s *Session) ApplyMidCallSDPValidation(validation MidCallSDPValidation) {
	if !validation.Accept {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RemoteAudioDirection = validation.Audio.Direction
	s.RemoteVideoDirection = validation.Video.Direction
	s.MidCallHasActiveVideo = validation.HasActiveVideo
	s.UpdatedAt = time.Now()
}

// GetMidCallMediaState returns the last accepted mid-call media state.
func (s *Session) GetMidCallMediaState() MidCallMediaState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return MidCallMediaState{
		AudioDirection: s.RemoteAudioDirection,
		VideoDirection: s.RemoteVideoDirection,
		HasActiveVideo: s.MidCallHasActiveVideo,
	}
}

// ValidateMidCallSDP validates a SIP mid-call SDP offer against gateway media policy.
func ValidateMidCallSDP(sdp string) MidCallSDPValidation {
	if strings.TrimSpace(sdp) == "" {
		return rejectMidCallSDP(488, "missing_sdp")
	}

	result := MidCallSDPValidation{
		Accept:     true,
		StatusCode: 200,
		Audio:      MidCallMedia{Direction: "sendrecv", Port: -1, Codecs: map[int]string{}},
		Video:      MidCallMedia{Direction: "sendrecv", Port: -1, Codecs: map[int]string{}},
	}

	var current *MidCallMedia
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "m=") {
			media, ok := parseMidCallMediaLine(line)
			if !ok {
				return rejectMidCallSDP(488, "malformed_sdp")
			}
			switch media.kind {
			case "audio":
				result.Audio.Present = true
				result.Audio.Port = media.port
				result.Audio.Codecs = media.payloads
				current = &result.Audio
			case "video":
				result.Video.Present = true
				result.Video.Port = media.port
				result.Video.Codecs = media.payloads
				current = &result.Video
			default:
				current = nil
			}
			continue
		}

		if current == nil {
			continue
		}
		switch line {
		case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
			current.Direction = strings.TrimPrefix(line, "a=")
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "a=rtpmap:") {
			pt, codec, ok := parseRTPMap(line)
			if ok {
				current.Codecs[pt] = codec
			}
		}
	}

	result.HasOpus = mediaHasCodec(result.Audio, "opus")
	if !result.Audio.Present || result.Audio.Port <= 0 || !result.HasOpus {
		return rejectMidCallSDP(488, "unsupported_audio_codec")
	}

	result.HasH264 = mediaHasCodec(result.Video, "h264")
	result.HasActiveVideo = result.Video.Present && result.Video.Port > 0 && result.Video.Direction != "inactive"
	if result.HasActiveVideo && !result.HasH264 {
		return rejectMidCallSDP(488, "unsupported_video_codec")
	}
	if result.Video.Present && (result.Video.Port == 0 || result.Video.Direction == "inactive") {
		result.RequiresClientRenegotiation = true
	}

	return result
}

type parsedMidCallMediaLine struct {
	kind     string
	port     int
	payloads map[int]string
}

func parseMidCallMediaLine(line string) (parsedMidCallMediaLine, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return parsedMidCallMediaLine{}, false
	}
	kind := strings.TrimPrefix(fields[0], "m=")
	port, err := strconv.Atoi(fields[1])
	if err != nil {
		return parsedMidCallMediaLine{}, false
	}
	payloads := map[int]string{}
	for _, rawPT := range fields[3:] {
		pt, err := strconv.Atoi(rawPT)
		if err == nil {
			payloads[pt] = ""
		}
	}
	return parsedMidCallMediaLine{kind: kind, port: port, payloads: payloads}, true
}

func parseRTPMap(line string) (int, string, bool) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0, "", false
	}
	ptText := strings.TrimPrefix(parts[0], "a=rtpmap:")
	pt, err := strconv.Atoi(ptText)
	if err != nil {
		return 0, "", false
	}
	codec := strings.ToLower(strings.Split(parts[1], "/")[0])
	return pt, codec, true
}

func mediaHasCodec(media MidCallMedia, codec string) bool {
	codec = strings.ToLower(codec)
	for _, name := range media.Codecs {
		if strings.EqualFold(name, codec) {
			return true
		}
	}
	return false
}

func rejectMidCallSDP(statusCode int, reason string) MidCallSDPValidation {
	return MidCallSDPValidation{Accept: false, StatusCode: statusCode, Reason: reason}
}
