# WebRTC-SIP Gateway

Monorepo สำหรับสะพานระหว่าง **WebRTC** (เบราว์เซอร์, มือถือ, เอเจนต์) กับ **SIP/RTP** (Asterisk, Kamailio)

- `apps/gateway` — บริการ Go ที่ถือ signaling และ media
- `apps/frontend` — หน้า operations สำหรับดู session, trunk และสถานะ gateway
- `packages/` — config และ UI ที่ใช้ร่วมกัน

```
Browser / Mobile / Agent
        │  JSON over WebSocket + SRTP
        ▼
 WebRTC-SIP Gateway
        │  SIP + RTP/RTCP
        ▼
 Kamailio / Asterisk ──► SIP endpoint
```

ฝั่ง WebRTC เป็น SRTP ฝั่ง SIP เป็น RTP ธรรมดา เสียงค่าเริ่มต้นคือ Opus แบบ passthrough วิดีโอคือ H.264 พร้อม cache SPS/PPS และฉีด keyframe

---

## WebSocket

มีสี่เส้นทาง ข้อความทั้งหมดเป็น JSON รายละเอียดชนิดข้อความอยู่ใน [`docs/gateway/ws-contract.md`](docs/gateway/ws-contract.md)

| เส้นทาง | แฟล็ก | ค่าเริ่มต้น | ใครใช้ | การยืนยันตัวตน | Presence |
| --- | --- | --- | --- | --- | --- |
| `/ws` | `API_ENABLE_WS` | เปิด | มือถือ Android/iOS | `access_token` เมื่อ `AUTH_ENABLE=true` | sticky: หลุดแล้วไม่ SIP UNREGISTER เพื่อให้ปลุกด้วย push ได้ |
| `/ws-public` | `API_ENABLE_PUBLIC_WS` | ปิด | สายสาธารณะครั้งเดียว | ไม่ใช้ token | credential ส่งในข้อความ `call` ของ connection นั้น |
| `/ws-agent` | `API_ENABLE_AGENT_WS` | ปิด | เอเจนต์บน PC | `access_token` ไม่บังคับ ถ้ามีจะตรวจ JWT | client ส่ง `agent_register` พร้อม SIP credential; REGISTER อยู่ขณะมี client; client สุดท้ายหลุดแล้ว hangup แล้ว UNREGISTER |
| `/ws-agent-device` | `API_ENABLE_AGENT_DEVICE_WS` | ปิด | เอเจนต์บนมือถือ | บังคับ `devicePlatform=android\|ios`; `access_token` ไม่บังคับตอนเชื่อม/ลงทะเบียน/รับสาย (ถ้ามีจะตรวจ JWT; ต้องใช้สำหรับ `call` โหมด trunk เมื่อ `AUTH_ENABLE=true`) | client ส่ง `device_register`; หลุดแล้วไม่ UNREGISTER และเก็บ FCM; ปลดด้วยข้อความ `unregister` |

พฤติกรรมเฉพาะเส้นทาง:

- **`/ws`** เมื่อตั้ง `SIPCLIENT_AUTH_REGISTER_URL` ร่วมกับ `AUTH_ENABLE=true`, `DB_ENABLE=true` และ `SIP_TRUNK_ENABLE=true` จะ provision trunk มือถืออัตโนมัติเฉพาะ JWT ของ realm `user` ก่อน upgrade สำเร็จ โดยต้องมี `devicePlatform=android|ios` ใน URL แล้ว gateway ส่ง `trunk_resolved` กลับ (หากมีงาน provision หรือ `unregister` ของบัญชีเดียวกันกำลังทำอยู่ซ้อนกันจะตอบ `409 Conflict`)
- **`/ws-public`** รับเฉพาะสายของ connection นั้น `call` ต้องมี `sipDomain`, `sipUsername`, `sipPassword`
- **`/ws-agent`** trunk คงที่รูปแบบ `sipclient-agent-<username>@<domain>:<port>` หลาย client ใช้ SIP identity เดียวกันได้ สายเข้าถูกยื่นให้ทุก client ที่ว่าง คนแรกที่ `offer` หรือ `accept` ได้สาย เมื่อ `AUTH_ENABLE=true` ใช้ได้สองแบบ: (A) เชื่อมด้วย `access_token` แล้วโทรผ่าน trunk ที่ `agent_register` ผูกไว้ (B) ไม่ส่ง token แล้วส่ง `sipDomain`/`sipUsername`/`sipPassword` ใน `agent_register` (แบบ Electron SIP local) หรือในข้อความ `call` (public mode) ส่วนการส่ง `call` ขาออกโหมด trunk โดยไม่มี claims บน `/ws` และ `/ws-agent-device` ยังถูกปฏิเสธด้วย `Token authentication required to use trunk`
- **`/ws-agent-device`** บังคับ `devicePlatform=android|ios` เสมอ ส่วน `access_token` ไม่บังคับตอนเชื่อม/`device_register`/รับสายเข้า (`notify_user_id` ใช้ `sub` จาก JWT ถ้ามี หรือถอยกลับเป็น `sip:<username>@<domain>:<port>`) และต้องใช้ JWT เฉพาะตอนโทรออกโหมด trunk เมื่อ `AUTH_ENABLE=true` ใช้ trunk คนละชุดกับ agent PC (`sipclient-agent-device-...`) ไม่เรียก mobile provisioner เก็บ FCM ผ่าน `device_push_token` สายเข้า username/domain/port เดียวกันถูกยื่นให้ client ที่ว่างทั้ง `/ws-agent` และ `/ws-agent-device` โดย trunk ของ agent-device จะส่ง FCM ที่เก็บไว้ควบคู่ไปด้วยแม้มี device WS ออนไลน์อยู่ เพื่อให้ระบบปฏิบัติการมือถือปลุกแอปขึ้นมารับสาย (`ws_incoming.go`) ในขณะที่ `/ws-agent` บน PC ใช้เฉพาะ WebSocket ขณะออนไลน์

ถ้าแฟล็กของเส้นทางนั้นปิดอยู่ route จะไม่ถูกลงทะเบียน

---

## สายโทร

**สายออก** — client เปิด WebSocket ส่ง `offer` ได้ `answer` พร้อม `sessionId` แล้วส่ง `call` Gateway สร้าง SIP INVITE ไป PBX เมื่อปลายทางรับสาย client ได้สถานะ `active`

**สายเข้า** — PBX ส่ง INVITE มาที่ trunk ที่ลงทะเบียนไว้ Gateway ส่ง `incoming` ให้ client ที่ตรง trunk client ตอบ `accept` แล้ว Gateway ตอบ `200 OK` ไป SIP ถ้ามีหลาย client คนแรกที่รับได้สาย

**กลับเข้าสาย** — WebSocket ขาดระหว่างสาย SIP ยังค้างบน gateway client เชื่อมใหม่แล้วส่ง `resume` พร้อม `sessionId` เดิม เส้นทางที่รองรับ `resume` คือ `/ws`, `/ws-public` และ `/ws-agent-device`

ข้อความ control ที่ใช้ร่วมกันมี `offer`, `answer`, `ice`, `call`, `incoming`, `accept`, `reject`, `hangup`, `dtmf` ชนิดที่ผูกกับเส้นทางใดเส้นทางหนึ่ง เช่น `agent_register`, `device_register`, `trunk_resolve` อยู่ในสัญญา WebSocket

---

## Media

- เสียง: Opus passthrough ถ้าเปิด `SIP_AUDIO_INBOUND_GAIN_ENABLE` จะ transcode เฉพาะทิศ SIP → WebRTC
- วิดีโอ: H.264 Gateway แยก STAP-A, cache SPS/PPS แล้วฉีดก่อน IDR เพื่อให้ decoder ฝั่ง SIP เริ่มภาพได้
- วิดีโอขาออก WebRTC → SIP ส่งตรงโดยไม่มี pacer เมื่อเบราว์เซอร์เปลี่ยนความละเอียดกลางสาย ค่าเริ่มต้น `SIP_UPLINK_PICTURE_SIZE_CHANGE=accept` จะพักภาพขนาดใหม่ไว้จนถึง IDR แรกแล้วส่งต่อ (ไม่ทิ้งวิดีโอ) รายละเอียดใน [`docs/gateway/config-reference.md`](docs/gateway/config-reference.md)
- NAT ฝั่ง WebRTC ใช้ TURN (`TURN_SERVER`) ฝั่ง SIP ใช้ symmetric RTP และ `SIP_PUBLIC_IP` เมื่อ gateway อยู่หลัง NAT
- พื้นที่ที่เปลี่ยนแล้วกระทบสายจริง: การแยก NAL, จังหวะฉีด SPS/PPS, การเร่ง PLI/FIR, และการเรียนรู้ที่อยู่ RTP

---

## โครงสร้าง

| เส้นทาง | หน้าที่ |
| --- | --- |
| `apps/gateway` | Go service `webrtc-sip-gateway` — HTTP, WebSocket, SIP, RTP |
| `apps/frontend` | Operations UI (React, TanStack Start, พอร์ต 3150) |
| `packages/ui` | คอมโพเนนต์ที่ใช้ร่วมกัน |
| `packages/eslint-config`, `packages/typescript-config` | config ของ workspace |
| `deploy/` | Docker Compose และ nginx สำหรับโดเมนเดียว |
| `docs/gateway/` | สัญญา WebSocket, config, ops, troubleshooting |

โมดูลหลักใน gateway:

| โมดูล | หน้าที่ |
| --- | --- |
| `internal/api/` | route HTTP/WebSocket, REST, SSE |
| `internal/session/` | PeerConnection, ส่งต่อ RTP, กู้ keyframe |
| `internal/sip/` | SIP signaling, SDP, trunk |
| `internal/auth/` | ตรวจ JWT ผ่าน JWKS |
| `internal/sipclientauth/` | provision trunk มือถือจาก JWT |
| `internal/push/` | FCM, APNs, TTRS สำหรับสายเข้าตอนออฟไลน์ |
| `internal/logstore/` | เก็บบันทึกสายใน PostgreSQL เมื่อเปิด DB |
| `internal/translator/` | คำบรรยายและเสียงแปลแบบสด |
| `internal/chatimage/` | รูปในแชทระหว่างสาย |

---

## เริ่มพัฒนา

ต้องมี Node.js `>= 18`, pnpm 10 (`packageManager` ใน `package.json` คือ `pnpm@10.32.1`) และ Go `1.27.3`

```bash
pnpm install
```

คัดลอก env ก่อนรัน:

- `apps/frontend/.env.example` → `apps/frontend/.env`
- `apps/gateway/.env.example` → `apps/gateway/.env`

จากราก repo:

```bash
pnpm dev:frontend    # http://localhost:3150
pnpm build
pnpm lint
pnpm check-types     # รันเฉพาะ workspace ที่มี script check-types (packages/ui, ไม่รวม apps/frontend)
pnpm format
```

รัน Go gateway บนเครื่อง (API พอร์ต `8080`; หมายเหตุ: `pnpm dev:backend` เรียก `turbo run dev --filter=gateway` ซึ่งปัจจุบันเป็น no-op เพราะ `apps/gateway/package.json` ไม่มี script `dev`):

```bash
cd apps/gateway && go run .
```

ทดสอบและตรวจสอบเฉพาะส่วน:

```bash
cd apps/gateway
go test ./...

# ตรวจสอบ apps/frontend (build / lint / test)
pnpm --filter frontend run build
pnpm --filter frontend run lint
pnpm --filter frontend run test -- src/features/trunk/types.test.ts
```

`FRONTEND_PASSWORD` ต้องตรงกันทั้ง frontend และ gateway หน้า admin ใช้รหัสนี้เป็น bearer ของ REST (การตั้ง `FRONTEND_PASSWORD` บน gateway จะบังคับตรวจ Bearer บนเส้นทาง `/api/*` ที่ป้องกันไว้ แม้ `AUTH_ENABLE=false`) ส่วนมือถือและ `/ws` ใช้ JWT ของ Keycloak เมื่อ `AUTH_ENABLE=true` ทั้งนี้เส้นทางสาธารณะที่ไม่ต้องส่ง Bearer ได้แก่ `GET /api/version`, `GET /api/logs*`, `GET /api/client-diagnostics*` และ `POST|GET /api/chat-images*`

ตัวแปรที่พบบ่อยอยู่ที่ [`docs/gateway/config-reference.md`](docs/gateway/config-reference.md) ชุดที่เกี่ยวกับเส้นทางด้านบน:

| ตัวแปร | หน้าที่ |
| --- | --- |
| `API_PORT` | พอร์ต HTTP และ WebSocket (ค่าเริ่มต้น `8080`) |
| `API_ENABLE_WS` | เปิด `/ws` |
| `API_ENABLE_PUBLIC_WS` | เปิด `/ws-public` |
| `API_ENABLE_AGENT_WS` | เปิด `/ws-agent` |
| `API_ENABLE_AGENT_DEVICE_WS` | เปิด `/ws-agent-device` |
| `AUTH_ENABLE` | บังคับ JWT สำหรับ `/ws`, REST ที่ป้องกันไว้ และการส่ง `call` โหมด trunk บน `/ws-agent-device`; ส่วนการเชื่อม/`device_register`/สายเข้าบน `/ws-agent-device` ไม่บังคับ `access_token` และ `/ws-agent` ใช้ได้ทั้ง `access_token`+trunk หรือ SIP user/pass โดยไม่ส่ง token |
| `SIPCLIENT_AUTH_REGISTER_URL` | provision trunk มือถือตอนเชื่อม `/ws` (เฉพาะ JWT realm `user` เมื่อเปิด `AUTH_ENABLE`, `DB_ENABLE`, `SIP_TRUNK_ENABLE`) |
| `SIP_PORT` | พอร์ต SIP (ค่าเริ่มต้น `5060`) |
| `DB_ENABLE` / `DB_DSN` | PostgreSQL สำหรับ trunk และบันทึกสาย |
| `GATEWAY_INSTANCE_ID` | รหัส instance คงที่ |
| `GATEWAY_PUBLIC_WS_URL` | URL สาธารณะของ WebSocket สำหรับ redirect ข้าม instance |

Deploy โดเมนเดียว ดู [`deploy/README.md`](deploy/README.md) reverse proxy ต้องส่ง `/api/` และทุกเส้นทาง WebSocket ที่เปิดใช้ (`/ws`, `/ws-public`, `/ws-agent`, `/ws-agent-device`) ไปที่ gateway พอร์ต `8080`

---

## ดู log และ diagnostics

อ่าน log ของโปรเซสผ่าน API ได้โดยไม่ต้องใช้ bearer:

```bash
curl https://gateway.example.com/api/logs
curl "https://gateway.example.com/api/logs/current?tail=500"
```

Client อัปโหลด diagnostics ที่กรองความลับแล้ว:

```bash
curl -X POST https://gateway.example.com/api/client-diagnostics \
  -H "Authorization: Bearer <access-token>" \
  -H "Content-Type: application/json" \
  -d '{"clientTraceId":"trace-1","events":[{"source":"app","level":"info","name":"app.boot"}]}'
```

อ่านย้อนหลังโดยไม่ต้องใช้ bearer:

```bash
curl "https://gateway.example.com/api/client-diagnostics?page=1&pageSize=100"
curl "https://gateway.example.com/api/client-diagnostics/sessions/<sessionId>/events?page=1&pageSize=100"
```

รายละเอียดตัวกรองและการเก็บบันทึกอยู่ใน [`docs/gateway/ops-guide.md`](docs/gateway/ops-guide.md)

---

## เอกสารที่เกี่ยวข้อง

| เรื่อง | ไฟล์ |
| --- | --- |
| สัญญา WebSocket | [`docs/gateway/ws-contract.md`](docs/gateway/ws-contract.md) |
| ตัวแปรสภาพแวดล้อม | [`docs/gateway/config-reference.md`](docs/gateway/config-reference.md) |
| ปฏิบัติการและ diagnostics | [`docs/gateway/ops-guide.md`](docs/gateway/ops-guide.md) |
| แก้ปัญหาสายและ media | [`docs/gateway/troubleshooting.md`](docs/gateway/troubleshooting.md) |
| Deploy | [`deploy/README.md`](deploy/README.md) |
| แนวทางแก้ gateway | [`apps/gateway/AGENTS.md`](apps/gateway/AGENTS.md) |
| Trunk REST/SSE API | [`apps/gateway/trunk-api.md`](apps/gateway/trunk-api.md) |
| ระบบแปลเสียงสด (S2S Translator) | [`docs/translator.md`](docs/translator.md) |
| แนวทางแก้ frontend | [`apps/frontend/AGENTS.md`](apps/frontend/AGENTS.md) |
| หน้า operations | [`apps/frontend/README.md`](apps/frontend/README.md) |
