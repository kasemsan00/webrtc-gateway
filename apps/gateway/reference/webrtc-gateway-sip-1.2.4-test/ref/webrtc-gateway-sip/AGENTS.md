# K2 Gateway - AI Agent Development Guide

> **Role:** Senior Go Engineer / Systems Architect
> **Objective:** Maintain a high-performance WebRTC-to-SIP bridge.
> **Constraint:** Stability is paramount. This system bridges real-time calls; panics or race conditions are unacceptable.

---

## 1. Project Overview

**K2 Gateway** is a specialized bridge connecting WebRTC clients (Browsers, React Native, Mobile) to legacy SIP/RTP servers (Asterisk, Kamailio).

- **Language:** Go 1.25.5
- **Core Libraries:**
  - `pion/webrtc/v4` - WebRTC stack
  - `emiago/sipgo` - SIP signaling
  - `gorilla/websocket` - WebSocket transport
  - `gorilla/mux` - HTTP router
  - `pion/rtp`, `pion/rtcp` - RTP/RTCP handling
  - `pion/sdp/v3` - SDP parsing
- **Architecture:**
  - **Signaling:** Translates JSON (WebSocket) <-> SIP (UDP/TCP).
  - **Media:** Translates SRTP (WebRTC) <-> RTP (UDP). Handles H.264 SPS/PPS injection.
  - **Audio:** Opus passthrough end-to-end (no transcoding). Browser ↔ Gateway ↔ Asterisk all use Opus/48kHz.

### Operating Modes

1.  **API Mode (Default):** Runs an HTTP/WebSocket server. Manages multiple concurrent sessions.
2.  **Legacy Mode (`--legacy`):** Single-session, stdin/stdout signaling. (Keep for backward compatibility).

### LogStore Integration Status (2026-01-28)

- **Phases 1-4 complete:** PostgreSQL schema, async LogStore, full signaling hooks (WS/REST + SIP), and wiring in both API & legacy modes.
- **Coverage:** ~30 structured events per call, SDP/SIP payload capture, session/dialog snapshots, SIP MESSAGE + DTMF logging.
- **Next steps:**
  - Optional SQL helper scripts (Phase 5) for partition maintenance helpers (create/drop/check).
  - Phase 6 validation: DB connectivity, partition auto-creation, event/payload/session ingestion, queue backpressure behavior, and retention checks.

---

## 2. Project Structure

```text
k2-gateway/
├── main.go                     # Entry point (Flag parsing, mode selection)
├── internal/
│   ├── api/                    # HTTP & WebSocket Layer
│   │   ├── server.go           # WS handling, JSON parsing, Keep-alives
│   │   └── handlers.go         # REST API endpoints
│   ├── config/
│   │   └── config.go           # Configuration (Env vars -> Structs)
│   ├── logger/
│   │   └── logger.go           # Structured logging wrapper
│   ├── session/
│   │   └── manager.go          # CORE: WebRTC <-> RTP binding, PeerConnection management
│   ├── sip/                    # SIP Layer
│   │   ├── server.go           # SIP Listener (TCP/UDP)
│   │   ├── call.go             # INVITE/BYE flows
│   │   ├── dtmf.go             # RFC 2833 DTMF (RTP events)
│   │   ├── registration.go     # REGISTER flow (static & dynamic)
│   │   ├── dialog.go           # SIP state machine
│   │   ├── handlers.go         # SIP request handlers (INVITE, BYE, ACK, MESSAGE)
│   │   ├── rtp.go              # RTP packet forwarding & processing
│   │   ├── sdp.go              # SDP generation & parsing
│   │   ├── ice.go              # ICE candidate handling
│   │   └── message.go          # SIP MESSAGE (instant messaging)
│   ├── webrtc/
│   │   └── webrtc.go           # WebRTC utilities
│   └── pkg/webrtc/
│       └── utils.go            # Shared WebRTC helpers
├── web/                        # Embedded Test Client
│   ├── index.html
│   ├── app.js
│   └── styles.css
└── docs/                       # Platform Integration Guides
    ├── web.md
    ├── react-native.md
    ├── ios.md
    ├── android.md
    └── call-resume.md
```

---

## 3. WebSocket Protocol (Strict Specification)

**Endpoint:** `/ws`
**Format:** JSON

### Client -> Server (Requests)

| `type`         | Required Fields                                      | Description                                                             |
| :------------- | :--------------------------------------------------- | :---------------------------------------------------------------------- |
| `offer`        | `sdp`                                                | WebRTC SDP Offer from browser. Returns `answer`.                        |
| `call`         | `sessionId`, `destination`, `from`                   | Initiate outbound SIP INVITE.                                           |
| `hangup`       | `sessionId`                                          | Terminate call (SIP BYE).                                               |
| `accept`       | `sessionId`                                          | **(Incoming Call)** User accepted call.                                 |
| `reject`       | `sessionId`, `reason`                                | **(Incoming Call)** User rejected call. Reason: `busy`, `decline`, etc. |
| `dtmf`         | `sessionId`, `digits`                                | Send DTMF tones via RFC 2833.                                           |
| `register`     | `sipDomain`, `sipUsername`, `sipPassword`, `sipPort` | Dynamic SIP Registration.                                               |
| `unregister`   | -                                                    | Remove current dynamic registration.                                    |
| `send_message` | `destination`, `body`, `contentType`?                | Send SIP MESSAGE (Instant Message).                                     |
| `resume`       | `sessionId`, `sdp`?                                  | Reconnect to existing session after network change.                     |
| `ping`         | -                                                    | Keep-alive. Expect `pong`.                                              |

### Server -> Client (Responses/Events)

| `type`           | Key Fields                                 | Description                                                                       |
| :--------------- | :----------------------------------------- | :-------------------------------------------------------------------------------- |
| `answer`         | `sdp`, `sessionId`                         | SDP Answer for WebRTC offer.                                                      |
| `state`          | `sessionId`, `state`                       | Session state: `new`, `connecting`, `ringing`, `active`, `reconnecting`, `ended`. |
| `incoming`       | `sessionId`, `from`, `to`                  | **Event:** Incoming SIP INVITE.                                                   |
| `registerStatus` | `registered` (bool), `sipDomain`           | Result of register/unregister.                                                    |
| `message`        | `from`, `to`, `body`, `contentType`        | **Event:** Incoming SIP MESSAGE.                                                  |
| `dtmf`           | `sessionId`, `digits`                      | **Event:** Incoming DTMF from SIP peer.                                           |
| `messageSent`    | `destination`, `body`                      | Confirmation of sent SIP MESSAGE.                                                 |
| `resumed`        | `sessionId`, `state`, `from`, `to`, `sdp`? | Session successfully resumed.                                                     |
| `resume_failed`  | `sessionId`, `reason`                      | Resume failed (session expired/invalid).                                          |
| `pong`           | -                                          | Keep-alive response.                                                              |
| `error`          | `error`, `sessionId`?                      | Something went wrong.                                                             |

---

## 4. Coding Standards & Patterns

### 4.1 Concurrency & Safety

- **Mutexes:** This is a highly concurrent system.
  - Use `sync.RWMutex` for all struct fields accessed by multiple goroutines (especially in `session.Session`).
  - **Rule:** Lock `defer Unlock()` immediately. Do not hold locks across network calls (blocking SIP/HTTP).
- **Context:** Propagate `context.Context` for timeouts and cancellation.
  - `func (s *Server) DoSomething(ctx context.Context, ...)`

### 4.2 Error Handling

- **Wrap Errors:** Use `fmt.Errorf("action failed: %w", err)`.
- **Log & Continue:** In main loops (RTP forwarding), log errors but **do not panic**.
- **Fatal:** Only in `main.go` startup.

### 4.3 Logging

- Use `k2-gateway/internal/logger`.
- **Format:** `log.Printf("EMOJI [Context] Message: %v", args)`
- **Emojis:**
  - 📞 SIP Signaling
  - 🎬 WebRTC Media
  - 🚀 Performance / Optimization
  - 💬 SIP Messaging
  - 🔄 Session Resume / Reconnection
  - ⚠️ Warnings
  - ❌ Errors
  - ✅ Success

### 4.4 Media Handling (Crucial)

- **H.264 Only:** We force H.264 for video.
- **SPS/PPS Injection:** SIP endpoints (Linphone/Kamailio) often fail if keyframes lack SPS/PPS headers.
  - **Logic:** `session/manager.go` caches SPS/PPS from the SDP or RTP stream and _re-injects_ them before every Keyframe (IDR).
  - **Cache updates:** If the RTP stream delivers new SPS/PPS values, refresh the cache to avoid injecting stale parameter sets.
  - **Do NOT remove this logic.** It is the "magic" fix for black screens.
- **Audio (Opus-only passthrough):**
  - **No transcoding.** Gateway forwards Opus RTP packets unchanged in both directions.
  - **WebRTC side:** Browser negotiates Opus (48kHz stereo).
  - **SIP side:** Gateway offers Opus-only (`m=audio ... 111 101`). Asterisk **must** be configured with `allow=opus` in `sip.conf`.
  - **Critical:** Both legs use the same codec (Opus). Codec mismatch = broken/static audio.
  - **DTMF:** Bidirectional via RFC 2833 (payload type 101).

---

## 5. Development Workflow

### Build & Run

```bash
# Build
go build -o k2-gateway .

# Run (Dev)
./k2-gateway

# Run (Production/Docker)
docker-compose up -d
```

### Testing

- **Status:** No unit tests currently exist.
- **Agent Task:** If asked to fix a bug, create a reproduction test case in a `_test.go` file within the relevant package if possible, or verify manually via the `web/` client.

### Common Tasks

1.  **New SIP Header:** Modify `internal/sip/call.go` to add custom headers.
2.  **RTP Debugging:** Check `internal/sip/rtp.go` and `internal/session/manager.go` RTP loops.
3.  **API Expansion:** Add struct fields to `WSMessage` in `internal/api/server.go`.
4.  **SIP Messaging:** Check `internal/sip/message.go` for MESSAGE handling.
5.  **Audio Issues:** Verify Asterisk is configured with `allow=opus` in `sip.conf`. Check RTP payload type logs (should be PT=111 for Opus).

---

## 6. Environment Variables (`.env`)

### Core Configuration

| Variable         | Default   | Notes                                                   |
| :--------------- | :-------- | :------------------------------------------------------ |
| `SIP_LOCAL_IP`   | `0.0.0.0` | Bind address. Use specific IPv4 to prevent IPv6 issues. |
| `SIP_PUBLIC_IP`  | -         | **Critical for NAT.** Must be the external IP.          |
| `SIP_LOCAL_PORT` | `5060`    | Local SIP port to bind.                                 |
| `SIP_PORT`       | `5060`    | Target SIP server port.                                 |

### API Server

| Variable           | Default | Notes                            |
| :----------------- | :------ | :------------------------------- |
| `API_PORT`         | `8080`  | HTTP/WebSocket server port.      |
| `API_ENABLE_WS`    | `true`  | Enable WebSocket endpoint `/ws`. |
| `API_ENABLE_REST`  | `true`  | Enable REST API `/api/*`.        |
| `API_CORS_ORIGINS` | `*`     | CORS allowed origins.            |

### RTP Media

| Variable          | Default | Notes                                               |
| :---------------- | :------ | :-------------------------------------------------- |
| `RTP_PORT_MIN`    | `10500` | UDP range start.                                    |
| `RTP_PORT_MAX`    | `10600` | UDP range end. Supports ~25 concurrent video calls. |
| `RTP_BUFFER_SIZE` | `4096`  | RTP packet buffer size in bytes.                    |

### AVPF/RTCP Feedback

| Variable             | Default | Notes                                                               |
| :------------------- | :------ | :------------------------------------------------------------------ |
| `SIP_AUDIO_USE_AVPF` | `false` | Use RTP/AVPF for audio. Enables RTCP feedback for audio stream.     |
| `SIP_VIDEO_USE_AVPF` | `false` | Use RTP/AVPF for video with PLI/FIR/NACK. Better keyframe recovery. |

**Important:** AVPF requires SIP endpoint support:

- **Asterisk chan_sip:** May need `avpf=yes` in peer config (version-dependent)
- **Asterisk res_pjsip:** Set `use_avpf=yes` in endpoint config
- If Asterisk rejects video (port=0), check logs at `internal/sip/sdp.go:145` for troubleshooting

### TURN Server

| Variable        | Default | Notes                                                     |
| :-------------- | :------ | :-------------------------------------------------------- |
| `TURN_SERVER`   | -       | TURN server URL (e.g., `turn:server:3478?transport=udp`). |
| `TURN_USERNAME` | -       | TURN authentication username.                             |
| `TURN_PASSWORD` | -       | TURN authentication password.                             |

### Debug Flags

| Variable              | Default | Notes                                        |
| :-------------------- | :------ | :------------------------------------------- |
| `DEBUG_WEBSOCKET`     | `false` | Log WebSocket ping/pong for troubleshooting. |
| `DEBUG_SIP_MESSAGE`   | `false` | Log full SIP MESSAGE headers/body.           |
| `DEBUG_TURN`          | `false` | Log ICE/TURN candidate selection details.    |
| `SWITCH_PLI_DELAY_MS` | `1000`  | Delay before PLI on `@switch` message.       |

---

## 7. Asterisk Configuration (Required)

### Audio Codec (Opus-only)

The gateway **requires** Asterisk to use Opus for audio. In `sip.conf` for the peer/user/trunk used by the gateway:

```ini
[your-gateway-peer]
type=peer
host=dynamic
context=from-gateway
disallow=all
allow=opus
dtmfmode=rfc2833
```

**Critical:** Without `allow=opus`, calls will fail or have broken/static audio.

**Verification:**

- `asterisk -rx "sip show peer <peername>"` should list only Opus in codec capability.
- Asterisk SDP answer should include `m=audio ... 111 101` (Opus + telephone-event).

---

## 7. Key Features

### 7.1 Call Resume (Network Recovery)

When a mobile client switches networks (WiFi -> LTE), the WebSocket reconnects and sends:

```json
{ "type": "resume", "sessionId": "abc123", "sdp": "<new offer>" }
```

The gateway renegotiates the PeerConnection while preserving the SIP dialog.

### 7.2 Dynamic SIP Registration

Clients can register dynamically without restarting the gateway:

```json
{ "type": "register", "sipDomain": "sip.example.com", "sipUsername": "1001", "sipPassword": "secret", "sipPort": 5060 }
```

### 7.3 SIP Messaging

In-dialog or out-of-dialog instant messaging:

```json
{ "type": "send_message", "destination": "sip:1002@sip.example.com", "body": "Hello!" }
```

### 7.4 DTMF (Bidirectional)

- **WebRTC -> SIP:** Client sends `{"type": "dtmf", "sessionId": "...", "digits": "123#"}`
- **SIP -> WebRTC:** Server sends `{"type": "dtmf", "sessionId": "...", "digits": "4"}`
