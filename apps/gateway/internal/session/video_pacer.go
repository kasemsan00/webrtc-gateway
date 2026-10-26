package session

import "time"

const (
	defaultUplinkVideoPacerBitsPerSec = 3_500_000
	defaultUplinkVideoPacerBurstPkts  = 4
	defaultUplinkVideoPacerMaxQueue   = 500 * time.Millisecond
	defaultUplinkIDRPace              = 225 * time.Millisecond
	defaultUplinkFrameCatchUp         = 400 * time.Millisecond
	defaultUplinkFrameCatchUpMax      = 12
	uplinkVideoClockRate              = 90000
	minUplinkFrameInterval            = 8 * time.Millisecond
	maxUplinkFrameInterval            = 100 * time.Millisecond
	// uplinkVideoPacerNominalBytes is the size used to turn the burst
	// allowance (packets) into token-bucket capacity. A ~1060 byte
	// single-NAL packet still fits four times inside a 4-packet burst.
	uplinkVideoPacerNominalBytes = 1200
)

type videoPacerConfig struct {
	BytesPerSec      float64
	BurstBytes       float64
	MaxDelay         time.Duration
	IDRTarget        time.Duration
	IDRPackets       int
	CatchUpCap       time.Duration
	MaxCatchUpFrames int
}

func videoPacerConfigFrom(bitsPerSec, burstPackets int, maxDelay, idrTarget time.Duration) videoPacerConfig {
	if bitsPerSec <= 0 {
		bitsPerSec = defaultUplinkVideoPacerBitsPerSec
	}
	if burstPackets <= 0 {
		burstPackets = defaultUplinkVideoPacerBurstPkts
	}
	if maxDelay <= 0 {
		maxDelay = defaultUplinkVideoPacerMaxQueue
	}
	idrPackets := TypicalUplinkIDRPackets
	if idrPackets < 2 {
		idrPackets = 2
	}
	catchUp := defaultUplinkFrameCatchUp
	if maxDelay > 0 && catchUp > maxDelay {
		catchUp = maxDelay
	}
	return videoPacerConfig{
		BytesPerSec:      float64(bitsPerSec) / 8,
		BurstBytes:       float64(burstPackets * uplinkVideoPacerNominalBytes),
		MaxDelay:         maxDelay,
		IDRTarget:        idrTarget,
		IDRPackets:       idrPackets,
		CatchUpCap:       catchUp,
		MaxCatchUpFrames: defaultUplinkFrameCatchUpMax,
	}
}

type pacedVideoPacket struct {
	seq         int
	size        int
	enqueued    time.Time
	released    time.Time
	keyframe    bool
	marker      bool
	rtpTS       uint32
	packetCount int
	write       func()
}

type idrAccount struct {
	ts           uint32
	packets      int
	bytes        int
	pending      int
	closed       bool
	firstEnqueue time.Time
	lastEnqueue  time.Time
	firstRelease time.Time
	lastRelease  time.Time
}

type idrSizeEvent struct {
	Timestamp uint32
	Packets   int
	Bytes     int
	Burst     time.Duration
	Paced     time.Duration
}

type videoPacer struct {
	cfg             videoPacerConfig
	tokens          float64
	refilled        time.Time
	primed          bool
	queue           []pacedVideoPacket
	head            int
	idrs            map[uint32]*idrAccount
	logs            []idrSizeEvent
	dropped         int
	releaseSeen     bool
	lastReleaseTS   uint32
	lastReleaseAt   time.Time
	catchUpDeadline time.Time
}

func newVideoPacer(cfg videoPacerConfig) *videoPacer {
	return &videoPacer{cfg: cfg, idrs: make(map[uint32]*idrAccount)}
}

func (p *videoPacer) queued() int {
	return len(p.queue) - p.head
}

func (p *videoPacer) Enqueue(pkt pacedVideoPacket) {
	if pkt.size <= 0 {
		pkt.size = 1
	}
	if pkt.enqueued.IsZero() {
		pkt.enqueued = time.Now()
	}
	p.queue = append(p.queue, pkt)
	if !pkt.keyframe {
		return
	}
	// A picture can arrive in several bursts. Closing the account every time
	// pending hits zero splits one multi-slice IDR into several logs.
	p.finishIdleIDR(pkt.rtpTS)
	acc := p.idrs[pkt.rtpTS]
	if acc == nil {
		acc = &idrAccount{ts: pkt.rtpTS, firstEnqueue: pkt.enqueued}
		p.idrs[pkt.rtpTS] = acc
	}
	if pkt.marker {
		acc.closed = true
	}
	acc.packets++
	acc.bytes += pkt.size
	acc.pending++
	acc.lastEnqueue = pkt.enqueued
}

func (p *videoPacer) Release(now time.Time) (released []pacedVideoPacket, flushed bool, wait time.Duration) {
	if p.queued() == 0 {
		p.refill(now)
		return nil, false, time.Hour
	}
	if delay := now.Sub(p.queue[p.head].enqueued); delay >= p.cfg.MaxDelay {
		released = p.takeAll(now)
		p.tokens = 0
		p.refilled = now
		p.primed = true
		return released, true, time.Hour
	}
	p.refill(now)
	for p.queued() > 0 {
		p.dropCatchUpIfNeeded()
		if p.queued() == 0 {
			break
		}
		pkt := p.queue[p.head]
		if idrWait := p.idrPaceWait(pkt, now); idrWait > 0 {
			return released, false, idrWait
		}
		if frameWait := p.framePaceWait(pkt, now); frameWait > 0 {
			return released, false, frameWait
		}
		need := float64(pkt.size)
		if need > p.cfg.BurstBytes {
			need = p.cfg.BurstBytes
		}
		if p.tokens < need {
			deficit := need - p.tokens
			wait = time.Duration(deficit / p.cfg.BytesPerSec * float64(time.Second))
			if wait < time.Millisecond {
				wait = time.Millisecond
			}
			return released, false, wait
		}
		p.tokens -= float64(pkt.size)
		if p.tokens < 0 {
			p.tokens = 0
		}
		p.head++
		pkt.released = now
		released = append(released, pkt)
		p.noteRelease(pkt, now)
	}
	p.compact()
	return released, false, time.Hour
}

func (p *videoPacer) takeAll(now time.Time) []pacedVideoPacket {
	released := make([]pacedVideoPacket, 0, p.queued())
	for p.queued() > 0 {
		pkt := p.queue[p.head]
		p.head++
		pkt.released = now
		released = append(released, pkt)
		p.noteRelease(pkt, now)
	}
	p.compact()
	return released
}

func (p *videoPacer) idrPaceWait(pkt pacedVideoPacket, now time.Time) time.Duration {
	if p.cfg.IDRTarget <= 0 || !pkt.keyframe {
		return 0
	}
	acc := p.idrs[pkt.rtpTS]
	if acc == nil || acc.firstRelease.IsZero() {
		return 0
	}
	sent := acc.packets - acc.pending
	if sent <= 0 {
		return 0
	}
	denom := p.cfg.IDRPackets - 1
	if denom < 1 {
		denom = 1
	}
	due := acc.firstRelease.Add(time.Duration(float64(sent) / float64(denom) * float64(p.cfg.IDRTarget)))
	if !now.Before(due) {
		return 0
	}
	wait := due.Sub(now)
	if wait < time.Millisecond {
		return time.Millisecond
	}
	return wait
}

func (p *videoPacer) refill(now time.Time) {
	if !p.primed {
		p.tokens = p.cfg.BurstBytes
		p.refilled = now
		p.primed = true
		return
	}
	if now.Before(p.refilled) {
		p.refilled = now
		return
	}
	elapsed := now.Sub(p.refilled).Seconds()
	p.tokens += elapsed * p.cfg.BytesPerSec
	if p.tokens > p.cfg.BurstBytes {
		p.tokens = p.cfg.BurstBytes
	}
	p.refilled = now
}

func rtpTicksToDuration(delta uint32) time.Duration {
	return time.Duration(float64(delta) / float64(uplinkVideoClockRate) * float64(time.Second))
}

func clampFrameInterval(interval time.Duration) time.Duration {
	if interval < minUplinkFrameInterval {
		return minUplinkFrameInterval
	}
	if interval > maxUplinkFrameInterval {
		return maxUplinkFrameInterval
	}
	return interval
}

func (p *videoPacer) framePaceWait(pkt pacedVideoPacket, now time.Time) time.Duration {
	if pkt.keyframe || !p.releaseSeen {
		return 0
	}
	if pkt.rtpTS == p.lastReleaseTS {
		return 0
	}
	interval := clampFrameInterval(rtpTicksToDuration(pkt.rtpTS - p.lastReleaseTS))
	interval = p.compressInterval(interval, now)
	due := p.lastReleaseAt.Add(interval)
	if !now.Before(due) {
		return 0
	}
	wait := due.Sub(now)
	if wait < time.Millisecond {
		return time.Millisecond
	}
	return wait
}

func (p *videoPacer) compressInterval(interval time.Duration, now time.Time) time.Duration {
	if p.cfg.CatchUpCap <= 0 {
		return interval
	}
	remain := p.remainingRTPDuration()
	if remain <= p.cfg.CatchUpCap && p.catchUpDeadline.IsZero() {
		return interval
	}
	frames, _ := p.queuedFrameStats()
	if frames < 1 {
		return interval
	}
	if p.catchUpDeadline.IsZero() {
		p.catchUpDeadline = now.Add(p.cfg.CatchUpCap)
	}
	left := p.catchUpDeadline.Sub(now)
	if left <= 0 {
		return minUplinkFrameInterval
	}
	scaled := left / time.Duration(frames)
	if scaled < minUplinkFrameInterval {
		return minUplinkFrameInterval
	}
	if scaled > interval {
		return interval
	}
	return scaled
}

func (p *videoPacer) remainingRTPDuration() time.Duration {
	if !p.releaseSeen || p.queued() == 0 {
		return 0
	}
	var maxTS uint32
	seen := false
	for i := p.head; i < len(p.queue); i++ {
		ts := p.queue[i].rtpTS
		if !seen || int32(ts-maxTS) > 0 {
			maxTS = ts
			seen = true
		}
	}
	if !seen {
		return 0
	}
	return rtpTicksToDuration(maxTS - p.lastReleaseTS)
}

func (p *videoPacer) dropCatchUpIfNeeded() {
	maxFrames := p.cfg.MaxCatchUpFrames
	if maxFrames <= 0 {
		return
	}
	frames, hasLaterKeyframe := p.queuedFrameStats()
	if frames <= maxFrames || !hasLaterKeyframe {
		return
	}
	for p.queued() > 0 && !p.queue[p.head].keyframe {
		p.head++
		p.dropped++
	}
	p.compact()
}

func (p *videoPacer) queuedFrameStats() (frames int, hasLaterKeyframe bool) {
	if p.queued() == 0 {
		return 0, false
	}
	var prev uint32
	prevSet := false
	for i := p.head; i < len(p.queue); i++ {
		pkt := p.queue[i]
		if pkt.keyframe && (i > p.head || !p.queue[p.head].keyframe) {
			hasLaterKeyframe = true
		}
		if !prevSet || pkt.rtpTS != prev {
			frames++
			prev = pkt.rtpTS
			prevSet = true
		}
	}
	return frames, hasLaterKeyframe
}

func (p *videoPacer) TakeDropped() int {
	n := p.dropped
	p.dropped = 0
	return n
}

func (p *videoPacer) noteRelease(pkt pacedVideoPacket, now time.Time) {
	p.lastReleaseAt = now
	p.lastReleaseTS = pkt.rtpTS
	p.releaseSeen = true
	if pkt.keyframe {
		p.catchUpDeadline = time.Time{}
	}
	if !pkt.keyframe {
		return
	}
	acc := p.idrs[pkt.rtpTS]
	if acc == nil || acc.pending == 0 {
		return
	}
	if acc.firstRelease.IsZero() {
		acc.firstRelease = now
	}
	acc.lastRelease = now
	acc.pending--
	if acc.pending > 0 || !acc.closed {
		return
	}
	p.logIDR(acc)
	delete(p.idrs, pkt.rtpTS)
}

// finishIdleIDR logs a picture that already saw its marker and has drained,
// once a later timestamp starts. An unclosed account is a picture whose last
// slice is still in the hold; logging it would split one IDR into several
// uplink_idr_size lines.
func (p *videoPacer) finishIdleIDR(except uint32) {
	for ts, acc := range p.idrs {
		if ts == except || acc == nil || acc.pending > 0 || !acc.closed {
			continue
		}
		p.logIDR(acc)
		delete(p.idrs, ts)
	}
}

func (p *videoPacer) logIDR(acc *idrAccount) {
	if acc == nil || acc.packets == 0 {
		return
	}
	p.logs = append(p.logs, idrSizeEvent{
		Timestamp: acc.ts,
		Packets:   acc.packets,
		Bytes:     acc.bytes,
		Burst:     acc.lastEnqueue.Sub(acc.firstEnqueue),
		Paced:     acc.lastRelease.Sub(acc.firstRelease),
	})
}

func (p *videoPacer) TakeIDRLogs() []idrSizeEvent {
	if len(p.logs) == 0 {
		return nil
	}
	out := p.logs
	p.logs = nil
	return out
}

func (p *videoPacer) QueueStats(now time.Time) (packets, bytes int, delay time.Duration) {
	for i := p.head; i < len(p.queue); i++ {
		packets++
		bytes += p.queue[i].size
	}
	if packets == 0 {
		return 0, 0, 0
	}
	delay = now.Sub(p.queue[p.head].enqueued)
	if delay < 0 {
		delay = 0
	}
	return packets, bytes, delay
}

func (p *videoPacer) compact() {
	if p.head == 0 {
		return
	}
	if p.head == len(p.queue) {
		p.queue = nil
		p.head = 0
		p.catchUpDeadline = time.Time{}
		return
	}
	if p.head > 32 {
		p.queue = append([]pacedVideoPacket(nil), p.queue[p.head:]...)
		p.head = 0
	}
}
