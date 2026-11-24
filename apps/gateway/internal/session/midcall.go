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
	// ExtraVideo lists every m=video after the first, in offer order. Audio,
	// Video and the fields derived from them describe the first m=audio and
	// the first m=video only.
	ExtraVideo                  []MidCallMedia
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
	if s.PendingMidCallRenegotiation == nil || s.PendingMidCallRenegotiation.ID != id {
		s.mu.Unlock()
		return false
	}
	clearSwitchVideoRenegotiateHoldLocked(s, s.PendingMidCallRenegotiation.Source)
	s.PendingMidCallRenegotiation.Status = "failed"
	s.PendingMidCallRenegotiation.StatusCode = statusCode
	s.PendingMidCallRenegotiation.Reason = reason
	s.PendingMidCallRenegotiation = nil
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
	s.AbortMakeBeforeBreak()
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
	s.midCallMediaSeeded = true
	s.UpdatedAt = time.Now()
}

// SeedInitialSIPMediaState initializes mid-call media baseline from the
// remote peer's initial SDP offer or answer (INVITE, 183, or 200 OK) so the
// first mid-call re-INVITE does not misclassify an already-active camera
// video stream as newly added.
func (s *Session) SeedInitialSIPMediaState(remoteSDP string) {
	if s == nil || strings.TrimSpace(remoteSDP) == "" {
		return
	}
	audioDir := parsePrimaryAudioDirection(remoteSDP)
	videoDiag := AnalyzeOfferVideo(remoteSDP)
	videoDir := videoDiag.VideoDirection
	if !videoDiag.HasVideoMLine {
		videoDir = ""
	} else if videoDir == "" || videoDir == "unspecified" {
		videoDir = "sendrecv"
	}
	hasActiveVideo := videoDiag.HasActiveVideo() && s.SIPOfferIncludeVideo()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.RemoteAudioDirection = audioDir
	s.RemoteVideoDirection = videoDir
	s.MidCallHasActiveVideo = hasActiveVideo
	s.midCallMediaSeeded = true
	s.UpdatedAt = time.Now()
}

func parsePrimaryAudioDirection(sdp string) string {
	var sessionDir, audioDir string
	audioPort := -1
	section := "session"
	audioSeen := false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "m=") {
			if strings.HasPrefix(line, "m=audio ") && !audioSeen {
				audioSeen = true
				section = "audio"
				if m, ok := parseMidCallMediaLine(line); ok {
					audioPort = m.port
				}
			} else {
				section = "other"
			}
			continue
		}
		if section == "other" {
			continue
		}
		switch line {
		case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
			dir := strings.TrimPrefix(line, "a=")
			if section == "audio" {
				audioDir = dir
			} else {
				sessionDir = dir
			}
		}
	}
	if !audioSeen {
		return ""
	}
	if audioDir != "" {
		return audioDir
	}
	if sessionDir != "" {
		return sessionDir
	}
	if audioPort > 0 {
		return "sendrecv"
	}
	return "inactive"
}

// GetMidCallMediaState returns the last accepted mid-call media state.
func (s *Session) GetMidCallMediaState() MidCallMediaState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hasActiveVideo := s.MidCallHasActiveVideo
	if !s.midCallMediaSeeded && s.AsteriskVideoAddr != nil && s.AsteriskVideoAddr.Port > 0 &&
		(s.sipOfferIncludeVideo == nil || *s.sipOfferIncludeVideo) {
		hasActiveVideo = true
	}
	return MidCallMediaState{
		AudioDirection: s.RemoteAudioDirection,
		VideoDirection: s.RemoteVideoDirection,
		HasActiveVideo: hasActiveVideo,
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
	var extraVideo []*MidCallMedia
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
			switch {
			case media.kind == "audio" && !result.Audio.Present:
				result.Audio.Present = true
				result.Audio.Port = media.port
				result.Audio.Codecs = media.payloads
				current = &result.Audio
			case media.kind == "video" && !result.Video.Present:
				result.Video.Present = true
				result.Video.Port = media.port
				result.Video.Codecs = media.payloads
				current = &result.Video
			case media.kind == "video":
				// A later m=video is a separate stream. It gets its own
				// record so it cannot change the primary video's state.
				extra := &MidCallMedia{Present: true, Port: media.port, Direction: "sendrecv", Codecs: media.payloads}
				extraVideo = append(extraVideo, extra)
				current = extra
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

	for _, extra := range extraVideo {
		result.ExtraVideo = append(result.ExtraVideo, *extra)
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
