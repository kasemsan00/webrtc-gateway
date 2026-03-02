# Fix: ปุ่ม End Call หลังกดแล้วสายวางแต่ไม่สามารถกดโทรออกรอบถัดไปได้

## สาเหตุ (Root Cause)

1. **Server ลบ session ทิ้งหลัง hangup** (`internal/api/server.go:616`)
   - `s.sessionMgr.DeleteSession(msg.SessionID)`
   
2. **Client ยังถือ `sessionId` เก่าไว้** หลังสายจบ
   - `state.sessionId` ไม่ถูกเคลียร์
   - `state.pc` (PeerConnection) ยังอยู่
   
3. **ปุ่ม Call ต้องการ `sessionId`** ถึงจะ enable
   - `updateCallButtonState()` เช็ค `hasSession = state.sessionId != null`
   - ทำให้รอบถัดไปกดไม่ได้

## การแก้ไข

### 1. เมื่อสายจบ (`state = "ended"`) ให้ cleanup session ทันที

**File:** `web/app.js:532-567`

```javascript
function handleCallState(callState) {
  // ...
  } else if (callState === "ended") {
    // ...
    stopTimer();
    // Cleanup session when call ends (server deletes session on hangup)
    cleanupSession();  // ← เพิ่มบรรทัดนี้
  }
  updateCallButtonState();  // ← เพิ่มบรรทัดนี้
}
```

**เหตุผล:**
- Server ลบ session แล้ว → Client ต้องลบ `pc` และ `sessionId` ด้วย
- ถ้าไม่ลบ `pc` รอบถัดไปจะไม่สร้าง session ใหม่

---

### 2. ป้องกัน state message "ended" มาตั้ง sessionId กลับ

**File:** `web/app.js:223-230`

```javascript
case "state":
  // Update sessionId if provided in state message (important for incoming calls)
  // BUT: don't set sessionId for "ended" state to avoid resurrecting old session
  if (msg.sessionId && msg.state !== "ended") {  // ← เพิ่มเงื่อนไข
    state.sessionId = msg.sessionId;
    $("sessionIdDisplay").textContent = state.sessionId;
  }
  handleCallState(msg.state);
  break;
```

**เหตุผล:**
- ป้องกัน state message "ended" ของ session เก่ามา "ฟื้น" `sessionId` หลังเราล้างไปแล้ว

---

### 3. ให้ปุ่ม Call กดได้แม้ยังไม่มี session (จะ auto-start)

**File:** `web/app.js:1022-1044`

```javascript
function updateCallButtonState() {
  const wsReady = state.ws && state.ws.readyState === WebSocket.OPEN;
  
  let credentialReady = false;
  
  if (state.mode === 'siptrunk') {
    const trunkIdInput = $("trunkId");
    const trunkId = trunkIdInput ? parseInt(trunkIdInput.value || "0") : 0;
    credentialReady = !Number.isNaN(trunkId) && trunkId > 0;
  } else {
    // Public mode
    const sipDomain = $("sipDomain").value.trim();
    const sipUsername = $("sipUsername").value.trim();
    const sipPassword = $("sipPassword").value;
    credentialReady = sipDomain && sipUsername && sipPassword;
  }
  
  // Enable Call button if:
  // - WebSocket is ready
  // - Credentials are ready
  // - Not currently in a call (callState is not connecting/ringing/active)
  // - Not currently auto-starting a session
  const inCall = state.callState === "connecting" || state.callState === "ringing" || state.callState === "active";
  const ready = wsReady && credentialReady && !inCall && !state.autoStartingSession;
  $("btnCall").disabled = !ready;
}
```

**เปลี่ยนแปลง:**
- **เดิม:** ต้องมี `hasSession` (sessionId != null)
- **ใหม่:** ไม่ต้องมี session ก็กดได้ (จะ auto-start)
- เพิ่ม guard: ถ้ากำลัง `inCall` หรือ `autoStartingSession` จะ disable

---

### 4. แก้ makeCall() ให้ queue + auto-start session ได้

**File:** `web/app.js:753-804`

```javascript
function makeCall() {
  if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
    log("WebSocket not connected", "error");
    return;
  }

  const dest = $("destination").value;
  if (!dest) {  // ← เช็คแค่ dest เท่านั้น (ไม่เช็ค sessionId)
    log("Please enter a destination", "error");
    return;
  }
  
  let callParams = {
    destination: dest,
  };
  
  // Build call params based on mode...
  
  // If no session exists, queue call and auto-start session
  if (!state.sessionId) {
    state.pendingCallRequest = callParams;
    log("Media session not ready - preparing automatically before placing call...", "info");
    ensureMediaSessionForCall();
    return;
  }

  sendCallPayload(callParams);
}
```

**เปลี่ยนแปลง:**
- **เดิม:** `if (!dest || !state.sessionId) return;` → กด Call ไม่ได้ถ้าไม่มี session
- **ใหม่:** `if (!dest) return;` → ถ้าไม่มี session ให้ queue + auto-start

---

### 5. เพิ่ม log ใน flushPendingCallQueue

**File:** `web/app.js:867-874`

```javascript
function flushPendingCallQueue() {
  if (!state.pendingCallRequest || !state.sessionId) {
    return;
  }

  log("Auto-placing queued call...", "info");  // ← เพิ่มบรรทัดนี้
  const params = state.pendingCallRequest;
  state.pendingCallRequest = null;
  sendCallPayload(params);
}
```

---

### 6. Set sessionId จาก incoming event

**File:** `web/app.js:603-623`

```javascript
function handleIncomingCall(msg) {
  log(`Incoming call from ${msg.from} (to: ${msg.to || 'unknown'})`, "info");
  
  // Set sessionId from incoming event if present
  if (msg.sessionId) {  // ← เพิ่มบรรทัดนี้
    state.sessionId = msg.sessionId;
    $("sessionIdDisplay").textContent = state.sessionId;
  }
  
  // ...
}
```

---

## ผลลัพธ์

### ก่อนแก้
1. กด Call → โทรออก
2. กด End → สายจบ
3. กด Call อีกครั้ง → **ปุ่ม disabled (ไม่สามารถกดได้)**
4. ต้องกด "Start Session" ใหม่ถึงจะโทรได้

### หลังแก้
1. กด Call → Auto-start session → โทรออก
2. กด End → สายจบ + cleanup session
3. กด Call อีกครั้ง → **Auto-start session ใหม่ → โทรออกได้ทันที**

---

## User Flow หลังแก้

```
[Connect WS] → ปุ่ม Call พร้อมใช้งาน (ไม่ต้อง Start Session)
     ↓
[กด Call #1]
     ↓
Auto-start media session (getUserMedia + createOffer)
     ↓
รับ answer → ได้ sessionId → auto-place call
     ↓
[สนทนา...]
     ↓
[กด End/Hangup]
     ↓
cleanupSession() (ลบ pc + sessionId)
     ↓
[กด Call #2] → ← ไม่ต้องกด Start Session อีก
     ↓
Auto-start media session ใหม่ (ทำซ้ำ flow)
```

---

## ไฟล์ที่แก้

- ✅ `web/app.js` (6 functions แก้ไข)
  - `handleMessage` (case "state")
  - `handleCallState`
  - `makeCall`
  - `updateCallButtonState`
  - `handleIncomingCall`
  - `flushPendingCallQueue`

- ✅ ไม่ต้องแก้ฝั่ง Go (server ทำงานถูกต้องแล้ว)

---

## Testing Checklist

- [ ] Connect WS → ปุ่ม Call enabled (ไม่ต้อง Start)
- [ ] กด Call → auto-start session → โทรออก
- [ ] กด End → สายจบ + ล้าง session
- [ ] กด Call อีกครั้ง → auto-start + โทรได้ (ไม่ค้าง)
- [ ] ทดสอบ Incoming call → accept/reject → กด Call ใหม่
- [ ] ทดสอบเปลี่ยน mode (Public ↔ SIP Trunk)
- [ ] ตรวจสอบ remote video ไม่แข็ง (frozen frame) หลังวางสาย
