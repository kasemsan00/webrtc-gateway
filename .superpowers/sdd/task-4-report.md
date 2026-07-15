# Task 4 Report — Docs + full package verification

## What I implemented

Added the operational note for `switch_webrtc_ssrc_remap` to `docs/gateway/troubleshooting.md` in the **Remote video is black on mobile web after queue-to-agent switch** section, immediately after the `complete-idr` bullet and before the normalization rollback bullet — adjacent to existing `@switch` / complete-IDR troubleshooting text per the brief.

Verbatim text added:

```markdown
- `switch_webrtc_ssrc_remap` means the gateway allocated a new WebRTC-visible
  video SSRC for an accepted `@switch` so clients reset decoders even when
  RTPengine preserved the SIP SSRC. `old`/`new` are egress values; `sipSsrc`
  remains the SIP-learned media SSRC used for FIR/PLI/NACK toward Asterisk.
```

## Broader gateway tests (Step 2)

```
$ cd apps/gateway && go test ./internal/session ./internal/sip -count=1
ok  	k2-gateway/internal/session	1.528s
ok  	k2-gateway/internal/sip	2.521s
```

Both packages PASS.

## Commits

- `8856494` docs(gateway): explain WebRTC egress SSRC remap on @switch
  - 1 file changed: `docs/gateway/troubleshooting.md`
  - Only the brief-listed docs file was staged; unrelated changes (e.g. `docker-ci.ps1`) were left unstaged.

## Spec coverage checklist (Task 4 item)

| Spec requirement | Status |
|------------------|--------|
| Troubleshooting note for `switch_webrtc_ssrc_remap` | Done |

## Self-review

- Note placed near existing `@switch` / complete-IDR troubleshooting bullets.
- No TBD/TODO placeholders.
- Wording matches brief verbatim.
- Broader package tests pass as expected.

## Concerns

None.
