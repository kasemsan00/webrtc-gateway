package sip

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func findFreeContiguousUDPBlock(t *testing.T, slots int) (portMin, portMax int) {
	t.Helper()
	portsNeeded := slots * rtpPortSlotStep
	for base := 41000; base <= 58000; base += 100 {
		conns := make([]*net.UDPConn, 0, portsNeeded)
		ok := true
		for p := base; p < base+portsNeeded; p++ {
			c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: p})
			if err != nil {
				ok = false
				break
			}
			conns = append(conns, c)
		}
		for _, c := range conns {
			c.Close()
		}
		if ok {
			return base, base + portsNeeded - 1
		}
	}
	t.Fatalf("could not find %d free contiguous UDP ports for test", portsNeeded)
	return 0, 0
}

func TestRTPPortSlotCount(t *testing.T) {
	cases := []struct {
		min, max, want int
	}{
		{10500, 10502, 0},
		{10500, 10503, 1},
		{10500, 10506, 1},
		{10500, 10507, 2},
		{10500, 10600, 25},
	}
	for _, tc := range cases {
		if got := rtpPortSlotCount(tc.min, tc.max); got != tc.want {
			t.Fatalf("rtpPortSlotCount(%d, %d) = %d, want %d", tc.min, tc.max, got, tc.want)
		}
	}
}

func TestBindRTPPortQuad_RoundRobinWrapSkipBusyAndExhaust(t *testing.T) {
	const slots = 4
	portMin, portMax := findFreeContiguousUDPBlock(t, slots)
	srv := &Server{
		rtpConfig: config.RTPConfig{
			PortMin: portMin,
			PortMax: portMax,
		},
	}

	type quad struct {
		port  int
		conns []*net.UDPConn
	}
	closeQuad := func(q quad) {
		for _, c := range q.conns {
			if c != nil {
				c.Close()
			}
		}
	}

	var active []quad
	defer func() {
		for _, q := range active {
			closeQuad(q)
		}
	}()

	// Allocate all 4 slots sequentially: cursor must advance O(1) slot by slot.
	for i := 0; i < slots; i++ {
		port, a, ar, v, vr, err := srv.bindRTPPortQuad()
		if err != nil {
			t.Fatalf("slot %d allocation failed: %v", i, err)
		}
		wantPort := portMin + i*rtpPortSlotStep
		if port != wantPort {
			closeQuad(quad{port, []*net.UDPConn{a, ar, v, vr}})
			t.Fatalf("slot %d port = %d, want %d", i, port, wantPort)
		}
		active = append(active, quad{port, []*net.UDPConn{a, ar, v, vr}})
	}

	// Range exhausted -> error.
	if _, a, ar, v, vr, err := srv.bindRTPPortQuad(); err == nil {
		closeQuad(quad{0, []*net.UDPConn{a, ar, v, vr}})
		t.Fatal("expected error when RTP port range is exhausted")
	}

	// Free slot 1 (leaving slots 0, 2, 3 busy) -> next allocation must wrap and find slot 1.
	closeQuad(active[1])
	active[1].conns = nil

	port, a, ar, v, vr, err := srv.bindRTPPortQuad()
	if err != nil {
		t.Fatalf("wrap-around allocation failed: %v", err)
	}
	defer closeQuad(quad{port, []*net.UDPConn{a, ar, v, vr}})
	wantPort := portMin + 1*rtpPortSlotStep
	if port != wantPort {
		t.Fatalf("wrapped allocation port = %d, want %d", port, wantPort)
	}
}

func TestBindRTPPortQuad_ConcurrentDisjointPorts(t *testing.T) {
	const slots = 6
	portMin, portMax := findFreeContiguousUDPBlock(t, slots)
	srv := &Server{
		rtpConfig: config.RTPConfig{
			PortMin: portMin,
			PortMax: portMax,
		},
	}

	var mu sync.Mutex
	seen := make(map[int]bool)
	var allConns []*net.UDPConn
	defer func() {
		for _, c := range allConns {
			c.Close()
		}
	}()

	var wg sync.WaitGroup
	errCh := make(chan error, slots)
	for i := 0; i < slots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			port, a, ar, v, vr, err := srv.bindRTPPortQuad()
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			if seen[port] {
				errCh <- fmt.Errorf("duplicate port allocated: %d", port)
			}
			seen[port] = true
			allConns = append(allConns, a, ar, v, vr)
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent bindRTPPortQuad error: %v", err)
	}
	if len(seen) != slots {
		t.Fatalf("allocated %d distinct slots, want %d", len(seen), slots)
	}
}

func TestServerOfferVideoPrimeTimeout(t *testing.T) {
	srv := &Server{}
	if got := srv.OfferVideoPrimeTimeout(); got != DefaultOfferVideoPrimeTimeout {
		t.Fatalf("default OfferVideoPrimeTimeout = %v, want %v", got, DefaultOfferVideoPrimeTimeout)
	}

	srv.SetOfferVideoPrimeTimeout(450 * time.Millisecond)
	if got := srv.OfferVideoPrimeTimeout(); got != 450*time.Millisecond {
		t.Fatalf("custom OfferVideoPrimeTimeout = %v, want 450ms", got)
	}

	srv.SetOfferVideoPrimeTimeout(0)
	if got := srv.OfferVideoPrimeTimeout(); got != 0 {
		t.Fatalf("disabled OfferVideoPrimeTimeout = %v, want 0", got)
	}

	srv.SetOfferVideoPrimeTimeout(-10 * time.Millisecond)
	if got := srv.OfferVideoPrimeTimeout(); got != 0 {
		t.Fatalf("negative OfferVideoPrimeTimeout = %v, want 0", got)
	}
}

func TestApplyEarlyMediaSDP_SetsEndpointsPayloadTypesAndSeedsSPSPPS(t *testing.T) {
	sps := []byte{0x67, 0x42, 0xe0, 0x1f}
	pps := []byte{0x68, 0xce, 0x3c, 0x80}
	sprop := base64.StdEncoding.EncodeToString(sps) + "," + base64.StdEncoding.EncodeToString(pps)

	sdp183 := "v=0\r\n" +
		"o=- 1 1 IN IP4 198.51.100.20\r\n" +
		"s=Early\r\n" +
		"c=IN IP4 198.51.100.20\r\n" +
		"t=0 0\r\n" +
		"m=audio 20000 RTP/AVP 107\r\n" +
		"a=rtpmap:107 opus/48000/2\r\n" +
		"m=video 20002 RTP/AVPF 102\r\n" +
		"a=rtpmap:102 H264/90000\r\n" +
		"a=fmtp:102 packetization-mode=1;sprop-parameter-sets=" + sprop + "\r\n"

	srv := &Server{}
	sess := &session.Session{ID: "early-183"}
	sess.SetSIPOpusPT(111)

	srv.applyEarlyMediaSDP([]byte(sdp183), sess)

	if got := sess.GetSIPOpusPT(); got != 107 {
		t.Fatalf("Opus PT = %d, want 107", got)
	}
	if got := sess.GetSIPVideoPayloadType(); got != 102 {
		t.Fatalf("Video PT = %d, want 102", got)
	}
	if sess.AsteriskAudioAddr == nil || sess.AsteriskAudioAddr.String() != "198.51.100.20:20000" {
		t.Fatalf("AsteriskAudioAddr = %v, want 198.51.100.20:20000", sess.AsteriskAudioAddr)
	}
	if sess.AsteriskVideoAddr == nil || sess.AsteriskVideoAddr.String() != "198.51.100.20:20002" {
		t.Fatalf("AsteriskVideoAddr = %v, want 198.51.100.20:20002", sess.AsteriskVideoAddr)
	}
	gotSPS, gotPPS, ok := sess.GetSIPCachedSPSPPS()
	if !ok || string(gotSPS) != string(sps) || string(gotPPS) != string(pps) {
		t.Fatalf("SIPCached SPS/PPS = ok=%v sps=%x pps=%x, want %x/%x", ok, gotSPS, gotPPS, sps, pps)
	}

	// Unspecified IP in 183 must not overwrite endpoints, but still seeds SPS/PPS on a fresh session.
	sdpUnspecified := "v=0\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"m=audio 20000 RTP/AVP 111\r\n" +
		"m=video 20002 RTP/AVP 96\r\n" +
		"a=fmtp:96 sprop-parameter-sets=" + sprop + "\r\n"
	sess2 := &session.Session{ID: "early-183-unspec"}
	srv.applyEarlyMediaSDP([]byte(sdpUnspecified), sess2)
	if sess2.AsteriskAudioAddr != nil || sess2.AsteriskVideoAddr != nil {
		t.Fatalf("unspecified 183 IP must not set endpoints: audio=%v video=%v", sess2.AsteriskAudioAddr, sess2.AsteriskVideoAddr)
	}
	if _, _, ok := sess2.GetSIPCachedSPSPPS(); !ok {
		t.Fatal("expected SPS/PPS seeded even when 183 IP is unspecified")
	}
}

func TestApplyEarlyMediaSDP_200OKOverwrites183UntilLiveRTP(t *testing.T) {
	sps183 := []byte{0x67, 0x42, 0xe0, 0x1f}
	pps183 := []byte{0x68, 0xce, 0x3c, 0x80}
	sprop183 := base64.StdEncoding.EncodeToString(sps183) + "," + base64.StdEncoding.EncodeToString(pps183)

	sps200 := []byte{0x67, 0x64, 0x00, 0x28}
	pps200 := []byte{0x68, 0xee, 0x3c, 0x80}
	sprop200 := base64.StdEncoding.EncodeToString(sps200) + "," + base64.StdEncoding.EncodeToString(pps200)

	makeSDP := func(sprop string) []byte {
		return []byte("v=0\r\n" +
			"o=- 1 1 IN IP4 198.51.100.20\r\n" +
			"s=Call\r\n" +
			"c=IN IP4 198.51.100.20\r\n" +
			"t=0 0\r\n" +
			"m=audio 20000 RTP/AVP 111\r\n" +
			"a=rtpmap:111 opus/48000/2\r\n" +
			"m=video 20002 RTP/AVPF 102\r\n" +
			"a=rtpmap:102 H264/90000\r\n" +
			"a=fmtp:102 packetization-mode=1;sprop-parameter-sets=" + sprop + "\r\n")
	}

	srv := &Server{}

	// Case 1: 183 ringback SDP followed by 200 OK SDP (no live RTP SPS/PPS yet) -> 200 OK overwrites.
	sess1 := &session.Session{ID: "sdp-183-then-200"}
	srv.applyEarlyMediaSDP(makeSDP(sprop183), sess1)
	srv.parseAsteriskSDPAndSetEndpoints(makeSDP(sprop200), sess1)
	gotSPS, gotPPS, ok := sess1.GetSIPCachedSPSPPS()
	if !ok || string(gotSPS) != string(sps200) || string(gotPPS) != string(pps200) {
		t.Fatalf("expected 200 OK SDP to overwrite 183 SDP, got ok=%v sps=%x pps=%x", ok, gotSPS, gotPPS)
	}

	// Case 2: 183 SDP -> live RTP SPS/PPS -> 200 OK SDP -> live RTP SPS/PPS must be preserved.
	sess2 := &session.Session{ID: "sdp-183-rtp-then-200"}
	srv.applyEarlyMediaSDP(makeSDP(sprop183), sess2)
	liveSPS := []byte{0x67, 0x4d, 0x00, 0x1e}
	livePPS := []byte{0x68, 0xeb, 0xe3, 0xcb}
	sess2.CacheSIPSPS(liveSPS)
	sess2.CacheSIPPPS(livePPS)
	srv.parseAsteriskSDPAndSetEndpoints(makeSDP(sprop200), sess2)
	gotSPS, gotPPS, ok = sess2.GetSIPCachedSPSPPS()
	if !ok || string(gotSPS) != string(liveSPS) || string(gotPPS) != string(livePPS) {
		t.Fatalf("expected live RTP SPS/PPS to survive 200 OK SDP, got ok=%v sps=%x pps=%x", ok, gotSPS, gotPPS)
	}
}

func TestDetachedSnapshotContext_SurvivesCanceledParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	snapCtx, snapCancel := detachedSnapshotContext(parent)
	defer snapCancel()

	if err := snapCtx.Err(); err != nil {
		t.Fatalf("detachedSnapshotContext inherited parent cancellation: %v", err)
	}
	deadline, ok := snapCtx.Deadline()
	if !ok {
		t.Fatal("expected deadline on detached snapshot context")
	}
	remaining := time.Until(deadline)
	if remaining <= 2*time.Second || remaining > 4*time.Second {
		t.Fatalf("unexpected detached snapshot timeout remaining: %v", remaining)
	}
}
