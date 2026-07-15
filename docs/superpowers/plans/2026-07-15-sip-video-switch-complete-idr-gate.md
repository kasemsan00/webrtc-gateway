# SIP Video Switch Complete-IDR Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Prevent block corruption and incorrect colors when SIP video switches from queue media to Linphone by withholding the new stream until the gateway has a complete IDR and fresh SPS/PPS for the active switch generation.

**Architecture:** Extend the existing H.264 access-unit normalizer with generation and parameter-set readiness metadata, then place a session-scoped gate after normalization and before VideoTrack.Write. A real @switch starts the gate; the RTP path resets reorder, partial-AU, and parameter-set state on generation change; only a complete new-generation IDR can release video. Audio and public signaling remain unchanged.

**Tech Stack:** Go 1.26, Pion RTP/WebRTC v4, existing session mutex/state, Go testing, SIP/RTP recovery and diagnostics.

---

## File Structure

- Create apps/gateway/internal/session/switch_video_gate.go: complete-IDR state machine and bounded diagnostics.
- Create apps/gateway/internal/session/switch_video_gate_test.go: gate and pipeline tests.
- Modify apps/gateway/internal/session/session.go: gate state fields only.
- Modify apps/gateway/internal/session/h264_access_unit_normalizer.go: generation metadata, fresh SPS/PPS readiness, switch reset.
- Modify apps/gateway/internal/session/h264_access_unit_normalizer_test.go: switch reset and continuity tests.
- Modify apps/gateway/internal/session/media_endpoints.go and media_endpoints_test.go: clear SIP-side parameter sets without affecting the opposite direction.
- Modify apps/gateway/internal/sip/handlers.go and handlers_switch_test.go: start the gate only for an accepted real switch.
- Modify apps/gateway/internal/sip/rtp.go: reset source state before processing a new generation and gate normalized AUs before track writes.
- Modify apps/gateway/internal/session/video_recovery_test.go: prove recovery expiry cannot fail open.
- Modify docs/gateway/troubleshooting.md and docs/gateway/config-reference.md: operational guidance.

## Task 1: Make Normalized H.264 AUs Generation-Aware

**Files:**
- Modify: apps/gateway/internal/session/h264_access_unit_normalizer.go:26-66,216-253,312-318
- Test: apps/gateway/internal/session/h264_access_unit_normalizer_test.go

- [ ] **Step 1: Write failing tests for switch reset and fresh parameter sets**

Add:

~~~go
func TestH264AccessUnitNormalizerResetForSwitchClearsParameterSets(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})

	n.Push(h264Packet(10, 3000, false, []byte{0x67, 0x42, 0x00, 0x1f}))
	n.Push(h264Packet(11, 3000, false, []byte{0x68, 0xce, 0x06, 0xe2}))
	n.Push(h264Packet(12, 3000, true, []byte{0x65, 0x01}))
	lastOldSeq := emitted[0].Packets[len(emitted[0].Packets)-1].SequenceNumber

	n.ResetForSwitch(7)
	n.Push(h264Packet(100, 90000, true, []byte{0x65, 0x02}))

	got := emitted[len(emitted)-1]
	if got.Generation != 7 || got.ParameterSetsReady {
		t.Fatalf("expected generation 7 without fresh parameter sets, got %+v", got)
	}
	if got.Packets[0].SequenceNumber != lastOldSeq+1 {
		t.Fatalf("expected continuous outbound sequence, got %d after %d", got.Packets[0].SequenceNumber, lastOldSeq)
	}
}

func TestH264AccessUnitNormalizerResetForSwitchAcceptsFreshParameterSets(t *testing.T) {
	var emitted []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		emitted = append(emitted, au)
	})
	n.ResetForSwitch(8)
	n.Push(h264Packet(200, 93000, false, []byte{0x67, 0x64, 0x00, 0x1f}))
	n.Push(h264Packet(201, 93000, false, []byte{0x68, 0xee, 0x3c, 0x80}))
	n.Push(h264Packet(202, 93000, true, []byte{0x65, 0x03}))

	if len(emitted) != 1 || emitted[0].Generation != 8 ||
		!emitted[0].ParameterSetsReady || !emitted[0].IsIDR {
		t.Fatalf("expected ready generation-8 IDR, got %+v", emitted)
	}
}
~~~

- [ ] **Step 2: Run tests and confirm the expected compile failure**

From apps/gateway:

~~~powershell
go test ./internal/session -run "TestH264AccessUnitNormalizerResetForSwitch" -count=1
~~~

Expected: build failure because Generation, ParameterSetsReady, and ResetForSwitch do not exist.

- [ ] **Step 3: Add metadata and the switch reset**

Extend NormalizedH264AccessUnit:

~~~go
type NormalizedH264AccessUnit struct {
	Packets               []*rtp.Packet
	IsIDR                 bool
	InjectedParameterSets bool
	ParameterSetsReady    bool
	SourceTimestamp       uint32
	Generation            int
}
~~~

Add generation to H264AccessUnitNormalizer. In finishLocked, calculate readiness after caching current AU parameter sets:

~~~go
parameterSetsReady := len(n.cachedSPS) > 0 && len(n.cachedPPS) > 0

return NormalizedH264AccessUnit{
	Packets:               packets,
	IsIDR:                 isIDR,
	InjectedParameterSets: injected,
	ParameterSetsReady:    parameterSetsReady,
	SourceTimestamp:       sourceTimestamp,
	Generation:            n.generation,
}
~~~

Add:

~~~go
// ResetForSwitch drops the partial AU and source parameter sets while keeping
// the rewritten WebRTC sequence/timestamp timeline continuous.
func (n *H264AccessUnitNormalizer) ResetForSwitch(generation int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dropCurrentLocked(false)
	n.cachedSPS = nil
	n.cachedPPS = nil
	n.generation = generation
}
~~~

Keep ResetSource unchanged for non-switch SSRC resets.

- [ ] **Step 4: Run all normalizer tests**

~~~powershell
go test ./internal/session -run "TestH264AccessUnitNormalizer" -count=1
~~~

Expected: PASS, including incomplete FU-A, injection, overflow, and timestamp continuity.

- [ ] **Step 5: Commit**

~~~powershell
git add apps/gateway/internal/session/h264_access_unit_normalizer.go apps/gateway/internal/session/h264_access_unit_normalizer_test.go
git commit -m "feat(gateway): track H264 switch generations"
~~~

## Task 2: Add the Session Complete-IDR Gate

**Files:**
- Create: apps/gateway/internal/session/switch_video_gate.go
- Create: apps/gateway/internal/session/switch_video_gate_test.go
- Modify: apps/gateway/internal/session/session.go:98-172

- [ ] **Step 1: Write failing gate tests**

Create the helper and core transitions:

~~~go
func readySwitchAU(generation int, idr, parameterSets bool) NormalizedH264AccessUnit {
	return NormalizedH264AccessUnit{
		Generation:         generation,
		IsIDR:              idr,
		ParameterSetsReady: parameterSets,
		Packets: []*rtp.Packet{{
			Header: rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 10, Timestamp: 3000, Marker: true},
			Payload: []byte{0x65, 0x01},
		}},
	}
}

func TestSwitchVideoGateReleasesOnlyCompleteReadyIDR(t *testing.T) {
	sess := &Session{ID: "gate-release", VideoAUNormalizeEnabled: true, SwitchGeneration: 4}
	now := time.Unix(100, 0)
	sess.StartSwitchVideoGate(now, "switch")

	for _, au := range []NormalizedH264AccessUnit{
		readySwitchAU(4, false, true),
		readySwitchAU(4, true, false),
		readySwitchAU(3, true, true),
	} {
		decision := sess.EvaluateSwitchVideoAccessUnit(au, now.Add(time.Second))
		if decision.Emit || !sess.IsSwitchVideoGateActive() {
			t.Fatalf("unsafe AU released gate: au=%+v decision=%+v", au, decision)
		}
	}

	decision := sess.EvaluateSwitchVideoAccessUnit(
		readySwitchAU(4, true, true), now.Add(1500*time.Millisecond),
	)
	if !decision.Emit || !decision.Released || sess.IsSwitchVideoGateActive() {
		t.Fatalf("expected complete ready IDR release, got %+v", decision)
	}
}

func TestSwitchVideoGateRemainsClosedAfterRecoveryDeadline(t *testing.T) {
	sess := &Session{ID: "gate-timeout", VideoAUNormalizeEnabled: true, SwitchGeneration: 5}
	now := time.Unix(200, 0)
	sess.StartSwitchVideoGate(now, "switch")

	decision := sess.EvaluateSwitchVideoAccessUnit(
		readySwitchAU(5, false, true), now.Add(30*time.Second),
	)
	if decision.Emit || !sess.IsSwitchVideoGateActive() {
		t.Fatalf("expected fail-closed gate after deadline, got %+v", decision)
	}
}

func TestSwitchVideoGateRejectsStaleGeneration(t *testing.T) {
	sess := &Session{ID: "gate-generation", VideoAUNormalizeEnabled: true, SwitchGeneration: 7}
	now := time.Unix(300, 0)
	sess.StartSwitchVideoGate(now, "switch")

	if got := sess.EvaluateSwitchVideoAccessUnit(readySwitchAU(6, true, true), now); got.Emit {
		t.Fatalf("stale generation released gate: %+v", got)
	}
	if got := sess.EvaluateSwitchVideoAccessUnit(readySwitchAU(7, true, true), now); !got.Emit || !got.Released {
		t.Fatalf("active generation did not release: %+v", got)
	}
}

func TestSwitchVideoGateUsesGenerationWhenRTPengineKeepsSSRC(t *testing.T) {
	sess := &Session{ID: "gate-same-ssrc", VideoAUNormalizeEnabled: true, SwitchGeneration: 12}
	now := time.Unix(350, 0)
	sess.StartSwitchVideoGate(now, "switch")
	stale := readySwitchAU(11, true, true)
	fresh := readySwitchAU(12, true, true)
	stale.Packets[0].SSRC = 4444
	fresh.Packets[0].SSRC = 4444

	if got := sess.EvaluateSwitchVideoAccessUnit(stale, now); got.Emit {
		t.Fatalf("same-SSRC stale generation released gate: %+v", got)
	}
	if got := sess.EvaluateSwitchVideoAccessUnit(fresh, now); !got.Emit || !got.Released {
		t.Fatalf("same-SSRC active generation did not release: %+v", got)
	}
}

func TestSwitchVideoGateDoesNotMutateAudioState(t *testing.T) {
	sess := &Session{
		ID: "gate-audio", VideoAUNormalizeEnabled: true, SwitchGeneration: 13,
		AudioSSRC: 91, RemoteAudioSSRC: 92, AudioSeq: 93,
	}
	sess.StartSwitchVideoGate(time.Unix(360, 0), "switch")
	_ = sess.EvaluateSwitchVideoAccessUnit(readySwitchAU(13, false, true), time.Unix(361, 0))

	if sess.AudioSSRC != 91 || sess.RemoteAudioSSRC != 92 || sess.AudioSeq != 93 {
		t.Fatalf("video gate changed audio state: local=%d remote=%d seq=%d",
			sess.AudioSSRC, sess.RemoteAudioSSRC, sess.AudioSeq)
	}
}
~~~

- [ ] **Step 2: Run tests and confirm compile failure**

~~~powershell
go test ./internal/session -run "TestSwitchVideoGate" -count=1
~~~

Expected: build failure because gate methods and decision types do not exist.

- [ ] **Step 3: Add focused gate fields to Session**

Add adjacent to switch recovery state:

~~~go
SwitchVideoGateActive           bool
SwitchVideoGateGeneration       int
SwitchVideoGateStartedAt        time.Time
SwitchVideoGateLastRejectAt     time.Time
SwitchVideoGateLastRejectReason string
SwitchVideoGateRejectedAUs      uint64
SwitchVideoGatePLIStartCount    int
SwitchVideoGateLastStallAt      time.Time
~~~

Apply json exclusion tags in the implementation to match neighboring internal fields.

- [ ] **Step 4: Implement the state machine**

In switch_video_gate.go define:

~~~go
type SwitchVideoGateDecision struct {
	Emit       bool
	Released   bool
	Reason     string
	Generation int
}

func (s *Session) StartSwitchVideoGate(now time.Time, reason string) {
	if !s.VideoAUNormalizeEnabled {
		return
	}
	s.mu.Lock()
	s.SwitchVideoGateActive = true
	s.SwitchVideoGateGeneration = s.SwitchGeneration
	s.SwitchVideoGateStartedAt = now
	s.SwitchVideoGateLastRejectAt = time.Time{}
	s.SwitchVideoGateLastRejectReason = ""
	s.SwitchVideoGateRejectedAUs = 0
	s.SwitchVideoGatePLIStartCount = s.PLISent
	s.SwitchVideoGateLastStallAt = time.Time{}
	generation := s.SwitchVideoGateGeneration
	s.mu.Unlock()

	fmt.Printf("[%s] switch_video_gate_start generation=%d reason=%s feedback=%s\n",
		s.ID, generation, reason, s.GetVideoFeedbackTransport())
}

func (s *Session) EvaluateSwitchVideoAccessUnit(
	au NormalizedH264AccessUnit,
	now time.Time,
) SwitchVideoGateDecision {
	s.mu.Lock()
	if !s.SwitchVideoGateActive {
		s.mu.Unlock()
		return SwitchVideoGateDecision{Emit: true, Reason: "gate-inactive", Generation: au.Generation}
	}

	reason := ""
	switch {
	case au.Generation != s.SwitchVideoGateGeneration:
		reason = "stale-generation"
	case !au.IsIDR:
		reason = "awaiting-idr"
	case !au.ParameterSetsReady:
		reason = "missing-fresh-parameter-sets"
	default:
		startedAt := s.SwitchVideoGateStartedAt
		generation := s.SwitchVideoGateGeneration
		rejected := s.SwitchVideoGateRejectedAUs
		feedbackRequests := s.PLISent - s.SwitchVideoGatePLIStartCount
		s.clearSwitchVideoGateLocked()
		s.mu.Unlock()
		fmt.Printf("[%s] switch_video_gate_release generation=%d wait_ms=%d packets=%d injected_parameter_sets=%v rejected_aus=%d feedback_requests=%d\n",
			s.ID, generation, now.Sub(startedAt).Milliseconds(), len(au.Packets),
			au.InjectedParameterSets, rejected, feedbackRequests)
		return SwitchVideoGateDecision{Emit: true, Released: true, Reason: "complete-idr", Generation: generation}
	}

	s.SwitchVideoGateRejectedAUs++
	shouldLog := reason != s.SwitchVideoGateLastRejectReason ||
		s.SwitchVideoGateLastRejectAt.IsZero() ||
		now.Sub(s.SwitchVideoGateLastRejectAt) >= time.Second
	if shouldLog {
		s.SwitchVideoGateLastRejectReason = reason
		s.SwitchVideoGateLastRejectAt = now
	}
	generation := s.SwitchVideoGateGeneration
	s.mu.Unlock()

	if shouldLog {
		fmt.Printf("[%s] switch_video_au_rejected generation=%d au_generation=%d reason=%s idr=%v parameter_sets_ready=%v\n",
			s.ID, generation, au.Generation, reason, au.IsIDR, au.ParameterSetsReady)
	}
	return SwitchVideoGateDecision{Reason: reason, Generation: generation}
}
~~~

Also implement clearSwitchVideoGateLocked, StopSwitchVideoGate, and IsSwitchVideoGateActive using the same mutex. clearSwitchVideoGateLocked must reset every gate field listed in Step 3.

- [ ] **Step 5: Run unit and race tests**

~~~powershell
go test ./internal/session -run "TestSwitchVideoGate" -count=1
go test -race ./internal/session -run "TestSwitchVideoGate" -count=1
~~~

Expected: PASS with no race report.

- [ ] **Step 6: Commit**

~~~powershell
git add apps/gateway/internal/session/session.go apps/gateway/internal/session/switch_video_gate.go apps/gateway/internal/session/switch_video_gate_test.go
git commit -m "feat(gateway): add complete-IDR switch gate"
~~~

## Task 3: Start the Gate and Isolate SIP Parameter Sets

**Files:**
- Modify: apps/gateway/internal/session/media_endpoints.go:369-450,512-550
- Test: apps/gateway/internal/session/media_endpoints_test.go
- Modify: apps/gateway/internal/sip/handlers.go:1213-1252
- Test: apps/gateway/internal/sip/handlers_switch_test.go

- [ ] **Step 1: Write failing cache-isolation test**

~~~go
func TestClearSIPVideoParameterSetsPreservesWebRTCToSIPCache(t *testing.T) {
	sess := &Session{
		CachedSPS:    []byte{0x67, 0x01},
		CachedPPS:    []byte{0x68, 0x01},
		SIPCachedSPS: []byte{0x67, 0x02},
		SIPCachedPPS: []byte{0x68, 0x02},
	}
	sess.ClearSIPVideoParameterSets()

	if _, _, ok := sess.GetSIPCachedSPSPPS(); ok {
		t.Fatal("expected SIP-side parameter sets cleared")
	}
	if len(sess.CachedSPS) == 0 || len(sess.CachedPPS) == 0 {
		t.Fatal("expected WebRTC-to-SIP parameter sets preserved")
	}
}
~~~

Extend TestHandleSwitchMessage_StartsVideoRecoveryBurst:

~~~go
if !sess.IsSwitchVideoGateActive() {
	t.Fatal("expected genuine @switch to start complete-IDR gate")
}
if sess.SwitchVideoGateGeneration != sess.GetSwitchGeneration() {
	t.Fatalf("gate generation=%d switch generation=%d",
		sess.SwitchVideoGateGeneration, sess.GetSwitchGeneration())
}
~~~

The duplicate-switch test must assert that ignored duplicates preserve gate generation and start time. Add a malformed/force-keyframe test asserting the gate remains inactive.

- [ ] **Step 2: Run tests and confirm failures**

~~~powershell
go test ./internal/session -run "TestClearSIPVideoParameterSets" -count=1
go test ./internal/sip -run "TestHandleSwitchMessage" -count=1
~~~

Expected: missing method compile failure and genuine-switch inactive-gate failure.

- [ ] **Step 3: Implement cache clearing and reset cleanup**

~~~go
func (s *Session) ClearSIPVideoParameterSets() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SIPCachedSPS = nil
	s.SIPCachedPPS = nil
}
~~~

Inside ResetMediaState, under its existing lock, clear SIPCachedSPS, SIPCachedPPS, and call clearSwitchVideoGateLocked. Do not clear CachedSPS/CachedPPS in ClearSIPVideoParameterSets because those belong to WebRTC-to-SIP forwarding.

- [ ] **Step 4: Start the gate after duplicate acceptance**

Immediately after StartSwitchVideoRecovery:

~~~go
if queueNumber != "force send PLI" {
	sess.StartSwitchVideoGate(time.Now(), "switch")
}
~~~

Keep it after PrepareSwitchVideoTarget returns an accepted decision. Do not start a gate for the synthetic force-keyframe path.

- [ ] **Step 5: Run focused tests**

~~~powershell
go test ./internal/session -run "TestClearSIPVideoParameterSets|TestPendingBrowserKeyframe_ClearedOnResetMediaState" -count=1
go test ./internal/sip -run "TestHandleSwitchMessage" -count=1
~~~

Expected: PASS.

- [ ] **Step 6: Commit**

~~~powershell
git add apps/gateway/internal/session/media_endpoints.go apps/gateway/internal/session/media_endpoints_test.go apps/gateway/internal/sip/handlers.go apps/gateway/internal/sip/handlers_switch_test.go
git commit -m "feat(gateway): gate accepted SIP video switches"
~~~

## Task 4: Gate the Normalized RTP Output

**Files:**
- Modify: apps/gateway/internal/sip/rtp.go:334-725
- Test: apps/gateway/internal/session/switch_video_gate_test.go
- Test: apps/gateway/internal/session/h264_access_unit_normalizer_test.go

- [ ] **Step 1: Add pipeline safety tests**

Drive the real normalizer callback through the gate:

~~~go
func TestSwitchVideoGatePipelineDropsPFramesAfterIncompleteIDR(t *testing.T) {
	sess := &Session{ID: "pipeline-incomplete", VideoAUNormalizeEnabled: true, SwitchGeneration: 9}
	sess.StartSwitchVideoGate(time.Unix(400, 0), "switch")
	var written []NormalizedH264AccessUnit
	n := NewH264AccessUnitNormalizer(H264AccessUnitNormalizerConfig{}, func(au NormalizedH264AccessUnit) {
		if sess.EvaluateSwitchVideoAccessUnit(au, time.Now()).Emit {
			written = append(written, au)
		}
	})
	n.ResetForSwitch(9)

	n.Push(h264Packet(1, 3000, false, []byte{0x7c, 0x85, 0x01}))
	n.Push(h264Packet(3, 3000, true, []byte{0x7c, 0x45, 0x03}))
	n.Push(h264Packet(4, 6000, true, []byte{0x41, 0x04}))

	if len(written) != 0 || !sess.IsSwitchVideoGateActive() {
		t.Fatalf("expected incomplete IDR and P-frame gated, written=%+v", written)
	}
}
~~~

Add a companion test with fresh SPS, PPS, and a complete fragmented IDR. Assert one AU passes and the gate releases.

In that companion test, initialize the existing RTP history, cache every released rewritten packet exactly as the production callback does, and verify NACK lookup uses the rewritten sequence number:

~~~go
sess.initVideoRTPHistory()
for _, packet := range released.Packets {
	data, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	sess.CacheVideoRTPPacket(packet.SequenceNumber, data)
	cached := sess.getCachedVideoRTPPacket(packet.SequenceNumber)
	var got rtp.Packet
	if err := got.Unmarshal(cached); err != nil || got.SequenceNumber != packet.SequenceNumber {
		t.Fatalf("NACK cache not aligned to rewritten seq=%d: packet=%+v err=%v",
			packet.SequenceNumber, got.Header, err)
	}
}
~~~

- [ ] **Step 2: Run the safety harness**

~~~powershell
go test ./internal/session -run "TestSwitchVideoGatePipeline" -count=1
~~~

Expected: PASS after Tasks 1-3. This protects the RTP integration.

- [ ] **Step 3: Evaluate before any VideoTrack write**

At the beginning of the normalizer callback in handleVideoRTPPacketsForSession:

~~~go
decision := sess.EvaluateSwitchVideoAccessUnit(au, time.Now())
if !decision.Emit {
	return
}

for _, packet := range au.Packets {
	data, marshalErr := packet.Marshal()
	// retain existing cache and VideoTrack.Write handling
}
~~~

Call RecordKeyframe, MarkSwitchVideoKeyframe, and MarkSwitchVideoProgress only after the AU passes the gate and every packet write succeeds. Rejected IDRs must not refresh LastKeyframe.

- [ ] **Step 4: Reset before processing the first packet of a new generation**

Move generation comparison to immediately after RTP validation and before SPS/PPS caching or reorder push:

~~~go
currentGeneration := sess.GetSwitchGeneration()
if currentGeneration != lastSwitchGeneration {
	reorderBuf.Reset()
	if auNormalizer != nil {
		auNormalizer.ResetForSwitch(currentGeneration)
	}
	sess.ClearSIPVideoParameterSets()
	fmt.Printf("[%s] h264_au_source_reset reason=switch-generation previous_generation=%d generation=%d\n",
		sess.ID, lastSwitchGeneration, currentGeneration)
	lastSwitchGeneration = currentGeneration
}
~~~

Remove the old later generation-reset block near reorderBuf.Push.

- [ ] **Step 5: Keep packet-level release only as rollback**

Replace the unconditional packet hold with:

~~~go
if auNormalizer == nil && sess.ShouldHoldSwitchVideoPacket(time.Now(), isKeyframe) {
	continue
}
~~~

With normalization enabled, a bare IDR/FU-A start cannot release or bypass the complete-AU gate.

- [ ] **Step 6: Run media-path tests**

~~~powershell
go test ./internal/session -run "TestH264AccessUnitNormalizer|TestSwitchVideoGate|TestSwitchTransition|TestSwitchBlackout|TestVideoReorderBuffer" -count=1
go test ./internal/sip -run "TestHandleSwitchMessage" -count=1
~~~

Expected: PASS; missing IDR fragments never allow following P-frames through the active gate.

- [ ] **Step 7: Commit**

~~~powershell
git add apps/gateway/internal/sip/rtp.go apps/gateway/internal/session/switch_video_gate_test.go apps/gateway/internal/session/h264_access_unit_normalizer_test.go
git commit -m "fix(gateway): release switched video after complete IDR"
~~~

## Task 5: Separate Fail-Closed Safety from Bounded Recovery

**Files:**
- Modify: apps/gateway/internal/session/switch_video_gate.go
- Modify: apps/gateway/internal/session/switch_video_gate_test.go
- Modify: apps/gateway/internal/session/video_recovery_test.go
- Modify: apps/gateway/internal/sip/rtp.go:591-610,847-917

- [ ] **Step 1: Write failing stall and timeout tests**

~~~go
func TestSwitchVideoGateStallSnapshotIsThrottled(t *testing.T) {
	sess := &Session{ID: "gate-stalled", VideoAUNormalizeEnabled: true, SwitchGeneration: 10}
	started := time.Unix(500, 0)
	sess.StartSwitchVideoGate(started, "switch")
	summary := VideoRecoverySummary{Packets: 300, Gaps: 4, Missing: 9, OutOfOrder: 3, ReorderTimedOut: 2}

	first, ok := sess.ObserveSwitchVideoGateStall(started.Add(2*time.Second), summary)
	if !ok || first.Generation != 10 || first.Summary.Missing != 9 {
		t.Fatalf("expected first stalled snapshot, got %+v ok=%v", first, ok)
	}
	if _, ok := sess.ObserveSwitchVideoGateStall(started.Add(2500*time.Millisecond), summary); ok {
		t.Fatal("expected stall report throttled")
	}
	if _, ok := sess.ObserveSwitchVideoGateStall(started.Add(5*time.Second), summary); !ok {
		t.Fatal("expected later stall report")
	}
}
~~~

Extend TestSwitchVideoRecoveryTimesOut:

~~~go
sess.VideoAUNormalizeEnabled = true
sess.SwitchGeneration = 11
sess.StartSwitchVideoGate(time.Now(), "switch")
// retain existing recovery-expiry assertions
if !sess.IsSwitchVideoGateActive() {
	t.Fatal("recovery timeout must not fail open the decoder-safety gate")
}
~~~

- [ ] **Step 2: Run tests and confirm compile failure**

~~~powershell
go test ./internal/session -run "TestSwitchVideoGateStallSnapshot|TestSwitchVideoRecoveryTimesOut" -count=1
~~~

Expected: missing ObserveSwitchVideoGateStall compile failure.

- [ ] **Step 3: Implement throttled snapshots**

~~~go
type SwitchVideoGateStall struct {
	Generation  int
	Elapsed     time.Duration
	RejectedAUs uint64
	Summary     VideoRecoverySummary
}

func (s *Session) ObserveSwitchVideoGateStall(
	now time.Time,
	summary VideoRecoverySummary,
) (SwitchVideoGateStall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.SwitchVideoGateActive || now.Sub(s.SwitchVideoGateStartedAt) < 2*time.Second {
		return SwitchVideoGateStall{}, false
	}
	if !s.SwitchVideoGateLastStallAt.IsZero() &&
		now.Sub(s.SwitchVideoGateLastStallAt) < 2*time.Second {
		return SwitchVideoGateStall{}, false
	}
	s.SwitchVideoGateLastStallAt = now
	return SwitchVideoGateStall{
		Generation:  s.SwitchVideoGateGeneration,
		Elapsed:     now.Sub(s.SwitchVideoGateStartedAt),
		RejectedAUs: s.SwitchVideoGateRejectedAUs,
		Summary:     summary,
	}, true
}
~~~

- [ ] **Step 4: Log at the existing 300-packet cadence**

After building VideoRecoverySummary in rtp.go:

~~~go
if stall, ok := sess.ObserveSwitchVideoGateStall(time.Now(), summary); ok {
	fmt.Printf("[%s] switch_video_gate_stalled generation=%d elapsed_ms=%d rejected_aus=%d packets=%d gaps=%d missing=%d ooo=%d reorder_timeout=%d pending=%d feedback=%s\n",
		sess.ID, stall.Generation, stall.Elapsed.Milliseconds(), stall.RejectedAUs,
		stall.Summary.Packets, stall.Summary.Gaps, stall.Summary.Missing,
		stall.Summary.OutOfOrder, stall.Summary.ReorderTimedOut,
		stall.Summary.ReorderPending, sess.GetVideoFeedbackTransport())
}
~~~

Reuse the existing watchdog and recovery burst for FIR/PLI escalation. Do not add another timer loop. Do not clear the gate from finishSwitchVideoRecoveryLocked or endVideoRecoveryBurst.

- [ ] **Step 5: Audit teardown**

Run:

~~~powershell
rg -n "ResetMediaState|StopSwitchVideoRecoveryIfActive|StateEnded|CloseMediaTransports" apps/gateway/internal/session apps/gateway/internal/sip
~~~

Add StopSwitchVideoGate("session-ended") only at teardown paths not already covered by ResetMediaState. A newer switch replaces the gate generation; recovery expiry leaves it closed.

- [ ] **Step 6: Run unit and race tests**

~~~powershell
go test ./internal/session -run "TestSwitchVideoGate|TestSwitchVideoRecovery" -count=1
go test -race ./internal/session -run "TestSwitchVideoGate|TestSwitchVideoRecoveryTimesOut" -count=1
~~~

Expected: PASS with no race report.

- [ ] **Step 7: Commit**

~~~powershell
git add apps/gateway/internal/session/session.go apps/gateway/internal/session/switch_video_gate.go apps/gateway/internal/session/switch_video_gate_test.go apps/gateway/internal/session/video_recovery_test.go apps/gateway/internal/sip/rtp.go
git commit -m "feat(gateway): report stalled SIP video switches"
~~~

## Task 6: Documentation and Verification

**Files:**
- Modify: docs/gateway/troubleshooting.md
- Modify: docs/gateway/config-reference.md
- Verify: apps/gateway/internal/session and apps/gateway/internal/sip

- [ ] **Step 1: Document the events and RTPengine diagnosis**

Add:

~~~markdown
### Queue-to-agent video shows block corruption or wrong colors

With SIP_VIDEO_AU_NORMALIZE_ENABLE=true, a real @switch keeps the previous
decoded frame visible until the gateway receives fresh SPS/PPS and a complete
IDR for the new switch generation.

- switch_video_gate_start: the new media generation is gated.
- switch_video_au_rejected: an AU was unsafe; inspect reason.
- switch_video_gate_release: clean video was released; wait_ms is switch latency.
- switch_video_gate_stalled: inspect RTP loss, ordering, and RTCP routing.

Capture both Asterisk-to-RTPengine and RTPengine-to-Gateway legs. Compare first
fresh SPS/PPS, first complete IDR, sequence gaps, ordering, timestamps, and
whether Gateway FIR/PLI reaches Asterisk. Do not bypass RTPengine in production
for this diagnosis.
~~~

- [ ] **Step 2: Clarify configuration**

Update SIP_VIDEO_AU_NORMALIZE_ENABLE to state that false selects the legacy packet-level rollback behavior. Update SIP_VIDEO_FEEDBACK_TRANSPORT to state that dual is useful for diagnosing RTPengine feedback routing but does not replace complete-IDR gating.

- [ ] **Step 3: Format and inspect**

From the repository root:

~~~powershell
gofmt -w apps/gateway/internal/session/h264_access_unit_normalizer.go apps/gateway/internal/session/h264_access_unit_normalizer_test.go apps/gateway/internal/session/switch_video_gate.go apps/gateway/internal/session/switch_video_gate_test.go apps/gateway/internal/session/session.go apps/gateway/internal/session/media_endpoints.go apps/gateway/internal/session/media_endpoints_test.go apps/gateway/internal/session/video_recovery_test.go apps/gateway/internal/sip/handlers.go apps/gateway/internal/sip/handlers_switch_test.go apps/gateway/internal/sip/rtp.go
git diff --check
git status --short
~~~

Expected: no diff-check output and only planned gateway/docs files.

- [ ] **Step 4: Run focused tests**

From apps/gateway:

~~~powershell
go test ./internal/session -run "TestH264AccessUnitNormalizer|TestSwitchVideoGate|TestSwitchVideoRecovery|TestSwitchTransition|TestSwitchBlackout|TestVideoReorderBuffer" -count=1
go test ./internal/sip -run "TestHandleSwitchMessage" -count=1
~~~

Expected: both commands exit 0.

- [ ] **Step 5: Run the full gateway suite**

~~~powershell
go test ./... -count=1
~~~

Expected: all packages pass.

- [ ] **Step 6: Run the call-flow verifier**

From the repository root:

~~~powershell
C:/Users/Kasemsan/.codex/skills/sip-webrtc-callflow-auditor/scripts/verify-callflow.ps1
~~~

Expected: gateway/mobile call-flow checks pass and no WebSocket contract change is reported.

- [ ] **Step 7: Perform controlled RTPengine interop verification**

1. Establish queue video through Asterisk, Kamailio, RTPengine, and Gateway.
2. Trigger a genuine @switch to a Linphone agent.
3. Confirm browser, Android, and iOS retain the queue frame until clean live video appears.
4. Confirm audio remains continuous.
5. Confirm switch_video_gate_start is followed by switch_video_gate_release.
6. Confirm there is no block corruption or incorrect-color frame.
7. If stalled, capture both RTPengine legs and correlate stall logs with SPS/PPS, IDR completeness, RTP gaps, and FIR/PLI delivery.

Expected: delayed keyframes produce a frozen prior frame rather than corrupted video.

- [ ] **Step 8: Commit documentation**

~~~powershell
git add docs/gateway/troubleshooting.md docs/gateway/config-reference.md
git commit -m "docs(gateway): explain complete-IDR switch recovery"
~~~

Record focused tests, full tests, verifier output, and interop observations in the implementation handoff. Do not claim the production symptom is fixed until the RTPengine interop check is observed.
