# Call Resume After Network Change

## Overview

K2 Gateway supports call resumption after client network changes (e.g., WiFi to 5G switch). When a mobile client's network changes, the WebSocket connection drops but the SIP call continues on the server. The client can reconnect and resume the same call session.

---

## How It Works

### Session Preservation

When a WebSocket client disconnects:

1. **Session is NOT deleted** - only the WebSocket client mapping is removed
2. **SIP call continues** - RTP media keeps flowing between SIP endpoints
3. **Session waits for reconnection** - client can resume within the call duration

Sessions are only deleted when:

- Client sends `hangup` message
- Client sends `reject` message (for incoming calls)
- Remote party sends SIP `BYE`

### Resume Flow

```
┌─────────────────┐                    ┌─────────────────┐
│  Mobile Client  │                    │   K2 Gateway    │
└────────┬────────┘                    └────────┬────────┘
         │                                      │
         │  ════ Active Call ══════════════════ │
         │                                      │
         │  ✗ Network changes (WiFi → 5G)       │
         │  ✗ WebSocket disconnects             │
         │                                      │
         │  (SIP call continues on server)      │
         │                                      │
         │ ─── New WebSocket connection ──────► │
         │                                      │
         │ ─── { type: "register", ... } ─────► │
         │ ◄── { type: "registered" } ───────── │
         │                                      │
         │ ─── { type: "resume",        ──────► │
         │       sessionId: "abc123" }          │
         │                                      │
         │ ◄── { type: "resumed",       ─────── │
         │       sessionId: "abc123",           │
         │       state: "active",               │
         │       from: "1001",                  │
         │       to: "1002" }                   │
         │                                      │
         │  ══════ Call Resumed ═══════════════ │
         │                                      │
```

---

## Message Types

### Resume Request (Client → Server)

```json
{
  "type": "resume",
  "sessionId": "a8Zx3KpQ9wEr"
}
```

| Field     | Type   | Required | Description                           |
| --------- | ------ | -------- | ------------------------------------- |
| type      | string | Yes      | Must be `"resume"`                    |
| sessionId | string | Yes      | The session ID from the original call |

### Resume Success (Server → Client)

```json
{
  "type": "resumed",
  "sessionId": "a8Zx3KpQ9wEr",
  "state": "active",
  "from": "1001",
  "to": "1002"
}
```

| Field     | Type   | Description                                               |
| --------- | ------ | --------------------------------------------------------- |
| type      | string | `"resumed"`                                               |
| sessionId | string | The resumed session ID                                    |
| state     | string | Current session state (`active`, `connecting`, `ringing`) |
| from      | string | Caller number/URI                                         |
| to        | string | Callee number/URI                                         |

### Resume Failed (Server → Client)

```json
{
  "type": "resume_failed",
  "sessionId": "a8Zx3KpQ9wEr",
  "reason": "Session not found or expired"
}
```

| Field     | Type   | Description                  |
| --------- | ------ | ---------------------------- |
| type      | string | `"resume_failed"`            |
| sessionId | string | The requested session ID     |
| reason    | string | Human-readable error message |

### Possible Failure Reasons

| Reason                                       | Description                                         |
| -------------------------------------------- | --------------------------------------------------- |
| `Session not found or expired`               | Session ID doesn't exist (call ended or invalid ID) |
| `Session is in state 'ended', cannot resume` | Call already terminated                             |
| `Session is in state 'new', cannot resume`   | Session not yet in a call                           |

---

## Implementation Details

### Server-Side (Go)

**File:** `internal/api/server.go`

**Handler:** `handleWSResume()`

```go
func (s *Server) handleWSResume(client *WSClient, msg WSMessage) {
    // 1. Validate sessionId is provided
    if msg.SessionID == "" {
        s.sendWSError(client, "", "Session ID required for resume")
        return
    }

    // 2. Look up the session
    sess, ok := s.sessionMgr.GetSession(msg.SessionID)
    if !ok {
        // Session not found - send failure
        response := WSMessage{
            Type:      "resume_failed",
            SessionID: msg.SessionID,
            Reason:    "Session not found or expired",
        }
        s.sendWSMessage(client, response)
        return
    }

    // 3. Check session is in resumable state
    if sess.State != session.StateActive &&
       sess.State != session.StateConnecting &&
       sess.State != session.StateRinging {
        // Not resumable
        response := WSMessage{
            Type:      "resume_failed",
            SessionID: msg.SessionID,
            Reason:    fmt.Sprintf("Session is in state '%s', cannot resume", sess.State),
        }
        s.sendWSMessage(client, response)
        return
    }

    // 4. Re-associate client with session
    s.mu.Lock()
    s.wsClients[msg.SessionID] = client
    client.sessionID = msg.SessionID
    s.mu.Unlock()

    // 5. Send success response
    response := WSMessage{
        Type:      "resumed",
        SessionID: msg.SessionID,
        State:     string(sess.State),
        From:      sess.From,
        To:        sess.To,
    }
    s.sendWSMessage(client, response)
}
```

### Client-Side (React Native)

The mobile client should:

1. **Save session ID** before network-triggered disconnect
2. **Reconnect WebSocket** when network is restored
3. **Re-register** with SIP server
4. **Send resume message** with saved session ID
5. **Handle response** - restore call UI on success, or callback on failure

---

## Session States

| State        | Resumable | Description                           |
| ------------ | --------- | ------------------------------------- |
| `new`        | ❌        | Session created but no call initiated |
| `incoming`   | ❌        | Incoming call waiting for answer      |
| `connecting` | ✅        | Outbound call in progress             |
| `ringing`    | ✅        | Remote party ringing                  |
| `active`     | ✅        | Call connected and active             |
| `ended`      | ❌        | Call terminated                       |

---

## Limitations

1. **Media renegotiation is best-effort with timeout** - When client sends SDP in `resume`, server recreates PeerConnection and waits for ICE gathering with a bounded timeout before proceeding with partial candidates. This improves recovery speed but does not guarantee media on severely degraded paths.

2. **Resume still depends on active SIP session** - Sessions persist while SIP dialog is active. If the remote side has already ended the call, resume will return `resume_failed`.

3. **Single client per session** - Only one WebSocket client can be associated with a session at a time. Resuming replaces any previous client mapping.

---

## Future Enhancements

1. **Adaptive resume tuning** - Adjust client/server ICE and registration waits by network quality profile
2. **Session timeout** - Add configurable grace period for orphaned sessions
3. **State sync** - Send full call state (duration, mute status) on resume
4. **Protocol-level recovery hints** - Optional explicit recovery/keyframe hints in WS contract (if contract is extended)
