package session

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func TestBrowserRTCPFeedbackLine(t *testing.T) {
	feedback := newBrowserFeedback("s", true)
	feedback.noteReports([]rtcp.Packet{&rtcp.ReceiverReport{Reports: []rtcp.ReceptionReport{{
		FractionLost: 20,
		TotalLost:    7,
	}}}})
	feedback.mu.Lock()
	feedback.videoRTP = 270
	feedback.twccSent = 4
	feedback.twccReceived = 800
	feedback.twccLost = 3
	feedback.mu.Unlock()

	line, ok := feedback.snapshot(time.Unix(1_700_000_000, 0))
	if !ok {
		t.Fatal("snapshot was suppressed")
	}
	want := "browser_rtcp_feedback rr_sent=1 rr_fraction_lost=20 rr_cumulative_lost=7 twcc_sent=4 twcc_received=800 twcc_lost=3 browser_rtp_per_s=270"
	if line != want {
		t.Fatalf("line %q", line)
	}
}

func TestTWCCFeedbackCountsASequenceGapAsLoss(t *testing.T) {
	feedback := newBrowserFeedback("s", true)
	feedback.mu.Lock()
	feedback.recorder.Record(9, 1, 0)
	feedback.recorder.Record(9, 2, 1000)
	feedback.recorder.Record(9, 4, 3000)
	packets := feedback.recorder.BuildFeedbackPacket()
	feedback.mu.Unlock()

	received, lost := twccLossCounts(packets)
	if received != 3 || lost < 1 {
		t.Fatalf("twcc received=%d lost=%d, want received 3 and a gap", received, lost)
	}
}

func TestBrowserFeedbackObserveDoesNotWaitForRTCPWrite(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	writer := interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return len(pkts), nil
	})
	feedback := newBrowserFeedback("stall", true)
	defer func() {
		close(release)
		_ = feedback.Close()
	}()
	feedback.BindRTCPWriter(writer)
	feedback.mu.Lock()
	feedback.recorder.Record(9, 1, 1000)
	feedback.mu.Unlock()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("TWCC write did not start")
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			feedback.observe(9, 0, true, nil, nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("video RTP observe blocked while RTCP write was stuck")
	}
}

func TestRegisteredInterceptorEmitsTWCCForTransportCCRTP(t *testing.T) {
	registry, err := gatewayInterceptorRegistry(true, "")
	if err != nil {
		t.Fatal(err)
	}
	chain, err := registry.Build("twcc-test")
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()

	var mu sync.Mutex
	var got []rtcp.Packet
	chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
		mu.Lock()
		got = append(got, pkts...)
		mu.Unlock()
		return 0, nil
	}))

	packets := make([][]byte, 0, 5)
	for i := 0; i < 5; i++ {
		packets = append(packets, mustTransportCCRTP(t, uint16(100+i), uint16(i+1)))
	}
	next := 0
	reader := chain.BindRemoteStream(&interceptor.StreamInfo{
		SSRC:     7,
		MimeType: "video/H264",
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{
			URI: transportCCURI,
			ID:  3,
		}},
	}, interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n := copy(b, packets[next])
		next++
		return n, a, nil
	}))

	buf := make([]byte, 1500)
	for range packets {
		if _, _, err := reader.Read(buf, nil); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		received, _ := twccLossCounts(got)
		mu.Unlock()
		if received >= len(packets) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	received, lost := twccLossCounts(got)
	t.Fatalf("TWCC feedback received=%d lost=%d packets=%d", received, lost, len(got))
}

func mustTransportCCRTP(t *testing.T, sequence, transportSequence uint16) []byte {
	t.Helper()
	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    96,
			SequenceNumber: sequence,
			Timestamp:      90000,
			SSRC:           7,
			Marker:         true,
		},
		Payload: []byte{0x65, 0x80},
	}
	raw, err := (&rtp.TransportCCExtension{TransportSequence: transportSequence}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := packet.Header.SetExtension(3, raw); err != nil {
		t.Fatal(err)
	}
	buf, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return buf
}
