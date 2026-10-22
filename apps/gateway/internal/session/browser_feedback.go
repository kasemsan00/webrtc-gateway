package session

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

// browserFeedback records RTP for TWCC without blocking Read, and counts the
// RTCP we send back to the browser. Pion's sender interceptor records on an
// unbuffered channel inside Read; if that feedback write stalls, the video
// reader stalls with it and Chrome's bandwidth estimate collapses.
type browserFeedback struct {
	interceptor.NoOp

	sessionID string
	twcc      bool
	start     time.Time
	recorder  *twcc.Recorder

	mu           sync.Mutex
	videoRTP     int
	rrSent       int
	rrFraction   int
	rrCumulative int
	twccSent     int
	twccReceived int
	twccLost     int
	videoRTPMark int
	rrSentMark   int
	twccSentMark int
	twccRecvMark int
	twccLostMark int
	windowStart  time.Time

	closed    chan struct{}
	stop      sync.Once
	startOnce sync.Once
}

type browserFeedbackFactory struct {
	sessionID string
	twcc      bool
}

func (f *browserFeedbackFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return newBrowserFeedback(f.sessionID, f.twcc), nil
}

func newBrowserFeedback(sessionID string, enabled bool) *browserFeedback {
	feedback := &browserFeedback{
		sessionID: sessionID,
		twcc:      enabled,
		start:     time.Now(),
		closed:    make(chan struct{}),
	}
	if enabled {
		feedback.recorder = twcc.NewRecorder(1)
	}
	return feedback
}

func (b *browserFeedback) Close() error {
	if b == nil {
		return nil
	}
	b.stop.Do(func() { close(b.closed) })
	return nil
}

func (b *browserFeedback) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter {
	b.startOnce.Do(func() {
		go b.loop(writer)
	})
	return interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
		b.noteReports(pkts)
		return writer.Write(pkts, attributes)
	})
}

func (b *browserFeedback) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	if info == nil {
		return reader
	}
	var extensionID uint8
	for _, extension := range info.RTPHeaderExtensions {
		if extension.URI == transportCCURI && extension.ID > 0 && extension.ID < 256 {
			extensionID = uint8(extension.ID)
			break
		}
	}
	video := strings.HasPrefix(strings.ToLower(info.MimeType), "video")
	return interceptor.RTPReaderFunc(func(buf []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, attr, err := reader.Read(buf, attributes)
		if err != nil || n <= 0 {
			return n, attr, err
		}
		b.observe(info.SSRC, extensionID, video, buf[:n], attr)
		return n, attr, nil
	})
}

func (b *browserFeedback) observe(ssrc uint32, extensionID uint8, video bool, buf []byte, attr interceptor.Attributes) {
	seq, ok := transportSequence(extensionID, buf, attr)
	b.mu.Lock()
	defer b.mu.Unlock()
	if video {
		b.videoRTP++
	}
	if ok && b.recorder != nil {
		b.recorder.Record(ssrc, seq, time.Since(b.start).Microseconds())
	}
}

func transportSequence(extensionID uint8, buf []byte, attr interceptor.Attributes) (uint16, bool) {
	if extensionID == 0 || len(buf) == 0 {
		return 0, false
	}
	// Pion's SRTP reader returns the attributes it was given. Track.Read
	// passes nil, so the header has to be unmarshalled from the packet bytes.
	if attr == nil {
		attr = interceptor.Attributes{}
	}
	header, err := attr.GetRTPHeader(buf)
	if err != nil || header == nil {
		return 0, false
	}
	raw := header.GetExtension(extensionID)
	if len(raw) == 0 {
		return 0, false
	}
	var extension rtp.TransportCCExtension
	if err := extension.Unmarshal(raw); err != nil {
		return 0, false
	}
	return extension.TransportSequence, true
}

func (b *browserFeedback) noteReports(pkts []rtcp.Packet) {
	if b == nil || len(pkts) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, pkt := range pkts {
		report, ok := pkt.(*rtcp.ReceiverReport)
		if !ok || report == nil {
			continue
		}
		b.rrSent++
		for _, reception := range report.Reports {
			b.rrFraction = int(reception.FractionLost)
			b.rrCumulative = int(reception.TotalLost)
		}
	}
}

func (b *browserFeedback) loop(writer interceptor.RTCPWriter) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-b.closed:
			return
		case now := <-ticker.C:
			if b.twcc && writer != nil {
				pkts := b.takeFeedback()
				if len(pkts) > 0 {
					received, lost := twccLossCounts(pkts)
					b.mu.Lock()
					b.twccSent += len(pkts)
					b.twccReceived += received
					b.twccLost += lost
					b.mu.Unlock()
					_, _ = writer.Write(pkts, nil)
				}
			}
			if b.sessionID != "" {
				if line, ok := b.snapshot(now); ok {
					fmt.Printf("[%s] %s\n", b.sessionID, line)
				}
			}
		}
	}
}

func (b *browserFeedback) takeFeedback() []rtcp.Packet {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.recorder == nil {
		return nil
	}
	return b.recorder.BuildFeedbackPacket()
}

func (b *browserFeedback) snapshot(now time.Time) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.windowStart.IsZero() && now.Sub(b.windowStart) < time.Second {
		return "", false
	}
	elapsed := now.Sub(b.windowStart)
	if b.windowStart.IsZero() || elapsed <= 0 {
		elapsed = time.Second
	}
	snap := browserRTCPFeedback{
		RRSent:         b.rrSent - b.rrSentMark,
		RRFractionLost: b.rrFraction,
		RRCumulative:   b.rrCumulative,
		TWCCSent:       b.twccSent - b.twccSentMark,
		TWCCReceived:   b.twccReceived - b.twccRecvMark,
		TWCCLost:       b.twccLost - b.twccLostMark,
		BrowserRTPPerS: int(float64(b.videoRTP-b.videoRTPMark) / elapsed.Seconds()),
	}
	b.videoRTPMark = b.videoRTP
	b.rrSentMark = b.rrSent
	b.twccSentMark = b.twccSent
	b.twccRecvMark = b.twccReceived
	b.twccLostMark = b.twccLost
	b.windowStart = now
	return formatBrowserRTCPFeedback(snap), true
}

type browserRTCPFeedback struct {
	RRSent         int
	RRFractionLost int
	RRCumulative   int
	TWCCSent       int
	TWCCReceived   int
	TWCCLost       int
	BrowserRTPPerS int
}

func formatBrowserRTCPFeedback(snap browserRTCPFeedback) string {
	return fmt.Sprintf(
		"browser_rtcp_feedback rr_sent=%d rr_fraction_lost=%d rr_cumulative_lost=%d twcc_sent=%d twcc_received=%d twcc_lost=%d browser_rtp_per_s=%d",
		snap.RRSent, snap.RRFractionLost, snap.RRCumulative, snap.TWCCSent, snap.TWCCReceived, snap.TWCCLost, snap.BrowserRTPPerS,
	)
}

func twccLossCounts(pkts []rtcp.Packet) (received, lost int) {
	for _, pkt := range pkts {
		feedback, ok := pkt.(*rtcp.TransportLayerCC)
		if !ok || feedback == nil {
			continue
		}
		got := len(feedback.RecvDeltas)
		total := int(feedback.PacketStatusCount)
		received += got
		if total > got {
			lost += total - got
		}
	}
	return received, lost
}
