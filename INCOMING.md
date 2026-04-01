# Incoming Call Flow

สรุป flow ของ incoming call ตั้งแต่ SIP INVITE เข้ามาจนถึง push notification และ mobile app แสดงสายเรียกเข้า

## End-to-End Diagram

```
Asterisk/PBX                 Gateway                           Mobile App
     │                          │                                  │
     │── SIP INVITE ───────────▶│                                  │
     │                   handleINVITE()                            │
     │                   MatchTrunkFromInviteDetailed()            │
     │◀── 100 Trying ──────────│                                  │
     │                   CreateSessionForIncoming()                │
     │                   SetCallInfo("inbound", from, to, callID) │
     │                   SetIncomingInvite(tx, req, body)          │
     │◀── 180 Ringing ─────────│                                  │
     │                   NotifyIncomingCall(sessionID, from, to, trunkID)
     │                     │                                       │
     │                     ├─ WS fanout ──────────────────────────▶│ onMessage("incoming")
     │                     │  {"type":"incoming",                  │ handleIncomingCall()
     │                     │   "sessionId","from","to"}            │ sip-store → CallState.INCOMING
     │                     │                                       │ CallKeep → native ring UI
     │                     │                                       │
     │                     └─ goroutine: push (fire-and-forget)    │
     │                        GetTrunkByID(trunkID)                │
     │                        trunk.NotifyUserID (UUID)            │
     │                        TTRS API → FetchNotifications(uuid)  │
     │                        FCM SendPush(token, data) ──────────▶│ FCM data message
     │                                                             │ (wakes app if closed)
```

## 1. SIP INVITE Reception

**File:** `apps/gateway/internal/sip/handlers.go` — `handleINVITE(req, tx)`

1. Extract headers: Call-ID, From URI/display name, To URI
2. **Trunk matching** — `MatchTrunkFromInviteDetailed(req)` ลำดับ priority:
   - Request-URI `username+domain+port`
   - To header `username+domain+port`
   - Request-URI `username+domain`
   - To header `username+domain`
   - Fallback: `domain+port` only (ต้อง match เดียว)
   - Fallback: `username` only บน owned/online trunks
3. ถ้าไม่ match → **403 Forbidden**, ถ้า match แต่ไม่ใช่ owner → **404 Not Found**
4. **Re-INVITE check** — ถ้า Call-ID ซ้ำกับ session ที่มีอยู่ → update transaction, resend 180
5. **New call:**
   - ส่ง **100 Trying**
   - สร้าง Session — `CreateSessionForIncoming(turnConfig)`
   - Set call info + SIP auth context (trunk credentials)
   - State → `StateIncoming`
   - เก็บ INVITE transaction + SDP body ไว้สำหรับ delayed 200 OK
   - ส่ง **180 Ringing**
   - เรียก **`NotifyIncomingCall(sessionID, from, to, trunkID)`**

## 2. Session Creation

**File:** `apps/gateway/internal/session/session.go` — `NewSession()`

สร้าง `Session` ที่มี:
- **ID** — 12-char base62 random
- **PeerConnection** — Pion WebRTC พร้อม custom `MediaEngine`
  - H.264 (PT 96), VP8 (PT 97), Opus (PT 111), PCMU (PT 0)
- **AudioTrack** (Opus) + **VideoTrack** (H.264) — added to PC
- ICE servers จาก TURN config
- Fields สำหรับ incoming: `IncomingSIPTx`, `IncomingSIPReq`, `IncomingINVITE`
- First-accept-wins: `IncomingClaimed`, `IncomingClaimedBy`

## 3. NotifyIncomingCall

**File:** `apps/gateway/internal/api/server.go` — `NotifyIncomingCall(sessionID, from, to, trunkID)`

### 3a. WebSocket Fanout

วน loop `s.wsConnections` → filter เฉพาะ client ที่:
- `client.trunkResolved == true`
- `client.resolvedTrunkID == trunkID`

ส่ง WS message:
```json
{
  "type": "incoming",
  "sessionId": "<session-id>",
  "from": "<sip:caller@domain>",
  "to": "<sip:callee@domain>"
}
```

### 3b. Push Notification (fire-and-forget goroutine)

ทำงาน **แยก goroutine** ไม่ block flow หลัก:

1. `GetTrunkByID(trunkID)` — ดึง trunk จาก in-memory cache
2. ตรวจ `trunk.NotifyUserID` (Keycloak `sub` UUID) — ถ้า nil/empty → skip
3. เรียก `pushService.NotifyIncomingCall(notifyUserID, sessionID, from, to)`

> **`NotifyUserID`** ถูก set ตอน trunk resolve และ **ไม่ clear ตอน hangup/disconnect** ทำให้ push ส่งได้แม้ user ปิดแอพ

## 4. Push Notification Chain

### 4a. Push Service

**File:** `apps/gateway/internal/push/service.go` — `NotifyIncomingCall(userID, sessionID, from, to)`

1. สร้าง context timeout 10 วินาที
2. เรียก TTRS API → `FetchNotifications(ctx, userID)` → `[]NotificationEntry`
3. Filter เฉพาะ `service_id == "4"` (FCM push)
4. สร้าง FCM data payload:
   ```go
   data := map[string]string{
       "type":      "incoming_call",
       "sessionId": sessionID,
       "from":      from,
       "to":        to,
   }
   ```
5. วน loop tokens → `fcm.SendPush(ctx, token, data)` ต่อ device

### 4b. TTRS API Client

**File:** `apps/gateway/internal/push/ttrs_client.go` — `FetchNotifications(ctx, userID)`

- **Endpoint:** `GET {baseURL}/employees/v3/accounts/{userID}/notifications`
- **Auth:** Keycloak client credentials grant (OAuth2 auto-refresh)
- **Response:**
  ```json
  [
    {
      "user_id": "b1570549-...",
      "service_id": "4",
      "token": "<fcm-device-token>",
      "mobile_device": "android_00001"
    }
  ]
  ```

### 4c. FCM Sender

**File:** `apps/gateway/internal/push/fcm.go` — `SendPush(ctx, token, data)`

- **Endpoint:** `POST https://fcm.googleapis.com/v1/projects/{projectID}/messages:send`
- **Auth:** Google service account → OAuth2 bearer token (`firebase.messaging` scope)
- **Payload (data-only, ไม่มี notification block):**
  ```json
  {
    "message": {
      "token": "<fcm-device-token>",
      "data": {
        "type": "incoming_call",
        "sessionId": "<id>",
        "from": "<caller>",
        "to": "<callee>"
      }
    }
  }
  ```

## 5. Mobile App — WS Connection & Trunk Resolve

### 5a. WebSocket Connection

**File:** `apps/gateway/internal/api/server.go` — route `GET /ws?access_token=<jwt>`

1. Extract `access_token` จาก query param
2. `VerifyToken()` → `VerifiedClaims { Subject, PreferredUsername }`
3. Gorilla WebSocket upgrade
4. สร้าง `WSClient{ conn, authClaims }` → register ใน `s.wsConnections`
5. Start read loop → `handleWSMessage(client, msg)`

### 5b. Trunk Resolve

**File:** `apps/gateway/internal/api/server.go` — `handleWSTrunkResolve()`

Client ส่ง:
```json
{ "type": "trunk_resolve", "trunkId": 1 }
```
หรือ:
```json
{ "type": "trunk_resolve", "trunkPublicId": "abc123" }
```
หรือ:
```json
{ "type": "trunk_resolve", "domain": "sip.example.com", "port": 5060, "username": "user", "password": "pass" }
```

Resolve 3 mode:
1. **By `trunkId`** — direct DB lookup
2. **By `trunkPublicId`** — normalize → `GetTrunkIDByPublicID()` → DB lookup
3. **By SIP credentials** — `ResolveTrunkByCredentials(domain, port, username, password)`

เมื่อ resolve สำเร็จ:
1. ตรวจ lease owner ตรงกับ instance นี้
2. **Set `notify_user_id`** = `authClaims.Subject` (Keycloak UUID) → persist ลง DB
3. Set `client.trunkResolved = true`, `client.resolvedTrunkID = trunkID`
4. ตอบ:
   ```json
   { "type": "trunk_resolved", "trunkId": 1, "trunkPublicId": "abc123" }
   ```
5. **Replay pending** — เรียก `notifyPendingIncomingForClient()` ส่ง `"incoming"` สำหรับ session ที่ค้างอยู่ (state: `StateIncoming` + ตรง trunkID)

## 6. Mobile App — Handling Incoming Call

### 6a. WS Message Handler

**File:** `lib/gateway/gateway-client.ts` — `handleMessage()`

เมื่อได้รับ `type: "incoming"`:
```typescript
const caller = msg.caller || msg.from || "Unknown";
this.handleIncomingCall(caller, msg.sessionId, msg.to, msg.sdp);
```

### 6b. Store Update

**File:** `store/sip-store.ts` — `onIncomingCall` callback

```typescript
set({
  callState: CallState.INCOMING,
  incomingCall: info,
  remoteNumber: info.caller,
  _callDirection: "incoming",
});
reportIncomingCall(info.caller, info.caller);
```

### 6c. Native Call UI

**File:** `lib/callkeep.ts` — `reportIncomingCall()`

เรียก `RNCallKeep.displayIncomingCall(uuid, handle, displayName)` เพื่อแสดง:
- **Android** — ConnectionService incoming call screen
- **iOS** — CallKit incoming call UI

## 7. WebSocket Message Types

| Direction | Type | Payload | Purpose |
|-----------|------|---------|---------|
| S→C | `incoming` | `sessionId`, `from`, `to` | สายเข้า |
| S→C | `trunk_resolved` | `trunkId`, `trunkPublicId` | Resolve สำเร็จ |
| S→C | `trunk_redirect` | `redirectUrl` | Redirect ไป instance อื่น |
| S→C | `trunk_not_found` | `reason` | ไม่เจอ trunk |
| S→C | `trunk_not_ready` | `reason` | Lease ไม่ active |
| C→S | `trunk_resolve` | `trunkId` / `trunkPublicId` / SIP creds | Request resolve |
| C→S | `accept` | `sessionId` | รับสาย |
| C→S | `reject` | `sessionId` | ปฏิเสธสาย |

## 8. Push Notification — Device Registration

**File:** `lib/notification/notification-service.ts` — `updateNotificationRegistration()`

- **Endpoint:** `PUT https://api.ttrs.or.th/users/v3/accounts/{userId}/notifications`
- ลงทะเบียน device token กับ `service_id: "4"`, `mobile_device: "android_00001"` หรือ `"ios_000001"`
- Token มาจาก SecureStore หรือ env var `EXPO_PUBLIC_DEVICE_NOTIFICATION_TOKEN`

## 9. Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| `notify_user_id` ≠ `in_use_by` | `in_use_by` tracks active call (cleared on hangup), `notify_user_id` persists for push |
| `notify_user_id` = Keycloak `sub` (UUID) | TTRS API endpoint ต้องการ UUID ไม่ใช่ username |
| Set ตอน trunk resolve, ไม่ clear | ให้ push ทำงานได้แม้ user ปิดแอพ/offline |
| FCM data-only (ไม่มี notification block) | ให้ app จัดการ display เอง ผ่าน CallKeep |
| Push เป็น fire-and-forget | ไม่ block SIP 180 Ringing flow |
| `service_id = "4"` | Filter เฉพาะ FCM tokens สำหรับ app นี้ |
