package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestSummarizeOfferICECandidates(t *testing.T) {
	sdp := "v=0\r\n" +
		"a=candidate:1 1 udp 2122260223 127.0.0.1 50000 typ host\r\n" +
		"a=candidate:2 1 udp 2122260222 ::1 50001 typ host\r\n" +
		"a=candidate:3 1 udp 2122260221 0.0.0.0 50002 typ host\r\n"
	loopOnly := summarizeOfferICECandidates(sdp)
	if loopOnly.hasNonLoopbackHost || loopOnly.hasSrflx || loopOnly.hasRelay || loopOnly.usableCount != 0 {
		t.Fatalf("loopback/unspecified candidates must not count as usable: %+v", loopOnly)
	}

	full := sdp +
		"a=candidate:4 1 udp 2122260220 203.0.113.10 50003 typ host\r\n" +
		"a=candidate:5 1 udp 1686052607 198.51.100.2 50004 typ srflx raddr 10.0.0.2 rport 50004\r\n" +
		"a=candidate:6 1 udp 41885439 198.51.100.9 40000 typ relay raddr 198.51.100.2 rport 50004\r\n"
	s := summarizeOfferICECandidates(full)
	if !s.hasNonLoopbackHost || !s.hasSrflx || !s.hasRelay || s.usableCount != 3 {
		t.Fatalf("unexpected candidate summary: %+v", s)
	}
}

func TestWaitForOfferICEGathering_NAT1To1WithoutTURNReturnsImmediatelyOnHost(t *testing.T) {
	var mu sync.Mutex
	sdp := "a=candidate:1 1 udp 2122260223 127.0.0.1 50000 typ host\r\n"
	readSDP := func() string {
		mu.Lock()
		defer mu.Unlock()
		return sdp
	}
	gatherDone := make(chan struct{})

	go func() {
		time.Sleep(25 * time.Millisecond)
		mu.Lock()
		sdp += "a=candidate:2 1 udp 2122260220 203.0.113.10 50001 typ host\r\n"
		mu.Unlock()
	}()

	start := time.Now()
	ok := waitForOfferICEGathering(
		context.Background(),
		gatherDone,
		readSDP,
		true,  // hasNAT1To1
		false, // hasTURN
		time.Second,
		5*time.Millisecond,
		150*time.Millisecond,
		150*time.Millisecond,
	)
	elapsed := time.Since(start)
	if !ok {
		t.Fatal("expected true once non-loopback host arrived")
	}
	if elapsed < 20*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Fatalf("unexpected wait duration: %v", elapsed)
	}
}

func TestWaitForOfferICEGathering_NAT1To1WithTURNGraceAndImmediateRelay(t *testing.T) {
	t.Run("waits brief grace when only host arrives", func(t *testing.T) {
		gatherDone := make(chan struct{})
		readSDP := func() string {
			return "a=candidate:1 1 udp 2122260220 203.0.113.10 50001 typ host\r\n"
		}
		start := time.Now()
		ok := waitForOfferICEGathering(
			context.Background(),
			gatherDone,
			readSDP,
			true, // hasNAT1To1
			true, // hasTURN
			time.Second,
			5*time.Millisecond,
			40*time.Millisecond,
			40*time.Millisecond,
		)
		elapsed := time.Since(start)
		if !ok {
			t.Fatal("expected true after brief grace")
		}
		if elapsed < 30*time.Millisecond || elapsed > 250*time.Millisecond {
			t.Fatalf("expected ~40ms grace wait, got %v", elapsed)
		}
	})

	t.Run("returns immediately when relay arrives during grace", func(t *testing.T) {
		var mu sync.Mutex
		sdp := "a=candidate:1 1 udp 2122260220 203.0.113.10 50001 typ host\r\n"
		readSDP := func() string {
			mu.Lock()
			defer mu.Unlock()
			return sdp
		}
		gatherDone := make(chan struct{})
		go func() {
			time.Sleep(15 * time.Millisecond)
			mu.Lock()
			sdp += "a=candidate:2 1 udp 41885439 198.51.100.9 40000 typ relay raddr 10.0.0.2 rport 50001\r\n"
			mu.Unlock()
		}()
		start := time.Now()
		ok := waitForOfferICEGathering(
			context.Background(),
			gatherDone,
			readSDP,
			true, // hasNAT1To1
			true, // hasTURN
			time.Second,
			5*time.Millisecond,
			300*time.Millisecond,
			300*time.Millisecond,
		)
		elapsed := time.Since(start)
		if !ok {
			t.Fatal("expected true on relay")
		}
		if elapsed > 150*time.Millisecond {
			t.Fatalf("expected immediate return on relay before 300ms grace, got %v", elapsed)
		}
	})
}

func TestWaitForOfferICEGathering_NoNAT1To1WithTURNWaitsForRelay(t *testing.T) {
	t.Run("does not return on host or srflx alone, returns on relay", func(t *testing.T) {
		var mu sync.Mutex
		sdp := "a=candidate:1 1 udp 2122260220 10.0.0.2 50001 typ host\r\n" +
			"a=candidate:2 1 udp 1686052607 198.51.100.2 50002 typ srflx raddr 10.0.0.2 rport 50001\r\n"
		readSDP := func() string {
			mu.Lock()
			defer mu.Unlock()
			return sdp
		}
		gatherDone := make(chan struct{})
		go func() {
			time.Sleep(40 * time.Millisecond)
			mu.Lock()
			sdp += "a=candidate:3 1 udp 41885439 198.51.100.9 40000 typ relay raddr 10.0.0.2 rport 50001\r\n"
			mu.Unlock()
		}()

		start := time.Now()
		ok := waitForOfferICEGathering(
			context.Background(),
			gatherDone,
			readSDP,
			false, // hasNAT1To1
			true,  // hasTURN
			time.Second,
			5*time.Millisecond,
			150*time.Millisecond,
			150*time.Millisecond,
		)
		elapsed := time.Since(start)
		if !ok {
			t.Fatal("expected true once relay arrived")
		}
		if elapsed < 35*time.Millisecond || elapsed > 250*time.Millisecond {
			t.Fatalf("expected wait until relay (~40ms), got %v", elapsed)
		}
	})

	t.Run("times out if relay never arrives", func(t *testing.T) {
		gatherDone := make(chan struct{})
		readSDP := func() string {
			return "a=candidate:1 1 udp 2122260220 10.0.0.2 50001 typ host\r\n"
		}
		start := time.Now()
		ok := waitForOfferICEGathering(
			context.Background(),
			gatherDone,
			readSDP,
			false, // hasNAT1To1
			true,  // hasTURN
			50*time.Millisecond,
			5*time.Millisecond,
			150*time.Millisecond,
			150*time.Millisecond,
		)
		elapsed := time.Since(start)
		if ok {
			t.Fatal("expected false on timeout when relay never arrived")
		}
		if elapsed < 40*time.Millisecond {
			t.Fatalf("timed out too early: %v", elapsed)
		}
	})
}

func TestWaitForOfferICEGathering_NoNAT1To1NoTURNAndCancel(t *testing.T) {
	t.Run("returns after short grace once usable candidate present", func(t *testing.T) {
		gatherDone := make(chan struct{})
		readSDP := func() string {
			return "a=candidate:1 1 udp 2122260220 192.168.1.10 50001 typ host\r\n"
		}
		start := time.Now()
		ok := waitForOfferICEGathering(
			context.Background(),
			gatherDone,
			readSDP,
			false,
			false,
			time.Second,
			5*time.Millisecond,
			150*time.Millisecond,
			35*time.Millisecond,
		)
		elapsed := time.Since(start)
		if !ok {
			t.Fatal("expected true after short grace")
		}
		if elapsed < 25*time.Millisecond || elapsed > 200*time.Millisecond {
			t.Fatalf("expected ~35ms grace, got %v", elapsed)
		}
	})

	t.Run("returns false on canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		gatherDone := make(chan struct{})
		ok := waitForOfferICEGathering(
			ctx,
			gatherDone,
			func() string { return "" },
			false,
			true,
			time.Second,
			5*time.Millisecond,
			150*time.Millisecond,
			150*time.Millisecond,
		)
		if ok {
			t.Fatal("expected false for canceled context")
		}
	})
}

func TestWaitForOfferICEGatheringComplete_RealPeerConnectionPreservesOnICECandidate(t *testing.T) {
	if WaitForOfferICEGatheringComplete(context.Background(), nil, false, false, 50*time.Millisecond) {
		t.Fatal("expected false for nil PeerConnection")
	}

	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new offerer pc: %v", err)
	}
	defer offerer.Close()
	if _, err := offerer.CreateDataChannel("test", nil); err != nil {
		t.Fatalf("create data channel: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}

	answerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new answerer pc: %v", err)
	}
	defer answerer.Close()

	var hookFired atomic.Bool
	answerer.OnICECandidate(func(c *webrtc.ICECandidate) {
		hookFired.Store(true)
	})

	if err := answerer.SetRemoteDescription(offer); err != nil {
		t.Fatalf("set remote description: %v", err)
	}
	answer, err := answerer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := answerer.SetLocalDescription(answer); err != nil {
		t.Fatalf("set local description: %v", err)
	}

	ok := WaitForOfferICEGatheringComplete(context.Background(), answerer, false, false, 2*time.Second)
	if !ok {
		t.Fatal("expected gathering helper to complete on local PeerConnection")
	}
	if !hookFired.Load() {
		t.Fatal("existing OnICECandidate callback was clobbered or never fired")
	}
}
