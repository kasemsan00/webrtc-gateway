package session

import (
	"net"
	"testing"
)

func TestAuxVideoLearnSymmetricRTPAndRTCP(t *testing.T) {
	sess := &Session{ID: "aux-sym"}
	aux, err := sess.OpenAuxVideo(nil, nil, 12004, 12005)
	if err != nil {
		t.Fatalf("OpenAuxVideo: %v", err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("done") })

	aux.SetRemoteEndpoints(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20006},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20007},
		false,
	)

	// Different IP must never hijack the auxiliary destination.
	aux.LearnSymmetricRTP(&net.UDPAddr{IP: net.ParseIP("198.51.100.9"), Port: 40000})
	if got := aux.RemoteRTPAddr(); got.String() != "10.0.0.5:20006" {
		t.Fatalf("different IP changed RemoteRTPAddr to %s", got)
	}

	// Same IP from NAT/rtpengine on a new source port updates RTP (and consecutive RTCP).
	aux.LearnSymmetricRTP(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 31006})
	if got := aux.RemoteRTPAddr(); got.String() != "10.0.0.5:31006" {
		t.Fatalf("RemoteRTPAddr = %s, want 10.0.0.5:31006", got)
	}
	if got := aux.RemoteRTCPAddr(); got.String() != "10.0.0.5:31007" {
		t.Fatalf("RemoteRTCPAddr = %s, want 10.0.0.5:31007", got)
	}

	aux.LearnSymmetricRTCP(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 31099})
	if got := aux.RemoteRTCPAddr(); got.String() != "10.0.0.5:31099" {
		t.Fatalf("RemoteRTCPAddr = %s, want 10.0.0.5:31099", got)
	}
}
