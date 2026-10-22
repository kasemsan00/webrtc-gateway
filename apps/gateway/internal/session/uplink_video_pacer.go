package session

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// uplinkVideoPacer spreads WebRTC→SIP video datagrams so an IDR cannot
// leave the gateway as a single sub-millisecond burst. Audio is never
// submitted here. The read loop only appends to inbox; a session
// goroutine performs the UDP writes.
type uplinkVideoPacer struct {
	session *Session
	pacer   *videoPacer
	mu      sync.Mutex
	inbox   []pacedVideoPacket
	wake    chan struct{}
	ctx     context.Context
	stopped bool
	seen    bool
}

func (s *Session) paceUplinkVideo() bool {
	return s != nil && s.UplinkVideoPacerEnabled
}

func (s *Session) startUplinkVideoPacer() *uplinkVideoPacer {
	s.uplinkVideoPacerOnce.Do(func() {
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		s.uplinkVideoPacer = &uplinkVideoPacer{
			session: s,
			pacer: newVideoPacer(videoPacerConfigFrom(
				s.uplinkVideoPacerBPS,
				s.uplinkVideoPacerBurstPackets,
				s.uplinkVideoPacerMaxQueue,
			)),
			wake: make(chan struct{}, 1),
			ctx:  ctx,
		}
		go s.uplinkVideoPacer.loop()
	})
	return s.uplinkVideoPacer
}

func (s *Session) enqueueUplinkVideo(conn *net.UDPConn, addr *net.UDPAddr, data []byte, keyframe bool, rtpTS uint32, packetCount int) {
	if s == nil || conn == nil || addr == nil || len(data) == 0 {
		return
	}
	next := heldUplinkVideo{
		conn:        conn,
		addr:        *addr,
		data:        append([]byte(nil), data...),
		keyframe:    keyframe,
		rtpTS:       rtpTS,
		packetCount: packetCount,
	}
	s.uplinkPictureMu.Lock()
	prev, ok := s.uplinkPicture.push(next)
	s.uplinkPictureGen++
	gen := s.uplinkPictureGen
	s.uplinkPictureMu.Unlock()
	if ok {
		s.deliverUplinkVideo(prev)
	}
	s.armUplinkPictureFlush(gen)
}

func (s *Session) deliverUplinkVideo(pkt heldUplinkVideo) {
	payload := rtpPayload(pkt.data)
	keyframe := pkt.keyframe
	if len(payload) > 0 {
		switch payload[0] & 0x1f {
		case 5:
			keyframe = true
		case 7, 8:
			keyframe = false
		}
	}
	if keyframe {
		s.maybeLogFIRIDRSlice(payload, pkt.rtpTS, pkt.marker)
	}
	if s.paceUplinkVideo() {
		s.paceHeldUplinkVideo(pkt, keyframe)
		return
	}
	if pkt.conn == nil {
		return
	}
	if _, err := pkt.conn.WriteToUDP(pkt.data, &pkt.addr); err != nil {
		if pkt.packetCount <= 5 {
			fmt.Printf("[%s] Error forwarding video to Asterisk: %v\n", s.ID, err)
		}
		return
	}
	if keyframe {
		s.RecordUplinkKeyframe()
		if pkt.marker {
			fmt.Printf("[%s] 🔑 KEYFRAME (Forwarded) detected in video packet #%d → Asterisk\n", s.ID, pkt.packetCount)
		}
	}
	s.ObserveUplinkVideoForward(keyframe, pkt.rtpTS, time.Now())
}

func (s *Session) paceHeldUplinkVideo(pkt heldUplinkVideo, keyframe bool) {
	p := s.startUplinkVideoPacer()
	payload := pkt.data
	dest := pkt.addr
	conn := pkt.conn
	packetCount := pkt.packetCount
	rtpTS := pkt.rtpTS
	marker := pkt.marker
	paced := pacedVideoPacket{
		size:        len(payload),
		enqueued:    time.Now(),
		keyframe:    keyframe,
		marker:      marker,
		rtpTS:       rtpTS,
		packetCount: packetCount,
		write: func() {
			_, err := conn.WriteToUDP(payload, &dest)
			if err != nil {
				if packetCount <= 5 {
					fmt.Printf("[%s] Error forwarding video to Asterisk: %v\n", s.ID, err)
				}
				return
			}
			if keyframe {
				s.RecordUplinkKeyframe()
				if marker {
					fmt.Printf("[%s] 🔑 KEYFRAME (Forwarded) detected in video packet #%d → Asterisk\n", s.ID, packetCount)
				}
			}
			s.ObserveUplinkVideoForward(keyframe, rtpTS, time.Now())
		},
	}
	if !p.submit(paced) {
		paced.write()
	}
}

func (p *uplinkVideoPacer) submit(pkt pacedVideoPacket) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return false
	}
	p.inbox = append(p.inbox, pkt)
	p.seen = true
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return true
}

func (p *uplinkVideoPacer) loop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	var nextStats time.Time
	var peakPackets, peakBytes int
	var peakDelay time.Duration
	for {
		p.mu.Lock()
		now := time.Now()
		if len(p.inbox) > 0 {
			for _, pkt := range p.inbox {
				p.pacer.Enqueue(pkt)
			}
			p.inbox = nil
		}
		var headDelay time.Duration
		if p.pacer.queued() > 0 {
			headDelay = now.Sub(p.pacer.queue[p.pacer.head].enqueued)
			if headDelay < 0 {
				headDelay = 0
			}
		}
		waitingPackets, waitingBytes, waitingDelay := p.pacer.QueueStats(now)
		if waitingPackets > peakPackets {
			peakPackets = waitingPackets
			peakBytes = waitingBytes
			peakDelay = waitingDelay
		}
		released, flushed, wait := p.pacer.Release(now)
		if p.stopped && p.pacer.queued() > 0 {
			extra := p.pacer.takeAll(now)
			if len(extra) > 0 {
				released = append(released, extra...)
				flushed = true
			}
		}
		logs := p.pacer.TakeIDRLogs()
		stopped := p.stopped
		seen := p.seen
		p.mu.Unlock()

		if flushed && len(released) > 0 {
			fmt.Printf("[%s] uplink_pacer_flush packets=%d bytes=%d delay_ms=%d\n",
				p.session.ID, len(released), pacedBytes(released), headDelay.Milliseconds())
		}
		for _, pkt := range released {
			if pkt.write != nil {
				pkt.write()
			}
		}
		for _, ev := range logs {
			fmt.Printf("[%s] uplink_idr_size ts=%d packets=%d bytes=%d burst_ms=%d paced_ms=%d\n",
				p.session.ID, ev.Timestamp, ev.Packets, ev.Bytes, ev.Burst.Milliseconds(), ev.Paced.Milliseconds())
		}
		if seen && (nextStats.IsZero() || !now.Before(nextStats)) {
			// Peak depth during the second, so a short IDR queue is visible
			// after the bucket has already drained it.
			fmt.Printf("[%s] uplink_pacer_queue packets=%d bytes=%d delay_ms=%d\n",
				p.session.ID, peakPackets, peakBytes, peakDelay.Milliseconds())
			peakPackets, peakBytes, peakDelay = 0, 0, 0
			nextStats = now.Add(time.Second)
		}
		if stopped {
			return
		}
		if wait <= 0 || wait > time.Hour {
			wait = time.Hour
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-p.wake:
		case <-timer.C:
		case <-p.ctx.Done():
			p.mu.Lock()
			p.stopped = true
			p.mu.Unlock()
		}
	}
}

func pacedBytes(pkts []pacedVideoPacket) int {
	n := 0
	for _, pkt := range pkts {
		n += pkt.size
	}
	return n
}
