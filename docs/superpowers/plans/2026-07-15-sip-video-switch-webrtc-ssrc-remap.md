# SIP Video Switch WebRTC Egress SSRC Remap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On every accepted `@switch`, rewrite SIP→WebRTC video RTP with a new egress SSRC so client decoders treat queue→Linphone as a new media source without client reconnect.

**Architecture:** Add session-scoped `WebRTCVideoEgressSSRC`. Allocate a new random value inside `PrepareAndActivateSwitchVideoTarget` when a switch is accepted. Apply that SSRC via one helper immediately before every SIP→WebRTC video write (normalized AU path and legacy raw path). Keep SIP `RemoteVideoSSRC` unchanged for FIR/PLI/NACK.

**Tech Stack:** Go 1.26, Pion `rtp`/`webrtc` v4, existing session mutex, Go testing.

## Global Constraints

- Remap on every accepted `@switch` (not only when SIP SSRC is unchanged).
- Do not remap on ignored duplicate `@switch`.
- Do not change SIP `RemoteVideoSSRC` / audio SSRC / WS contracts.
- Preserve complete-IDR gate, AU normalize, SPS/PPS inject, transition hold.
- No feature flag for v1; behavior is always-on for accepted switches.
- No panics in RTP hot paths; hold session mutex only for short state updates.

---

## File Structure

- Create: `apps/gateway/internal/session/webrtc_video_egress_ssrc.go` — allocate/apply/get egress SSRC helpers
- Create: `apps/gateway/internal/session/webrtc_video_egress_ssrc_test.go` — unit tests for helpers + switch remap
- Modify: `apps/gateway/internal/session/session.go` — add `WebRTCVideoEgressSSRC` field
- Modify: `apps/gateway/internal/session/switch_target.go` — remap on accepted switch
- Modify: `apps/gateway/internal/session/switch_target_test.go` — assert remap / no-remap-on-duplicate
- Modify: `apps/gateway/internal/session/media_endpoints.go` — clear egress SSRC in `ResetMediaState`
- Modify: `apps/gateway/internal/sip/rtp.go` — apply egress SSRC on all SIP→WebRTC video writes
- Modify: `apps/gateway/internal/sip/rtp_video_gate_test.go` — assert written packets use egress SSRC
- Modify: `docs/gateway/troubleshooting.md` — one operational note for `switch_webrtc_ssrc_remap`

---

### Task 1: Session egress SSRC helpers

**Files:**
- Create: `apps/gateway/internal/session/webrtc_video_egress_ssrc.go`
- Create: `apps/gateway/internal/session/webrtc_video_egress_ssrc_test.go`
- Modify: `apps/gateway/internal/session/session.go` (add field near other video SSRC fields)
- Modify: `apps/gateway/internal/session/media_endpoints.go` (`ResetMediaState`)

**Interfaces:**
- Produces:
  - `func (s *Session) EnsureWebRTCVideoEgressSSRC(packetSSRC uint32) uint32`
  - `func (s *Session) RemapWebRTCVideoEgressSSRC(reason string) uint32`
  - `func (s *Session) ApplyWebRTCVideoEgressSSRC(packet *rtp.Packet)`
  - `func (s *Session) GetWebRTCVideoEgressSSRC() uint32`

- [ ] **Step 1: Write failing tests**

```go
package session

import (
	"testing"

	"github.com/pion/rtp"
)

func TestEnsureWebRTCVideoEgressSSRCUsesPacketThenSticky(t *testing.T) {
	sess := &Session{ID: "egress-init"}
	got := sess.EnsureWebRTCVideoEgressSSRC(4242)
	if got != 4242 {
		t.Fatalf("expected packet SSRC 4242, got %d", got)
	}
	if sess.EnsureWebRTCVideoEgressSSRC(9999) != 4242 {
		t.Fatalf("expected sticky egress SSRC")
	}
}

func TestEnsureWebRTCVideoEgressSSRCGeneratesWhenPacketZero(t *testing.T) {
	sess := &Session{ID: "egress-gen"}
	got := sess.EnsureWebRTCVideoEgressSSRC(0)
	if got == 0 {
		t.Fatal("expected generated non-zero SSRC")
	}
}

func TestRemapWebRTCVideoEgressSSRCChangesValue(t *testing.T) {
	sess := &Session{ID: "egress-remap"}
	first := sess.EnsureWebRTCVideoEgressSSRC(1111)
	second := sess.RemapWebRTCVideoEgressSSRC("accepted-switch")
	if second == 0 || second == first {
		t.Fatalf("expected new SSRC, first=%d second=%d", first, second)
	}
	if sess.GetWebRTCVideoEgressSSRC() != second {
		t.Fatalf("getter mismatch")
	}
}

func TestApplyWebRTCVideoEgressSSRCRewritesPacket(t *testing.T) {
	sess := &Session{ID: "egress-apply"}
	sess.EnsureWebRTCVideoEgressSSRC(5555)
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SSRC: 1111, SequenceNumber: 1}}
	sess.ApplyWebRTCVideoEgressSSRC(pkt)
	if pkt.SSRC != 5555 {
		t.Fatalf("expected rewrite to 5555, got %d", pkt.SSRC)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run from `apps/gateway`:

```bash
go test ./internal/session -run "TestEnsureWebRTCVideoEgressSSRC|TestRemapWebRTCVideoEgressSSRC|TestApplyWebRTCVideoEgressSSRC" -count=1
```

Expected: FAIL (methods undefined)

- [ ] **Step 3: Add field + implement helpers**

In `session.go` near `RemoteVideoSSRC`:

```go
WebRTCVideoEgressSSRC uint32 `json:"-"` // SIP→WebRTC rewritten video SSRC
```

Create `webrtc_video_egress_ssrc.go`:

```go
package session

import (
	"fmt"

	"github.com/pion/rtp"
)

func (s *Session) GetWebRTCVideoEgressSSRC() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.WebRTCVideoEgressSSRC
}

func (s *Session) EnsureWebRTCVideoEgressSSRC(packetSSRC uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.WebRTCVideoEgressSSRC != 0 {
		return s.WebRTCVideoEgressSSRC
	}
	if packetSSRC != 0 {
		s.WebRTCVideoEgressSSRC = packetSSRC
	} else {
		s.WebRTCVideoEgressSSRC = generateSSRC()
		if s.WebRTCVideoEgressSSRC == 0 {
			s.WebRTCVideoEgressSSRC = 1
		}
	}
	fmt.Printf("[%s] webrtc_video_egress_ssrc_init ssrc=%d source=%s\n",
		s.ID, s.WebRTCVideoEgressSSRC, map[bool]string{true: "packet", false: "generated"}[packetSSRC != 0])
	return s.WebRTCVideoEgressSSRC
}

func (s *Session) RemapWebRTCVideoEgressSSRC(reason string) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remapWebRTCVideoEgressSSRCLocked(reason)
}

func (s *Session) remapWebRTCVideoEgressSSRCLocked(reason string) uint32 {
	old := s.WebRTCVideoEgressSSRC
	next := generateSSRC()
	if next == 0 || next == old {
		next = generateSSRC()
	}
	if next == 0 {
		next = old + 1
		if next == 0 {
			next = 1
		}
	}
	s.WebRTCVideoEgressSSRC = next
	fmt.Printf("[%s] switch_webrtc_ssrc_remap generation=%d mediaEpoch=%d old=%d new=%d sipSsrc=%d reason=%s\n",
		s.ID, s.SwitchGeneration, s.MediaEpoch, old, next, s.RemoteVideoSSRC, reason)
	return next
}

func (s *Session) ApplyWebRTCVideoEgressSSRC(packet *rtp.Packet) {
	if packet == nil {
		return
	}
	ssrc := s.EnsureWebRTCVideoEgressSSRC(packet.SSRC)
	packet.SSRC = ssrc
}
```

In `ResetMediaState` after `s.RemoteVideoSSRC = 0`:

```go
s.WebRTCVideoEgressSSRC = 0
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/session -run "TestEnsureWebRTCVideoEgressSSRC|TestRemapWebRTCVideoEgressSSRC|TestApplyWebRTCVideoEgressSSRC" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/gateway/internal/session/session.go apps/gateway/internal/session/media_endpoints.go apps/gateway/internal/session/webrtc_video_egress_ssrc.go apps/gateway/internal/session/webrtc_video_egress_ssrc_test.go
git commit -m "feat(gateway): add WebRTC video egress SSRC helpers"
```

---

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

### Task 3: Apply egress SSRC on SIP→WebRTC write paths

**Files:**
- Modify: `apps/gateway/internal/sip/rtp.go` (`writeNormalizedVideoAccessUnit` and legacy raw writes)
- Modify: `apps/gateway/internal/sip/rtp_video_gate_test.go`

**Interfaces:**
- Consumes: `ApplyWebRTCVideoEgressSSRC(*rtp.Packet)`
- Produces: all bytes written to `VideoTrack` / write callback use egress SSRC; cache stores remapped bytes

- [ ] **Step 1: Write failing write-path test**

Append to `rtp_video_gate_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/sip -run TestWriteNormalizedVideoAccessUnitUsesWebRTCEgressSSRC -count=1
```

Expected: FAIL (`gotSSRC` still 4242 from fixture)

- [ ] **Step 3: Apply remap before marshal/write**

In `writeNormalizedVideoAccessUnit`, inside the packet loop, before `Marshal()`:

```go
sess.ApplyWebRTCVideoEgressSSRC(packet)
```

For legacy raw path in `handleVideoRTPPacketsForSession` where `auNormalizer == nil` and `VideoTrack.Write(data)` is used, unmarshal → apply → marshal before write and before `CacheVideoRTPPacket`. Prefer:

```go
packet := &rtp.Packet{}
if err := packet.Unmarshal(data); err == nil {
	sess.ApplyWebRTCVideoEgressSSRC(packet)
	if out, err := packet.Marshal(); err == nil {
		sess.CacheVideoRTPPacket(packet.SequenceNumber, out)
		_, _ = sess.VideoTrack.Write(out)
	}
}
```

Also cover the malformed-bypass `VideoTrack.Write(buffer[:n])` branch the same way when `auNormalizer == nil`.

Do **not** change SIP→WebRTC RTCP feedback targeting (`RemoteVideoSSRC`).

- [ ] **Step 4: Run focused + related tests**

```bash
go test ./internal/sip -run "TestWriteNormalizedVideoAccessUnit" -count=1
go test ./internal/session -run "TestPrepareSwitchVideoTarget|TestEnsureWebRTCVideoEgressSSRC|TestRemapWebRTCVideoEgressSSRC|TestApplyWebRTCVideoEgressSSRC" -count=1
go test ./internal/sip -run "Switch|switch" -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/gateway/internal/sip/rtp.go apps/gateway/internal/sip/rtp_video_gate_test.go
git commit -m "feat(gateway): rewrite SIP-to-WebRTC video SSRC on egress writes"
```

---

### Task 4: Docs + full package verification

**Files:**
- Modify: `docs/gateway/troubleshooting.md` (section about remote video after queue-to-agent switch)

- [ ] **Step 1: Add operational note**

Near existing `@switch` / complete-IDR troubleshooting text, add:

```markdown
- `switch_webrtc_ssrc_remap` means the gateway allocated a new WebRTC-visible
  video SSRC for an accepted `@switch` so clients reset decoders even when
  RTPengine preserved the SIP SSRC. `old`/`new` are egress values; `sipSsrc`
  remains the SIP-learned media SSRC used for FIR/PLI/NACK toward Asterisk.
```

- [ ] **Step 2: Run broader gateway tests**

```bash
go test ./internal/session ./internal/sip -count=1
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add docs/gateway/troubleshooting.md
git commit -m "docs(gateway): explain WebRTC egress SSRC remap on @switch"
```

---

## Spec coverage checklist

| Spec requirement | Task |
|------------------|------|
| `WebRTCVideoEgressSSRC` session field | Task 1 |
| Lazy init on first write | Task 1 |
| Remap every accepted `@switch` | Task 2 |
| No remap on duplicate ignore | Task 2 |
| Apply before all SIP→WebRTC video writes | Task 3 |
| Cache stores post-remap bytes | Task 3 |
| Keep SIP `RemoteVideoSSRC` unchanged | Task 2 tests |
| Logging `switch_webrtc_ssrc_remap` | Task 1/2 |
| Reset on media reset | Task 1 |
| Troubleshooting note | Task 4 |

## Placeholder / consistency self-review

- No TBD/TODO placeholders.
- Helper names consistent: `Ensure` / `Remap` / `Apply` / `Get` / locked `remap...Locked`.
- Field name consistent: `WebRTCVideoEgressSSRC`.
