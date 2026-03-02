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
| `call`         | `sessionId`, `destination`, `from`                   | Initiate outbound SIP INVITE. Supports `trunkId` or SIP Public fields.  |
| `hangup`       | `sessionId`                                          | Terminate call (SIP BYE).                                               |
| `accept`       | `sessionId`                                          | **(Incoming Call)** User accepted call.                                 |
| `reject`       | `sessionId`, `reason`                                | **(Incoming Call)** User rejected call. Reason: `busy`, `decline`, etc. |
| `dtmf`         | `sessionId`, `digits`                                | Send DTMF tones via RFC 2833.                                           |
| `trunk_resolve`| `sipDomain`, `sipUsername`, `sipPassword`, `sipPort` | Resolve trunkId from credentials (plaintext match).                    |
| `send_message` | `destination`, `body`, `contentType`?                | Send SIP MESSAGE (Instant Message).                                     |
| `resume`       | `sessionId`, `sdp`?                                  | Reconnect to existing session after network change.                     |
| `ping`         | -                                                    | Keep-alive. Expect `pong`.                                              |

### Server -> Client (Responses/Events)

| `type`           | Key Fields                                 | Description                                                                       |
| :--------------- | :----------------------------------------- | :-------------------------------------------------------------------------------- |
| `answer`         | `sdp`, `sessionId`                         | SDP Answer for WebRTC offer.                                                      |
| `state`          | `sessionId`, `state`                       | Session state: `new`, `connecting`, `ringing`, `active`, `reconnecting`, `ended`. |
| `incoming`       | `sessionId`, `from`, `to`                  | **Event:** Incoming SIP INVITE.                                                   |
| `trunk_resolved`  | `trunkId`                                  | Trunk resolved for this instance.                                                |
| `trunk_redirect`  | `redirectUrl`                              | Trunk is owned by another instance; reconnect there.                             |
| `trunk_not_found` | `reason`                                   | No matching trunk in DB.                                                         |
| `trunk_not_ready` | `reason`                                   | Trunk found but owner instance unavailable.                                      |
| `message`        | `from`, `to`, `body`, `contentType`        | **Event:** Incoming SIP MESSAGE.                                                  |
| `dtmf`           | `sessionId`, `digits`                      | **Event:** Incoming DTMF from SIP peer.                                           |
| `messageSent`    | `destination`, `body`                      | Confirmation of sent SIP MESSAGE.                                                 |
| `resumed`        | `sessionId`, `state`, `from`, `to`, `sdp`? | Session successfully resumed.                                                     |
| `resume_failed`  | `sessionId`, `reason`                      | Resume failed (session expired/invalid).                                          |
| `resume_redirect`| `sessionId`, `redirectUrl`                 | Resume must reconnect to another instance.                                       |
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

- **Unit Tests:** Run tests with `go test -v ./internal/sip -run TestBuildPublicAccountKey` to verify public account key formatting logic.
- **Agent Task:** If asked to fix a bug, create a reproduction test case in a `_test.go` file within the relevant package if possible, or verify manually via the `web/` client.

### Common Tasks

1.  **New SIP Header:** Modify `internal/sip/call.go` to add custom headers.
2.  **RTP Debugging:** Check `internal/sip/rtp.go` and `internal/session/manager.go` RTP loops.
3.  **API Expansion:** Add struct fields to `WSMessage` in `internal/api/server.go`.
4.  **SIP Messaging:** Check `internal/sip/message.go` for MESSAGE handling.
5.  **Audio Issues:** Verify Asterisk is configured with `allow=opus` in `sip.conf`. Check RTP payload type logs (should be PT=111 for Opus).

### Critical SDP Issues & Troubleshooting

#### 488 Not Acceptable Here (SDP Rejected)

**Symptom:** Asterisk responds with `488 Not acceptable here` after authentication succeeds (after 401/407).

**Common Causes:**

1. **Malformed SDP Origin Field (`o=`)**
   - **Problem:** Empty username in origin line (e.g., `o= 1234567890 ...`)
   - **Valid format:** `o=<username> <sess-id> <sess-version> IN IP4 <address>`
   - **Fix:** In `internal/sip/sdp.go`, the username must come from session context, not global config
   - **Public Mode:** Uses per-session username (e.g., `00025`) from `sess.GetSIPAuthContext()`
   - **Trunk/Legacy Mode:** Falls back to `s.getActiveUsername()` if session username is empty
   - **RFC 4566 Fallback:** Uses `"-"` if both are empty

2. **No Compatible Codecs**
   - Gateway offers **Opus-only** for audio and **H.264-only** for video
   - Asterisk peer **must** have `allow=opus` and `allow=h264`
   - Check Asterisk logs: `asterisk -rvvv` for "No compatible codecs" errors

3. **Video Support Not Enabled**
   - Asterisk peer must have `videosupport=yes` (chan_sip) or endpoint configured for video (res_pjsip)
   - Without video support, Asterisk rejects entire SDP if video is offered

**Verification:**
```bash
# Check Asterisk peer configuration
asterisk -rx "sip show peer <peername>"

# Expected output should show:
#   Codecs: (opus|h264)
#   Video Support: Yes

# View full SIP dialog in Asterisk CLI
asterisk -rvvv
# Then make call and watch for SDP errors
```

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

### SIP Public / Trunk / Gateway

| Variable                                    | Default | Notes                                                     |
| :------------------------------------------ | :------ | :-------------------------------------------------------- |
| `SIP_PUBLIC_REGISTER_EXPIRES_SECONDS`       | `3600`  | SIP Public register expires.                              |
| `SIP_PUBLIC_REGISTER_TIMEOUT_SECONDS`       | `10`    | SIP Public register timeout.                              |
| `SIP_PUBLIC_IDLE_TTL_SECONDS`               | `600`   | Auto cleanup idle public accounts.                        |
| `SIP_PUBLIC_CLEANUP_INTERVAL_SECONDS`       | `30`    | Cleanup interval for public accounts.                     |
| `SIP_PUBLIC_MAX_ACCOUNTS`                   | `1000`  | Max active public accounts.                               |
| `SIP_TRUNK_ENABLE`                          | `true`  | Enable trunk auto-register from DB (requires DB).         |
| `SIP_TRUNK_LEASE_TTL_SECONDS`               | `60`    | Trunk lease TTL (single-active per trunk).                |
| `SIP_TRUNK_LEASE_RENEW_INTERVAL`            | `20`    | Lease renewal interval.                                   |
| `SIP_TRUNK_REGISTER_TIMEOUT`                | `10`    | Trunk register timeout.                                   |
| `GATEWAY_INSTANCE_ID`                       | -       | Unique instance ID (auto-generated if empty).             |
| `GATEWAY_PUBLIC_WS_URL`                     | -       | Public WS URL for resume_redirect.                        |
| `SESSION_DIRECTORY_TTL_SECONDS`             | `7200`  | Session directory TTL.                                    |
| `SESSION_DIRECTORY_CLEANUP_INTERVAL_SECONDS`| `300`   | Cleanup interval for session directory entries.           |

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

## 8. Test Client (`web/`)

The test client (`web/app.js`, `web/index.html`) supports two calling modes:

### 8.1 Mode Selector

UI provides radio buttons to switch between modes:

- **Public Mode (default):** Uses per-call SIP credentials (`sipDomain`, `sipUsername`, `sipPassword`, `sipPort`)
- **SIP Trunk Mode:** Uses `trunkId` for trunk calls (optionally resolve from credentials via `trunk_resolve`)

### 8.2 Call Modes

**Public Mode:**
- Fields: SIP Domain, Username, Password, Port
- Call payload: `{ type: "call", sessionId, destination, sipDomain, sipUsername, sipPassword, sipPort }`
- No trunk resolve needed

**SIP Trunk Mode:**
- Fields: Trunk ID (manual entry) OR Trunk credentials for resolve
- Call payload: `{ type: "call", sessionId, destination, trunkId }`
- Supports `trunk_resolve` to get trunkId from credentials

### 8.3 WebRTC Cleanup Best Practice

When ending call/session, always clear remote video to prevent frozen frames:

```javascript
const remoteVideo = document.getElementById("remoteVideo");
if (remoteVideo) {
  remoteVideo.pause();
  remoteVideo.srcObject = null;
  remoteVideo.load();
}
```

### 8.4 Hangup Button State Management

- Use `activeCallSessionId` separate from `sessionId` to track active calls
- Check `callState` before sending hangup (only send in `connecting`, `ringing`, `active`, `reconnecting`)
- Disable hangup button when state is `ended` or null
- Handle `error: "Session not found"` by treating as call ended and cleaning up UI

### 8.5 Auto-Start Session Flow (Call Button After Hangup Fix)

**Problem:** After ending a call, the Call button becomes disabled and users cannot make a second call without manually clicking "Start Session" again.

**Root Cause:**
1. Server deletes session immediately after `hangup` (`internal/api/server.go:616` calls `DeleteSession()`)
2. Client holds stale `sessionId` and `pc` (PeerConnection) after call ends
3. `updateCallButtonState()` requires `sessionId != null` to enable Call button
4. Result: Call button disabled until user manually starts new session

**Solution (Implemented 2026-01-30):**

1. **Auto-cleanup on call end** (`web/app.js:532-567`)
   - When `callState === "ended"`, call `cleanupSession()`
   - Clears `pc`, `sessionId`, media tracks, and remote video
   - Enables Call button for next call

2. **Call button enabled without session** (`web/app.js:1022-1044`)
   - Changed logic: Call button enabled when `wsReady && credentialReady && !inCall && !autoStartingSession`
   - No longer requires `sessionId != null`
   - Prevents duplicate calls with `inCall` check

3. **Auto-start session on Call** (`web/app.js:753-804`)
   - When user clicks Call without active session:
     - Queue call request in `pendingCallRequest`
     - Call `ensureMediaSessionForCall()` to auto-start media session
     - When `answer` received, `flushPendingCallQueue()` auto-places queued call

4. **Prevent stale sessionId resurrection** (`web/app.js:223-230`)
   - Don't set `sessionId` from `state` message if `msg.state === "ended"`
   - Prevents server's "ended" state message from restoring old sessionId

**User Flow After Fix:**
```
[Connect WS] → Call button ready (no manual Start Session needed)
     ↓
[Click Call #1] → Auto-start media session → Auto-place call
     ↓
[Active call...]
     ↓
[Click Hangup] → cleanupSession() clears pc + sessionId
     ↓
[Click Call #2] → Auto-start new session → Auto-place call (seamless)
```

**Testing Checklist:**
- [ ] Connect WS → Call button enabled without Start Session
- [ ] Click Call → auto-starts session + places call
- [ ] Click Hangup → call ends + session cleaned up
- [ ] Click Call again → auto-starts new session + places call (no manual Start)
- [ ] Verify remote video doesn't freeze after hangup
- [ ] Test with both Public and SIP Trunk modes
- [ ] Test incoming call → accept/reject → outbound call works

---

## 9. Key Features

### 9.1 SIP Public Account Registry

The gateway supports **SIP Public Mode** for per-call authentication, allowing each call to use different SIP credentials without pre-configuration.

**Account Key Format (internal/sip/public_registry.go):**

Public accounts are uniquely identified by keys that vary based on domain type:

- **Hostname domains**: `username@domain` (port omitted from key for cleaner logs)
  - Example: `0000177005714@sipclient.ttrs.or.th`
  - Works for both default port (5060) and custom ports

- **IP literal domains**: `username@ip:port` (port required in key)
  - IPv4: `bob@192.168.1.100:5060`
  - IPv6: `dave@[2001:db8::1]:5060` (RFC-compliant brackets)

**DNS SRV Resolution (internal/sip/public_registry.go):**

The gateway supports automatic SRV lookup for hostname domains when port is not specified:

- **Hostname + port > 0**: Dials `domain:port` directly
- **Hostname + port = 0**: Tries SRV lookup for `_sip._tcp.<domain>`, falls back to `domain:5060`
- **IP literal**: Always dials `ip:port` (or `ip:5060` if port=0), no SRV lookup

**Implementation:**
- Account key helper: `buildPublicAccountKey(domain, username, port)`
- SRV resolver: `resolveSIPDestination(domain, port, transport)`
- Test coverage: `internal/sip/public_registry_test.go`, `internal/sip/srv_test.go`

**Benefits:**
- Supports DNS SRV-based load balancing and failover for hostname domains
- Clean account keys (no redundant `:5060` for default port hostnames)
- IP literals bypass DNS for direct routing

### 9.2 Call Resume (Network Recovery)

When a mobile client switches networks (WiFi -> LTE), the WebSocket reconnects and sends:

```json
{ "type": "resume", "sessionId": "abc123", "sdp": "<new offer>" }
```

The gateway renegotiates the PeerConnection while preserving the SIP dialog.

### 9.3 Trunk Resolve

Clients resolve trunkId before making trunk calls:

```json
{ "type": "trunk_resolve", "sipDomain": "sip.example.com", "sipUsername": "1001", "sipPassword": "secret", "sipPort": 5060 }
```

### 9.4 SIP Messaging

In-dialog or out-of-dialog instant messaging:

```json
{ "type": "send_message", "destination": "sip:1002@sip.example.com", "body": "Hello!" }
```

### 9.5 DTMF (Bidirectional)

- **WebRTC -> SIP:** Client sends `{"type": "dtmf", "sessionId": "...", "digits": "123#"}`
- **SIP -> WebRTC:** Server sends `{"type": "dtmf", "sessionId": "...", "digits": "4"}`
