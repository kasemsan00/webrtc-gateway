package session

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	reorderWindowSize       = 64
	defaultReorderTimeoutMS = 60
	maxReorderPacing        = 10 * time.Millisecond
)

var reorderTimeout = loadReorderTimeout()

func loadReorderTimeout() time.Duration {
	timeoutMS := defaultReorderTimeoutMS
	envValue := os.Getenv("SIP_VIDEO_REORDER_TIMEOUT_MS")
	if envValue != "" {
		parsed, err := strconv.Atoi(envValue)
		if err == nil {
			if parsed < 20 {
				parsed = 20
			}
			if parsed > 200 {
				parsed = 200
			}
			timeoutMS = parsed
		}
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

// reorderEntry holds a buffered RTP packet with metadata.
type reorderEntry struct {
	data       []byte
	isKeyframe bool
}

type reorderWrite struct {
	data       []byte
	isKeyframe bool
	paced      bool
}

type reorderSkip struct {
	count int
	from  uint16
	to    uint16
}

// VideoReorderBuffer provides a small bounded reorder window for SIP->WebRTC video RTP.
// Out-of-order packets are held briefly and flushed in sequence order, reducing
// H.264 decoder poisoning from network jitter without adding significant latency.
//
// Design:
//   - Window of 64 packets (~2 frames at 30fps, ~100ms)
//   - 25ms timeout to flush when a gap isn't filled
//   - Packets behind nextSeq are dropped (old/duplicate)
//   - Packets too far ahead force-flush the gap
//   - Skip callbacks fire only for unrecovered gaps (after timeout/force-flush)
type VideoReorderBuffer struct {
	mu      sync.Mutex
	emitMu  sync.Mutex
	packets map[uint16]*reorderEntry
	nextSeq uint16
	hasBase bool
	sessID  string
	writeFn func(data []byte, isKeyframe bool)
	onSkip  func(skipped int, fromSeq, toSeq uint16)
	pace    time.Duration
	timer   *time.Timer
	closed  bool

	// Stats (read under mu)
	Buffered   int64 // packets inserted into buffer (not immediate flush)
	Released   int64 // packets flushed in-order (immediate or consecutive)
	DroppedOld int64 // packets dropped (behind nextSeq)
	TimedOut   int64 // gap-skipped slots (timeout or force-flush)
	SkipEvents int64 // unrecovered gap events (timeout or force-flush)
}

// NewVideoReorderBuffer creates a reorder buffer for SIP->WebRTC video.
// writeFn is called for each packet in sequence order when flushed.
func NewVideoReorderBuffer(sessID string, writeFn func(data []byte, isKeyframe bool)) *VideoReorderBuffer {
	return &VideoReorderBuffer{
		packets: make(map[uint16]*reorderEntry, reorderWindowSize),
		sessID:  sessID,
		writeFn: writeFn,
	}
}

// SetSkipHandler registers a callback for unrecovered sequence gaps. It is
// invoked without the buffer lock held, after the skip has been applied.
func (b *VideoReorderBuffer) SetSkipHandler(fn func(skipped int, fromSeq, toSeq uint16)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onSkip = fn
}

// SetPacing spaces consecutive packets released from a filled gap. 0 disables.
func (b *VideoReorderBuffer) SetPacing(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d < 0 {
		d = 0
	}
	if d > maxReorderPacing {
		d = maxReorderPacing
	}
	b.pace = d
}

func (b *VideoReorderBuffer) emit(writes []reorderWrite, skips []reorderSkip) {
	if len(writes) > 0 {
		burst := false
		for _, w := range writes {
			if w.paced {
				burst = true
				break
			}
		}
		b.emitMu.Lock()
		for i, w := range writes {
			if i > 0 && burst && b.pace > 0 {
				time.Sleep(b.pace)
			}
			if b.writeFn != nil {
				b.writeFn(w.data, w.isKeyframe)
			}
		}
		b.emitMu.Unlock()
	}
	if b.onSkip == nil {
		return
	}
	for _, s := range skips {
		if s.count > 0 {
			b.onSkip(s.count, s.from, s.to)
		}
	}
}

// Push adds a packet to the reorder buffer. It may trigger immediate or
// deferred flushes depending on sequence position relative to nextSeq.
func (b *VideoReorderBuffer) Push(seq uint16, data []byte, isKeyframe bool) {
	var writes []reorderWrite
	var skips []reorderSkip

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}

	// Copy data since the caller's buffer may be reused.
	pkt := make([]byte, len(data))
	copy(pkt, data)

	if !b.hasBase {
		// First packet: establish baseline and flush immediately.
		b.nextSeq = seq + 1
		b.hasBase = true
		b.Released++
		writes = append(writes, reorderWrite{data: pkt, isKeyframe: isKeyframe})
		b.mu.Unlock()
		b.emit(writes, nil)
		return
	}

	offset := uint16(seq - b.nextSeq)

	switch {
	case offset == 0:
		// Exactly the next expected packet: flush immediately, then any that were waiting.
		writes = append(writes, reorderWrite{data: pkt, isKeyframe: isKeyframe})
		b.Released++
		b.nextSeq++
		b.flushConsecutiveLocked(&writes)

	case offset < 0x8000 && offset < reorderWindowSize:
		// Ahead of nextSeq but within window: buffer it.
		if _, exists := b.packets[seq]; !exists {
			b.packets[seq] = &reorderEntry{data: pkt, isKeyframe: isKeyframe}
			b.Buffered++
			b.resetTimerLocked()
		}

	case offset < 0x8000 && offset >= reorderWindowSize:
		// Too far ahead: force-flush the gap so the buffer stays bounded.
		newBase := seq - reorderWindowSize/2
		b.forceFlushToLocked(newBase, &writes, &skips)
		if _, exists := b.packets[seq]; !exists {
			b.packets[seq] = &reorderEntry{data: pkt, isKeyframe: isKeyframe}
			b.Buffered++
		}
		b.flushConsecutiveLocked(&writes)
		if len(b.packets) > 0 {
			b.resetTimerLocked()
		}

	default:
		// Behind nextSeq (old/duplicate): drop.
		b.DroppedOld++
	}
	b.mu.Unlock()
	b.emit(writes, skips)
}

// flushConsecutiveLocked appends all consecutive buffered packets starting from nextSeq.
// Must be called with mu held.
func (b *VideoReorderBuffer) flushConsecutiveLocked(writes *[]reorderWrite) {
	for {
		entry, ok := b.packets[b.nextSeq]
		if !ok {
			break
		}
		delete(b.packets, b.nextSeq)
		*writes = append(*writes, reorderWrite{data: entry.data, isKeyframe: entry.isKeyframe, paced: true})
		b.Released++
		b.nextSeq++
	}
	// Cancel timer if buffer is now empty.
	if len(b.packets) == 0 && b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}

// forceFlushToLocked advances nextSeq to target, flushing any buffered packets
// in between and counting gaps as timed-out.
func (b *VideoReorderBuffer) forceFlushToLocked(target uint16, writes *[]reorderWrite, skips *[]reorderSkip) {
	start := b.nextSeq
	skipped := 0
	for b.nextSeq != target {
		entry, ok := b.packets[b.nextSeq]
		if ok {
			delete(b.packets, b.nextSeq)
			*writes = append(*writes, reorderWrite{data: entry.data, isKeyframe: entry.isKeyframe, paced: true})
			b.Released++
		} else {
			skipped++
			b.TimedOut++
		}
		b.nextSeq++
	}
	if skipped > 0 {
		b.SkipEvents++
		*skips = append(*skips, reorderSkip{count: skipped, from: start, to: target - 1})
	}
}

// resetTimerLocked starts or resets the flush timeout timer.
// Must be called with mu held.
func (b *VideoReorderBuffer) resetTimerLocked() {
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(reorderTimeout, b.timeoutFlush)
}

// timeoutFlush is called when the reorder timer fires.
// It skips the gap to the lowest buffered packet and flushes consecutive from there.
func (b *VideoReorderBuffer) timeoutFlush() {
	var writes []reorderWrite
	var skips []reorderSkip

	b.mu.Lock()
	if b.closed || len(b.packets) == 0 {
		b.mu.Unlock()
		return
	}

	// Find the lowest buffered seq that is ahead of nextSeq.
	var lowestSeq uint16
	found := false
	for seq := range b.packets {
		offset := uint16(seq - b.nextSeq)
		if offset < 0x8000 { // ahead of nextSeq
			if !found || seqBeforeU16(seq, lowestSeq) {
				lowestSeq = seq
				found = true
			}
		}
	}

	if !found {
		// All buffered packets are behind nextSeq (stale); clear them.
		for seq := range b.packets {
			delete(b.packets, seq)
			b.DroppedOld++
		}
		b.mu.Unlock()
		return
	}

	// Skip gap: advance to the lowest buffered packet.
	skipped := uint16(lowestSeq - b.nextSeq)
	if skipped > 0 {
		fmt.Printf("[%s] reorder: timeout-skip gap=%d (seq %d..%d)\n",
			b.sessID, skipped, b.nextSeq, lowestSeq-1)
		skips = append(skips, reorderSkip{count: int(skipped), from: b.nextSeq, to: lowestSeq - 1})
		b.SkipEvents++
	}
	b.nextSeq = lowestSeq
	b.TimedOut += int64(skipped)

	// Flush consecutive from lowestSeq.
	b.flushConsecutiveLocked(&writes)

	// If still have buffered packets, restart timer.
	if len(b.packets) > 0 {
		b.resetTimerLocked()
	}
	b.mu.Unlock()
	b.emit(writes, skips)
}

// GetStats returns current reorder buffer statistics.
func (b *VideoReorderBuffer) GetStats() (buffered, released, droppedOld, timedOut int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffered, b.Released, b.DroppedOld, b.TimedOut
}

// SkipEventCount returns unrecovered gap events after reordering.
func (b *VideoReorderBuffer) SkipEventCount() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.SkipEvents
}

// Pending returns the number of packets currently buffered (waiting for flush).
func (b *VideoReorderBuffer) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.packets)
}

// Reset drops packets pending from the previous SIP media generation and lets
// the next packet establish a fresh source sequence baseline.
func (b *VideoReorderBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	for seq := range b.packets {
		delete(b.packets, seq)
	}
	b.hasBase = false
}

// Drain flushes all remaining buffered packets and stops the timer.
// Called on session teardown.
func (b *VideoReorderBuffer) Drain() {
	var writes []reorderWrite

	b.mu.Lock()
	b.closed = true
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}

	// Collect remaining packets in order, skipping gaps. Teardown does not
	// pace or emit skip callbacks — the session is going away.
	for len(b.packets) > 0 {
		entry, ok := b.packets[b.nextSeq]
		if ok {
			delete(b.packets, b.nextSeq)
			writes = append(writes, reorderWrite{data: entry.data, isKeyframe: entry.isKeyframe})
			b.Released++
		}
		b.nextSeq++

		// Safety: check if we've advanced past all remaining packets.
		if !ok && len(b.packets) > 0 {
			anyAhead := false
			for seq := range b.packets {
				offset := uint16(seq - b.nextSeq)
				if offset < 0x8000 {
					anyAhead = true
					break
				}
			}
			if !anyAhead {
				// Remaining are all stale; discard.
				for seq := range b.packets {
					delete(b.packets, seq)
					b.DroppedOld++
				}
				break
			}
		}
	}
	b.mu.Unlock()
	b.emit(writes, nil)
}

// seqBeforeU16 returns true if a comes before b in 16-bit sequence space.
func seqBeforeU16(a, b uint16) bool {
	d := uint16(b - a)
	return d != 0 && d < 0x8000
}
