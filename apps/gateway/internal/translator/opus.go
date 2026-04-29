package translator

import "time"

type OpusCodec interface {
	Decode(opusData []byte) ([]int16, error)
	Encode(pcm []int16) ([]byte, error)
	FrameDuration() time.Duration
	SampleRate() int
	Close()
}
