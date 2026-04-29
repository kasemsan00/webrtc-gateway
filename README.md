https://webrtc-gateway-ui.app.kasemsan.com/

# WebRTC Gateway Monorepo

Monorepo สำหรับระบบ **K2 Gateway** — bridge ที่เชื่อม **WebRTC** (เบราว์เซอร์/มือถือ) กับ **SIP** (ระบบโทรศัพท์ VoIP) เข้าด้วยกัน

ประกอบด้วย frontend สำหรับ operations UI และ backend gateway service สำหรับ signaling/media

---

## ระบบทำอะไร? (System Overview)

K2 Gateway ทำหน้าที่เป็น **สะพานเชื่อมระหว่างโลก WebRTC กับโลก SIP** โดย:

1. **รับ WebRTC จากเบราว์เซอร์** — ผ่าน WebSocket + SRTP (เข้ารหัส)
2. **แปลงและส่งต่อไปยัง SIP** — ส่ง RTP ธรรมดาไปยัง Kamailio/Asterisk
3. **จัดการ media** — แปลง codec, จัดการ keyframe, รับมือ NAT

```
┌─────────────────┐         ┌───────────────┐         ┌─────────────────────┐
│  Browser/Mobile  │◄──────►│  K2 Gateway   │◄──────►│  Kamailio/Asterisk  │
│  (WebRTC+SRTP)   │  WS +  │  (Bridge)     │  SIP +  │  (SIP PBX)          │
│                   │  SRTP  │               │  RTP    │                     │
└─────────────────┘         └───────────────┘         └─────────┬───────────┘
                                                                │
                                                    ┌───────────▼───────────┐
                                                    │  Linphone / Browser B │
                                                    │  (SIP Endpoint)       │
                                                    └───────────────────────┘
```

**ภาษาง่ายๆ:** เบราว์เซอร์พูดภาษา WebRTC ←→ Gateway แปลภาษา ←→ ระบบโทรศัพท์พูดภาษา SIP

---

## Supported Call Flows

ระบบรองรับ 2 flow พร้อมกัน:

### Flow A: Browser → SIP Desktop (Linphone)

```
Browser  ──WebSocket──►  Gateway  ──SIP INVITE──►  Kamailio  ──►  Linphone
   │                        │                                         │
   │◄──── SRTP audio/video ─┤──── RTP audio/video ──────────────────►│
```

เบราว์เซอร์โทรหา Linphone desktop ผ่าน SIP core — Gateway เป็นตัวแปลง media ระหว่างสองโลก

### Flow B: Browser ↔ Browser (ผ่าน SIP Core)

```
Browser A  ──WS──►  Gateway A  ──SIP──►  Kamailio  ──SIP──►  Gateway B  ──WS──►  Browser B
    │                   │                                          │                   │
    │◄── SRTP ──────────┤◄──────── RTP ──────────────────────────►├──── SRTP ────────►│
```

ทั้งสองเบราว์เซอร์เชื่อมผ่าน gateway คนละตัว โดย SIP core เป็นตัว route สาย

> รายละเอียดเต็มดูที่ `docs/dual-flow.md`

---

## Gateway ทำงานอย่างไร? (How It Works)

### 1. Signaling Flow (ขั้นตอนตั้งสาย)

#### สายออก (Outbound Call)

```
1. เบราว์เซอร์ เปิด WebSocket ไปที่ Gateway
2. ส่ง "offer" พร้อม SDP (บอก Gateway ว่าจะส่ง media อะไรได้บ้าง)
3. Gateway ตอบ "answer" กลับ พร้อม sessionId
4. เบราว์เซอร์ส่ง "call" พร้อมเบอร์ปลายทาง
5. Gateway สร้าง SIP INVITE ส่งไป Kamailio/Asterisk
6. Asterisk route สายไปยังปลายทาง (เช่น Linphone)
7. ปลายทางรับสาย → Gateway แจ้งเบราว์เซอร์ว่า state: "active"
8. เสียงและวิดีโอเริ่มไหลผ่าน Gateway
```

#### สายเข้า (Inbound Call)

```
1. SIP endpoint โทรเข้ามาที่เบอร์ที่ลงทะเบียนไว้
2. Kamailio/Asterisk ส่ง INVITE มาที่ Gateway
3. Gateway สร้าง session แล้วส่ง "incoming" ไปยังเบราว์เซอร์ที่ตรง trunk
4. เบราว์เซอร์ส่ง "accept" กลับมา
5. Gateway ตอบ 200 OK ไป SIP → media เริ่มไหล
```

#### WebSocket Messages (สิ่งที่คุยกันผ่าน WebSocket)

| ทิศทาง          | Message         | หน้าที่                          |
| --------------- | --------------- | -------------------------------- |
| Client → Server | `offer`         | ส่ง SDP เพื่อเริ่ม session       |
| Server → Client | `answer`        | ตอบ SDP พร้อม sessionId          |
| Client → Server | `call`          | โทรออกไปยังเบอร์ปลายทาง          |
| Server → Client | `incoming`      | แจ้งสายเข้า                      |
| Client → Server | `accept`        | รับสายเข้า                       |
| Client → Server | `hangup`        | วางสาย                           |
| Client → Server | `dtmf`          | ส่งเสียงปุ่มกด                   |
| Client → Server | `resume`        | กลับเข้ามาหลัง network หลุด      |
| Client → Server | `trunk_resolve` | ลงทะเบียน trunk สำหรับรับสายเข้า |

---

### 2. Media Flow (เสียงและวิดีโอไหลอย่างไร)

#### เสียง (Audio)

```
Browser (Opus, SRTP เข้ารหัส)
   │
   ▼  ถอดรหัส SRTP → RTP
Gateway
   │
   ▼  ส่ง RTP ธรรมดา (Opus หรือ PCMU)
Asterisk / Linphone
```

- **WebRTC → SIP:** Gateway ถอดรหัส SRTP แล้วส่ง RTP ธรรมดาไปยัง Asterisk
- **SIP → WebRTC:** Gateway เข้ารหัส RTP เป็น SRTP แล้วส่งกลับเบราว์เซอร์
- รองรับ Opus (codec คุณภาพสูง) และ PCMU (codec legacy)

#### วิดีโอ (Video)

```
Browser (H.264, SRTP)
   │
   ▼  ถอด SRTP + แกะ STAP-A → แยก NAL units
Gateway
   │  + ฉีด SPS/PPS (parameter sets สำหรับ decoder)
   ▼  ส่ง RTP ทีละ NAL unit
Asterisk / Linphone
```

- **H.264 เป็น codec หลัก** (VP8 เป็น fallback)
- Gateway จัดการ **STAP-A de-aggregation** — เบราว์เซอร์อาจรวมหลาย NAL ในแพ็คเกตเดียว แต่ Linphone/Asterisk ต้องการทีละตัว
- Gateway **cache SPS/PPS** (ข้อมูลตั้งค่า decoder) จาก keyframe แรก แล้วฉีดก่อน IDR frame ทุกครั้ง เพื่อให้ decoder ฝั่ง SIP เริ่มแสดงผลได้

#### Keyframe Recovery (วิดีโอค้าง → กู้คืน)

เมื่อวิดีโอค้าง gateway มีกลไกอัตโนมัติ:

| สถานการณ์                   | การตอบสนอง                                     |
| --------------------------- | ---------------------------------------------- |
| เบราว์เซอร์ส่ง PLI (ภาพหาย) | Gateway ส่งต่อเป็น RTCP compound packet ไป SIP |
| ไม่มี keyframe > 1.5 วินาที | Gateway ส่ง PLI อัตโนมัติ                      |
| ไม่มี keyframe > 3 วินาที   | Gateway ส่ง FIR (บังคับ keyframe ใหม่ทั้งหมด)  |
| เปลี่ยนเครือข่าย            | Recovery burst mode — ส่ง PLI ถี่ขึ้นชั่วคราว  |

---

### 3. NAT Traversal & Network

```
Browser (อยู่หลัง NAT)
   │  ใช้ TURN server relay
   ▼
Gateway
   │  Symmetric RTP — เรียนรู้ IP ปลายทางจากแพ็คเกตจริง
   ▼
SIP Endpoint (อาจอยู่หลัง NAT เช่นกัน)
```

- **TURN server** สำหรับเบราว์เซอร์ที่อยู่หลัง NAT ที่เข้มงวด
- **Symmetric RTP** ฝั่ง SIP — Gateway เรียนรู้ IP จริงจากแพ็คเกตแรกที่ได้รับ (รับมือ NAT โดยไม่ต้อง STUN)
- **ICE-lite** ฝั่ง SIP — ไม่ต้อง candidate harvesting, ลด latency ตอนเริ่มต้น

---

### 4. Call Resume (กลับเข้าสายหลัง network หลุด)

เมื่อ WebSocket หลุด (เช่น เปลี่ยน WiFi → 5G) สาย SIP ยังคงค้างอยู่บน Gateway:

```
1. มือถือเปลี่ยนเครือข่าย → WebSocket ขาด
2. สาย SIP ยังทำงานอยู่บน Gateway (RTP ยังไหล)
3. มือถือเชื่อมต่อ WebSocket ใหม่
4. ส่ง "resume" พร้อม sessionId เดิม
5. Gateway ตรวจสอบว่า session ยังอยู่ → ตอบ "resumed"
6. Media กลับมาไหลต่อโดยไม่ต้องโทรใหม่
```

---

### 5. Trunk Routing (ระบบจัดเส้นทางสาย)

Trunk คือ "บัญชี SIP" ที่ใช้สำหรับรับ/ส่งสาย — จัดการผ่าน database:

- **Trunk mapping** อยู่ในตาราง `sip_trunks` — มี domain, port, username, password
- **สายออก:** Gateway ใช้ trunk ที่ระบุเพื่อ register กับ SIP core แล้วโทรออก
- **สายเข้า:** Asterisk route INVITE มายัง trunk ที่ตรง → Gateway ส่งต่อให้เบราว์เซอร์ที่ resolve trunk นั้นไว้
- **First-accept-wins:** ถ้ามีหลายเบราว์เซอร์รับ incoming พร้อมกัน คนแรกที่ accept ได้สาย

---

## Project Structure

| โฟลเดอร์                     | คำอธิบาย                                      |
| ---------------------------- | --------------------------------------------- |
| `apps/frontend`              | React + TypeScript + Vite — Operations UI     |
| `apps/gateway`               | Go service — WebRTC ↔ SIP bridge (K2 Gateway) |
| `packages/ui`                | Shared UI components                          |
| `packages/eslint-config`     | Shared ESLint config                          |
| `packages/typescript-config` | Shared TypeScript config                      |

### Gateway Internal Modules

| Module                | หน้าที่                                                               |
| --------------------- | --------------------------------------------------------------------- |
| `internal/api/`       | HTTP/WebSocket server — รับ signaling จากเบราว์เซอร์                  |
| `internal/session/`   | จัดการ session — สร้าง PeerConnection, forward RTP, keyframe recovery |
| `internal/sip/`       | SIP client/server — register, INVITE, BYE, trunk management           |
| `internal/sip/sdp.go` | สร้างและจัดการ SDP — ฉีด codec params, จัดการ media ports             |
| `internal/logstore/`  | บันทึก call events/stats ลง PostgreSQL                                |
| `internal/auth/`      | JWT token verification                                                |

## Tech Stack

- **Monorepo:** `pnpm` workspaces + `turborepo`
- **Frontend:** React, TypeScript, Vite, TanStack Router
- **Backend:** Go (`k2-gateway`), Pion WebRTC, SIP stack
- **Codecs:** H.264 (หลัก), VP8 (สำรอง), Opus (เสียง), PCMU (legacy)
- **Database:** PostgreSQL (optional, สำหรับ trunk routing + call logging)

## Prerequisites

- Node.js `>= 18`
- `pnpm@9`
- Go `1.26.2` (สำหรับ `apps/gateway`)

## Install Dependencies

รันที่ root ของ repo:

```bash
pnpm install
```

## Root Commands

```bash
pnpm build
pnpm lint
pnpm check-types
pnpm dev
```

## Run Each App

Frontend (จาก root):

```bash
pnpm dev:frontend
```

Backend (จาก root):

```bash
pnpm dev:backend
```

## App-Level Commands

Frontend scripts: ดูที่ `apps/frontend/package.json`

Backend targets: ดูที่ `apps/gateway/project.json`

ตัวอย่าง backend แบบตรงโฟลเดอร์:

```bash
cd apps/gateway
go run .
go test ./...
```

## Environment Setup

ตั้งค่า environment แยกตามแอปก่อนรัน:

- Frontend: `apps/frontend/.env.example`
- Backend: `apps/gateway/.env.example`

### Environment Variables ที่สำคัญ (Gateway)

| ตัวแปร                                            | หน้าที่                                           |
| ------------------------------------------------- | ------------------------------------------------- |
| `SIP_DOMAIN` / `SIP_USERNAME` / `SIP_PASSWORD`    | Identity ของ Gateway ในระบบ SIP                   |
| `SIP_PORT`                                        | Port สำหรับ SIP (default: 5060)                   |
| `TURN_SERVER` / `TURN_USERNAME` / `TURN_PASSWORD` | TURN server สำหรับ NAT traversal                  |
| `API_PORT`                                        | WebSocket/REST API port (default: 8080)           |
| `DB_ENABLE` / `DB_DSN`                            | เปิดใช้ PostgreSQL สำหรับ trunk routing + logging |
| `SIP_TRUNK_ENABLE`                                | เปิดใช้ trunk-based routing                       |
| `GATEWAY_INSTANCE_ID`                             | ID คงที่ของ instance (สำหรับ HA)                  |
| `GATEWAY_PUBLIC_WS_URL`                           | Public URL สำหรับ redirect/recovery ข้าม instance |
| `AUTH_ENABLE` / `AUTH_JWKS_URL`                   | JWT authentication                                |

สำหรับ flow browser-to-browser ผ่าน SIP core ต้องใช้ trunk/DB:

- `DB_ENABLE=true`
- `SIP_TRUNK_ENABLE=true`
- ตั้ง `GATEWAY_INSTANCE_ID` ให้คงที่
- ตั้ง `GATEWAY_PUBLIC_WS_URL` ใน environment จริงเพื่อรองรับ redirect/recovery ข้าม instance

ไฟล์อ้างอิงเพิ่มเติม:

- `apps/frontend/README.md`
- `apps/gateway/AGENTS.md`
- `docs/dual-flow.md`

## Development Notes

- แก้โค้ดให้ scope อยู่ในแอป/แพ็กเกจที่เกี่ยวข้องเท่านั้น
- หลีกเลี่ยงแก้ไฟล์ generated เช่น `apps/frontend/src/routeTree.gen.ts`
- การเปลี่ยน media path ใน `apps/gateway` มีความเสี่ยงสูง ควรเลี่ยงหากไม่ได้ตั้งใจแก้พฤติกรรมโปรโตคอล

### พื้นที่ความเสี่ยงสูง (High-Risk Areas)

| พื้นที่                     | ความเสี่ยง                                  |
| --------------------------- | ------------------------------------------- |
| STAP-A de-aggregation       | แยก NAL ผิด → วิดีโอหาย                     |
| SPS/PPS injection           | ฉีดผิดจังหวะ → จอดำฝั่ง SIP                 |
| PLI/FIR throttling          | เข้มไป = วิดีโอค้าง / หลวมไป = network ท่วม |
| Profile-level-id derivation | ค่าไม่ตรง → decoder ปฏิเสธ stream           |
| Symmetric RTP trust window  | Grace period ผิด → endpoint สลับไปมา        |

## Key References

- Monorepo tasks: `turbo.json`
- Workspace config: `pnpm-workspace.yaml`
- Root scripts: `package.json`
- Frontend guide: `apps/frontend/AGENTS.md`
- Backend guide: `apps/gateway/AGENTS.md`
- Dual flow docs: `docs/dual-flow.md`
