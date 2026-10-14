package session

import (
	"testing"
	"time"
)

func TestLateSwitchSameStreamDoesNotGate(t *testing.T) {
	sess := newBurstTestSession("same-stream-switch")
	sess.VideoAUNormalizeEnabled = true
	sess.RemoteVideoSSRC = 2081251329
	sess.SIPVideoRTPSource = "203.150.245.39:24800"
	sess.CacheSIPSPS([]byte{0x67, 0x42})
	sess.CacheSIPPPS([]byte{0x68, 0xce})
	now := time.Now()
	sess.NoteOpenedVideoStream(2081251329, now.Add(-30*time.Second))

	decision, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	if decision.Ignore || decision.Reason != SwitchVideoGateActivationSameStream || decision.Generation != 0 {
		t.Fatalf("expected already-satisfied decision, got %+v", decision)
	}
	if activation.Active || activation.Outcome != SwitchVideoGateActivationSameStream {
		t.Fatalf("expected no gate, got %+v", activation)
	}
	if sess.IsSwitchVideoGateActive() {
		t.Fatal("same-stream @switch armed a gate")
	}
	sps, pps, ok := sess.GetSIPCachedSPSPPS()
	if !ok || len(sps) == 0 || len(pps) == 0 {
		t.Fatal("same-stream @switch cleared cached parameter sets")
	}

	dup, dupActivation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(20*time.Second), time.Minute, true)
	if !dup.Ignore || dup.Reason != "duplicate-target" {
		t.Fatalf("expected debounce after the same-stream switch, got %+v", dup)
	}
	if dupActivation.Active {
		t.Fatal("duplicate same-stream switch armed a gate")
	}
}

func TestSwitchDifferentSSRCStillGates(t *testing.T) {
	sess := newBurstTestSession("changed-stream-switch")
	sess.VideoAUNormalizeEnabled = true
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"
	sess.NoteOpenedVideoStream(1111, time.Now().Add(-time.Second))
	sess.RemoteVideoSSRC = 2222
	sess.SIPVideoRTPSource = "203.0.113.10:4010"

	decision, activation := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", time.Now(), time.Minute, true)
	if decision.Ignore || decision.Reason == SwitchVideoGateActivationSameStream || !activation.Active {
		t.Fatalf("changed media must gate, decision=%+v activation=%+v", decision, activation)
	}
}

func TestStalledGateFailsOpen(t *testing.T) {
	now := time.Now()
	healthy := VideoRecoverySummary{Packets: 300, ReorderPending: 0}
	stuck := VideoRecoverySummary{Packets: 300, ReorderPending: 20}

	t.Run("same stream", func(t *testing.T) {
		sess := armedGate(t, "fail-open-same", 2081251329, "203.150.245.39:24800")
		if released, _ := sess.MaybeFailOpenSwitchVideoGate(now.Add(2999*time.Millisecond), healthy); released {
			t.Fatal("released before the same-stream limit")
		}
		if released, _ := sess.MaybeFailOpenSwitchVideoGate(now.Add(3*time.Second), stuck); released {
			t.Fatal("unhealthy RTP must wait for the cap")
		}
		released, reason := sess.MaybeFailOpenSwitchVideoGate(now.Add(3*time.Second), healthy)
		if !released || reason != "same-stream-rtp-healthy" || sess.IsSwitchVideoGateActive() {
			t.Fatalf("expected same-stream fail-open, released=%v reason=%s active=%v", released, reason, sess.IsSwitchVideoGateActive())
		}
		decision := sess.EvaluateSwitchVideoAccessUnit(NormalizedH264AccessUnit{Generation: 0}, now.Add(4*time.Second))
		if !decision.Emit {
			t.Fatalf("frames after fail-open must forward, got %+v", decision)
		}
	})

	t.Run("other stream waits for cap", func(t *testing.T) {
		sess := armedGate(t, "fail-open-cap", 1111, "203.0.113.10:4000")
		sess.RemoteVideoSSRC = 2222
		if released, _ := sess.MaybeFailOpenSwitchVideoGate(now.Add(3*time.Second), healthy); released {
			t.Fatal("different stream must not fail open at 3s")
		}
		released, reason := sess.MaybeFailOpenSwitchVideoGate(now.Add(5*time.Second), stuck)
		if !released || reason != "stall-cap" || sess.IsSwitchVideoGateActive() {
			t.Fatalf("expected stall-cap fail-open, released=%v reason=%s active=%v", released, reason, sess.IsSwitchVideoGateActive())
		}
	})
}

func armedGate(t *testing.T, id string, ssrc uint32, source string) *Session {
	t.Helper()
	sess := newBurstTestSession(id)
	sess.VideoAUNormalizeEnabled = true
	sess.SwitchVideoGateFailOpen = 3 * time.Second
	sess.SwitchVideoGateFailOpenCap = 5 * time.Second
	sess.RemoteVideoSSRC = ssrc
	sess.SIPVideoRTPSource = source
	now := time.Now()
	sess.NoteOpenedVideoStream(ssrc, now)
	if !sess.StartSwitchVideoGate(sess.GetSwitchGeneration(), now, "timestamp-jump") {
		t.Fatal("expected implicit gate to arm")
	}
	return sess
}

func TestBridgedKeyframeRelayed(t *testing.T) {
	caller := newBurstTestSession("caller")
	caller.SetCallInfo("outbound", "sip:00025@pbx.example", "sip:14131@pbx.example", "caller-call")
	agent := newBurstTestSession("agent")
	agent.SetCallInfo("inbound", "sip:00025@pbx.example", "sip:170@pbx.example", "agent-call")
	agent.RecordUplinkKeyframe()
	agent.LastWebRTCPLISent = time.Now()

	now := time.Now()
	peer, ok := RelayBridgedKeyframe([]*Session{caller, agent}, caller, "switch", now)
	if !ok || peer != agent {
		t.Fatalf("expected PLI relayed to the agent, peer=%v ok=%v", peer, ok)
	}
	if _, ok := RelayBridgedKeyframe([]*Session{caller, agent}, caller, "gate-stall", now.Add(time.Second)); ok {
		t.Fatal("expected the second bridged PLI inside 2s to be rate-limited")
	}
	if _, ok := RelayBridgedKeyframe([]*Session{caller, agent}, caller, "keyframe-stale", now.Add(2*time.Second)); !ok {
		t.Fatal("expected a bridged PLI after the rate limit even though the uplink IDR is fresh")
	}
}
