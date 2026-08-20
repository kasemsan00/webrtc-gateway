package audio

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

type mockOpusCodec struct {
	decodePCM []int16
	encodeOut []byte
}

func (m *mockOpusCodec) Decode([]byte) ([]int16, error) {
	out := make([]int16, len(m.decodePCM))
	copy(out, m.decodePCM)
	return out, nil
}

func (m *mockOpusCodec) Encode(pcm []int16) ([]byte, error) {
	if len(m.encodeOut) > 0 {
		out := make([]byte, len(m.encodeOut))
		copy(out, m.encodeOut)
		return out, nil
	}
	return []byte{0x01, 0x02}, nil
}

func (m *mockOpusCodec) FrameDuration() time.Duration { return 20 * time.Millisecond }
func (m *mockOpusCodec) SampleRate() int              { return 48000 }
func (m *mockOpusCodec) Close()                       {}

func TestInboundGainProcessorProcess(t *testing.T) {
	codec := &mockOpusCodec{decodePCM: []int16{1000, -1000}}
	proc := NewInboundGainProcessor(codec, 2.0)

	in := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    107,
			SequenceNumber: 42,
			Timestamp:      960,
			SSRC:           1234,
			Marker:         true,
		},
		Payload: []byte{0xAB},
	}

	out, err := proc.Process(in)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if out.Header.PayloadType != 107 {
		t.Fatalf("payload type = %d, want 107", out.Header.PayloadType)
	}
	if out.Header.SequenceNumber != 42 {
		t.Fatalf("sequence = %d, want 42", out.Header.SequenceNumber)
	}
	if len(out.Payload) == 0 {
		t.Fatal("expected encoded payload")
	}
}

func TestInboundGainProcessorEmptyPacket(t *testing.T) {
	proc := NewInboundGainProcessor(&mockOpusCodec{}, 1.5)
	if _, err := proc.Process(&rtp.Packet{}); err == nil {
		t.Fatal("expected error for empty packet")
	}
}
