package session

import (
	"testing"
	"time"
)

func testPacer() *videoPacer {
	return newVideoPacer(videoPacerConfigFrom(
		defaultUplinkVideoPacerBitsPerSec,
		defaultUplinkVideoPacerBurstPkts,
		defaultUplinkVideoPacerMaxQueue,
	))
}

func enqueueAt(p *videoPacer, seq, size int, at time.Time, keyframe bool, ts uint32) {
	enqueueMarked(p, seq, size, at, keyframe, ts, false)
}

func enqueueMarked(p *videoPacer, seq, size int, at time.Time, keyframe bool, ts uint32, marker bool) {
	p.Enqueue(pacedVideoPacket{
		seq:      seq,
		size:     size,
		enqueued: at,
		keyframe: keyframe,
		marker:   marker,
		rtpTS:    ts,
	})
}

func drainPacer(t *testing.T, p *videoPacer, now time.Time) (time.Time, []pacedVideoPacket, []idrSizeEvent) {
	t.Helper()
	var released []pacedVideoPacket
	var logs []idrSizeEvent
	for i := 0; i < 10000; i++ {
		batch, flushed, wait := p.Release(now)
		released = append(released, batch...)
		logs = append(logs, p.TakeIDRLogs()...)
		if p.queued() == 0 {
			return now, released, logs
		}
		if flushed {
			t.Fatalf("pacer flushed while draining with %d still queued", p.queued())
		}
		if wait <= 0 {
			t.Fatalf("pacer returned non-positive wait with %d queued", p.queued())
		}
		now = now.Add(wait)
	}
	t.Fatal("pacer did not drain")
	return now, nil, nil
}

func TestVideoPacerPreservesOrderAndDropsNothing(t *testing.T) {
	p := testPacer()
	start := time.Unix(0, 0)
	const n = 40
	const size = 1060
	for i := 0; i < n; i++ {
		enqueueAt(p, i, size, start, i%5 == 0, 100)
	}
	_, released, _ := drainPacer(t, p, start)
	if len(released) != n {
		t.Fatalf("released %d packets, want %d", len(released), n)
	}
	for i, pkt := range released {
		if pkt.seq != i {
			t.Fatalf("packet %d has seq %d", i, pkt.seq)
		}
		if pkt.size != size {
			t.Fatalf("packet %d size %d", i, pkt.size)
		}
		if pkt.released.Before(pkt.enqueued) {
			t.Fatalf("packet %d released before it was enqueued", i)
		}
	}
	if p.queued() != 0 {
		t.Fatalf("queue still holds %d packets", p.queued())
	}
}

func TestVideoPacerSpreadsIDRUnderRate(t *testing.T) {
	p := testPacer()
	start := time.Unix(0, 0)
	const n = 40
	const size = 1060
	for i := 0; i < n; i++ {
		enqueueMarked(p, i, size, start, true, 9001, i == n-1)
	}
	_, released, logs := drainPacer(t, p, start)
	if len(released) != n {
		t.Fatalf("released %d packets, want %d", len(released), n)
	}
	burst := 0
	for _, pkt := range released {
		if pkt.released.Equal(start) {
			burst++
		}
	}
	if burst != defaultUplinkVideoPacerBurstPkts {
		t.Fatalf("immediate burst released %d packets, want %d", burst, defaultUplinkVideoPacerBurstPkts)
	}
	paced := released[n-1].released.Sub(released[0].released)
	if paced < 80*time.Millisecond || paced > 150*time.Millisecond {
		t.Fatalf("IDR release span %s, want 80-150ms", paced)
	}
	pacedBytes := (n - defaultUplinkVideoPacerBurstPkts) * size
	elapsed := released[n-1].released.Sub(released[defaultUplinkVideoPacerBurstPkts-1].released)
	cfg := videoPacerConfigFrom(defaultUplinkVideoPacerBitsPerSec, defaultUplinkVideoPacerBurstPkts, defaultUplinkVideoPacerMaxQueue)
	minElapsed := time.Duration(float64(pacedBytes) / cfg.BytesPerSec * float64(time.Second) * 0.9)
	if elapsed < minElapsed {
		t.Fatalf("paced tail took %s, faster than %s at %d bps", elapsed, minElapsed, defaultUplinkVideoPacerBitsPerSec)
	}
	if len(logs) != 1 {
		t.Fatalf("IDR logs %d, want 1", len(logs))
	}
	if logs[0].Packets != n || logs[0].Bytes != n*size {
		t.Fatalf("IDR log packets=%d bytes=%d, want %d and %d", logs[0].Packets, logs[0].Bytes, n, n*size)
	}
	if logs[0].Burst != 0 {
		t.Fatalf("burst %s, want 0 for a single enqueue instant", logs[0].Burst)
	}
	if logs[0].Paced < 80*time.Millisecond || logs[0].Paced > 150*time.Millisecond {
		t.Fatalf("logged paced span %s, want 80-150ms", logs[0].Paced)
	}
}

func TestVideoPacerFlushesOverdueQueueWithoutDropping(t *testing.T) {
	p := testPacer()
	start := time.Unix(0, 0)
	const n = 30
	for i := 0; i < n; i++ {
		enqueueMarked(p, i, 1060, start, true, 7, i == n-1)
	}
	released, flushed, _ := p.Release(start.Add(600 * time.Millisecond))
	logs := p.TakeIDRLogs()
	if !flushed {
		t.Fatal("expected flush once the head packet waited 600ms")
	}
	if len(released) != n {
		t.Fatalf("flush released %d packets, want %d", len(released), n)
	}
	if p.queued() != 0 {
		t.Fatalf("queue still holds %d packets after flush", p.queued())
	}
	for i, pkt := range released {
		if pkt.seq != i {
			t.Fatalf("flushed packet %d has seq %d", i, pkt.seq)
		}
	}
	if len(logs) != 1 || logs[0].Packets != n {
		t.Fatalf("flush IDR log %+v, want one event of %d packets", logs, n)
	}
}

func TestVideoPacerReportsArrivalBurstSeparately(t *testing.T) {
	p := testPacer()
	start := time.Unix(0, 0)
	const size = 1060
	for i := 0; i < 11; i++ {
		enqueueMarked(p, i, size, start, true, 42, false)
	}
	for i := 11; i < 40; i++ {
		enqueueMarked(p, i, size, start.Add(time.Duration(i-10)*time.Millisecond), true, 42, i == 39)
	}
	_, released, logs := drainPacer(t, p, start)
	if len(released) != 40 {
		t.Fatalf("released %d, want 40", len(released))
	}
	for i, pkt := range released {
		if pkt.seq != i {
			t.Fatalf("packet %d has seq %d", i, pkt.seq)
		}
	}
	if len(logs) != 1 {
		t.Fatalf("logs %d, want 1", len(logs))
	}
	if logs[0].Burst != 29*time.Millisecond {
		t.Fatalf("burst_ms source span %s, want 29ms", logs[0].Burst)
	}
	if logs[0].Packets != 40 || logs[0].Bytes != 40*size {
		t.Fatalf("log packets=%d bytes=%d", logs[0].Packets, logs[0].Bytes)
	}
}

func TestVideoPacerDoesNotDelayAnIDRByTheFlushWindow(t *testing.T) {
	p := testPacer()
	start := time.Unix(0, 0)
	const size = 1060
	for i := 0; i < 8; i++ {
		enqueueAt(p, i, size, start, false, 1)
	}
	for i := 8; i < 48; i++ {
		enqueueMarked(p, i, size, start, true, 2, i == 47)
	}
	finished, released, logs := drainPacer(t, p, start)
	if len(released) != 48 {
		t.Fatalf("released %d, want 48", len(released))
	}
	for i, pkt := range released {
		if pkt.seq != i {
			t.Fatalf("packet %d has seq %d", i, pkt.seq)
		}
	}
	if finished.Sub(start) >= 500*time.Millisecond {
		t.Fatalf("IDR was held until %s", finished.Sub(start))
	}
	if len(logs) != 1 || logs[0].Packets != 40 {
		t.Fatalf("IDR log %+v", logs)
	}
}

func TestVideoPacerKeepsOnePictureAcrossDrainBursts(t *testing.T) {
	p := testPacer()
	start := time.Unix(0, 0)
	const size = 1072
	for i := 0; i < 3; i++ {
		enqueueMarked(p, i, size, start, true, 90000, false)
	}
	_, released, logs := drainPacer(t, p, start)
	if len(released) != 3 {
		t.Fatalf("first burst released %d, want 3", len(released))
	}
	if len(logs) != 0 {
		t.Fatalf("picture logged before its marker slice: %+v", logs)
	}
	for i := 3; i < 9; i++ {
		enqueueMarked(p, i, size, start.Add(20*time.Millisecond), true, 90000, i == 8)
	}
	_, released, logs = drainPacer(t, p, start.Add(20*time.Millisecond))
	if len(released) != 6 {
		t.Fatalf("second burst released %d, want 6", len(released))
	}
	for i, pkt := range released {
		if pkt.seq != i+3 {
			t.Fatalf("packet %d has seq %d", i, pkt.seq)
		}
	}
	if len(logs) != 1 {
		t.Fatalf("logs %d, want 1", len(logs))
	}
	if logs[0].Timestamp != 90000 || logs[0].Packets != 9 || logs[0].Bytes != 9*size {
		t.Fatalf("IDR log %+v, want ts=90000 packets=9", logs[0])
	}
}

func TestUplinkVideoPacerDisabledByDefaultOnBareSession(t *testing.T) {
	s := &Session{}
	if s.paceUplinkVideo() {
		t.Fatal("a session that did not load config must not pace")
	}
}
