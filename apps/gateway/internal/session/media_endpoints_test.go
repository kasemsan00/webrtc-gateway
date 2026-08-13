package session

import (
	"net"
	"testing"
	"time"
)

func newUDPConn(t *testing.T) (*net.UDPConn, int) {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	if err != nil {
		t.Fatalf("failed to open UDP listener: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	return conn, port
}

func assertUDPConnClosed(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	if conn == nil {
		t.Fatalf("nil conn")
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := conn.WriteToUDP([]byte{0x01}, addr); err == nil {
		t.Fatalf("expected closed UDP conn write to fail")
	}
}

func TestCloseMediaTransportsClearsAndCloses(t *testing.T) {
	audioRTP, audioRTPPort := newUDPConn(t)
	videoRTP, videoRTPPort := newUDPConn(t)
	audioRTCP, audioRTCPPort := newUDPConn(t)
	videoRTCP, videoRTCPPort := newUDPConn(t)

	sess := &Session{
		ID:            "test-close-media",
		RTPConn:       audioRTP,
		VideoRTPConn:  videoRTP,
		AudioRTCPConn: audioRTCP,
		VideoRTCPConn: videoRTCP,
		RTPPort:       audioRTPPort,
		VideoRTPPort:  videoRTPPort,
		AudioRTCPPort: audioRTCPPort,
		VideoRTCPPort: videoRTCPPort,
	}
	sess.MarkMediaForwardReady()

	sess.CloseMediaTransports()

	status := sess.GetMediaEndpointStatus()
	if status.AudioRTPReady || status.VideoRTPReady || status.AudioRTCPReady || status.VideoRTCPReady {
		t.Fatalf("expected all RTP/RTCP transports to be cleared, got %+v", status)
	}
	if sess.IsMediaForwardReady() {
		t.Fatal("expected media forward ready to be cleared when RTP sockets close")
	}
	if status.AudioRTPPort != 0 || status.VideoRTPPort != 0 || status.AudioRTCPPort != 0 || status.VideoRTCPPort != 0 {
		t.Fatalf("expected media ports to be reset, got %+v", status)
	}

	assertUDPConnClosed(t, audioRTP)
	assertUDPConnClosed(t, videoRTP)
	assertUDPConnClosed(t, audioRTCP)
	assertUDPConnClosed(t, videoRTCP)
}

func TestResetMediaStateClearsSIPEndpointsKeepsCachedSPSPPS(t *testing.T) {
	cachedSPS := []byte{0x67, 0x42, 0x00, 0x1f}
	cachedPPS := []byte{0x68, 0xce, 0x06, 0xe2}

	sess := &Session{
		ID: "test-reset-media",
		AsteriskAudioAddr: &net.UDPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 4000,
		},
		AsteriskVideoAddr: &net.UDPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 4002,
		},
		CachedSPS:                          cachedSPS,
		CachedPPS:                          cachedPPS,
		AudioSeq:                           111,
		VideoSeq:                           222,
		remoteAudioReadyNotified:           true,
		remoteVideoReadyNotified:           true,
		uplinkKeyframeKickOnRemoteJoinDone: true,
		uplinkKeyframeKickOnFirstSIPRTCP:   true,
		sipVideoDestReadyAt:                time.Now(),
	}

	sess.ResetMediaState()

	if sess.AsteriskAudioAddr != nil {
		t.Fatalf("expected AsteriskAudioAddr to be cleared")
	}
	if sess.AsteriskVideoAddr != nil {
		t.Fatalf("expected AsteriskVideoAddr to be cleared")
	}
	if len(sess.CachedSPS) == 0 || len(sess.CachedPPS) == 0 {
		t.Fatalf("expected cached SPS/PPS to be preserved")
	}
	if string(sess.CachedSPS) != string(cachedSPS) {
		t.Fatalf("cached SPS changed unexpectedly")
	}
	if string(sess.CachedPPS) != string(cachedPPS) {
		t.Fatalf("cached PPS changed unexpectedly")
	}
	if !sess.TryMarkRemoteAudioReady() {
		t.Fatalf("expected remote-audio ready notify to be re-armed after reset")
	}
	if !sess.TryMarkRemoteVideoReady(true) {
		t.Fatalf("expected remote-video ready notify to be re-armed after reset")
	}
	if !sess.TryClaimUplinkKeyframeKickOnRemoteJoin() {
		t.Fatalf("expected uplink keyframe kick to be re-armed after reset")
	}
	if !sess.TryClaimUplinkKeyframeKickOnFirstSIPRTCP() {
		t.Fatalf("expected first SIP RTCP uplink kick to be re-armed after reset")
	}
	if !sess.sipVideoDestReadyAt.IsZero() {
		t.Fatalf("expected sipVideoDestReadyAt to be cleared after reset")
	}
	if sess.IsMediaForwardReady() {
		t.Fatal("expected media forward ready to be cleared on ResetMediaState")
	}
	sess.MarkMediaForwardReady()
	if !sess.IsMediaForwardReady() {
		t.Fatal("expected MarkMediaForwardReady to arm uplink forwarding")
	}
}

func TestClearSIPVideoParameterSetsPreservesWebRTCToSIPCache(t *testing.T) {
	sess := &Session{
		CachedSPS:    []byte{0x67, 0x42, 0x00, 0x1f},
		CachedPPS:    []byte{0x68, 0xce, 0x06, 0xe2},
		SIPCachedSPS: []byte{0x67, 0x64, 0x00, 0x28},
		SIPCachedPPS: []byte{0x68, 0xee, 0x3c, 0x80},
	}

	sess.ClearSIPVideoParameterSets()

	if len(sess.SIPCachedSPS) != 0 || len(sess.SIPCachedPPS) != 0 {
		t.Fatalf("expected SIP-side parameter sets cleared, got SPS=%x PPS=%x", sess.SIPCachedSPS, sess.SIPCachedPPS)
	}
	if string(sess.CachedSPS) != string([]byte{0x67, 0x42, 0x00, 0x1f}) ||
		string(sess.CachedPPS) != string([]byte{0x68, 0xce, 0x06, 0xe2}) {
		t.Fatalf("expected WebRTC-to-SIP parameter sets preserved, got SPS=%x PPS=%x", sess.CachedSPS, sess.CachedPPS)
	}
}

func TestResetMediaStateClearsSIPParameterSetsAndSwitchGateWithoutReusingLease(t *testing.T) {
	sess := &Session{
		ID:                                "test-reset-switch-gate",
		VideoAUNormalizeEnabled:           true,
		SwitchGeneration:                  7,
		SIPCachedSPS:                      []byte{0x67, 0x64, 0x00, 0x28},
		SIPCachedPPS:                      []byte{0x68, 0xee, 0x3c, 0x80},
		CachedSPS:                         []byte{0x67, 0x42, 0x00, 0x1f},
		CachedPPS:                         []byte{0x68, 0xce, 0x06, 0xe2},
		SwitchVideoGateActive:             true,
		SwitchVideoGateReleasing:          true,
		SwitchVideoGateGeneration:         7,
		SwitchVideoGateAcceptedGeneration: 6,
		SwitchVideoGateStartedAt:          time.Now(),
		SwitchVideoGateStartReason:        "agent-switch",
		SwitchVideoGateFeedbackBaseline:   9,
		SwitchVideoGateRejectedCount:      3,
		SwitchVideoGateLastRejectReason:   "non-idr",
		SwitchVideoGateLastRejectLogAt:    time.Now(),
		SwitchVideoGateLastStallLogAt:     time.Now(),
		SwitchVideoGateLeaseNonce:         41,
		SwitchVideoGateReservation:        41,
		SwitchVideoGateReservedPackets:    3,
		SwitchVideoGateReservedSSRC:       1234,
		SwitchVideoGateReservedInjection:  true,
		MediaEpoch:                        9,
	}

	sess.ResetMediaState()

	if len(sess.SIPCachedSPS) != 0 || len(sess.SIPCachedPPS) != 0 {
		t.Fatalf("expected SIP-side parameter sets cleared, got SPS=%x PPS=%x", sess.SIPCachedSPS, sess.SIPCachedPPS)
	}
	if len(sess.CachedSPS) == 0 || len(sess.CachedPPS) == 0 {
		t.Fatalf("expected WebRTC-to-SIP parameter sets preserved")
	}
	if sess.SwitchVideoGateActive || sess.SwitchVideoGateReleasing || sess.SwitchVideoGateGeneration != 0 ||
		sess.SwitchVideoGateAcceptedGeneration != 0 || !sess.SwitchVideoGateStartedAt.IsZero() ||
		sess.SwitchVideoGateStartReason != "" || sess.SwitchVideoGateFeedbackBaseline != 0 ||
		sess.SwitchVideoGateRejectedCount != 0 || sess.SwitchVideoGateLastRejectReason != "" ||
		!sess.SwitchVideoGateLastRejectLogAt.IsZero() || !sess.SwitchVideoGateLastStallLogAt.IsZero() ||
		sess.SwitchVideoGateReservation != 0 ||
		sess.SwitchVideoGateReservedPackets != 0 || sess.SwitchVideoGateReservedSSRC != 0 ||
		sess.SwitchVideoGateReservedInjection {
		t.Fatalf("expected complete switch gate reset, got %+v", sess)
	}
	if sess.SwitchVideoGateLeaseNonce != 41 {
		t.Fatalf("expected lease nonce preserved at 41, got %d", sess.SwitchVideoGateLeaseNonce)
	}
	if sess.MediaEpoch != 10 {
		t.Fatalf("expected media epoch to advance to 10, got %d", sess.MediaEpoch)
	}

	sess.VideoAUNormalizeEnabled = true
	sess.SwitchGeneration = 1
	if !sess.StartSwitchVideoGate(1, time.Now(), "after-reset") {
		t.Fatalf("expected gate to start after reset")
	}
	decision := sess.EvaluateSwitchVideoAccessUnit(gateTestAU(1, true, true, 1), time.Now())
	if decision.Reservation != 42 {
		t.Fatalf("expected monotonic reservation 42 after reset, got %d", decision.Reservation)
	}
}

func TestSetAsteriskEndpointsKicksUplinkOnFirstVideoDest(t *testing.T) {
	sess := &Session{ID: "sip-dest-ready"}
	sess.SetAsteriskEndpoints(
		&net.UDPAddr{IP: net.ParseIP("203.150.245.41"), Port: 20000},
		&net.UDPAddr{IP: net.ParseIP("203.150.245.41"), Port: 20002},
	)
	if sess.AsteriskVideoAddr == nil || sess.AsteriskVideoAddr.Port != 20002 {
		t.Fatalf("expected video dest to be stored, got %#v", sess.AsteriskVideoAddr)
	}
	sess.SetState(StateEnded)
}
