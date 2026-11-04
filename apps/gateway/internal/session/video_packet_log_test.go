package session

import "testing"

func TestWebRTCVideoPacketDebugLineDefaultOff(t *testing.T) {
	payload := []byte{0x1C, 0x85} // FU-A, NRI=0
	if got := webRTCVideoPacketDebugLine(false, "sess", 1, payload); got != "" {
		t.Fatalf("default debug off emitted %q", got)
	}
	if got := webRTCVideoPacketDebugLine(true, "sess", 101, payload); got != "" {
		t.Fatalf("packet past the first 100 emitted %q", got)
	}
	if got := webRTCVideoPacketDebugLine(true, "sess", 1, nil); got != "" {
		t.Fatalf("empty payload emitted %q", got)
	}
}

func TestWebRTCVideoPacketDebugLineFormatsNAL(t *testing.T) {
	// F=0, NRI=2, type=28 (FU-A) => 0x5C
	payload := []byte{0x5C, 0x85, 0x00}
	got := webRTCVideoPacketDebugLine(true, "sess-1", 3, payload)
	want := "[sess-1] 🔍 WebRTC Video Packet #3: NAL Type=28 (FU-A), NRI=2, Size=3 bytes\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	idr := webRTCVideoPacketDebugLine(true, "sess-1", 1, []byte{0x65})
	wantIDR := "[sess-1] 🔍 WebRTC Video Packet #1: NAL Type=5 (IDR (Keyframe)), NRI=3, Size=1 bytes\n"
	if idr != wantIDR {
		t.Fatalf("got %q want %q", idr, wantIDR)
	}
}
