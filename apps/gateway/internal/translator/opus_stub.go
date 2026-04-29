//go:build !cgo

package translator

import (
	"fmt"
	"time"
)

type stubOpusCodec struct {
	bitrate int
}

func NewOpusCodec(bitrate int) (OpusCodec, error) {
	return &stubOpusCodec{bitrate: bitrate}, nil
}

func (c *stubOpusCodec) Decode(opusData []byte) ([]int16, error) {
	if len(opusData) == 0 {
		return nil, fmt.Errorf("empty opus data")
	}
	return nil, fmt.Errorf("opus decode not available without cgo")
}

func (c *stubOpusCodec) Encode(pcm []int16) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("empty pcm data")
	}
	return nil, fmt.Errorf("opus encode not available without cgo")
}

func (c *stubOpusCodec) FrameDuration() time.Duration {
	return 20 * time.Millisecond
}

func (c *stubOpusCodec) SampleRate() int {
	return 48000
}

func (c *stubOpusCodec) Close() {
}
