# เอกสารส่งงาน WebRTC Gateway ตาม TOR

เอกสารนี้อธิบายการส่งมอบส่วน **WebRTC Gateway** ตามขอบเขต TOR  
“จ้างพัฒนาแอปพลิเคชัน N1669 สำหรับผู้แจ้งเหตุ ให้รองรับการสื่อสารมัลติมีเดียแบบเรียลไทม์…”

> **หมายเหตุสำคัญ**  
> TOR เขียนในรูปแบบฟังก์ชัน/คำสั่ง แต่ช่องทางหลักของ call-control คือ **WebSocket JSON** (`/ws`) ไม่ใช่ REST resource  
> Gateway ปัจจุบันพัฒนาต่อจาก TOR แล้ว — หากชื่อ message ไม่ตรงทีละตัวอักษร จะแมปไปยัง **สัญญาปัจจุบันที่ใกล้เคียงที่สุด** และระบุความต่างไว้ชัดเจน  
> สัญญาปัจจุบัน: [docs/gateway/ws-contract.md](gateway/ws-contract.md), translation: [docs/translator.md](translator.md)

---



## 1. หลักการเขียนเอกสารส่งงาน (ไม่ใช่ REST API)


| ใช้คำเหล่านี้                              | หลีกเลี่ยงคำเหล่านี้               |
| ------------------------------------------ | ---------------------------------- |
| WebSocket Command / Event                  | API endpoint ของ call-control      |
| Message type (`offer`, `call`, …)          | HTTP method (GET/POST) เป็นหลัก    |
| ทิศทาง Client → Gateway / Gateway → Client | Path `/api/offer` เป็นหลักฐานเดียว |
| Session lifecycle / Call flow              | OpenAPI ของสายสนทนา                |


รูปแบบอธิบายแต่ละรายการ:

1. หัวข้อตาม TOR
2. Message type / ช่องทางปัจจุบัน
3. ทิศทาง
4. Payload หลัก
5. ผลลัพธ์ที่คาดหวัง
6. หลักฐานโค้ด
7. หมายเหตุถ้า TOR เก่าไม่ตรง

---



## 2. โครงสร้างระบบ (ตาม TOR)


| หัวข้อ TOR                               | สิ่งที่ส่งมอบจริง                                                                                                     | หลักฐาน                          |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| WebRTC Gateway พัฒนาด้วยภาษา Golang      | `apps/gateway` (Go)                                                                                                   | `go.mod`, `main.go`              |
| WebRTC Gateway API ใช้รูปแบบ Websocket   | `/ws`, `/ws-public`, `/ws-agent`, `/ws-agent-device` ส่งรับ JSON                                                      | `ws_conn.go`, `ws_dispatch.go`   |
| Web API ติดตั้งผ่าน Container Technology | รองรับ container deploy                                                                                               | Docker / CI scripts ของ repo     |
| SSO ด้วย Keycloak ของแพลตฟอร์ม NDEMS     | JWT auth ผ่าน JWKS (`iss`/`aud`) เมื่อ `AUTH_ENABLE=true` (และรองรับ `FRONTEND_PASSWORD` สำหรับ REST admin)           | `auth_http.go`, `internal/auth/` |


ช่องทางเชื่อมต่อ WebSocket ตามบทบาท:

```text
Mobile/User Client:  wss://<gateway>/ws?access_token=<jwt>&devicePlatform=android|ios
Public Per-call:     wss://<gateway>/ws-public
PC/Web Agent:        wss://<gateway>/ws-agent[?access_token=<jwt>]
Mobile Agent Device: wss://<gateway>/ws-agent-device?devicePlatform=android|ios[&access_token=<jwt>]
```

---



## 3. พัฒนา WebRTC Gateway Service (Capability ตาม TOR)

หัวข้อนี้คือ “ความสามารถของระบบ” ไม่ใช่รายชื่อ REST endpoint


| #   | หัวข้อ TOR                                               | สิ่งที่ตรง/ใกล้เคียงใน Gateway ปัจจุบัน                                                                                              | ช่องทาง                        | หลักฐานหลัก                                           |
| --- | -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------ | ----------------------------------------------------- |
| 1   | สร้าง WebRTC Session (SDP offer/answer, สถานะ Session)   | `offer` → `answer` (และส่ง `ice` เสริม; สถานะ `state: connecting` จะส่งเมื่อสั่ง `call`)                                             | WS (+ REST `/api/offer` สำรอง) | `ws_call.go`, `handlers_call.go`, `internal/session/` |
| 2   | โทรออกไปยัง SIP Endpoint / Trunk                         | `call` หลังมี session → `state` (`connecting`/`ringing`/`active`)                                                                    | WS (+ REST `/api/call`)        | `ws_call.go`, `internal/sip/`                         |
| 3   | รับสายเรียกเข้าจาก SIP Core แล้วแจ้ง Client              | `incoming` + push (FCM/APNS) → `offer`/`answer` → `accept` (หรือ `reject`)                                                           | WS + Push                      | `ws_incoming.go`, `internal/push/`                    |
| 4   | วางสายและ Cleanup Session                                | `hangup` → `state: ended` + cleanup                                                                                                  | WS (+ REST hangup)             | `ws_call.go`                                          |
| 5   | ส่ง DTMF ระหว่างสนทนา                                    | `dtmf` (ส่ง) และ event `dtmf` (รับ)                                                                                                  | WS (+ REST DTMF)               | `ws_call.go`, `ws_notify.go`                          |
| 6   | ส่ง/รับ SIP MESSAGE + สถานะส่ง                           | `send_message` → `messageSent` / รับเข้าเป็น `message`                                                                               | WS                             | `ws_notify.go`                                        |
| 7   | กู้คืนสาย Resume หลังเครือข่ายหลุด                       | `resume` → `resumed` / `resume_failed` / `resume_redirect`                                                                           | WS                             | `ws_resume.go`                                        |
| 8   | ค้นหาและ Resolve Trunk                                   | `trunk_resolve` → `trunk_resolved` / `trunk_*` (ส่วน mobile `/ws` ที่มี `SIPCLIENT_AUTH_REGISTER_URL` จะ auto-provision ตอน connect) | WS                             | `ws_trunk.go`, `ws_conn.go`                           |
| 9   | บริหาร Trunk (สร้าง/แก้ไข/เปิด-ปิด/Soft Delete)          | REST Trunk CRUD; ปิดใช้งานด้วย `enabled=false` (soft delete)                                                                         | REST `/api/trunks*`            | `handlers_trunk.go`, `sip/trunk_manager.go`           |
| 10  | Register / Unregister Trunk + Refresh สถานะ              | REST register/unregister + refresh + WS `unregister`/`unregistered`                                                                  | REST + WS                      | `handlers_trunk.go`, `ws_unregister.go`               |
| 11  | JWT Auth สำหรับ `/api/*` และ `/ws`                       | Bearer `<jwt>` หรือ `<FRONTEND_PASSWORD>` บน `/api/*` (ยกเว้น public routes); `?access_token=<jwt>` บน `/ws`                         | HTTP/WS                        | `auth_http.go`, `server.go`                           |
| 12  | Multi-instance: Registry, Redirect, Resume ข้าม Instance | `resume_redirect`, `trunk_redirect`, session directory / instances                                                                   | WS + REST ops                  | `ws_resume.go`, `ws_trunk.go`, `handlers_ops.go`      |
| 13  | บันทึก Event, Payload, Stats, Dialogs, Session History   | LogStore + REST history APIs                                                                                                         | REST + DB                      | `internal/logstore/`, `handlers_session.go`           |




### ความต่างจาก TOR (Capability)

- Call-control หลักอยู่ที่ **WebSocket**; REST call APIs ยังมีเป็นช่องทางสำรอง/admin
- SIP MESSAGE ปัจจุบันส่ง/รับข้อความตรง ๆ — **ไม่แปลข้อความแชทอัตโนมัติ** ตามถ้อยคำ TOR เก่า (การแปลหลักของระบบปัจจุบันคือ **เสียง S2S** ผ่าน `translate`)
- มีความสามารถเพิ่มจาก TOR: `hold`/`unhold`/`hold_state`, `unregister`/`unregistered`, `agent_register`, `device_register`, `device_push_token`, `cancel`, `request_keyframe`, `request-keyframe`, `renegotiate`/`renegotiate_answer`/`renegotiate_result`, `ringing`, `media`, `client_state`, `trunk_push_token`, chat images, client diagnostics

---



## 4. พัฒนา WebRTC Gateway Service API และ Interface



### 4.1 คำสั่ง WebSocket ควบคุมสาย (Client → Gateway)


| หัวข้อ TOR             | Message type ปัจจุบัน | Payload หลัก                                                                     | ผลลัพธ์ที่คาดหวัง                                                                                                                       | หลักฐาน          |
| ---------------------- | --------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| คำสั่ง `offer`         | `offer`               | `sdp`, `sessionId?`                                                              | `answer` (เฉพาะ SDP Answer; ยังไม่ส่ง `state: connecting` จนกว่าจะส่ง `call`)                                                           | `ws_call.go`     |
| คำสั่ง `call`          | `call`                | `sessionId`, `destination`, trunk หรือ public SIP creds (ละเว้นได้ถ้ามี trunk ที่ resolve แล้ว) | `state` (`connecting` แล้วเปลี่ยนเป็น `ringing`/`active` ตามผล SIP)                                                                     | `ws_call.go`     |
| คำสั่ง `accept`        | `accept`              | `sessionId`                                                                      | รับสายฝั่ง SIP (`200 OK`) + `state` (`active`)                                                                                          | `ws_incoming.go` |
| คำสั่ง `reject`        | `reject`              | `sessionId`, `reason?`, `reasonSource?`                                          | ปฏิเสธสาย + `state` (`ended`)                                                                                                           | `ws_incoming.go` |
| คำสั่ง `hangup`        | `hangup`              | `sessionId`, `reason?`                                                           | `state` (`ended`) + ยุติ session                                                                                                        | `ws_call.go`     |
| คำสั่ง `dtmf`          | `dtmf`                | `sessionId`, `digits`                                                            | ส่ง DTMF ไปปลายทาง                                                                                                                      | `ws_call.go`     |
| คำสั่ง `send_message`  | `send_message`        | `body`, `destination?`, `sessionId?`                                             | `messageSent` หรือ `error`                                                                                                              | `ws_notify.go`   |
| คำสั่ง `resume`        | `resume`              | `sessionId`, `sdp?`                                                              | `resumed` / `resume_failed` / `resume_redirect`                                                                                         | `ws_resume.go`   |
| คำสั่ง `trunk_resolve` | `trunk_resolve`       | `trunkId` หรือ `trunkPublicId` หรือ SIP creds (`sipDomain`, `sipUsername`, `sipPassword`, `sipPort?`) | `trunk_resolved` / `trunk_not_found` / `trunk_not_ready` / `trunk_redirect` (หมายเหตุ: mobile `/ws` ที่มี `SIPCLIENT_AUTH_REGISTER_URL` จะ auto-provision ตอน connect และส่ง `trunk_resolved` ทันทีโดยไม่ต้องส่ง `trunk_resolve`) | `ws_trunk.go`    |
| คำสั่ง `ping`          | `ping`                | —                                                                                | `pong`                                                                                                                                  | `ws_midcall.go`  |


คำสั่งเสริมที่ TOR ไม่ได้ระบุ แต่ใช้จริงในระบบปัจจุบัน:


| Message type                   | หน้าที่                                                                                  |
| ------------------------------ | ---------------------------------------------------------------------------------------- |
| `ice`                          | ส่ง trickle ICE candidate                                                                |
| `hold` / `unhold`              | พักสาย / นำสายกลับมาสนทนาผ่าน in-dialog SIP re-INVITE (`hold_state`)                     |
| `unregister`                   | ออกจากระบบแบบโจ่งแจ้งบน mobile `/ws` หรือ `/ws-agent-device` (SIP UNREGISTER + ล้าง push) |
| `agent_register`               | ลงทะเบียน SIP trunk แบบชั่วคราวบน `/ws-agent`                                           |
| `device_register`              | ลงทะเบียน SIP trunk แบบ sticky บน `/ws-agent-device`                                    |
| `device_push_token`            | บันทึก FCM token บน trunk สำหรับ `/ws-agent-device`                                      |
| `trunk_push_token`             | ผูก Apple PushKit contact token กับ trunk บน `/ws`                                       |
| `request_keyframe`             | ขอ keyframe เพื่อกู้วิดีโอฝั่ง SIP → Browser                                            |
| `request-keyframe`             | ขอ IDR ฝั่ง Browser → SIP uplink (เช่นหลัง `@switch`)                                    |
| `renegotiate_answer`           | ตอบรับ mid-call renegotiation / `@switch`                                                |
| `client_state`                 | แจ้งสถานะ client (เช่น `availability`, `callState`, `multiCall`, `activeCalls`)          |
| `translate` / `translate_stop` | เปิด/ปิดแปลเสียง S2S บน `/ws` และ `/ws-public` (ดูข้อ 4.3)                              |




### 4.2 เหตุการณ์ WebSocket แจ้งกลับ (Gateway → Client)


| หัวข้อ TOR               | Message type ปัจจุบัน | ความหมาย                                                             | หลักฐาน                      |
| ------------------------ | --------------------- | -------------------------------------------------------------------- | ---------------------------- |
| คำสั่ง `answer`          | `answer`              | SDP Answer กลับหลังประมวลผล `offer`                                  | `ws_call.go`                 |
| คำสั่ง `state`           | `state`               | สถานะสาย: `connecting`, `ringing`, `active`, `ended`, `reconnecting` | `ws_notify.go`, `ws_call.go` |
| คำสั่ง `incoming`        | `incoming`            | มีสายเรียกเข้า (`sessionId`, `from`, `to`, `hasVideo`: `"true"`/`"false"`) | `ws_incoming.go`             |
| คำสั่ง `message`         | `message`             | รับ SIP MESSAGE จากปลายทาง                                           | `ws_notify.go`               |
| คำสั่ง `messageSent`     | `messageSent`         | ยืนยันส่งข้อความออกจาก Gateway แล้ว (`sessionId?`, `destination?`, `body`) | `ws_notify.go`               |
| คำสั่ง `dtmf` (ขาเข้า)   | `dtmf`                | แจ้ง DTMF จากอีกฝั่ง                                                 | `ws_notify.go`               |
| คำสั่ง `resumed`         | `resumed`             | Resume สำเร็จ                                                        | `ws_resume.go`               |
| คำสั่ง `resume_failed`   | `resume_failed`       | Resume ไม่สำเร็จ                                                     | `ws_resume.go`               |
| คำสั่ง `resume_redirect` | `resume_redirect`     | ให้ไปต่อที่ Gateway instance อื่น                                    | `ws_resume.go`               |
| คำสั่ง `trunk_resolved`  | `trunk_resolved`      | Resolve trunk สำเร็จ (`trunkId`, `trunkPublicId`)                    | `ws_trunk.go`                |
| คำสั่ง `trunk_redirect`  | `trunk_redirect`      | ให้เปลี่ยน instance ตามเจ้าของ trunk                                 | `ws_trunk.go`                |
| คำสั่ง `trunk_not_found` | `trunk_not_found`     | ไม่พบ trunk                                                          | `ws_trunk.go`                |
| คำสั่ง `trunk_not_ready` | `trunk_not_ready`     | พบแต่ยังไม่พร้อม (เช่นยังไม่ register)                               | `ws_trunk.go`                |
| คำสั่ง `pong`            | `pong`                | ตอบ `ping`                                                           | `ws_midcall.go`              |
| คำสั่ง `error`           | `error`               | ข้อผิดพลาดระหว่างประมวลผลคำสั่ง                                      | `ws_util.go`                 |


เหตุการณ์เสริมที่ TOR ไม่ได้ระบุ:


| Message type                         | หน้าที่                                                                              |
| ------------------------------------ | ------------------------------------------------------------------------------------ |
| `ringing`                            | สัญญาณ ringing เสริมสำหรับ softphone                                                 |
| `media`                              | แจ้งว่า remote media เริ่ม `receiving` แล้ว                                         |
| `hold_state`                         | ยืนยันสถานะพักสาย/ยกเลิกพักสาย (`held: true\|false`)                                 |
| `cancel`                             | ยกเลิกสายเรียกเข้าที่แจ้งไว้ก่อนหน้า (`caller_cancelled`, `no_answer`, `answered_elsewhere`) |
| `unregistered`                       | ยืนยันการทำ explicit logout (`unregister`) สำเร็จ                                    |
| `renegotiate` / `renegotiate_result` | mid-call SDP assistance / agent switch                                               |




### 4.3 คำสั่ง WebSocket ควบคุมการแปลภาษา


| หัวข้อ TOR                                                            | สิ่งปัจจุบันที่ใกล้เคียง                                     | หมายเหตุความต่าง                                                                                                  |
| --------------------------------------------------------------------- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| คำสั่ง `translate` (Source_language, Target_language, tts_voice_name) | `translate` ด้วยฟิลด์ `sourceLang`, `targetLang`, `ttsVoice` | ชื่อฟิลด์เป็น camelCase; รองรับบน `/ws` และ `/ws-public` เท่านั้น (ไม่อนุญาตบน `/ws-agent` และ `/ws-agent-device`) |
| คำสั่ง `translate_stop`                                               | `translate_stop`                                             | ตรงกัน (รองรับบน `/ws` และ `/ws-public`)                                                                          |


> **หมายเหตุเรื่อง `ttsVoice`:** สำหรับภาษาปลายทางมาตรฐาน 9 ภาษา (`th`, `en`, `zh`, `ko`, `ja`, `ru`, `hi`, `de`, `fr`) Gateway จะเลือกเสียง Neural มาตรฐานของภาษานั้นโดยอัตโนมัติเสมอ (ไม่ใช้ค่า `ttsVoice` ที่ส่งมา); ค่า `ttsVoice` จะถูกใช้เป็น fallback เฉพาะกรณีที่ `targetLang` ไม่ได้อยู่ใน 9 ภาษานี้

ตัวอย่างปัจจุบัน:

```json
{
  "type": "translate",
  "sessionId": "<id>",
  "sourceLang": "en",
  "targetLang": "th",
  "ttsVoice": "th-TH-PremwadeeNeural"
}
```



### 4.4 ส่งผลลัพธ์/สถานะการแปลกลับ Client

TOR เดิมใช้ชื่อ event ชุดหนึ่ง แต่สัญญาปัจจุบันรวม/เปลี่ยนชื่อแล้ว:


| หัวข้อ TOR            | ใช้ของปัจจุบันที่ใกล้เคียง                        | วิธีเทียบ                                                                                            |
| --------------------- | ------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `translation_started` | `translate` ตอบกลับพร้อม `state: "enabled"`       | ยืนยันว่า pipeline เปิดแล้ว + ภาษาที่ใช้                                                             |
| `translation_stopped` | `translate_stop` ตอบกลับพร้อม `state: "disabled"` | ยืนยันว่าหยุดแปลแล้ว กลับโหมดเสียงปกติ                                                               |
| `translation_error`   | `error` (เช่น Translator not available)           | แจ้งข้อผิดพลาดผ่าน `error` มาตรฐาน                                                                   |
| `translated_text`     | `translation_caption`                             | ส่ง `recognizedText`, `translatedText`, `sourceLang`, `targetLang`, `isFinal` สำหรับ subtitle/status |
| `translated_message`  | `translation_caption` (ทิศทาง `sip_to_webrtc`)    | ใช้ caption event เดียวกันแสดงต้นฉบับ+ข้อความแปล; ไม่แยก message type ตาม TOR เก่า                   |


ตัวอย่างปัจจุบัน (เมื่อเปิดแปล `sourceLang: "en"`, `targetLang: "th"` ขากลับ `sip_to_webrtc` จะสลับภาษาเป็น `"th"` → `"en"`):

```json
{
  "type": "translation_caption",
  "sessionId": "<id>",
  "direction": "sip_to_webrtc",
  "sourceLang": "th",
  "targetLang": "en",
  "recognizedText": "...",
  "translatedText": "...",
  "isFinal": true
}
```



### 4.5 ฟังก์ชันขอสถานะ Translation Service


| หัวข้อ TOR                  | สิ่งปัจจุบันที่ใกล้เคียง                                                                                                               |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| ขอสถานะ Translation Service | ตรวจสุขภาพ translator ตอนสตาร์ท (`CheckHealth`) + สถานะผ่านการตอบ `translate`/`error`; ดู dashboard/config ของ gateway สำหรับสถานะระบบ |


ไม่ใช่ message type แยกชื่อตาม TOR แต่ครอบคลุมด้วย health check ของ translator client และ error path ของ `translate`

---



## 5. REST ที่เกี่ยวข้อง (แยกจาก WebSocket call-control)

ใช้สำหรับบริหารระบบ/รายงาน ไม่ใช่ช่องทางหลักของ Mobile call flow ตาม TOR

- **การยืนยันตัวตน REST (`/api/*`):** เมื่อเปิด `AUTH_ENABLE=true` หรือตั้งค่า `FRONTEND_PASSWORD` ระบบจะบังคับใช้ `Authorization: Bearer <jwt>` หรือ `Authorization: Bearer <FRONTEND_PASSWORD>` กับเส้นทาง `/api/*` ที่ต้องป้องกัน (แม้ `AUTH_ENABLE=false` หากตั้ง `FRONTEND_PASSWORD` ไว้ก็จะยังบังคับใช้ Bearer)
- **เส้นทาง Public ที่ยกเว้น Auth โดยตั้งใจ:** `GET /api/version` (เปิดเสมอแม้ `API_ENABLE_REST=false`), `GET /api/logs*`, `GET /api/client-diagnostics*` (เฉพาะฝั่งอ่าน GET 4 เส้นทาง), และ `POST/GET /api/chat-images*` (โดย `POST /api/chat-images` ต้องระบุ `sessionId` ของสายที่กำลังทำงานอยู่)


| กลุ่ม                | ตัวอย่าง                                                                           |
| -------------------- | ---------------------------------------------------------------------------------- |
| Call สำรอง           | `POST /api/offer`, `/api/call`, `/api/hangup/{sessionId}`, `/api/dtmf/{sessionId}` |
| Trunk บริหาร         | `GET/POST /api/trunks`, `GET /api/trunks/stream`, `PUT /api/trunk/{id}`, register/unregister/refresh, `PUT /api/user/trunk` |
| Audit / History      | `/api/sessions/history`, `.../overview`, `.../events`, `.../payloads`, `.../dialogs`, `.../stats` |
| Multi-instance / Ops | `/api/gateway/instances`, `/api/session-directory`, `/api/dashboard`, `/api/health/details`, `/api/config`, `/api/version` |


---



## 6. Sequence ส่งมอบ (ใช้ตรวจรับ)



### 6.1 โทรออก

```text
Client                Gateway                 SIP
  |-- offer ---------->|
  |<------ answer -----|
  |-- call ----------->|-- INVITE ----------->|
  |<------ state ------|<----- 180/200 -------|
  |        (connecting/ringing/active)
  |-- hangup --------->|-- BYE -------------->|
  |<------ state ended-|
```



### 6.2 รับสายเข้า

```text
SIP                   Gateway                 Client
  |-- INVITE --------->|-- incoming --------->|
  |                    |<-- offer (sessionId)-|
  |                    |--- answer ---------->|
  |                    |<-- accept -----------|
  |<----- 200 OK ------|--- state (active) -->|

(กรณีปฏิเสธสาย: Client ส่ง reject(sessionId) -> Gateway ตอบ SIP 486/603 และส่ง state: ended)
```



### 6.3 Resume หลังเครือข่ายหลุด

```text
Client -- resume --> Gateway
Gateway -- resumed | resume_failed | resume_redirect --> Client
```



### 6.4 Translation (สัญญาปัจจุบัน)

```text
Client -- translate --> Gateway
Gateway -- translate (state=enabled) --> Client
Gateway -- translation_caption* --> Client
Client -- translate_stop --> Gateway
Gateway -- translate_stop (state=disabled) --> Client
```

---



## 7. Checklist ตรวจรับตามลำดับ TOR (Gateway)



### โครงสร้างระบบ

- [ ] Gateway เป็น Golang service
- [ ] Call-control ผ่าน WebSocket JSON (`/ws`, `/ws-public`, `/ws-agent`, `/ws-agent-device`)
- [ ] Deploy ได้ผ่าน container
- [ ] รองรับ JWT/Keycloak เมื่อเปิด Auth Mode (และรองรับ `FRONTEND_PASSWORD` สำหรับ REST admin)



### Capability

- [ ] สร้าง session ด้วย `offer`/`answer`
- [ ] โทรออกด้วย `call` (`state: connecting/ringing/active`)
- [ ] รับสายเข้าด้วย `incoming` → `offer`/`answer` → `accept` (หรือ `reject`)
- [ ] วางสายด้วย `hangup` และ cleanup
- [ ] ส่ง/รับ DTMF
- [ ] ส่ง/รับ SIP MESSAGE (`send_message`/`message`/`messageSent`)
- [ ] Resume session (`resume`/`resumed`/`resume_failed`/`resume_redirect`)
- [ ] Resolve trunk (`trunk_resolve`/`trunk_*` หรือ auto-provision ตอน connect บน `/ws`)
- [ ] บริหาร trunk ผ่าน REST + soft delete (`enabled=false`)
- [ ] Register/Unregister/Refresh trunk (ทั้ง REST และ explicit WS `unregister`)
- [ ] บังคับใช้ Auth กับ `/api/*` (ยกเว้น public routes ที่กำหนด) และ `/ws`
- [ ] Multi-instance redirect/resume
- [ ] บันทึก history/events/payloads/stats/dialogs



### WebSocket Interface

- [ ] Client→Gateway commands ตามตาราง 4.1
- [ ] Gateway→Client events ตามตาราง 4.2
- [ ] Translation commands ตามตาราง 4.3
- [ ] Translation results แมปตามตาราง 4.4 (`translation_caption` แทนชื่อ TOR เก่า)

---



## 8. สรุปการแมปที่ TOR เก่าไม่ตรงทีละชื่อ


| TOR เดิม                                                 | ใช้ของปัจจุบัน                                                               |
| -------------------------------------------------------- | ---------------------------------------------------------------------------- |
| เรียกเป็น API                                            | ส่งมอบเป็น **WebSocket message contract**                                    |
| `translation_started`                                    | `translate` + `state:"enabled"`                                              |
| `translation_stopped`                                    | `translate_stop` + `state:"disabled"`                                        |
| `translation_error`                                      | `error`                                                                      |
| `translated_text` / `translated_message`                 | `translation_caption`                                                        |
| `Source_language` / `Target_language` / `tts_voice_name` | `sourceLang` / `targetLang` / `ttsVoice`                                     |
| `send_message` แปลข้อความอัตโนมัติก่อนส่ง SIP            | ปัจจุบันโฟกัสแปลเสียง S2S; chat message ส่งตรง (ระบุเป็นข้อต่างจาก TOR เก่า) |


เอกสารนี้อ้างอิงสถานะโค้ดปัจจุบันของ `webrtc-sip-gateway` และใช้เป็นหลักฐานส่งงาน/ตรวจรับส่วน Gateway โดยไม่บังคับให้ชื่อ message ตรง TOR ทีละตัวหากมีการพัฒนารุ่นใหม่แทนแล้ว
