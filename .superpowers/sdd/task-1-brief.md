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

