package audio

// ApplyGain multiplies each PCM sample by gain and clips to int16 range.
func ApplyGain(pcm []int16, gain float32) {
	if gain == 1.0 {
		return
	}
	for i, sample := range pcm {
		v := float32(sample) * gain
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		pcm[i] = int16(v)
	}
}
