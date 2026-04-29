# S2S Speech Translation Pipeline

## Overview

This feature enables real-time Speech-to-Speech (S2S) translation of audio during active WebRTC calls. Audio from the WebRTC browser/mobile client is decoded from Opus, sent to an external Azure gRPC translation server, and the translated audio is re-encoded to Opus and forwarded to the SIP peer (Asterisk).

Architecture:

```
Browser (WebRTC)
  │  Opus RTP
  ▼
Gateway ──Opus→PCM──► Azure gRPC Server
  │                        │
  │                  ┌─────┘
  │  Translated PCM
  ▼
Asterisk (SIP)
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
| `TRANSLATOR_SOURCE_LANG` | `en` | Source language code |
| `TRANSLATOR_TARGET_LANG` | `th` | Target language code |
| `TRANSLATOR_TTS_VOICE` | `th-TH-Sarawut` | TTS voice name |
| `TRANSLATOR_OPUS_BITRATE` | `24000` | Opus encoding bitrate |

### Example `.env`
```
TRANSLATOR_ENABLE=true
TRANSLATOR_ADDR=192.168.1.100:5000
TRANSLATOR_SOURCE_LANG=en
TRANSLATOR_TARGET_LANG=th
TRANSLATOR_TTS_VOICE=th-TH-Sarawut
TRANSLATOR_OPUS_BITRATE=24000
```

## Protocol

The proto service definition lives at `proto/translator.proto` (project root).

Service: `SpeechTranslator.Translate` (bidirectional stream)

### Request
```protobuf
TranslationRequest {
  source_language = "en"
  target_language = "th"
  return_audio = true
  tts_voice_name = "th-TH-Sarawut"
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
  tts_voice_used = "th-TH-Sarawut"
}
```

## WebSocket API

Translation is activated **per-call** via WebSocket messages. No translation happens unless the client explicitly enables it.

### Enable translation

Client → Server:
```json
{
  "type": "translate",
  "sessionId": "AbCdEfGh1234"
}
```

Server → Client:
```json
{
  "type": "translate",
  "sessionId": "AbCdEfGh1234",
  "state": "enabled"
}
```

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

1. **WebRTC Opus RTP** arrives at `forwardRTPToAsterisk()` in `internal/session/rtp_forward.go`
2. If `session.TranslatorEnabled == true` and `session.Translator != nil`:
   - Opus payload is decoded to PCM `int16` via `translator.OpusCodec.Decode()`
   - PCM is sent to the Azure gRPC `Translate` stream as `TranslationRequest{mode: MODE_S2S, return_audio: true}`
   - Response `TranslationResult.audio_data` (PCM) is received
   - PCM is re-encoded to Opus via `translator.OpusCodec.Encode()`
   - Translated Opus replaces the original packet's payload
   - The rewritten packet continues through the existing RTP rewrite logic (SSRC, seq, PT)
3. On any error (decode/gRPC/encode): logs the error and falls back to **original audio passthrough** — no audio loss

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
| `internal/session/session.go` | `SetTranslator()`, `EnableTranslator()`, `DisableTranslator()` methods |
| `internal/session/rtp_forward.go` | Audio fork to `S2SPipeline.Process()` in the RTP forward loop |
| `internal/api/server.go` | WS message dispatch for `translate`/`translate_stop` |
| `internal/api/handlers.go` | `handleWSTranslate()`, `handleWSTranslateStop()` |
| `internal/config/config.go` | `TranslatorConfig` struct + env loading |
| `main.go` | Translator client init + health check |

## Build Requirements

The real Opus codec requires CGo and libopus:

```bash
# Linux (install libopus)
apt install libopus-dev   # Debian/Ubuntu
yum install libopus-devel # RHEL/CentOS

# Build with CGo
CGO_ENABLED=1 go build -o k2-gateway .
```

Without CGo, the stub codec is used and translation will log decode/encode errors and fall back to passthrough audio.

## Troubleshooting

**Q: Translation is enabled but audio still sounds original (untranslated)**
- Check gateway logs for `Translation error` lines — indicates gRPC or codec failure, passthrough fallback active
- Verify `TRANSLATOR_ADDR` points to a running Azure gRPC server
- Run `grpcurl -plaintext <addr>:5000 list` to verify server is reachable
- Check `CGO_ENABLED=1` and `libopus` is installed

**Q: Client sends `translate` but gets `"Translator not available"` error**
- `TRANSLATOR_ENABLE` is `false` or the gRPC connection failed at startup
- Check gateway logs for `Translator client failed to connect` or `Translator health check failed`
