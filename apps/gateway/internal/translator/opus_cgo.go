//go:build cgo

package translator

/*
#cgo LDFLAGS: -lopus
#include <opus.h>
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"time"
	"unsafe"
)

const (
	opusSampleRate  = 48000
	opusFrameSizeMs = 20
	opusChannels    = 1
	opusFrameSize   = opusSampleRate * opusFrameSizeMs / 1000
	maxPCMFrameSize = opusFrameSize * 2
)

type realOpusCodec struct {
	decoder *C.OpusDecoder
	encoder *C.OpusEncoder
	bitrate int
}

func NewOpusCodec(bitrate int) (OpusCodec, error) {
	var decErr C.int
	decoder := C.opus_decoder_create(C.int(opusSampleRate), C.int(opusChannels), &decErr)
	if decErr != C.OPUS_OK || decoder == nil {
		return nil, fmt.Errorf("opus decoder create failed: %d", int(decErr))
	}

	var encErr C.int
	encoder := C.opus_encoder_create(C.int(opusSampleRate), C.int(opusChannels), C.OPUS_APPLICATION_VOIP, &encErr)
	if encErr != C.OPUS_OK || encoder == nil {
		C.opus_decoder_destroy(decoder)
		return nil, fmt.Errorf("opus encoder create failed: %d", int(encErr))
	}

	if bitrate > 0 {
		val := C.int(bitrate)
		encRet := C.opus_encoder_ctl(encoder, C.OPUS_SET_BITRATE(val))
		if encRet != C.OPUS_OK {
			C.opus_decoder_destroy(decoder)
			C.opus_encoder_destroy(encoder)
			return nil, fmt.Errorf("opus set bitrate failed: %d", int(encRet))
		}
	}

	return &realOpusCodec{
		decoder: decoder,
		encoder: encoder,
		bitrate: bitrate,
	}, nil
}

func (c *realOpusCodec) Decode(opusData []byte) ([]int16, error) {
	if len(opusData) == 0 {
		return nil, fmt.Errorf("empty opus data")
	}

	pcm := make([]int16, maxPCMFrameSize)
	var pcmPtr *C.opus_int16
	if len(pcm) > 0 {
		pcmPtr = (*C.opus_int16)(unsafe.Pointer(&pcm[0]))
	}

	var dataPtr *C.uchar
	if len(opusData) > 0 {
		dataPtr = (*C.uchar)(unsafe.Pointer(&opusData[0]))
	}

	n := C.opus_decode(c.decoder, dataPtr, C.opus_int32(len(opusData)), pcmPtr, C.int(opusFrameSize), 0)
	if n < 0 {
		return nil, fmt.Errorf("opus decode error: %d", int(n))
	}
	return pcm[:n], nil
}

func (c *realOpusCodec) Encode(pcm []int16) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("empty pcm data")
	}

	out := make([]byte, 1500)
	var pcmPtr *C.opus_int16
	if len(pcm) > 0 {
		pcmPtr = (*C.opus_int16)(unsafe.Pointer(&pcm[0]))
	}

	n := C.opus_encode(c.encoder, pcmPtr, C.int(len(pcm)), (*C.uchar)(unsafe.Pointer(&out[0])), C.opus_int32(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("opus encode error: %d", int(n))
	}
	return out[:n], nil
}

func (c *realOpusCodec) FrameDuration() time.Duration {
	return opusFrameSizeMs * time.Millisecond
}

func (c *realOpusCodec) SampleRate() int {
	return opusSampleRate
}

func (c *realOpusCodec) Close() {
	if c.decoder != nil {
		C.opus_decoder_destroy(c.decoder)
		c.decoder = nil
	}
	if c.encoder != nil {
		C.opus_encoder_destroy(c.encoder)
		c.encoder = nil
	}
}
