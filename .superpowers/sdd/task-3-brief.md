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

