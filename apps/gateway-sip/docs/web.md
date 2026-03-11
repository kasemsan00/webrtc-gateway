# WebRTC Integration Guide

คู่มือการใช้งาน WebRTC กับ K2 Gateway สำหรับการเชื่อมต่อและโทรผ่าน SIP

> อัปเดต: สำหรับ production flow ล่าสุด (รองรับ Linphone + Browser-to-Browser พร้อมกัน) ให้ใช้อ้างอิงหลักที่ `docs/dual-flow.md` ร่วมกับเอกสารนี้

## ✅ Features ที่รองรับ

**Core Features:**

- ✅ รับสายเข้า (Inbound calls) จาก SIP → WebRTC
- ✅ **โทรออก (Outbound calls) จาก WebRTC → SIP**
- ✅ **HTTP/WebSocket API สำหรับ signaling**
- ✅ **REST API สำหรับ call control**
- ✅ **Multiple concurrent WebRTC sessions**
- ✅ SIP registration อัตโนมัติ
- ✅ NAT traversal ผ่าน TURN server
- ✅ DTMF support
- ✅ **SIP MESSAGE (Text Messaging)** - ส่ง/รับข้อความระหว่างโทร
- ✅ รองรับ dual-flow พร้อมกัน:
  - `browser -> gateway -> kamailio/asterisk -> linphone`
  - `browser -> gateway -> kamailio/asterisk -> gateway -> browser`

**โหมดการใช้งาน:**

- **API Mode** (default): HTTP/WebSocket signaling ที่ port 8080
- **Legacy Mode** (`--legacy`): stdin/stdout signaling

## สารบัญ

1. [ภาพรวมการทำงาน](#ภาพรวมการทำงาน)
2. [การเชื่อมต่อ WebRTC (API Mode)](#การเชื่อมต่อ-webrtc-api-mode)
3. [การโทรออก (Outbound Calls)](#การโทรออก-outbound-calls)
4. [REST API Reference](#rest-api-reference)
5. [WebSocket API Reference](#websocket-api-reference)
6. [SIP MESSAGE (Text Messaging)](#sip-message-text-messaging)
7. [Legacy Mode](#legacy-mode)
8. [ตัวอย่างโค้ด Client-Side](#ตัวอย่างโค้ด-client-side)

---

## ภาพรวมการทำงาน

K2 Gateway ทำหน้าที่เป็น bridge ระหว่าง WebRTC (Browser) และ SIP (VoIP Server):

```
[Browser/WebRTC] <---> [K2 Gateway] <---> [SIP Server (Asterisk)]
```

สำหรับระบบหลาย user ที่ต้องรับสายเข้าแบบ route ตาม account ให้ใช้ trunk model ใน DB (`sip_trunks`) และ `trunk_resolve` บน frontend เป็นหลัก

### ขั้นตอนการทำงาน

1. **Gateway Start** - Gateway เริ่มทำงานและ register กับ SIP server
2. **WebRTC Connection** - Browser สร้าง offer และส่งไปยัง Gateway
3. **SDP Exchange** - Gateway สร้าง answer กลับไปยัง Browser
4. **SIP Call** - เมื่อมีการโทร SIP server จะ INVITE มาที่ Gateway
5. **RTP Streaming** - Gateway forward audio ระหว่าง WebRTC และ SIP

---

## การเชื่อมต่อ WebRTC

### 1. เริ่มต้น Gateway

```bash
# รัน k2-gateway
./k2-gateway

# Output:
# === Environment Configuration ===
#
# TURN Server:
#   Server: turn:turn.ttrs.or.th:3478?transport=udp
#   Username: turn01
#   Password: Te****34
#
# SIP Configuration:
#   Domain: sipclient.ttrs.or.th
#   Username: 0900200002
#   Password: 9I****************fL
#   Port: 5060
#   Local Port: 5060
#
# =================================
#
# TURN server configured: turn:turn.ttrs.or.th:3478?transport=udp (with credentials)
# SIP Client created for domain: sipclient.ttrs.or.th
#
# === SIP Registration ===
# Registering to: sipclient.ttrs.or.th:5060
# Username: 0900200002
# Authentication required (401), attempting with credentials...
# Authentication required, using credentials...
# ✓ SIP Registration successful with authentication (200 OK)
# ========================
#
# [รอรับ WebRTC offer จาก stdin...]
```

### 2. สร้าง WebRTC Connection จาก Browser

```javascript
// ตัวอย่าง JavaScript สำหรับ Browser
const pc = new RTCPeerConnection({
  iceServers: [
    {
      urls: "turn:turn.ttrs.or.th:3478?transport=udp",
      username: "turn01",
      credential: "Test1234",
    },
  ],
});

// เพิ่ม audio track
navigator.mediaDevices.getUserMedia({ audio: true, video: false }).then((stream) => {
  stream.getTracks().forEach((track) => {
    pc.addTrack(track, stream);
  });
});

// รับ audio จาก remote
pc.ontrack = (event) => {
  const audio = new Audio();
  audio.srcObject = event.streams[0];
  audio.play();
};

// สร้าง offer
pc.createOffer()
  .then((offer) => pc.setLocalDescription(offer))
  .then(() => {
    // รอให้ ICE gathering เสร็จ
    return new Promise((resolve) => {
      if (pc.iceGatheringState === "complete") {
        resolve();
      } else {
        pc.addEventListener("icegatheringstatechange", () => {
          if (pc.iceGatheringState === "complete") {
            resolve();
          }
        });
      }
    });
  })
  .then(() => {
    // Encode offer เป็น base64
    const offer = pc.localDescription;
    const encoded = btoa(JSON.stringify(offer));

    console.log("Send this to k2-gateway:");
    console.log(encoded);

    // คัดลอก encoded string และวาง (paste) ลงใน terminal ที่รัน k2-gateway
  });
```

### 3. รับ Answer จาก Gateway

```javascript
// หลังจากวาง offer ใน terminal, k2-gateway จะคืน answer กลับมา
// คัดลอก answer และใช้โค้ดนี้

const answerEncoded = "eyJ0eXBlIjoiYW5zd2VyIiwic2RwIjoiLi4uIn0="; // answer จาก gateway

// Decode และ set remote description
const answer = JSON.parse(atob(answerEncoded));
pc.setRemoteDescription(new RTCSessionDescription(answer)).then(() => {
  console.log("WebRTC connection established!");
});

// ตรวจสอบสถานะการเชื่อมต่อ
pc.oniceconnectionstatechange = () => {
  console.log("ICE Connection State:", pc.iceConnectionState);
};

pc.onconnectionstatechange = () => {
  console.log("Connection State:", pc.connectionState);
};
```

---

## การรับสายผ่าน SIP

หลังจากเชื่อมต่อ WebRTC สำเร็จแล้ว Gateway สามารถรับสายจาก SIP phone ได้

### วิธีการรับสาย

#### 1. จาก SIP Phone โทรเข้า Gateway

เมื่อ Gateway ได้ register แล้ว สามารถโทรเข้าเบอร์ที่ register ไว้ได้โดยตรง:

```
โทรไปที่: 0900200002
```

Gateway จะ:

1. รับ SIP INVITE
2. สร้าง SDP answer
3. เริ่ม RTP stream
4. ส่ง audio ไปยัง WebRTC connection

#### 2. โทรออกจาก Gateway (Outbound Calls) ✅

**ฟีเจอร์นี้รองรับแล้ว!** - สามารถโทรออกจาก Browser ไปยัง SIP destination ได้

**วิธีใช้งาน (WebSocket):**

```javascript
// 1. เชื่อมต่อ WebSocket
const ws = new WebSocket("ws://localhost:8080/ws");

// 2. ส่ง WebRTC offer
ws.send(
  JSON.stringify({
    type: "offer",
    sdp: pc.localDescription.sdp,
  })
);

// 3. รับ answer และ sessionId
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "answer") {
    pc.setRemoteDescription({ type: "answer", sdp: msg.sdp });
    sessionId = msg.sessionId;
  }
};

// 4. โทรออก
ws.send(
  JSON.stringify({
    type: "call",
    sessionId: sessionId,
    destination: "9999",
  })
);
```

**วิธีใช้งาน (REST API):**

```javascript
// 1. ส่ง offer และรับ answer
const offerResponse = await fetch("http://localhost:8080/api/offer", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ sdp: pc.localDescription.sdp }),
});
const { sdp, sessionId } = await offerResponse.json();
await pc.setRemoteDescription({ type: "answer", sdp });

// 2. โทรออก
const callResponse = await fetch("http://localhost:8080/api/call", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    sessionId: sessionId,
    destination: "9999",
  }),
});
console.log("Call initiated:", await callResponse.json());
```

---

## ตัวอย่างโค้ด Client-Side

### HTML + JavaScript สมบูรณ์

```html
<!DOCTYPE html>
<html lang="th">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>K2 Gateway WebRTC Client</title>
    <style>
      body {
        font-family: Arial, sans-serif;
        max-width: 800px;
        margin: 50px auto;
        padding: 20px;
      }
      .container {
        background: #f5f5f5;
        padding: 20px;
        border-radius: 8px;
      }
      button {
        background: #4caf50;
        color: white;
        border: none;
        padding: 10px 20px;
        border-radius: 4px;
        cursor: pointer;
        margin: 5px;
      }
      button:disabled {
        background: #ccc;
        cursor: not-allowed;
      }
      textarea {
        width: 100%;
        height: 100px;
        margin: 10px 0;
        padding: 10px;
        font-family: monospace;
      }
      .status {
        padding: 10px;
        margin: 10px 0;
        border-radius: 4px;
      }
      .status.success {
        background: #d4edda;
        color: #155724;
      }
      .status.error {
        background: #f8d7da;
        color: #721c24;
      }
      .status.info {
        background: #d1ecf1;
        color: #0c5460;
      }
    </style>
  </head>
  <body>
    <div class="container">
      <h1>K2 Gateway WebRTC Client</h1>

      <div id="status" class="status info">สถานะ: รอเริ่มต้น</div>

      <div>
        <button id="btnConnect" onclick="connect()">1. เชื่อมต่อ WebRTC</button>
        <button id="btnCreateOffer" onclick="createOffer()" disabled>2. สร้าง Offer</button>
      </div>

      <div>
        <label><strong>Offer (ส่งไปยัง Gateway):</strong></label>
        <textarea id="offerText" readonly></textarea>
        <button onclick="copyOffer()">คัดลอก Offer</button>
      </div>

      <div>
        <label><strong>Answer (จาก Gateway):</strong></label>
        <textarea id="answerText" placeholder="วาง Answer จาก Gateway ที่นี่"></textarea>
        <button id="btnSetAnswer" onclick="setAnswer()" disabled>3. ตั้งค่า Answer</button>
      </div>

      <div>
        <h3>Remote Audio:</h3>
        <audio id="remoteAudio" controls autoplay></audio>
      </div>

      <div>
        <h3>Log:</h3>
        <div id="log" style="background: white; padding: 10px; height: 200px; overflow-y: auto; font-family: monospace; font-size: 12px;"></div>
      </div>
    </div>

    <script>
      let pc = null;
      let localStream = null;

      function log(message) {
        const logDiv = document.getElementById("log");
        const time = new Date().toLocaleTimeString();
        logDiv.innerHTML += `[${time}] ${message}<br>`;
        logDiv.scrollTop = logDiv.scrollHeight;
        console.log(message);
      }

      function setStatus(message, type = "info") {
        const statusDiv = document.getElementById("status");
        statusDiv.textContent = "สถานะ: " + message;
        statusDiv.className = "status " + type;
      }

      async function connect() {
        try {
          setStatus("กำลังขอสิทธิ์ใช้ microphone...", "info");
          log("Requesting microphone access...");

          // ขอสิทธิ์ใช้ microphone
          localStream = await navigator.mediaDevices.getUserMedia({
            audio: true,
            video: false,
          });

          log("Microphone access granted");

          // สร้าง RTCPeerConnection
          pc = new RTCPeerConnection({
            iceServers: [
              {
                urls: "turn:turn.ttrs.or.th:3478?transport=udp",
                username: "turn01",
                credential: "Test1234",
              },
            ],
          });

          // เพิ่ม local tracks
          localStream.getTracks().forEach((track) => {
            pc.addTrack(track, localStream);
            log(`Added local track: ${track.kind}`);
          });

          // รับ remote tracks
          pc.ontrack = (event) => {
            log("Received remote track");
            const audio = document.getElementById("remoteAudio");
            audio.srcObject = event.streams[0];
          };

          // Monitor connection state
          pc.oniceconnectionstatechange = () => {
            log(`ICE Connection State: ${pc.iceConnectionState}`);
            if (pc.iceConnectionState === "connected") {
              setStatus("เชื่อมต่อสำเร็จ!", "success");
            } else if (pc.iceConnectionState === "failed") {
              setStatus("การเชื่อมต่อล้มเหลว", "error");
            }
          };

          pc.onconnectionstatechange = () => {
            log(`Connection State: ${pc.connectionState}`);
          };

          setStatus("พร้อมสร้าง Offer", "success");
          document.getElementById("btnCreateOffer").disabled = false;
          document.getElementById("btnConnect").disabled = true;
        } catch (error) {
          log(`Error: ${error.message}`);
          setStatus("เกิดข้อผิดพลาด: " + error.message, "error");
        }
      }

      async function createOffer() {
        try {
          setStatus("กำลังสร้าง Offer...", "info");
          log("Creating offer...");

          const offer = await pc.createOffer();
          await pc.setLocalDescription(offer);
          log("Local description set");

          // รอให้ ICE gathering เสร็จ
          setStatus("กำลังรวบรวม ICE candidates...", "info");
          await new Promise((resolve) => {
            if (pc.iceGatheringState === "complete") {
              resolve();
            } else {
              pc.addEventListener("icegatheringstatechange", () => {
                if (pc.iceGatheringState === "complete") {
                  resolve();
                }
              });
            }
          });

          // Encode offer
          const encoded = btoa(JSON.stringify(pc.localDescription));
          document.getElementById("offerText").value = encoded;

          log("Offer created and encoded");
          setStatus("Offer พร้อมแล้ว - คัดลอกและส่งไปยัง Gateway", "success");

          document.getElementById("btnSetAnswer").disabled = false;
          document.getElementById("btnCreateOffer").disabled = true;
        } catch (error) {
          log(`Error: ${error.message}`);
          setStatus("เกิดข้อผิดพลาด: " + error.message, "error");
        }
      }

      function copyOffer() {
        const offerText = document.getElementById("offerText");
        offerText.select();
        document.execCommand("copy");
        log("Offer copied to clipboard");
        alert("Offer ถูกคัดลอกแล้ว!\n\nวาง (paste) ลงใน terminal ที่รัน k2-gateway");
      }

      async function setAnswer() {
        try {
          const answerText = document.getElementById("answerText").value.trim();

          if (!answerText) {
            alert("กรุณาวาง Answer จาก Gateway");
            return;
          }

          setStatus("กำลังตั้งค่า Answer...", "info");
          log("Setting remote description...");

          // Decode answer
          const answer = JSON.parse(atob(answerText));
          await pc.setRemoteDescription(new RTCSessionDescription(answer));

          log("Remote description set");
          setStatus("กำลังเชื่อมต่อ...", "info");

          document.getElementById("btnSetAnswer").disabled = true;
        } catch (error) {
          log(`Error: ${error.message}`);
          setStatus("เกิดข้อผิดพลาด: " + error.message, "error");
          alert("Answer ไม่ถูกต้อง กรุณาตรวจสอบอีกครั้ง");
        }
      }

      // Initialize
      log("K2 Gateway WebRTC Client ready");
      setStatus("พร้อมเริ่มต้น", "info");
    </script>
  </body>
</html>
```

---

## ขั้นตอนการใช้งานทั้งหมด

### 1. เตรียม Gateway

```bash
# ตรวจสอบ .env file
cat .env

# เริ่ม Gateway
./k2-gateway
```

### 2. เปิด Browser Client

- เปิดไฟล์ HTML ข้างต้นใน Browser
- คลิก "1. เชื่อมต่อ WebRTC" (อนุญาตใช้ microphone)
- คลิก "2. สร้าง Offer"
- คลิก "คัดลอก Offer"

### 3. ส่ง Offer ไปยัง Gateway

- วาง (Paste) Offer ลงใน terminal ที่รัน k2-gateway
- กด Enter
- Gateway จะคืน Answer กลับมา

### 4. รับ Answer กลับมา

- คัดลอก Answer จาก terminal
- วางใน textarea "Answer (จาก Gateway)"
- คลิก "3. ตั้งค่า Answer"

### 5. ทดสอบการโทร

- ใช้ SIP phone โทรเข้าเบอร์: `0900200002`
- เสียงควรจะไหลผ่าน WebRTC connection
- ฟังเสียงได้จาก audio element ใน Browser

---

## การ Debug

### ตรวจสอบ WebRTC Connection

```javascript
// ดู ICE candidates
pc.onicecandidate = (event) => {
  if (event.candidate) {
    console.log("ICE Candidate:", event.candidate);
  }
};

// ดู statistics
setInterval(async () => {
  const stats = await pc.getStats();
  stats.forEach((report) => {
    if (report.type === "inbound-rtp" && report.kind === "audio") {
      console.log("Audio packets received:", report.packetsReceived);
    }
  });
}, 1000);
```

### ตรวจสอบ TURN Server Usage

เพื่อยืนยันว่า WebRTC connection ใช้ TURN relay จริง ๆ:

1. **เปิด TURN debug logging** ใน `.env`:
   ```bash
   DEBUG_TURN=true
   ```

2. **รัน gateway และดู logs**:
   - จะเห็น ICE candidates ทุกตัวที่ถูก discover (host, srflx, relay)
   - เมื่อ ICE connected จะเห็น selected candidate pair พร้อม type และ address
   - ถ้าใช้ TURN จะเห็น `✅ TURN RELAY ACTIVE` และ candidate type เป็น `relay`

3. **ตัวอย่าง log output**:
   ```
   [Session abc123] 🧊 ICE Candidate: type=host address=192.168.1.100:54321 protocol=UDP
   [Session abc123] 🧊 ICE Candidate: type=srflx address=49.49.54.94:54321 protocol=UDP
   [Session abc123] 🧊 ICE Candidate: type=relay address=203.154.83.50:3478 protocol=UDP
   [Session abc123] 🧊 Selected Candidate Pair: ✅ TURN RELAY ACTIVE
   [Session abc123]   Local:  type=relay address=203.154.83.50:3478 protocol=UDP
   [Session abc123]   Remote: type=relay address=203.154.83.51:49152 protocol=UDP
   ```

4. **วิธีบังคับให้ใช้ TURN เท่านั้น** (ฝั่ง browser):
   ```javascript
   const pc = new RTCPeerConnection({
     iceTransportPolicy: "relay",  // บังคับใช้ relay เท่านั้น
     iceServers: [{
       urls: "turn:turn.ttrs.or.th:3478?transport=udp",
       username: "turn01",
       credential: "Test1234"
     }]
   });
   ```

### ตรวจสอบ SIP Registration

```bash
# ดู log จาก Gateway
# จะเห็นข้อความ:
# ✓ SIP Registration successful with authentication (200 OK)
```

---

## Troubleshooting

### ไม่มีเสียง

1. ตรวจสอบว่า microphone ได้รับอนุญาต
2. ตรวจสอบว่า audio element มี `autoplay` attribute
3. ตรวจสอบ browser console สำหรับ errors
4. ตรวจสอบว่า TURN server ทำงานปกติ

### Connection ล้มเหลว

1. ตรวจสอบ TURN server credentials
2. ตรวจสอบ firewall settings
3. ตรวจสอบว่า Gateway รัน SIP listener อยู่
4. ตรวจสอบว่า SIP registration สำเร็จ

### SIP Registration ล้มเหลว

1. ตรวจสอบ `.env` configuration
2. ตรวจสอบ network connectivity ไปยัง SIP server
3. ตรวจสอบ username/password
4. ตรวจสอบ port (default: 5060)

---

## REST API Reference

K2 Gateway รองรับ REST API สำหรับ call control:

### Endpoints

| Method | Endpoint                   | Description                     |
| ------ | -------------------------- | ------------------------------- |
| POST   | `/api/offer`               | Submit WebRTC offer, get answer |
| POST   | `/api/call`                | Make outbound call              |
| POST   | `/api/hangup/{sessionId}`  | Hang up call                    |
| GET    | `/api/sessions`            | List all sessions               |
| GET    | `/api/session/{sessionId}` | Get session details             |
| POST   | `/api/dtmf/{sessionId}`    | Send DTMF tones                 |

### POST /api/offer

Submit WebRTC offer and receive answer:

```bash
curl -X POST http://localhost:8080/api/offer \
  -H "Content-Type: application/json" \
  -d '{"sdp": "v=0\r\no=..."}'
```

**Response:**

```json
{
  "sdp": "v=0\r\no=...",
  "sessionId": "uuid-session-id"
}
```

### POST /api/call

Initiate outbound call:

```bash
curl -X POST http://localhost:8080/api/call \
  -H "Content-Type: application/json" \
  -d '{"sessionId": "uuid", "destination": "9999"}'
```

**Response:**

```json
{
  "sessionId": "uuid",
  "state": "connecting",
  "message": "Call initiated"
}
```

### GET /api/sessions

List all active sessions:

```bash
curl http://localhost:8080/api/sessions
```

**Response:**

```json
[
  {
    "id": "uuid",
    "state": "active",
    "direction": "outbound",
    "from": "0900200002",
    "to": "9999",
    "createdAt": "2025-12-20T16:00:00Z"
  }
]
```

---

## WebSocket API Reference

Connect to `ws://localhost:8080/ws` for real-time signaling.

### Message Types

#### Client → Server

```json
// Submit offer
{ "type": "offer", "sdp": "v=0..." }

// Make call
{ "type": "call", "sessionId": "uuid", "destination": "9999" }

// Hang up
{ "type": "hangup", "sessionId": "uuid" }

// Send DTMF
{ "type": "dtmf", "sessionId": "uuid", "digits": "123" }
```

> Public mode note: if `sipUsername` or `sipDomain` changes from the existing session identity,
> the gateway rejects `call` with an `error` and requires a new `offer` (new session).

#### Server → Client

```json
// Answer
{ "type": "answer", "sessionId": "uuid", "sdp": "v=0..." }

// State update
{ "type": "state", "sessionId": "uuid", "state": "ringing" }

// Error
{ "type": "error", "sessionId": "uuid", "error": "Session not found" }
```

---

## Legacy Mode

สำหรับ backward compatibility สามารถใช้ legacy mode ได้:

```bash
./k2-gateway --legacy
```

Legacy mode ใช้ stdin/stdout สำหรับ signaling (copy/paste offer/answer)

---

## Test Client

K2 Gateway มี test client ในตัว:

1. รัน gateway: `./k2-gateway`
2. เปิด browser: `http://localhost:8080`
3. Click "Connect WebSocket"
4. Click "Start Session"
5. ใส่เบอร์ปลายทาง เช่น `9999`
6. Click "Call"

---

## SIP MESSAGE (Text Messaging)

K2 Gateway รองรับการส่ง/รับ SIP MESSAGE สำหรับ text messaging ระหว่าง WebRTC client และ SIP endpoint

### ✅ Features

- **Receive SIP MESSAGE**: รับข้อความจาก SIP endpoint และแสดงใน Web UI
- **Send In-Dialog MESSAGE**: ส่งข้อความผ่าน session ที่กำลังโทรอยู่ (ตรงไปยัง Contact ของ remote party)
- **Auto-routing**: Server จัดการ routing อัตโนมัติ ไม่ต้องระบุ destination
- **Digest Authentication**: รองรับ 401/407 challenge response

### การใช้งาน

#### 1. รับข้อความ

เมื่อมี SIP MESSAGE เข้ามา server จะส่ง WebSocket message ประเภท `message`:

```javascript
// Server → Client
{
  "type": "message",
  "from": "sip:100@domain.com",
  "body": "Hello from SIP!",
  "contentType": "text/plain"
}
```

#### 2. ส่งข้อความ (ระหว่างโทร)

เมื่ออยู่ในสาย server จะส่งข้อความตรงไปยัง remote Contact โดยอัตโนมัติ:

```javascript
// Client → Server
{
  "type": "send_message",
  "body": "Hello from WebRTC!",
  "contentType": "text/plain;charset=UTF-8"
}

// Server → Client (confirmation)
{
  "type": "messageSent",
  "destination": "",
  "body": "Hello from WebRTC!"
}
```

**หมายเหตุ:** ไม่ต้องระบุ `destination` - server จะใช้ `SIPRemoteContact` จาก session ที่กำลัง active อยู่

### WebSocket Message Types

| Direction | Type | Description |
|-----------|------|-------------|
| S → C | `message` | Incoming SIP MESSAGE |
| C → S | `send_message` | Send SIP MESSAGE |
| S → C | `messageSent` | Message sent confirmation |

### ตัวอย่าง JavaScript

```javascript
// รับข้อความ
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === "message") {
    console.log(`Message from ${msg.from}: ${msg.body}`);
  }
};

// ส่งข้อความ (ระหว่างโทร)
function sendMessage(text) {
  ws.send(JSON.stringify({
    type: "send_message",
    body: text,
    contentType: "text/plain;charset=UTF-8"
  }));
}
```

### Flow Diagram

```
[Browser/WebRTC] ←──WebSocket──→ [K2 Gateway] ←──SIP MESSAGE──→ [SIP Endpoint]

Outgoing Message:
1. Browser sends: { type: "send_message", body: "Hi" }
2. Gateway finds active session's SIPRemoteContact
3. Gateway sends SIP MESSAGE to remote Contact directly
4. Gateway sends confirmation: { type: "messageSent" }

Incoming Message:
1. SIP Endpoint sends MESSAGE to Gateway
2. Gateway parses and extracts From/Body
3. Gateway sends to Browser: { type: "message", from: "...", body: "..." }
```

### In-Dialog vs Out-of-Dialog

K2 Gateway ใช้ **In-Dialog MESSAGE** เมื่อมี active session:

- **In-Dialog**: ส่งตรงไปยัง `SIPRemoteContact` ที่ได้จาก 200 OK ของ INVITE
  - ใช้ Call-ID, From-tag, To-tag จาก session
  - Route ตาม Route-Set ถ้ามี
  - ไม่ผ่าน proxy (Asterisk) ไปตรงถึง endpoint

- **Out-of-Dialog**: ส่งผ่าน SIP domain (ต้องระบุ destination)
  - ใช้เมื่อไม่มี active session
  - Route ผ่าน SIP proxy/registrar

---

## ข้อมูลเพิ่มเติม

- [WebRTC API Documentation](https://developer.mozilla.org/en-US/docs/Web/API/WebRTC_API)
- [SIP Protocol (RFC 3261)](https://datatracker.ietf.org/doc/html/rfc3261)
- [SIP MESSAGE Method (RFC 3428)](https://datatracker.ietf.org/doc/html/rfc3428)
- [Pion WebRTC](https://github.com/pion/webrtc)
- [SIPgo Library](https://github.com/emiago/sipgo)
