package session

import (
	"fmt"
	"sync"
	"time"
)

const (
	reorderWindowSize = 64
	reorderTimeoutMS  = 40
)

// reorderEntry holds a buffered RTP packet with metadata.
type reorderEntry struct {
	data       []byte
	isKeyframe bool
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
type VideoReorderBuffer struct {
	mu      sync.Mutex
	packets map[uint16]*reorderEntry
	nextSeq uint16
	hasBase bool
	sessID  string
	writeFn func(data []byte, isKeyframe bool)
	timer   *time.Timer
	closed  bool

	// Stats (read under mu)
	Buffered   int64 // packets inserted into buffer (not immediate flush)
	Released   int64 // packets flushed in-order (immediate or consecutive)
	DroppedOld int64 // packets dropped (behind nextSeq)
	TimedOut   int64 // gap-skipped slots (timeout or force-flush)
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

// Push adds a packet to the reorder buffer. It may trigger immediate or
// deferred flushes depending on sequence position relative to nextSeq.
func (b *VideoReorderBuffer) Push(seq uint16, data []byte, isKeyframe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	// Copy data since the caller's buffer may be reused.
	pkt := make([]byte, len(data))
	copy(pkt, data)

	if !b.hasBase {
		// First packet: establish baseline and flush immediately.
		b.nextSeq = seq
		b.hasBase = true
		b.writeFn(pkt, isKeyframe)
		b.Released++
		b.nextSeq = seq + 1
		return
	}

	offset := uint16(seq - b.nextSeq)

	switch {
	case offset == 0:
		// Exactly the next expected packet: flush immediately.
		b.writeFn(pkt, isKeyframe)
		b.Released++
		b.nextSeq++
		b.flushConsecutiveLocked()

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
		b.forceFlushToLocked(newBase)
		if _, exists := b.packets[seq]; !exists {
			b.packets[seq] = &reorderEntry{data: pkt, isKeyframe: isKeyframe}
			b.Buffered++
		}
		b.flushConsecutiveLocked()
		if len(b.packets) > 0 {
			b.resetTimerLocked()
		}

	default:
		// Behind nextSeq (old/duplicate): drop.
		b.DroppedOld++
	}
}

// flushConsecutiveLocked writes all consecutive buffered packets starting from nextSeq.
// Must be called with mu held.
func (b *VideoReorderBuffer) flushConsecutiveLocked() {
	for {
		entry, ok := b.packets[b.nextSeq]
		if !ok {
			break
		}
		delete(b.packets, b.nextSeq)
		b.writeFn(entry.data, entry.isKeyframe)
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
func (b *VideoReorderBuffer) forceFlushToLocked(target uint16) {
	for b.nextSeq != target {
		entry, ok := b.packets[b.nextSeq]
		if ok {
			delete(b.packets, b.nextSeq)
			b.writeFn(entry.data, entry.isKeyframe)
			b.Released++
		} else {
			b.TimedOut++
		}
		b.nextSeq++
	}
}

// resetTimerLocked starts or resets the flush timeout timer.
// Must be called with mu held.
func (b *VideoReorderBuffer) resetTimerLocked() {
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(reorderTimeoutMS*time.Millisecond, b.timeoutFlush)
}

// timeoutFlush is called when the reorder timer fires.
// It skips the gap to the lowest buffered packet and flushes consecutive from there.
func (b *VideoReorderBuffer) timeoutFlush() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed || len(b.packets) == 0 {
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
		return
	}

	// Skip gap: advance to the lowest buffered packet.
	skipped := uint16(lowestSeq - b.nextSeq)
	if skipped > 0 {
		fmt.Printf("[%s] reorder: timeout-skip gap=%d (seq %d..%d)\n",
			b.sessID, skipped, b.nextSeq, lowestSeq-1)
	}
	b.nextSeq = lowestSeq
	b.TimedOut += int64(skipped)

	// Flush consecutive from lowestSeq.
	b.flushConsecutiveLocked()

	// If still have buffered packets, restart timer.
	if len(b.packets) > 0 {
		b.resetTimerLocked()
	}
}

// GetStats returns current reorder buffer statistics.
func (b *VideoReorderBuffer) GetStats() (buffered, released, droppedOld, timedOut int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffered, b.Released, b.DroppedOld, b.TimedOut
}

// Pending returns the number of packets currently buffered (waiting for flush).
func (b *VideoReorderBuffer) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.packets)
}

// Drain flushes all remaining buffered packets and stops the timer.
// Called on session teardown.
func (b *VideoReorderBuffer) Drain() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true

	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}

	// Flush remaining packets in order, skipping gaps.
	for len(b.packets) > 0 {
		entry, ok := b.packets[b.nextSeq]
		if ok {
			delete(b.packets, b.nextSeq)
			b.writeFn(entry.data, entry.isKeyframe)
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
}

// seqBeforeU16 returns true if a comes before b in 16-bit sequence space.
func seqBeforeU16(a, b uint16) bool {
	d := uint16(b - a)
	return d != 0 && d < 0x8000
}
