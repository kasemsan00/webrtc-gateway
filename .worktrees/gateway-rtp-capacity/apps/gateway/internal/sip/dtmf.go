package sip

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/pion/rtp"

	"k2-gateway/internal/session"
)

// DTMF constants for RFC 2833
const (
	DTMFPayloadType     = 101                    // Payload type for telephone-event (configured in SDP)
	DTMFClockRate       = 8000                   // Clock rate for telephone-event
	DTMFVolume          = 10                     // Volume level (0-63 dBm0, 10 is typical)
	DTMFDuration        = 160                    // Duration per packet (20ms at 8kHz = 160 samples)
	DTMFTotalDuration   = 1600                   // Total tone duration (200ms at 8kHz)
	DTMFInterDigitDelay = 100 * time.Millisecond // Delay between digits
)

// DTMFEvent represents an RFC 2833 DTMF event payload
// RFC 2833 Section 3.5:
// 0                   1                   2                   3
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |     event     |E|R| volume    |          duration             |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type DTMFEvent struct {
	Event    uint8  // DTMF digit (0-15)
	End      bool   // End bit (E)
	Volume   uint8  // Volume (0-63)
	Duration uint16 // Duration in timestamp units
}

// Marshal serializes the DTMF event to a 4-byte payload
func (d *DTMFEvent) Marshal() []byte {
	payload := make([]byte, 4)
	payload[0] = d.Event

	// Byte 1: E(1) R(1) Volume(6)
	payload[1] = d.Volume & 0x3F // Volume is 6 bits
	if d.End {
		payload[1] |= 0x80 // Set End bit
	}
	// R bit is reserved (always 0)

	// Bytes 2-3: Duration (big-endian)
	binary.BigEndian.PutUint16(payload[2:4], d.Duration)

	return payload
}

// Unmarshal parses a 4-byte DTMF event payload
func (d *DTMFEvent) Unmarshal(payload []byte) error {
	if len(payload) < 4 {
		return fmt.Errorf("DTMF payload too short: %d bytes", len(payload))
	}

	d.Event = payload[0]
	d.End = (payload[1] & 0x80) != 0
	d.Volume = payload[1] & 0x3F
	d.Duration = binary.BigEndian.Uint16(payload[2:4])

	return nil
}

// GetDigitName returns the human-readable name for a DTMF event
func GetDigitName(event uint8) string {
	digits := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "*", "#", "A", "B", "C", "D"}
	if int(event) < len(digits) {
		return digits[event]
	}
	return fmt.Sprintf("Unknown(%d)", event)
}

// GetEventID returns the RFC 2833 event ID for a DTMF digit character
func GetEventID(digit rune) (uint8, error) {
	switch digit {
	case '0':
		return 0, nil
	case '1':
		return 1, nil
	case '2':
		return 2, nil
	case '3':
		return 3, nil
	case '4':
		return 4, nil
	case '5':
		return 5, nil
	case '6':
		return 6, nil
	case '7':
		return 7, nil
	case '8':
		return 8, nil
	case '9':
		return 9, nil
	case '*':
		return 10, nil
	case '#':
		return 11, nil
	case 'A', 'a':
		return 12, nil
	case 'B', 'b':
		return 13, nil
	case 'C', 'c':
		return 14, nil
	case 'D', 'd':
		return 15, nil
	default:
		return 0, fmt.Errorf("invalid DTMF digit: %c", digit)
	}
}

// SendDTMFTone sends a single DTMF tone via RFC 2833 RTP events
// This sends the proper sequence: start packets, duration packets, and end packets
func SendDTMFTone(sess *session.Session, digit rune) error {
	eventID, err := GetEventID(digit)
	if err != nil {
		return err
	}

	// Get audio RTP connection info using accessor method
	conn, destAddr, ssrc := sess.GetAudioRTPInfo()

	if conn == nil || destAddr == nil {
		return fmt.Errorf("no audio RTP connection available")
	}

	// Initialize SSRC if needed
	if ssrc == 0 {
		ssrc = 0x12345678
		sess.SetAudioSSRC(ssrc)
	}

	// Calculate total packets we'll send: numPackets for tone + 3 for end
	numPackets := DTMFTotalDuration / DTMFDuration // 10 packets for 200ms
	totalPackets := uint16(numPackets + 3)

	// Get and increment sequence number atomically
	baseSeq := sess.GetAndIncrementAudioSeq(totalPackets)
	baseTimestamp := sess.GetAudioTimestamp()

	fmt.Printf("📞 [%s] Sending DTMF tone '%c' (event=%d) to %s\n", sess.ID, digit, eventID, destAddr)

	// Send start packet (Marker=true, End=false, Duration starts at 0)
	// Then send continuation packets with increasing duration
	// Finally send end packets (End=true) 3 times for redundancy

	packetInterval := 20 * time.Millisecond // 20ms per packet

	for i := 0; i < numPackets; i++ {
		isFirst := (i == 0)
		duration := uint16((i + 1) * DTMFDuration)

		dtmfEvent := &DTMFEvent{
			Event:    eventID,
			End:      false,
			Volume:   DTMFVolume,
			Duration: duration,
		}

		// Create RTP packet
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Padding:        false,
				Extension:      false,
				Marker:         isFirst, // Marker bit only on first packet
				PayloadType:    DTMFPayloadType,
				SequenceNumber: baseSeq + uint16(i) + 1,
				Timestamp:      baseTimestamp, // Same timestamp for entire event
				SSRC:           ssrc,
			},
			Payload: dtmfEvent.Marshal(),
		}

		data, err := packet.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal DTMF packet: %w", err)
		}

		if _, err := conn.WriteToUDP(data, destAddr); err != nil {
			return fmt.Errorf("failed to send DTMF packet: %w", err)
		}

		time.Sleep(packetInterval)
	}

	// Send end packets (3x for redundancy per RFC 2833)
	finalDuration := uint16(numPackets * DTMFDuration)
	for i := 0; i < 3; i++ {
		dtmfEvent := &DTMFEvent{
			Event:    eventID,
			End:      true,
			Volume:   DTMFVolume,
			Duration: finalDuration,
		}

		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Padding:        false,
				Extension:      false,
				Marker:         false,
				PayloadType:    DTMFPayloadType,
				SequenceNumber: baseSeq + uint16(numPackets) + uint16(i) + 1,
				Timestamp:      baseTimestamp, // Same timestamp
				SSRC:           ssrc,
			},
			Payload: dtmfEvent.Marshal(),
		}

		data, err := packet.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal DTMF end packet: %w", err)
		}

		if _, err := conn.WriteToUDP(data, destAddr); err != nil {
			return fmt.Errorf("failed to send DTMF end packet: %w", err)
		}

		// Small delay between end packets
		time.Sleep(5 * time.Millisecond)
	}

	fmt.Printf("📞 [%s] DTMF tone '%c' sent successfully (%d packets + 3 end packets)\n",
		sess.ID, digit, numPackets)

	return nil
}

// ParseDTMFFromRTP checks if an RTP packet contains DTMF and extracts the event
// Returns the DTMF event and whether this is an end packet
func ParseDTMFFromRTP(packet *rtp.Packet) (*DTMFEvent, bool, error) {
	if packet.Header.PayloadType != DTMFPayloadType {
		return nil, false, nil // Not a DTMF packet
	}

	if len(packet.Payload) < 4 {
		return nil, false, fmt.Errorf("DTMF payload too short: %d bytes", len(packet.Payload))
	}

	event := &DTMFEvent{}
	if err := event.Unmarshal(packet.Payload); err != nil {
		return nil, false, err
	}

	return event, event.End, nil
}
