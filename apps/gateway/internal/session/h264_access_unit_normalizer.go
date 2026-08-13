package session

import (
	"sync"
	"time"

	"github.com/pion/rtp"
)

const (
	defaultH264AUMaxPackets       = 2048
	defaultH264AUMaxBytes         = 4 * 1024 * 1024
	defaultH264AUMaxAge           = 500 * time.Millisecond
	defaultH264TimestampStep      = uint32(3000)
	maxReasonableH264TimestampGap = uint32(900000)
	// Reassembled parameter sets are reinjected as one RTP payload, so keep
	// them below the common WebRTC path-MTU-safe payload size.
	maxCachedFUAParameterSetPayload = 1200
	// Prefix cached SPS/PPS onto the first post-@switch IDRs even when the AU
	// already carries parameter sets. Decoders leaving queue still-video often
	// ignore in-band SPS unless it is the first NAL after the generation change.
	defaultSwitchParameterSetPrefixCount = 2
)

// H264AccessUnitNormalizerConfig bounds memory and latency while an H.264
// access unit is assembled from reordered RTP packets.
type H264AccessUnitNormalizerConfig struct {
	MaxPackets int
	MaxBytes   int
	MaxAge     time.Duration
}

// NormalizedH264AccessUnit is emitted only after the source marker and H.264
// fragmentation have proved that the whole access unit is present.
type NormalizedH264AccessUnit struct {
	Packets               []*rtp.Packet
	IsIDR                 bool
	InjectedParameterSets bool
	ParameterSetsReady    bool
	Generation            int
	SourceTimestamp       uint32
}

// H264AccessUnitNormalizer converts a reordered SIP H.264 RTP stream into
// complete, decoder-safe access units with continuous outbound RTP numbering.
type H264AccessUnitNormalizer struct {
	mu     sync.Mutex
	config H264AccessUnitNormalizerConfig
	emit   func(NormalizedH264AccessUnit)
	now    func() time.Time

	haveAU      bool
	sourceTS    uint32
	startedAt   time.Time
	packets     []*rtp.Packet
	bytes       int
	invalid     bool
	overflow    bool
	haveLastSeq bool
	lastSeq     uint16
	fuOpen      bool
	fuNALType   uint8

	haveOutput               bool
	nextSeq                  uint16
	outputTS                 uint32
	lastSourceTS             uint32
	stepTimestampAfterSwitch bool

	cachedSPS                        []byte
	cachedPPS                        []byte
	generation                       int
	forceParameterSetPrefixRemaining int

	droppedIncomplete uint64
	droppedOverflow   uint64
	emitted           uint64
}

type H264AccessUnitNormalizerStats struct {
	Emitted           uint64
	DroppedIncomplete uint64
	DroppedOverflow   uint64
	PendingPackets    int
}

func NewH264AccessUnitNormalizer(config H264AccessUnitNormalizerConfig, emit func(NormalizedH264AccessUnit)) *H264AccessUnitNormalizer {
	if config.MaxPackets <= 0 {
		config.MaxPackets = defaultH264AUMaxPackets
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = defaultH264AUMaxBytes
	}
	if config.MaxAge <= 0 {
		config.MaxAge = defaultH264AUMaxAge
	}
	return &H264AccessUnitNormalizer{config: config, emit: emit, now: time.Now}
}

// SetParameterSets seeds the normalizer with the most recent complete SIP-side
// SPS/PPS. The values are copied so callers may safely reuse their buffers.
func (n *H264AccessUnitNormalizer) SetParameterSets(sps, pps []byte) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if isSingleNALType(sps, 7) {
		n.cachedSPS = append(n.cachedSPS[:0], sps...)
	}
	if isSingleNALType(pps, 8) {
		n.cachedPPS = append(n.cachedPPS[:0], pps...)
	}
}

// Push accepts packets after sequence reordering. Emission is synchronous, but
// the callback runs without the normalizer lock held.
func (n *H264AccessUnitNormalizer) Push(packet *rtp.Packet) {
	if packet == nil || len(packet.Payload) == 0 {
		return
	}

	var result *NormalizedH264AccessUnit
	n.mu.Lock()
	n.pushLocked(packet, &result)
	n.mu.Unlock()
	if result != nil && n.emit != nil {
		n.emit(*result)
	}
}

func (n *H264AccessUnitNormalizer) pushLocked(packet *rtp.Packet, result **NormalizedH264AccessUnit) {
	now := n.now()
	if n.haveAU && (packet.Timestamp != n.sourceTS || now.Sub(n.startedAt) > n.config.MaxAge) {
		n.dropCurrentLocked(false)
	}
	if !n.haveAU {
		n.startLocked(packet.Timestamp, now)
	}

	if n.haveLastSeq && packet.SequenceNumber != n.lastSeq+1 {
		n.invalid = true
	}
	n.haveLastSeq = true
	n.lastSeq = packet.SequenceNumber

	n.bytes += packet.MarshalSize()
	if len(n.packets) >= n.config.MaxPackets || n.bytes > n.config.MaxBytes {
		n.invalid = true
		n.overflow = true
		n.packets = nil
	}
	if !n.invalid {
		cloned := packet.Clone()
		n.packets = append(n.packets, cloned)
		if !n.observePayloadLocked(cloned.Payload) {
			n.invalid = true
		}
	}

	if !packet.Marker {
		return
	}
	if n.invalid || n.fuOpen || len(n.packets) == 0 {
		n.dropCurrentLocked(n.overflow)
		return
	}

	au := n.finishLocked()
	*result = &au
}

func (n *H264AccessUnitNormalizer) startLocked(timestamp uint32, now time.Time) {
	n.haveAU = true
	n.sourceTS = timestamp
	n.startedAt = now
	n.packets = nil
	n.bytes = 0
	n.invalid = false
	n.overflow = false
	n.haveLastSeq = false
	n.fuOpen = false
	n.fuNALType = 0
}

func (n *H264AccessUnitNormalizer) observePayloadLocked(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	nalType := payload[0] & 0x1f
	switch nalType {
	case 1, 5, 6, 7, 8, 9, 10, 11, 12:
		return !n.fuOpen
	case 24: // STAP-A
		if n.fuOpen {
			return false
		}
		_, _, _, ok := inspectSTAPA(payload)
		return ok
	case 28: // FU-A
		if len(payload) < 3 {
			return false
		}
		start := payload[1]&0x80 != 0
		end := payload[1]&0x40 != 0
		fragmentType := payload[1] & 0x1f
		if fragmentType == 0 || fragmentType >= 24 || (start && end) {
			return false
		}
		if start {
			if n.fuOpen {
				return false
			}
			n.fuOpen = true
			n.fuNALType = fragmentType
			return true
		}
		if !n.fuOpen || n.fuNALType != fragmentType {
			return false
		}
		if end {
			n.fuOpen = false
			n.fuNALType = 0
		}
		return true
	default:
		return false
	}
}

func (n *H264AccessUnitNormalizer) finishLocked() NormalizedH264AccessUnit {
	packets := n.packets
	sourceTimestamp := n.sourceTS
	hasSPS, hasPPS, isIDR := inspectAccessUnit(packets)
	n.cachedSPS = updateCachedNALFromPackets(n.cachedSPS, packets, 7)
	n.cachedPPS = updateCachedNALFromPackets(n.cachedPPS, packets, 8)
	parameterSetsReady := len(n.cachedSPS) > 0 && len(n.cachedPPS) > 0

	injected := false
	if isIDR {
		forcePrefix := n.forceParameterSetPrefixRemaining > 0 && parameterSetsReady
		prefix := make([]*rtp.Packet, 0, 2)
		template := packets[0]
		if (!hasSPS || forcePrefix) && len(n.cachedSPS) > 0 {
			prefix = append(prefix, parameterSetPacket(template, n.cachedSPS))
		}
		if (!hasPPS || forcePrefix) && len(n.cachedPPS) > 0 {
			prefix = append(prefix, parameterSetPacket(template, n.cachedPPS))
		}
		if len(prefix) > 0 {
			packets = append(prefix, packets...)
			injected = true
			if forcePrefix {
				n.forceParameterSetPrefixRemaining--
			}
		}
	}

	// Outbound RTP sequence/timestamp are assigned later by NumberAccessUnit
	// only after the switch video gate accepts the access unit.
	n.resetCurrentLocked()
	return NormalizedH264AccessUnit{
		Packets: packets, IsIDR: isIDR, InjectedParameterSets: injected,
		ParameterSetsReady: parameterSetsReady, Generation: n.generation, SourceTimestamp: sourceTimestamp,
	}
}

// NumberAccessUnit assigns the continuous outbound RTP sequence and timestamp
// after the caller has decided to emit the access unit. Gate-rejected AUs must
// not call this, or they burn sequence numbers the decoder will NACK as loss.
func (n *H264AccessUnitNormalizer) NumberAccessUnit(au *NormalizedH264AccessUnit) {
	if au == nil || len(au.Packets) == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	ts := n.mapTimestampLocked(au.SourceTimestamp, au.Packets[0].SequenceNumber)
	n.applyOutboundRTPLocked(au.Packets, ts)
	n.emitted++
}

func (n *H264AccessUnitNormalizer) mapTimestampLocked(source uint32, firstSeq uint16) uint32 {
	if !n.haveOutput {
		n.haveOutput = true
		n.nextSeq = firstSeq
		n.outputTS = source
		n.lastSourceTS = source
		n.stepTimestampAfterSwitch = false
		return n.outputTS
	}
	if n.stepTimestampAfterSwitch {
		n.outputTS += defaultH264TimestampStep
		n.lastSourceTS = source
		n.stepTimestampAfterSwitch = false
		return n.outputTS
	}
	delta := source - n.lastSourceTS
	if int32(delta) <= 0 || delta > maxReasonableH264TimestampGap {
		delta = defaultH264TimestampStep
	}
	n.outputTS += delta
	n.lastSourceTS = source
	return n.outputTS
}

func (n *H264AccessUnitNormalizer) applyOutboundRTPLocked(packets []*rtp.Packet, ts uint32) {
	for _, packet := range packets {
		if packet == nil {
			continue
		}
		packet.SequenceNumber = n.nextSeq
		packet.Timestamp = ts
		n.nextSeq++
	}
}

func (n *H264AccessUnitNormalizer) dropCurrentLocked(overflow bool) {
	if n.haveAU {
		if overflow {
			n.droppedOverflow++
		} else {
			n.droppedIncomplete++
		}
	}
	n.resetCurrentLocked()
}

func (n *H264AccessUnitNormalizer) resetCurrentLocked() {
	n.haveAU = false
	n.packets = nil
	n.bytes = 0
	n.invalid = false
	n.overflow = false
	n.haveLastSeq = false
	n.fuOpen = false
	n.fuNALType = 0
}

func (n *H264AccessUnitNormalizer) Stats() H264AccessUnitNormalizerStats {
	n.mu.Lock()
	defer n.mu.Unlock()
	return H264AccessUnitNormalizerStats{
		Emitted: n.emitted, DroppedIncomplete: n.droppedIncomplete,
		DroppedOverflow: n.droppedOverflow, PendingPackets: len(n.packets),
	}
}

// Drain intentionally drops an unfinished access unit: no marker means it is
// not safe to deliver to strict browser decoders.
func (n *H264AccessUnitNormalizer) Drain() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dropCurrentLocked(false)
}

// ResetSource drops any partial source access unit while preserving cached
// parameter sets and the continuous outbound sequence/timestamp timeline.
func (n *H264AccessUnitNormalizer) ResetSource() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dropCurrentLocked(false)
}

// RewriteForReplay assigns a new continuous outbound sequence and timestamp to
// a cached access unit so a decoder-facing IDR replay is not dropped as a duplicate.
func (n *H264AccessUnitNormalizer) RewriteForReplay(packets []*rtp.Packet) []*rtp.Packet {
	if len(packets) == 0 {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.haveOutput {
		n.outputTS = n.mapTimestampLocked(packets[0].Timestamp, packets[0].SequenceNumber)
	} else {
		n.outputTS += defaultH264TimestampStep
		n.stepTimestampAfterSwitch = false
	}

	out := make([]*rtp.Packet, 0, len(packets))
	for i, packet := range packets {
		if packet == nil {
			continue
		}
		clone := packet.Clone()
		if clone == nil {
			header := packet.Header
			clone = &rtp.Packet{Header: header, Payload: append([]byte(nil), packet.Payload...)}
		} else {
			clone.Payload = append([]byte(nil), packet.Payload...)
		}
		clone.Marker = i == len(packets)-1
		out = append(out, clone)
	}
	n.applyOutboundRTPLocked(out, n.outputTS)
	return out
}

// ResetForSwitch drops source-specific state while preserving the continuous
// outbound sequence/timestamp timeline.
func (n *H264AccessUnitNormalizer) ResetForSwitch(generation int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dropCurrentLocked(false)
	n.cachedSPS = nil
	n.cachedPPS = nil
	n.generation = generation
	n.stepTimestampAfterSwitch = true
	n.forceParameterSetPrefixRemaining = defaultSwitchParameterSetPrefixCount
}

func inspectAccessUnit(packets []*rtp.Packet) (hasSPS, hasPPS, isIDR bool) {
	for _, packet := range packets {
		payload := packet.Payload
		if len(payload) == 0 {
			continue
		}
		switch payload[0] & 0x1f {
		case 5:
			isIDR = true
		case 7:
			hasSPS = true
		case 8:
			hasPPS = true
		case 24:
			sps, pps, idr, _ := inspectSTAPA(payload)
			hasSPS = hasSPS || sps
			hasPPS = hasPPS || pps
			isIDR = isIDR || idr
		case 28:
			if len(payload) > 1 && payload[1]&0x80 != 0 {
				switch payload[1] & 0x1f {
				case 5:
					isIDR = true
				case 7:
					hasSPS = true
				case 8:
					hasPPS = true
				}
			}
		}
	}
	return
}

func inspectSTAPA(payload []byte) (hasSPS, hasPPS, isIDR, valid bool) {
	if len(payload) < 4 || payload[0]&0x1f != 24 {
		return false, false, false, false
	}
	for offset := 1; offset < len(payload); {
		if offset+2 > len(payload) {
			return false, false, false, false
		}
		size := int(payload[offset])<<8 | int(payload[offset+1])
		offset += 2
		if size == 0 || offset+size > len(payload) {
			return false, false, false, false
		}
		switch payload[offset] & 0x1f {
		case 5:
			isIDR = true
		case 7:
			hasSPS = true
		case 8:
			hasPPS = true
		}
		offset += size
	}
	return hasSPS, hasPPS, isIDR, true
}

func updateCachedNALFromPackets(current []byte, packets []*rtp.Packet, nalType byte) []byte {
	nal, present, cacheable := cacheableNALFromPackets(packets, nalType)
	if !present {
		return current
	}
	if !cacheable {
		return nil
	}
	return append(current[:0], nal...)
}

func cacheableNALFromPackets(packets []*rtp.Packet, nalType byte) (nal []byte, present, cacheable bool) {
	for i, packet := range packets {
		if isSingleNALType(packet.Payload, nalType) {
			nal = packet.Payload
			present = true
			cacheable = true
			continue
		}
		if len(packet.Payload) > 0 && packet.Payload[0]&0x1f == 24 {
			for offset := 1; offset+2 <= len(packet.Payload); {
				size := int(packet.Payload[offset])<<8 | int(packet.Payload[offset+1])
				offset += 2
				if size == 0 || offset+size > len(packet.Payload) {
					break
				}
				candidate := packet.Payload[offset : offset+size]
				if isSingleNALType(candidate, nalType) {
					nal = candidate
					present = true
					cacheable = true
					break
				}
				offset += size
			}
		}
		payload := packet.Payload
		if len(payload) >= 2 && payload[0]&0x1f == 28 && payload[1]&0x80 != 0 && payload[1]&0x1f == nalType {
			present = true
			reassembled := reassembleFUAParameterSet(packets[i:], nalType)
			if len(reassembled) == 0 {
				return nil, true, false
			}
			nal = reassembled
			cacheable = true
		}
	}
	return nal, present, cacheable
}

func reassembleFUAParameterSet(packets []*rtp.Packet, nalType byte) []byte {
	if len(packets) == 0 || len(packets[0].Payload) < 3 {
		return nil
	}
	first := packets[0]
	if first.Payload[0]&0x1f != 28 || first.Payload[1]&0x80 == 0 || first.Payload[1]&0x40 != 0 || first.Payload[1]&0x1f != nalType {
		return nil
	}

	nal := make([]byte, 1, maxCachedFUAParameterSetPayload)
	nal[0] = first.Payload[0]&0xe0 | nalType
	nal = append(nal, first.Payload[2:]...)
	if len(nal) > maxCachedFUAParameterSetPayload {
		return nil
	}
	lastSeq := first.SequenceNumber
	for _, packet := range packets[1:] {
		payload := packet.Payload
		if len(payload) < 3 || packet.SequenceNumber != lastSeq+1 || payload[0]&0x1f != 28 ||
			payload[0]&0xe0 != first.Payload[0]&0xe0 || payload[1]&0x80 != 0 || payload[1]&0x1f != nalType {
			return nil
		}
		if len(nal)+len(payload)-2 > maxCachedFUAParameterSetPayload {
			return nil
		}
		nal = append(nal, payload[2:]...)
		lastSeq = packet.SequenceNumber
		if payload[1]&0x40 != 0 {
			return nal
		}
	}
	return nil
}

func isSingleNALType(payload []byte, nalType byte) bool {
	return len(payload) > 1 && payload[0]&0x1f == nalType
}

func parameterSetPacket(template *rtp.Packet, payload []byte) *rtp.Packet {
	packet := template.Clone()
	packet.Marker = false
	packet.Payload = append([]byte(nil), payload...)
	return packet
}
