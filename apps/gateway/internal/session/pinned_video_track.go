package session

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// VideoEgressTrack is a local WebRTC video track that accepts raw RTP bytes.
// Both *PinnedVideoTrack (production) and *webrtc.TrackLocalStaticRTP (tests)
// satisfy it.
type VideoEgressTrack interface {
	webrtc.TrackLocal
	Write(b []byte) (int, error)
}

type pinnedVideoBinding struct {
	id          string
	ssrc        webrtc.SSRC
	payloadType webrtc.PayloadType
	writeStream webrtc.TrackLocalWriter
}

var pinnedVideoPacketPool = sync.Pool{
	New: func() any {
		return &rtp.Packet{}
	},
}

// PinnedVideoTrack is a pion webrtc.TrackLocal for the primary camera stream
// that keeps wire RTP PayloadTypes aligned with mid-call SDP renegotiations.
//
// Pion's TrackLocalStaticRTP binds its PayloadType once at initial RTPSender.Send
// and never updates it afterwards. When a browser mid-call offer (e.g. adding
// a third m=video section for dual screen share) allocates new dynamic H.264
// payload types and drops the original PT from the primary video section,
// PreferWebRTCH264PacketizationMode answers with the new PT and calls
// SetPreferredCodec here so live bindings immediately stamp the new PT instead
// of continuing to emit the retired PT (which Chromium silently drops).
type PinnedVideoTrack struct {
	mu             sync.RWMutex
	id             string
	streamID       string
	codec          webrtc.RTPCodecCapability
	preferredCodec *webrtc.RTPCodecParameters
	bindings       []pinnedVideoBinding
}

func newPinnedVideoTrack(codec webrtc.RTPCodecCapability, id, streamID string) *PinnedVideoTrack {
	return &PinnedVideoTrack{
		id:       id,
		streamID: streamID,
		codec:    codec,
	}
}

func (t *PinnedVideoTrack) SetPreferredCodec(codec webrtc.RTPCodecParameters) {
	t.mu.Lock()
	c := codec
	t.preferredCodec = &c
	var changedOld webrtc.PayloadType
	changed := false
	for i := range t.bindings {
		if t.bindings[i].payloadType != c.PayloadType {
			changedOld = t.bindings[i].payloadType
			t.bindings[i].payloadType = c.PayloadType
			changed = true
		}
	}
	streamID := t.streamID
	t.mu.Unlock()
	if changed {
		fmt.Printf("[%s] primary_video_pt_updated old_pt=%d new_pt=%d\n", streamID, changedOld, c.PayloadType)
	}
}

func (t *PinnedVideoTrack) Bind(ctx webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var chosen webrtc.RTPCodecParameters
	found := false

	if t.preferredCodec != nil {
		for _, c := range ctx.CodecParameters() {
			if c.PayloadType == t.preferredCodec.PayloadType {
				chosen = c
				found = true
				break
			}
		}
		if !found {
			chosen = *t.preferredCodec
			found = true
		}
	}

	if !found {
		for _, c := range ctx.CodecParameters() {
			if strings.EqualFold(c.MimeType, t.codec.MimeType) && (t.codec.ClockRate == 0 || c.ClockRate == t.codec.ClockRate) {
				chosen = c
				found = true
				break
			}
		}
	}

	if !found {
		return webrtc.RTPCodecParameters{}, webrtc.ErrUnsupportedCodec
	}

	t.bindings = append(t.bindings, pinnedVideoBinding{
		id:          ctx.ID(),
		ssrc:        ctx.SSRC(),
		payloadType: chosen.PayloadType,
		writeStream: ctx.WriteStream(),
	})
	return chosen, nil
}

func (t *PinnedVideoTrack) Unbind(ctx webrtc.TrackLocalContext) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.bindings {
		if t.bindings[i].id == ctx.ID() {
			t.bindings[i] = t.bindings[len(t.bindings)-1]
			t.bindings = t.bindings[:len(t.bindings)-1]
			return nil
		}
	}
	return webrtc.ErrUnbindFailed
}

func (t *PinnedVideoTrack) ID() string              { return t.id }
func (t *PinnedVideoTrack) RID() string             { return "" }
func (t *PinnedVideoTrack) StreamID() string        { return t.streamID }
func (t *PinnedVideoTrack) Kind() webrtc.RTPCodecType { return webrtc.RTPCodecTypeVideo }
func (t *PinnedVideoTrack) Codec() webrtc.RTPCodecCapability {
	return t.codec
}

func (t *PinnedVideoTrack) WriteRTP(p *rtp.Packet) error {
	packet := pinnedVideoPacketPool.Get().(*rtp.Packet)
	defer func() {
		*packet = rtp.Packet{}
		pinnedVideoPacketPool.Put(packet)
	}()
	*packet = *p
	return t.writeRTP(packet)
}

func (t *PinnedVideoTrack) Write(b []byte) (int, error) {
	packet := pinnedVideoPacketPool.Get().(*rtp.Packet)
	defer func() {
		*packet = rtp.Packet{}
		pinnedVideoPacketPool.Put(packet)
	}()
	if err := packet.Unmarshal(b); err != nil {
		return 0, err
	}
	return len(b), t.writeRTP(packet)
}

func (t *PinnedVideoTrack) writeRTP(packet *rtp.Packet) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var writeErrs []error
	for _, binding := range t.bindings {
		header := packet.Header
		header.SSRC = uint32(binding.ssrc)
		header.PayloadType = uint8(binding.payloadType)
		if packet.PaddingSize != 0 && header.PaddingSize == 0 {
			header.PaddingSize = packet.PaddingSize
		}
		if _, err := binding.writeStream.WriteRTP(&header, packet.Payload); err != nil {
			writeErrs = append(writeErrs, err)
		}
	}
	return errors.Join(writeErrs...)
}
