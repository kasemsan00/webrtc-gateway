# S2S Speech Translation Pipeline

## Overview

This feature enables per-call Speech-to-Speech (S2S) translation of audio during active WebRTC calls on `/ws` and `/ws-public`. When a WebRTC client enables translation, the gateway creates two persistent streaming translation pipelines:

- **Outbound (WebRTC/browser/mobile → SIP/Linphone):** uses the requested direction, for example `en -> th`.
- **Inbound (SIP/Linphone → WebRTC/browser/mobile):** automatically uses the reverse direction, for example `th -> en`, and emits live `translation_caption` WebSocket events back to the WebRTC client.

In both directions, 48 kHz Opus RTP audio is decoded to PCM, resampled to 16 kHz, streamed in 40 ms chunks to an external Azure gRPC translation server, upsampled back to 48 kHz, re-encoded into 20 ms Opus frames, and paced out onto incoming RTP packet cadence.

> **Important audio behavior:** While translation is enabled on a session (`TranslatorEnabled == true`), **original untranslated audio is always suppressed/dropped** — both while waiting for translated speech synthesis (`out == nil`) and on any Opus decode/encode or gRPC error. There is **no** fallback to original-audio passthrough while translation remains active; call `translate_stop` to restore normal Opus passthrough.

Architecture:

```
Browser (WebRTC)                          SIP/Linphone
  │  48kHz Opus RTP                         │  48kHz Opus RTP
  ▼                                         ▼
Gateway ──decode+48k→16k──► Azure gRPC ◄──16k←48k+decode── Gateway
  │       (1280B / 40ms)    BiDi Stream   (1280B / 40ms)     │
  │                              │                           │
  │  pendingOpusFrames (20ms) ◄──┴──► pendingOpusFrames      │
  │  + MODE_T2S fallback              + translation_caption  │
  ▼                                                          ▼
SIP/Linphone                              Browser (WebRTC)
```

## Dependencies

- **gRPC** — `google.golang.org/grpc` (v1.80+)
- **Protobuf** — `google.golang.org/protobuf` (v1.36+)
- **libopus (direct CGo)** — `<opus.h>` bound directly via `#cgo pkg-config: opus` in `internal/translator/opus_cgo.go` (does **not** use `github.com/hraban/opus.v2`)
  - Requires `libopus-dev` / `pkg-config` (Linux) or libopus headers + library (Windows) at build/runtime when `CGO_ENABLED=1`
  - Compiles `internal/translator/opus_stub.go` when `CGO_ENABLED=0`, returning an error on `NewOpusCodec` / `Decode` / `Encode` (if `NewOpusCodec` fails during `EnableTranslator()`, translation stays disabled; if decode/encode errors occur while enabled, audio is dropped/muted)

## Configuration

Environment variables (see `apps/gateway/.env.example`):

| Variable | Default | Description |
|----------|---------|-------------|
| `TRANSLATOR_ENABLE` | `false` | Enable translation gRPC client on startup |
| `TRANSLATOR_ADDR` | `localhost:5000` | gRPC server address |
| `TRANSLATOR_SOURCE_LANG` | `en-US` | Default outbound source language code/locale |
| `TRANSLATOR_TARGET_LANG` | `th` | Default outbound target language code |
| `TRANSLATOR_TTS_VOICE` | `th-TH-PremwadeeNeural` | Fallback TTS voice for unmapped reverse/inbound target languages |
| `TRANSLATOR_OPUS_BITRATE` | `24000` | Parsed in `config.go` / shown in `GET /api/config`; note `Session.EnableTranslator()` currently hardcodes `24000` bps |

### Example `.env`
```env
TRANSLATOR_ENABLE=true
TRANSLATOR_ADDR=192.168.1.100:5000
TRANSLATOR_SOURCE_LANG=en-US
TRANSLATOR_TARGET_LANG=th
TRANSLATOR_TTS_VOICE=th-TH-PremwadeeNeural
TRANSLATOR_OPUS_BITRATE=24000
```

## Protocol

The proto service definition lives at `proto/translator.proto` (repo root) and generates `apps/gateway/internal/translator/pb/`.

Service: `SpeechTranslator.Translate(stream TranslationRequest) returns (stream TranslationResult)`

### `TranslationMode` enum

```protobuf
enum TranslationMode {
  MODE_UNSPECIFIED = 0; // Server defaults to S2S if return_audio=true, else S2T
  MODE_S2T = 1;         // Speech-to-Text translation (audio in -> text out)
  MODE_S2S = 2;         // Speech-to-Speech translation (audio in -> text + audio out)
  MODE_T2S = 3;         // Text-to-Speech synthesis (text_input -> audio out)
}
```

### `TranslationRequest`

```protobuf
message TranslationRequest {
  string source_language = 1; // e.g. "en-US", "th-TH"
  string target_language = 2; // e.g. "th", "en"
  bool return_audio = 3;      // true for S2S / T2S
  string tts_voice_name = 4;  // e.g. "th-TH-PremwadeeNeural"
  bytes audio_data = 5;       // 16kHz 16-bit mono PCM (1280-byte / 40ms chunks in MODE_S2S)
  string text_input = 6;      // Text to synthesize in MODE_T2S fallback
  TranslationMode mode = 7;   // MODE_S2S for live stream; MODE_T2S for SynthesizeText fallback
}
```

### `TranslationResult`

```protobuf
message TranslationResult {
  string recognized_text = 1;
  string translated_text = 2;
  bool is_final = 3;
  bytes audio_data = 4;       // 16kHz 16-bit mono PCM synthesized speech
  string tts_voice_used = 5;
  string error_message = 6;
}
```

## WebSocket API

Translation is activated **per-call** via WebSocket messages on **`/ws` and `/ws-public` only** (`translate` and `translate_stop` are rejected on `/ws-agent` and `/ws-agent-device`). On `/ws` and `/ws-public`, omitting `sessionId` falls back to the connection's current `client.sessionID`.

The requested `sourceLang` → `targetLang` applies to the **outbound (WebRTC → SIP)** leg; the gateway automatically derives the reverse **inbound (SIP → WebRTC)** leg (`targetLang` → `sourceLang`, normalizing base tags such as `en` → `en-US`, `th` → `th-TH`, etc. when used as a recognition source locale).

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

Server → Client acknowledgement:
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

`ttsVoice` in the acknowledgement reflects the resolved **outbound** voice (`outboundTTSVoice`).

### Live inbound captions (`translation_caption`)

Whenever the inbound (SIP → WebRTC) pipeline receives a `TranslationResult` with non-empty `recognized_text` or `translated_text`, the gateway sends a `translation_caption` event to the bound WebRTC client:

```json
{
  "type": "translation_caption",
  "sessionId": "AbCdEfGh1234",
  "direction": "sip_to_webrtc",
  "sourceLang": "th",
  "targetLang": "en",
  "recognizedText": "สวัสดีครับ",
  "translatedText": "Hello",
  "isFinal": true
}
```

Captions are emitted only for `direction: "sip_to_webrtc"` (outbound WebRTC → SIP pipelines do not register a caption handler).

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

## Streaming Audio Pipeline Details (`S2SPipeline`)

1. **Persistent BiDi Stream & Buffering (`internal/translator/s2s.go`):**
   - `Start()` opens a persistent `SpeechTranslator.Translate` stream (`MODE_S2S`, `return_audio: true`) and spawns a background `readResponses()` goroutine. If sending or receiving fails, the stream is closed and lazily reconnected on subsequent packets.
   - Each incoming 48 kHz mono Opus RTP packet (`960` samples = `20 ms`) is decoded to `int16` PCM, downsampled `48 kHz → 16 kHz` (3:1 averaging), serialized as little-endian bytes, and appended to `pcmSendBuf`.
   - Whenever `pcmSendBuf` reaches `1280` bytes (`40 ms` at 16 kHz mono 16-bit), chunks of `1280` bytes are sent over the persistent gRPC stream.
2. **Asynchronous Response Reading, Queuing & `MODE_T2S` Fallback:**
   - `readResponses()` reads `TranslationResult` messages from the gRPC stream without blocking the RTP loop.
   - When `result.audio_data` is non-empty (`16 kHz` mono `int16` PCM), it is upsampled `16 kHz → 48 kHz` (linear interpolation), sliced into `960`-sample (`20 ms`) frames (zero-padding any trailing partial frame), Opus-encoded at 48 kHz, and queued in `pendingOpusFrames`.
   - **Automatic `MODE_T2S` TTS fallback:** If `translated_text` is non-empty but `audio_data` is empty — either on `is_final == true`, or on a growing partial caption of at least `18` runes separated by at least `3s` since the previous partial synthesis — `readResponses()` calls `Client.SynthesizeText()` (a short-lived `Translate` stream with `mode: MODE_T2S` and `text_input`) and queues the synthesized Opus frames into `pendingOpusFrames`.
3. **RTP Pacing & Original-Audio Suppression:**
   - **WebRTC → SIP (`internal/session/rtp_forward.go:237-260`):** `s.Translator.Process(&packetCopy)` pops at most **one** queued 20 ms Opus frame per incoming WebRTC audio packet and forwards it through normal outbound SSRC/sequence/payload-type rewriting. When `pendingOpusFrames` is empty (`out == nil`) **or** an error occurs (`err != nil`), `dropPacket = true` suppresses the original packet.
   - **SIP → WebRTC (`internal/sip/rtp.go:393-417`):** `sess.ProcessInboundTranslator(packet)` likewise pops one queued translated Opus frame per incoming SIP audio packet (followed by optional `SIP_AUDIO_INBOUND_GAIN_ENABLE` processing and PT `111` rewrite). When `out == nil` **or** `err != nil`, the loop `continue`s and drops the original SIP audio packet.

## TTS Voice Selection

Both directions resolve target voices through `translatorVoiceForTargetLang()` (`internal/api/ws_translate.go:88-119`).

The **9 built-in base target languages always use their mapped Neural voice**, ignoring `msg.ttsVoice`:

| Target base language (`targetLang`) | Neural Voice (always used) |
|-------------------------------------|----------------------------|
| `th` (`th-*`) | `th-TH-PremwadeeNeural` |
| `en` (`en-*`) | `en-US-AriaNeural` |
| `zh` (`zh-*`) | `zh-CN-XiaoxiaoNeural` |
| `ko` (`ko-*`) | `ko-KR-SunHiNeural` |
| `ja` (`ja-*`) | `ja-JP-NanamiNeural` |
| `ru` (`ru-*`) | `ru-RU-SvetlanaNeural` |
| `hi` (`hi-*`) | `hi-IN-SwaraNeural` |
| `de` (`de-*`) | `de-DE-KatjaNeural` |
| `fr` (`fr-*`) | `fr-FR-DeniseNeural` |

Only when the target language's base code is **not** one of the 9 languages above is the fallback parameter consulted:

- **Outbound (WebRTC → SIP) unmapped target language:** uses `msg.ttsVoice` from the WS `translate` message, or `en-US-AriaNeural` when `msg.ttsVoice` is empty.
- **Inbound (SIP → WebRTC) unmapped target language:** uses `TRANSLATOR_TTS_VOICE`, or `en-US-AriaNeural` when `TRANSLATOR_TTS_VOICE` is empty.

## Key Source Files

| File | Role |
|------|------|
| `proto/translator.proto` | Protobuf service, `TranslationMode` enum, and message definitions |
| `internal/translator/pb/translator.pb.go` | Generated Protobuf message types |
| `internal/translator/pb/translator_grpc.pb.go` | Generated gRPC client/server stubs |
| `internal/translator/client.go` | `Client` struct: `Connect`, `CheckHealth`, `TranslateStream`, `SynthesizeText` (`MODE_T2S`) |
| `internal/translator/opus.go` | `OpusCodec` interface (`48 kHz` mono, `960` max frame samples) |
| `internal/translator/opus_cgo.go` | Real Opus codec via direct `<opus.h>` CGo + `pkg-config: opus` (`//go:build cgo`) |
| `internal/translator/opus_stub.go` | Error-returning stub when `CGO_ENABLED=0` (`//go:build !cgo`) |
| `internal/translator/s2s.go` | `S2SPipeline`: persistent BiDi stream, 48k↔16k resampling, 1280B chunking, async `readResponses`, `pendingOpusFrames` queue |
| `internal/session/session.go` | Bidirectional translator state (`SetTranslator`, `EnableTranslator`, `DisableTranslator`, `ProcessInboundTranslator`) |
| `internal/session/rtp_forward.go` | WebRTC → SIP audio hook + original-audio suppression (`dropPacket = true`) |
| `internal/sip/rtp.go` | SIP → WebRTC audio hook + original-audio suppression |
| `internal/api/ws_dispatch.go` | WS dispatch & endpoint restrictions (`/ws` and `/ws-public` only) |
| `internal/api/ws_translate.go` | `handleWSTranslate()`, `handleWSTranslateStop()`, 9-language Neural voice map |
| `internal/config/config.go` | `TranslatorConfig` struct + env loading |
| `main.go` | Translator client startup connect + `CheckHealth` |

## Build Requirements

The real Opus codec requires `CGO_ENABLED=1`, a C compiler, `pkg-config`, and `libopus`:

```bash
# Linux (install libopus + pkg-config)
apt install pkg-config libopus-dev   # Debian/Ubuntu
yum install pkgconfig opus-devel     # RHEL/CentOS

# Build with CGo (from apps/gateway)
CGO_ENABLED=1 go build -o webrtc-sip-gateway .
```

> **Warning:** Building with `CGO_ENABLED=0` compiles `opus_stub.go`. In that build, `EnableTranslator()` fails with `opus codec requires CGO` (leaving translation disabled), and if a gRPC or codec failure occurs while translation is active, audio is **dropped/muted** rather than passed through untranslated.

## Troubleshooting

**Q: Translation is enabled and call audio becomes silent / muted**
- Remember that active translation suppresses original audio; silence means either speech has not completed recognition/TTS synthesis yet, or gRPC/codec errors are dropping packets.
- Check gateway logs via `https://gateway.example.com/api/logs/current?tail=500` for `Translation error`, `Inbound translation error`, `stream send error`, `stream recv error`, or `Failed to create Opus codec for translator`.
- Verify `TRANSLATOR_ADDR` points to a healthy Azure gRPC translator server (`grpcurl -plaintext <addr>:5000 list`).
- Confirm the gateway binary was built with `CGO_ENABLED=1` and `libopus` installed. Send `translate_stop` to immediately restore untranslated Opus passthrough.

**Q: Client sends `translate` on `/ws-agent` or `/ws-agent-device` and receives an error**
- By design, `translate` and `translate_stop` are only allowed on `/ws` and `/ws-public`.

**Q: Client sends `translate` but gets `"Translator not available"` error**
- `TRANSLATOR_ENABLE` is `false` or the gRPC connection failed at startup.
- Check gateway logs via `https://gateway.example.com/api/logs/current?tail=500` for `Translator client failed to connect` or `Translator health check failed`.
