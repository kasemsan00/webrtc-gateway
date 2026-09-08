package session

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestIceCallbackIsForCurrentPeerConnection(t *testing.T) {
	if iceCallbackIsForCurrentPeerConnection(nil, nil) {
		t.Fatal("nil current must ignore ICE callbacks")
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if iceCallbackIsForCurrentPeerConnection(nil, pc) {
		t.Fatal("nil current must ignore ICE from any PC")
	}
	if !iceCallbackIsForCurrentPeerConnection(pc, pc) {
		t.Fatal("current PC events must be applied")
	}

	other, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if iceCallbackIsForCurrentPeerConnection(pc, other) {
		t.Fatal("replaced PC events must be ignored")
	}
}

func TestAddRemoteICECandidateQueuesUntilRemoteDescription(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	sess := &Session{ID: "ice-queue", PeerConnection: pc, State: StateActive}
	mid := "0"
	queued, err := sess.AddRemoteICECandidate(webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 UDP 2122260223 192.0.2.1 9 typ host",
		SDPMid:    &mid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !queued {
		t.Fatal("expected ICE candidate to queue before remote description")
	}
	if got := len(sess.pendingRemoteICE); got != 1 {
		t.Fatalf("queued=%d want 1", got)
	}
}
