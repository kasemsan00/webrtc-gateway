# S2S Speech Translation Pipeline

## Overview

This feature enables per-call Speech-to-Speech (S2S) translation of audio during active WebRTC calls. When a WebRTC client enables translation, the gateway creates two translation pipelines:

- WebRTC/browser/mobile -> SIP/Linphone uses the selected direction, for example `en -> th`.
- SIP/Linphone -> WebRTC/browser/mobile automatically uses the reverse direction, for example `th -> en`.

In both directions, Opus RTP audio is decoded to PCM, sent to an external Azure gRPC translation server, re-encoded to Opus, and forwarded to the opposite peer.

Architecture:

```
Browser (WebRTC)                          SIP/Linphone
  │  Opus RTP
  ▼
Gateway ──Opus→PCM──► Azure gRPC Server ◄──PCM←Opus── Gateway
  │                        │                         │
  │                  ┌─────┘                         │
  │  Translated Opus                                 │  Translated Opus
  ▼
SIP/Linphone                              Browser (WebRTC)
```

## Dependencies

- **gRPC** — `google.golang.org/grpc` (v1.80+)
- **Protobuf** — `google.golang.org/protobuf` (v1.36+)
- **libopus** — Opus codec via CGo (`github.com/hraban/opus.v2`)
  - Requires `libopus-dev` (Linux) or `opus.dll` (Windows) at runtime when CGO_ENABLED=1
  - Falls back to a no-op stub when `CGO_ENABLED=0` (translation will fail gracefully)

## Configuration

Environment variables (see `.env.example`):

| Variable | Default | Description |
|----------|---------|-------------|
| `TRANSLATOR_ENABLE` | `false` | Enable translation client on startup |
| `TRANSLATOR_ADDR` | `localhost:5000` | gRPC server address |
| `TRANSLATOR_SOURCE_LANG` | `en-US` | Source language code/locale |
| `TRANSLATOR_TARGET_LANG` | `th` | Target language code |
| `TRANSLATOR_TTS_VOICE` | `th-TH-PremwadeeNeural` | Default/fallback TTS voice name |
| `TRANSLATOR_OPUS_BITRATE` | `24000` | Opus encoding bitrate |

### Example `.env`
```
TRANSLATOR_ENABLE=true
TRANSLATOR_ADDR=192.168.1.100:5000
TRANSLATOR_SOURCE_LANG=en-US
TRANSLATOR_TARGET_LANG=th
TRANSLATOR_TTS_VOICE=th-TH-PremwadeeNeural
TRANSLATOR_OPUS_BITRATE=24000
```

## Protocol

The proto service definition lives at `proto/translator.proto` (project root).

Service: `SpeechTranslator.Translate` (bidirectional stream)

### Request
```protobuf
TranslationRequest {
  source_language = "en-US"
  target_language = "th"
  return_audio = true
  tts_voice_name = "th-TH-PremwadeeNeural"
  audio_data = <PCM int16 bytes>
  mode = MODE_S2S
}
```

### Response
```protobuf
TranslationResult {
  recognized_text = "..."
  translated_text = "..."
  audio_data = <PCM int16 bytes>
  tts_voice_used = "th-TH-PremwadeeNeural"
}
```

## WebSocket API

Translation is activated **per-call** via WebSocket messages. No translation happens unless the client explicitly enables it.

The request direction is interpreted as the WebRTC -> SIP direction. The gateway automatically enables the reverse SIP -> WebRTC direction.

### Enable translation

Client → Server:
```json
{
  "type": "translate",
  "sessionId": "AbCdEfGh1234",
  "sourceLang": "en",
  "targetLang": "th",
  "ttsVoice": "th-TH-PremwadeeNeural"
}
```

For this example:

- WebRTC -> SIP/Linphone uses `en -> th` with `th-TH-PremwadeeNeural`.
- SIP/Linphone -> WebRTC uses `th -> en` with an English voice selected by the gateway.

Server → Client:
```json
{
  "type": "translate",
  "sessionId": "AbCdEfGh1234",
  "state": "enabled",
  "sourceLang": "en",
  "targetLang": "th",
  "ttsVoice": "th-TH-PremwadeeNeural"
}
```

The response preserves the requested WebRTC -> SIP direction for backward compatibility. It does not currently expose the derived reverse direction.

### Disable translation

Client → Server:
```json
{
  "type": "translate_stop",
  "sessionId": "AbCdEfGh1234"
}
```

Server → Client:
```json
{
  "type": "translate_stop",
  "sessionId": "AbCdEfGh1234",
  "state": "disabled"
}
```

## Audio Pipeline Details

### WebRTC -> SIP/Linphone

1. **WebRTC Opus RTP** arrives at `forwardRTPToAsterisk()` in `internal/session/rtp_forward.go`.
2. If `session.TranslatorEnabled == true` and `session.Translator != nil`:
   - Opus payload is decoded to PCM `int16` via `translator.OpusCodec.Decode()`
   - PCM is sent to the Azure gRPC `Translate` stream as `TranslationRequest{mode: MODE_S2S, return_audio: true}`
   - Response `TranslationResult.audio_data` (PCM) is received
   - PCM is re-encoded to Opus via `translator.OpusCodec.Encode()`
   - Translated Opus replaces the original packet's payload
   - The rewritten packet continues through the existing RTP rewrite logic (SSRC, seq, PT)
3. On any error (decode/gRPC/encode), the gateway logs the error and falls back to **original audio passthrough**.

### SIP/Linphone -> WebRTC

1. **SIP/Linphone Opus RTP** arrives at `handleAudioRTPPacketsForSession()` in `internal/sip/rtp.go`.
2. If `session.TranslatorEnabled == true` and `session.InboundTranslator != nil`:
   - Opus payload is decoded to PCM `int16`.
   - PCM is sent to the Azure gRPC `Translate` stream using the reverse language direction.
   - Response `TranslationResult.audio_data` (PCM) is received.
   - PCM is re-encoded to Opus.
   - Translated Opus replaces the packet payload.
3. Optional inbound gain (`SIP_AUDIO_INBOUND_GAIN_ENABLE`) runs after inbound translation.
4. The packet payload type is rewritten to WebRTC Opus PT `111` when needed and written to the WebRTC audio track.
5. On any error, the gateway logs the error and falls back to **original SIP audio passthrough**.

## TTS Voice Selection

The WebRTC -> SIP direction uses the `ttsVoice` provided in the `translate` message, or a gateway default for the requested target language.

The SIP -> WebRTC direction automatically selects a TTS voice from the reverse target language:

| Target language | Voice |
|-----------------|-------|
| `en`, `en-*` | `en-US-AriaNeural` |
| `th`, `th-*` | `th-TH-PremwadeeNeural` |
| Other | `TRANSLATOR_TTS_VOICE`, or `en-US-AriaNeural` when unset |

## Key Source Files

| File | Role |
|------|------|
| `internal/translator/pb/translator.pb.go` | Proto message types |
| `internal/translator/pb/translator_grpc.pb.go` | gRPC client stubs |
| `internal/translator/client.go` | `Client` struct: Connect, CheckHealth, TranslateStream |
| `internal/translator/opus.go` | `OpusCodec` interface |
| `internal/translator/opus_cgo.go` | Real Opus codec via libopus CGo (`//go:build cgo`) |
| `internal/translator/opus_stub.go` | No-op stub when `CGO_ENABLED=0` (`//go:build !cgo`) |
| `internal/translator/s2s.go` | `S2SPipeline` — orchestrator: decode→send→recv→encode |
| `internal/session/session.go` | Bidirectional translator state plus `SetTranslator()`, `EnableTranslator()`, `DisableTranslator()`, `ProcessInboundTranslator()` |
| `internal/session/rtp_forward.go` | WebRTC -> SIP audio fork to `S2SPipeline.Process()` |
| `internal/sip/rtp.go` | SIP -> WebRTC audio fork to `ProcessInboundTranslator()` |
| `internal/api/ws_dispatch.go` | WS message dispatch for `translate`/`translate_stop` |
| `internal/api/ws_translate.go` | `handleWSTranslate()`, `handleWSTranslateStop()`, reverse direction and TTS voice selection |
| `internal/config/config.go` | `TranslatorConfig` struct + env loading |
| `main.go` | Translator client init + health check |
| `docs/bidirectional-s2s-translation.md` | Phase 1 and Phase 2 implementation plan |

## Build Requirements

The real Opus codec requires CGo and libopus:

```bash
# Linux (install libopus)
apt install libopus-dev   # Debian/Ubuntu
yum install libopus-devel # RHEL/CentOS

# Build with CGo
CGO_ENABLED=1 go build -o webrtc-sip-gateway .
```

Without CGo, the stub codec is used and translation will log decode/encode errors and fall back to passthrough audio.

## Troubleshooting

**Q: Translation is enabled but audio still sounds original (untranslated)**
- Check gateway logs via `https://gateway.example.com/api/logs/current?tail=500` for `Translation error` lines — indicates gRPC or codec failure, passthrough fallback active
- Verify `TRANSLATOR_ADDR` points to a running Azure gRPC server
- Run `grpcurl -plaintext <addr>:5000 list` to verify server is reachable
- Check `CGO_ENABLED=1` and `libopus` is installed

**Q: Client sends `translate` but gets `"Translator not available"` error**
- `TRANSLATOR_ENABLE` is `false` or the gRPC connection failed at startup
- Check gateway logs via `https://gateway.example.com/api/logs/current?tail=500` for `Translator client failed to connect` or `Translator health check failed`
