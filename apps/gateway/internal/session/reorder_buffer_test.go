package session

import (
	"sync"
	"testing"
	"time"
)

func TestVideoReorderBuffer_InOrder(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			flushed = append(flushed, seq)
		}
	})

	// Push 10 packets in order
	for i := uint16(100); i < 110; i++ {
		pkt := makeMinimalRTP(i)
		buf.Push(i, pkt, false)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 10 {
		t.Fatalf("expected 10 flushed packets, got %d", len(flushed))
	}
	for i, seq := range flushed {
		expected := uint16(100 + i)
		if seq != expected {
			t.Errorf("flushed[%d] = %d, want %d", i, seq, expected)
		}
	}

	buffered, released, droppedOld, timedOut := buf.GetStats()
	if buffered != 0 {
		t.Errorf("buffered = %d, want 0", buffered)
	}
	if released != 10 {
		t.Errorf("released = %d, want 10", released)
	}
	if droppedOld != 0 {
		t.Errorf("droppedOld = %d, want 0", droppedOld)
	}
	if timedOut != 0 {
		t.Errorf("timedOut = %d, want 0", timedOut)
	}
}

func TestVideoReorderBuffer_ReorderedPacketsDoNotSkip(t *testing.T) {
	var skips []int
	buf := NewVideoReorderBuffer("test", func([]byte, bool) {})
	buf.SetSkipHandler(func(skipped int, _, _ uint16) {
		skips = append(skips, skipped)
	})

	// Sender emitted 100..110 in order; arrival is shuffled but complete.
	order := []uint16{100, 102, 103, 101, 107, 104, 105, 106, 110, 108, 109}
	for _, seq := range order {
		buf.Push(seq, makeMinimalRTP(seq), false)
	}

	if len(skips) != 0 {
		t.Fatalf("reordered-but-complete packets triggered skip %v", skips)
	}
	if _, _, _, timedOut := buf.GetStats(); timedOut != 0 {
		t.Fatalf("timedOut=%d, want 0", timedOut)
	}
	if buf.SkipEventCount() != 0 {
		t.Fatalf("SkipEventCount=%d, want 0", buf.SkipEventCount())
	}
}

func TestVideoReorderBuffer_UnrecoveredGapNotifiesSkip(t *testing.T) {
	skippedCh := make(chan int, 1)
	buf := NewVideoReorderBuffer("test", func([]byte, bool) {})
	buf.SetSkipHandler(func(skipped int, fromSeq, toSeq uint16) {
		if fromSeq != 101 || toSeq != 109 {
			t.Errorf("skip range %d..%d, want 101..109", fromSeq, toSeq)
		}
		select {
		case skippedCh <- skipped:
		default:
		}
	})

	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(110, makeMinimalRTP(110), false)

	select {
	case skipped := <-skippedCh:
		if skipped != 9 {
			t.Fatalf("skipped=%d, want 9", skipped)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for unrecovered gap skip")
	}
	if buf.SkipEventCount() != 1 {
		t.Fatalf("SkipEventCount=%d, want 1", buf.SkipEventCount())
	}
}

func TestVideoReorderBuffer_SimpleReorder(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			flushed = append(flushed, seq)
		}
	})

	// Push packets: 100, 102, 101 (one swap)
	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(102, makeMinimalRTP(102), false) // buffered (waiting for 101)
	buf.Push(101, makeMinimalRTP(101), false) // fills gap, flushes 101 + 102

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 3 {
		t.Fatalf("expected 3 flushed packets, got %d", len(flushed))
	}
	expected := []uint16{100, 101, 102}
	for i, seq := range flushed {
		if seq != expected[i] {
			t.Errorf("flushed[%d] = %d, want %d", i, seq, expected[i])
		}
	}

	buffered, released, _, _ := buf.GetStats()
	if buffered != 1 { // 102 was buffered
		t.Errorf("buffered = %d, want 1", buffered)
	}
	if released != 3 {
		t.Errorf("released = %d, want 3", released)
	}
}

func TestVideoReorderBuffer_OldPacketDrop(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			flushed = append(flushed, seq)
		}
	})

	// Push 100, 101, 102, then push 99 (old/duplicate)
	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(101, makeMinimalRTP(101), false)
	buf.Push(102, makeMinimalRTP(102), false)
	buf.Push(99, makeMinimalRTP(99), false) // should be dropped

	mu.Lock()
	defer mu.Unlock()

	if len(flushed) != 3 {
		t.Fatalf("expected 3 flushed packets, got %d", len(flushed))
	}

	_, _, droppedOld, _ := buf.GetStats()
	if droppedOld != 1 {
		t.Errorf("droppedOld = %d, want 1", droppedOld)
	}
}

func TestVideoReorderBuffer_KeyframeFlag(t *testing.T) {
	var mu sync.Mutex
	var keyframes []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if isKeyframe && len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			keyframes = append(keyframes, seq)
		}
	})

	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(101, makeMinimalRTP(101), true) // keyframe
	buf.Push(102, makeMinimalRTP(102), false)

	mu.Lock()
	defer mu.Unlock()

	if len(keyframes) != 1 {
		t.Fatalf("expected 1 keyframe, got %d", len(keyframes))
	}
	if keyframes[0] != 101 {
		t.Errorf("keyframe seq = %d, want 101", keyframes[0])
	}
}

func TestVideoReorderBuffer_FarAheadForceFlush(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			flushed = append(flushed, seq)
		}
	})

	// Push seq 100, then jump far ahead to 200 (> window size 64)
	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(200, makeMinimalRTP(200), false) // should force-flush gap, buffer 200

	mu.Lock()
	// 100 was immediately flushed. 200 is buffered (waiting for 168..199 range).
	if len(flushed) != 1 || flushed[0] != 100 {
		mu.Unlock()
		t.Fatalf("expected [100] flushed, got %v", flushed)
	}
	mu.Unlock()

	// Now push 201 - doesn't help flush 200 (200 is next expected after gap advance to 168)
	// Push the exact nextSeq sequence to flush 200
	// After far-ahead, nextSeq was advanced to 200 - reorderWindowSize/2 = 168
	// So we need to push 168..200 range. But 200 is already buffered.
	// Drain to verify 200 is retrievable.
	buf.Drain()

	mu.Lock()
	defer mu.Unlock()
	// After drain, both 100 and 200 should be in the output
	found200 := false
	for _, seq := range flushed {
		if seq == 200 {
			found200 = true
		}
	}
	if !found200 {
		t.Errorf("expected 200 to be flushed after drain, got %v", flushed)
	}

	_, _, _, timedOut := buf.GetStats()
	// The gap from 101..167 should have been counted as timed-out slots
	if timedOut < 1 {
		t.Errorf("expected timedOut > 0 for gap skip, got %d", timedOut)
	}
}

func TestVideoReorderBuffer_SeqWrap(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			flushed = append(flushed, seq)
		}
	})

	// Test sequence number wraparound (65534 -> 65535 -> 0 -> 1)
	buf.Push(65534, makeMinimalRTP(65534), false)
	buf.Push(65535, makeMinimalRTP(65535), false)
	buf.Push(0, makeMinimalRTP(0), false)
	buf.Push(1, makeMinimalRTP(1), false)

	mu.Lock()
	defer mu.Unlock()

	expected := []uint16{65534, 65535, 0, 1}
	if len(flushed) != len(expected) {
		t.Fatalf("expected %d flushed, got %d", len(expected), len(flushed))
	}
	for i, seq := range flushed {
		if seq != expected[i] {
			t.Errorf("flushed[%d] = %d, want %d", i, seq, expected[i])
		}
	}
}

func TestVideoReorderBuffer_ResetAcceptsNewMediaGeneration(t *testing.T) {
	var flushed []uint16
	buf := NewVideoReorderBuffer("test", func(data []byte, _ bool) {
		flushed = append(flushed, uint16(data[2])<<8|uint16(data[3]))
	})

	buf.Push(30000, makeMinimalRTP(30000), false)
	buf.Reset()
	buf.Push(1000, makeMinimalRTP(1000), false)

	if len(flushed) != 2 || flushed[0] != 30000 || flushed[1] != 1000 {
		t.Fatalf("expected reset source sequence to establish a new baseline, got %v", flushed)
	}
}

func TestVideoReorderBuffer_Drain(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16

	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		if len(data) >= 4 {
			seq := uint16(data[2])<<8 | uint16(data[3])
			flushed = append(flushed, seq)
		}
	})

	// Push 100, 102 (gap at 101), then drain
	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(102, makeMinimalRTP(102), false) // buffered

	// Before drain, only 100 should be flushed
	mu.Lock()
	if len(flushed) != 1 {
		mu.Unlock()
		t.Fatalf("before drain: expected 1 flushed, got %d", len(flushed))
	}
	mu.Unlock()

	buf.Drain()

	mu.Lock()
	defer mu.Unlock()
	// After drain, 102 should also be flushed
	if len(flushed) != 2 {
		t.Fatalf("after drain: expected 2 flushed, got %d", len(flushed))
	}
}

// makeMinimalRTP creates a minimal valid 12-byte RTP packet with the given sequence number.
func makeMinimalRTP(seq uint16) []byte {
	return []byte{
		0x80, 96, // V=2, PT=96
		byte(seq >> 8), byte(seq), // Sequence number
		0, 0, 0, 0, // Timestamp
		0, 0, 0, 1, // SSRC
	}
}

// Same SSRC, sequence restarted lower (queue-to-agent handoff): after a run
// of in-order "old" packets the buffer must follow the new numbering instead
// of dropping everything until it catches up with nextSeq.
func TestVideoReorderBuffer_RebasesAfterBackwardJump(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16
	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		mu.Lock()
		defer mu.Unlock()
		flushed = append(flushed, uint16(data[2])<<8|uint16(data[3]))
	})

	for seq := uint16(30000); seq < 30010; seq++ {
		buf.Push(seq, makeMinimalRTP(seq), false)
	}
	for seq := uint16(1000); seq < 1000+reorderRebaseRun+5; seq++ {
		buf.Push(seq, makeMinimalRTP(seq), false)
	}

	mu.Lock()
	defer mu.Unlock()
	want := 10 + reorderRebaseRun + 5 // old stream + the whole run + 5 after it
	if len(flushed) != want {
		t.Fatalf("flushed %d packets, want %d: %v", len(flushed), want, flushed)
	}
	for i, seq := range flushed[10:] {
		if seq != uint16(1000+i) {
			t.Fatalf("new numbering must be released in order from 1000, got %v", flushed[10:])
		}
	}
	if buf.Rebases != 1 {
		t.Fatalf("Rebases = %d, want 1", buf.Rebases)
	}
}

// A late in-order burst just behind nextSeq (e.g. retransmissions after the
// gap was skipped) is old data, not a restart.
func TestVideoReorderBuffer_LateBurstNearNextSeqDoesNotRebase(t *testing.T) {
	buf := NewVideoReorderBuffer("test", func([]byte, bool) {})
	for seq := uint16(5000); seq < 5200; seq++ {
		buf.Push(seq, makeMinimalRTP(seq), false)
	}
	for seq := uint16(5100); seq < 5100+2*reorderRebaseRun; seq++ {
		buf.Push(seq, makeMinimalRTP(seq), false)
	}
	if buf.Rebases != 0 {
		t.Fatalf("Rebases = %d, want 0", buf.Rebases)
	}
	_, _, droppedOld, _ := buf.GetStats()
	if droppedOld != int64(2*reorderRebaseRun) {
		t.Fatalf("droppedOld = %d, want %d", droppedOld, 2*reorderRebaseRun)
	}
}

// Scattered late duplicates must keep being dropped.
func TestVideoReorderBuffer_ScatteredOldPacketsDoNotRebase(t *testing.T) {
	buf := NewVideoReorderBuffer("test", func([]byte, bool) {})
	for seq := uint16(500); seq < 600; seq++ {
		buf.Push(seq, makeMinimalRTP(seq), false)
	}
	for i := 0; i < 3*reorderRebaseRun; i++ {
		old := uint16(400 + 2*i) // not consecutive
		buf.Push(old, makeMinimalRTP(old), false)
	}
	if buf.Rebases != 0 {
		t.Fatalf("Rebases = %d, want 0", buf.Rebases)
	}
	_, _, droppedOld, _ := buf.GetStats()
	if droppedOld != int64(3*reorderRebaseRun) {
		t.Fatalf("droppedOld = %d, want %d", droppedOld, 3*reorderRebaseRun)
	}
}

// A batch released by the timeout must reach the writer before a packet
// pushed after it, even while the timeout is still writing.
func TestVideoReorderBuffer_TimeoutBatchIsNotOvertaken(t *testing.T) {
	var mu sync.Mutex
	var flushed []uint16
	firstTimeoutWrite := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	buf := NewVideoReorderBuffer("test", func(data []byte, isKeyframe bool) {
		seq := uint16(data[2])<<8 | uint16(data[3])
		if seq == 102 {
			once.Do(func() {
				close(firstTimeoutWrite)
				<-release // hold the timeout batch mid-write
			})
		}
		mu.Lock()
		flushed = append(flushed, seq)
		mu.Unlock()
	})

	buf.Push(100, makeMinimalRTP(100), false)
	buf.Push(102, makeMinimalRTP(102), false) // 101 lost
	buf.Push(103, makeMinimalRTP(103), false)
	<-firstTimeoutWrite // timeout is writing 102..103

	pushed := make(chan struct{})
	go func() {
		buf.Push(104, makeMinimalRTP(104), false)
		close(pushed)
	}()
	select {
	case <-pushed:
		t.Fatal("Push must wait for the timeout batch to finish writing")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-pushed

	mu.Lock()
	defer mu.Unlock()
	want := []uint16{100, 102, 103, 104}
	if len(flushed) != len(want) {
		t.Fatalf("flushed %v, want %v", flushed, want)
	}
	for i := range want {
		if flushed[i] != want[i] {
			t.Fatalf("flushed %v, want %v", flushed, want)
		}
	}
}
