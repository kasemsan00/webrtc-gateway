package sip

import (
	"errors"
	"testing"
	"time"

	"github.com/pion/rtp"

	"k2-gateway/internal/session"
)

func normalizedVideoAU(generation int, idr, ready bool, packets int) session.NormalizedH264AccessUnit {
	au := session.NormalizedH264AccessUnit{
		Generation:         generation,
		IsIDR:              idr,
		ParameterSetsReady: ready,
		SourceTimestamp:    90000,
	}
	for i := 0; i < packets; i++ {
		au.Packets = append(au.Packets, &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: uint16(100 + i),
				Timestamp:      90000,
				SSRC:           4242,
				Marker:         i == packets-1,
			},
			Payload: []byte{0x65, byte(i + 1)},
		})
	}
	return au
}

func TestWriteNormalizedVideoAccessUnitCommitsGateAfterAllPackets(t *testing.T) {
	now := time.Unix(100, 0)
	sess := &session.Session{ID: "gate-write", VideoAUNormalizeEnabled: true, SwitchGeneration: 3}
	if !sess.StartSwitchVideoGate(3, now, "test") {
		t.Fatal("expected gate start")
	}

	writes := 0
	written := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(3, true, true, 3), now.Add(time.Second), func([]byte) (int, error) {
		writes++
		return 1, nil
	})

	if !written || writes != 3 {
		t.Fatalf("written=%v writes=%d, want true/3", written, writes)
	}
	if sess.IsSwitchVideoGateActive() || sess.SwitchVideoGateAcceptedGeneration != 3 {
		t.Fatalf("gate active=%v accepted=%d", sess.IsSwitchVideoGateActive(), sess.SwitchVideoGateAcceptedGeneration)
	}
}

func TestWriteNormalizedVideoAccessUnitAbortsReservationOnWriteFailure(t *testing.T) {
	now := time.Unix(200, 0)
	sess := &session.Session{ID: "gate-write-fail", VideoAUNormalizeEnabled: true, SwitchGeneration: 4}
	sess.StartSwitchVideoGate(4, now, "test")

	writes := 0
	written := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(4, true, true, 3), now.Add(time.Second), func([]byte) (int, error) {
		writes++
		if writes == 2 {
			return 0, errors.New("track failed")
		}
		return 1, nil
	})

	if written || writes != 2 || !sess.IsSwitchVideoGateActive() || sess.SwitchVideoGateReleasing {
		t.Fatalf("written=%v writes=%d active=%v releasing=%v", written, writes, sess.IsSwitchVideoGateActive(), sess.SwitchVideoGateReleasing)
	}

	writes = 0
	if writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(4, false, true, 1), now.Add(2*time.Second), func([]byte) (int, error) {
		writes++
		return 1, nil
	}) || writes != 0 {
		t.Fatalf("P-frame passed after failed IDR: writes=%d", writes)
	}
}

func TestWriteNormalizedVideoAccessUnitUsesWebRTCEgressSSRC(t *testing.T) {
	now := time.Unix(300, 0)
	sess := &session.Session{ID: "egress-write", VideoAUNormalizeEnabled: true, SwitchGeneration: 1}
	sess.WebRTCVideoEgressSSRC = 7777
	if !sess.StartSwitchVideoGate(1, now, "test") {
		t.Fatal("expected gate start")
	}

	var gotSSRC uint32
	written := writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(1, true, true, 1), now.Add(time.Second), func(b []byte) (int, error) {
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(b); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		gotSSRC = pkt.SSRC
		return len(b), nil
	})
	if !written {
		t.Fatal("expected write success")
	}
	if gotSSRC != 7777 {
		t.Fatalf("expected egress SSRC 7777, got %d", gotSSRC)
	}
}

func TestWriteNormalizedVideoAccessUnitRejectsStaleGenerationAfterRelease(t *testing.T) {
	now := time.Unix(300, 0)
	sess := &session.Session{ID: "gate-stale", VideoAUNormalizeEnabled: true, SwitchGeneration: 5}
	sess.StartSwitchVideoGate(5, now, "test")
	if !writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(5, true, true, 1), now, func([]byte) (int, error) { return 1, nil }) {
		t.Fatal("expected generation 5 release")
	}

	writes := 0
	if writeNormalizedVideoAccessUnit(sess, normalizedVideoAU(4, true, true, 1), now, func([]byte) (int, error) {
		writes++
		return 1, nil
	}) || writes != 0 {
		t.Fatalf("stale generation passed after release: writes=%d", writes)
	}
}
