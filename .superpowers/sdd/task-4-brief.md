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
