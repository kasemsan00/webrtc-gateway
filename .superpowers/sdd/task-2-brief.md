### Task 2: Remap on accepted `@switch` only

**Files:**
- Modify: `apps/gateway/internal/session/switch_target.go`
- Modify: `apps/gateway/internal/session/switch_target_test.go`

**Interfaces:**
- Consumes: `remapWebRTCVideoEgressSSRCLocked(reason string) uint32`
- Produces: accepted switch always changes `WebRTCVideoEgressSSRC`; duplicate ignore does not

- [ ] **Step 1: Write failing tests**

Append to `switch_target_test.go`:

```go
func TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC(t *testing.T) {
	sess := newBurstTestSession("switch-egress-remap")
	now := time.Now()
	sess.RemoteVideoSSRC = 1111
	sess.SIPVideoRTPSource = "203.0.113.10:4000"
	sess.WebRTCVideoEgressSSRC = 2222

	first, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now, time.Minute, true)
	if first.Ignore {
		t.Fatal("expected first switch honored")
	}
	if sess.GetWebRTCVideoEgressSSRC() == 0 || sess.GetWebRTCVideoEgressSSRC() == 2222 {
		t.Fatalf("expected remapped egress SSRC, got %d", sess.GetWebRTCVideoEgressSSRC())
	}
	if sess.RemoteVideoSSRC != 1111 {
		t.Fatalf("SIP RemoteVideoSSRC must stay 1111, got %d", sess.RemoteVideoSSRC)
	}
	afterFirst := sess.GetWebRTCVideoEgressSSRC()

	dup, _ := sess.PrepareAndActivateSwitchVideoTarget("14131", "00025", now.Add(30*time.Second), time.Minute, true)
	if !dup.Ignore {
		t.Fatal("expected duplicate ignored")
	}
	if sess.GetWebRTCVideoEgressSSRC() != afterFirst {
		t.Fatalf("duplicate must not remap egress SSRC, before=%d after=%d", afterFirst, sess.GetWebRTCVideoEgressSSRC())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/session -run TestPrepareSwitchVideoTargetRemapsWebRTCEgressSSRC -count=1
```

Expected: FAIL (egress SSRC still 2222)

- [ ] **Step 3: Remap inside accepted path**

In `PrepareAndActivateSwitchVideoTarget`, after generation bump / before returning activation (still under `s.mu.Lock()`), call:

```go
s.remapWebRTCVideoEgressSSRCLocked("accepted-switch")
```

Place it with the other accepted-switch mutations (after `s.SwitchGeneration++` and target fields are set, before `clearSIPVideoParameterSetsLocked` / gate start is fine). Do **not** call it on the duplicate-ignore return path.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/session -run "TestPrepareSwitchVideoTarget" -count=1
```

Expected: PASS (including existing duplicate/debounce tests)

- [ ] **Step 5: Commit**

```bash
git add apps/gateway/internal/session/switch_target.go apps/gateway/internal/session/switch_target_test.go
git commit -m "feat(gateway): remap WebRTC egress SSRC on accepted @switch"
```

---

