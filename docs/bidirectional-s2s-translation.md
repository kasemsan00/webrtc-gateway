# Bidirectional S2S Translation Implementation Plan

## Goal

When a WebRTC frontend enables translation per call, the gateway should translate both call directions:

- WebRTC/frontend audio -> SIP/Linphone audio uses the selected direction, for example `en -> th`.
- SIP/Linphone audio -> WebRTC/frontend audio automatically uses the reverse direction, for example `th -> en`.

The WebSocket contract should stay backward compatible for Phase 1. Clients continue to send:

```json
{
  "type": "translate",
  "sessionId": "...",
  "sourceLang": "en",
  "targetLang": "th",
  "ttsVoice": "th-TH-PremwadeeNeural"
}
```

The gateway derives the reverse pipeline as:

```text
outbound: sourceLang -> targetLang, ttsVoice from the request/default
inbound:  targetLang -> sourceLang, ttsVoice selected by inbound target language
```

For `en -> th`, Linphone speech is therefore treated as `th -> en`, and the frontend receives English translated speech.

## Phase 1: Reuse Existing Packet-Level Pipeline

Phase 1 keeps the existing `S2SPipeline.Process(packet)` model and adds a second pipeline on each session.

```text
WebRTC -> Gateway -> SIP
  Opus RTP
  -> outbound S2SPipeline en -> th
  -> SIP payload type rewrite
  -> Linphone

Linphone -> Gateway -> WebRTC
  Opus RTP
  -> inbound S2SPipeline th -> en
  -> optional inbound gain
  -> WebRTC payload type rewrite to 111
  -> frontend
```

### Phase 1 Implementation Steps

1. Extend `session.Session` with an inbound translator pipeline and reverse language metadata.
2. Update `SetTranslator()` to configure both outbound and inbound directions.
3. Update `EnableTranslator()` to create two independent Opus codec instances, one per direction.
4. Update `DisableTranslator()` to stop and clear both pipelines.
5. Add `ProcessInboundTranslator()` and call it from `sip.handleAudioRTPPacketsForSession()` before inbound gain and before rewriting payload type to WebRTC PT `111`.
6. Keep fallback behavior: if reverse translation fails for a packet, forward the original SIP audio.
7. Keep WebSocket messages compatible. `translate_stop` disables both directions.
8. Add focused tests around pipeline lifecycle and reverse TTS voice selection.

### Phase 1 Advantages

- Smallest change to the media path.
- Reuses existing codec and gRPC code.
- Keeps current frontend/mobile contract intact.
- Easy fallback to original audio packet on translator failure.
- Good first milestone for validating call flow and product behavior.

### Phase 1 Limits

- Current `S2SPipeline.Process()` opens a translate stream per RTP packet.
- Audio packets are typically about 20 ms, which is too little context for high-quality speech translation.
- Per-packet translation can increase gRPC overhead, CPU usage, and end-to-end latency.
- Translation quality depends heavily on how tolerant the external translator service is of small audio chunks.

## Phase 2: Continuous Streaming/Buffering Pipeline

Phase 2 replaces packet-level translation with long-lived per-direction audio streams.

```text
RTP packets
-> jitter/buffer window
-> decode Opus to PCM stream
-> long-lived gRPC translation stream
-> receive translated PCM chunks
-> encode Opus
-> packetize RTP with controlled seq/timestamp
-> forward
```

### Phase 2 Implementation Steps

1. Define per-direction stream workers owned by the session.
2. Add bounded queues for inbound RTP packets to avoid blocking RTP read loops on translator latency.
3. Add PCM buffering with target chunk size and max latency budget.
4. Keep one long-lived gRPC stream per direction while translation is enabled.
5. Add RTP packetization for translated output, including sequence number and timestamp continuity.
6. Add backpressure behavior: drop, passthrough, or temporarily disable translation when queues exceed limits.
7. Add observability: queue depth, chunk latency, translator round-trip time, drops, fallback count, and stream reconnects.
8. Add integration tests with a fake translator stream that emits delayed chunks.

### Phase 2 Advantages

- Better suited for real speech translation because the translator gets continuous context.
- Lower gRPC overhead than opening a stream per packet.
- More stable latency once the stream is warm.
- Allows future voice activity detection, phrase boundary handling, and smoother TTS output.

### Phase 2 Risks

- Higher implementation complexity.
- Needs careful jitter, buffering, timestamp, and sequence-number handling.
- Backpressure decisions affect user experience directly.
- More room for media regressions such as one-way audio, drift, or bursty playback.

## Recommended Rollout

1. Ship Phase 1 first to validate bidirectional product behavior with Linphone and the existing frontend UI.
2. Measure logs and call quality: translator errors, fallback rate, perceived latency, and CPU load.
3. Use those measurements to tune Phase 2 chunk sizing and queue policies.
4. Implement Phase 2 behind a separate runtime flag so production can fall back to Phase 1 quickly.
