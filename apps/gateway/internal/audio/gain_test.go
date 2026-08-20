package audio

import "testing"

func TestApplyGainUnity(t *testing.T) {
	pcm := []int16{1000, -2000, 32767, -32768}
	want := []int16{1000, -2000, 32767, -32768}
	ApplyGain(pcm, 1.0)
	for i := range want {
		if pcm[i] != want[i] {
			t.Fatalf("sample %d = %d, want %d", i, pcm[i], want[i])
		}
	}
}

func TestApplyGainAndClip(t *testing.T) {
	pcm := []int16{20000, -20000, 1000}
	ApplyGain(pcm, 2.0)
	if pcm[0] != 32767 {
		t.Fatalf("positive clip = %d, want 32767", pcm[0])
	}
	if pcm[1] != -32768 {
		t.Fatalf("negative clip = %d, want -32768", pcm[1])
	}
	if pcm[2] != 2000 {
		t.Fatalf("unchanged sample = %d, want 2000", pcm[2])
	}
}

func TestApplyGainHalf(t *testing.T) {
	pcm := []int16{1000, -1000}
	ApplyGain(pcm, 0.5)
	if pcm[0] != 500 || pcm[1] != -500 {
		t.Fatalf("halved samples = %v, want [500 -500]", pcm)
	}
}
