# เอกสารส่งงาน WebRTC Gateway ตาม TOR

เอกสารนี้อธิบายการส่งมอบส่วน **WebRTC Gateway** ตามขอบเขต TOR  
“จ้างพัฒนาแอปพลิเคชัน N1669 สำหรับผู้แจ้งเหตุ ให้รองรับการสื่อสารมัลติมีเดียแบบเรียลไทม์…”

> **หมายเหตุสำคัญ**  
> TOR เขียนในรูปแบบฟังก์ชัน/คำสั่ง แต่ช่องทางหลักของ call-control คือ **WebSocket JSON** (`/ws`) ไม่ใช่ REST resource  
> Gateway ปัจจุบันพัฒนาต่อจาก TOR แล้ว — หากชื่อ message ไม่ตรงทีละตัวอักษร จะแมปไปยัง **สัญญาปัจจุบันที่ใกล้เคียงที่สุด** และระบุความต่างไว้ชัดเจน  
> สัญญาปัจจุบัน: `[docs/gateway/ws-contract.md](gateway/ws-contract.md)`, translation: `[docs/translator.md](../translator.md)`

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


| หัวข้อ TOR                               | สิ่งที่ส่งมอบจริง                                         | หลักฐาน                          |
| ---------------------------------------- | --------------------------------------------------------- | -------------------------------- |
| WebRTC Gateway พัฒนาด้วยภาษา Golang      | `apps/gateway` (Go)                                       | `go.mod`, `main.go`              |
| WebRTC Gateway API ใช้รูปแบบ Websocket   | `/ws`, `/ws-public` ส่งรับ JSON                           | `ws_conn.go`, `ws_dispatch.go`   |
| Web API ติดตั้งผ่าน Container Technology | รองรับ container deploy                                   | Docker / CI scripts ของ repo     |
| SSO ด้วย Keycloak ของแพลตฟอร์ม NDEMS     | JWT auth ผ่าน JWKS (`iss`/`aud`) เมื่อ `AUTH_ENABLE=true` | `auth_http.go`, `internal/auth/` |


ช่องทางหลักของ Mobile/Client:

```text
wss://<gateway>/ws?access_token=<jwt>&devicePlatform=android|ios
```

---



## 3. พัฒนา WebRTC Gateway Service (Capability ตาม TOR)

หัวข้อนี้คือ “ความสามารถของระบบ” ไม่ใช่รายชื่อ REST endpoint


| #   | หัวข้อ TOR                                               | สิ่งที่ตรง/ใกล้เคียงใน Gateway ปัจจุบัน                            | ช่องทาง                        | หลักฐานหลัก                                           |
| --- | -------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------ | ----------------------------------------------------- |
| 1   | สร้าง WebRTC Session (SDP offer/answer, สถานะ Session)   | `offer` → `answer` + `state` (และ `ice` เสริม)                     | WS (+ REST `/api/offer` สำรอง) | `ws_call.go`, `handlers_call.go`, `internal/session/` |
| 2   | โทรออกไปยัง SIP Endpoint / Trunk                         | `call` หลังมี session                                              | WS (+ REST `/api/call`)        | `ws_call.go`, `internal/sip/`                         |
| 3   | รับสายเรียกเข้าจาก SIP Core แล้วแจ้ง Client              | `incoming` + push (FCM/APNS) เมื่อจำเป็น                           | WS + Push                      | `ws_incoming.go`, `internal/push/`                    |
| 4   | วางสายและ Cleanup Session                                | `hangup` → `state: ended` + cleanup                                | WS (+ REST hangup)             | `ws_call.go`                                          |
| 5   | ส่ง DTMF ระหว่างสนทนา                                    | `dtmf` (ส่ง) และ event `dtmf` (รับ)                                | WS (+ REST DTMF)               | `ws_call.go`, `ws_notify.go`                          |
| 6   | ส่ง/รับ SIP MESSAGE + สถานะส่ง                           | `send_message` → `messageSent` / รับเข้าเป็น `message`             | WS                             | `ws_notify.go`                                        |
| 7   | กู้คืนสาย Resume หลังเครือข่ายหลุด                       | `resume` → `resumed` / `resume_failed` / `resume_redirect`         | WS                             | `ws_resume.go`                                        |
| 8   | ค้นหาและ Resolve Trunk                                   | `trunk_resolve` → `trunk_resolved` / `trunk_*`                     | WS                             | `ws_trunk.go`                                         |
| 9   | บริหาร Trunk (สร้าง/แก้ไข/เปิด-ปิด/Soft Delete)          | REST Trunk CRUD; ปิดใช้งานด้วย `enabled=false` (soft delete)       | REST `/api/trunks*`            | `handlers_trunk.go`, `sip/trunk_manager.go`           |
| 10  | Register / Unregister Trunk + Refresh สถานะ              | REST register/unregister + refresh                                 | REST                           | `handlers_trunk.go`                                   |
| 11  | JWT Auth สำหรับ `/api/*` และ `/ws`                       | Auth middleware เมื่อ Auth Mode เปิด                               | HTTP/WS                        | `auth_http.go`                                        |
| 12  | Multi-instance: Registry, Redirect, Resume ข้าม Instance | `resume_redirect`, `trunk_redirect`, session directory / instances | WS + REST ops                  | `ws_resume.go`, `ws_trunk.go`, `handlers_ops.go`      |
| 13  | บันทึก Event, Payload, Stats, Dialogs, Session History   | LogStore + REST history APIs                                       | REST + DB                      | `internal/logstore/`, `handlers_session.go`           |




### ความต่างจาก TOR (Capability)

- Call-control หลักอยู่ที่ **WebSocket**; REST call APIs ยังมีเป็นช่องทางสำรอง/admin
- SIP MESSAGE ปัจจุบันส่ง/รับข้อความตรง ๆ — **ไม่แปลข้อความแชทอัตโนมัติ** ตามถ้อยคำ TOR เก่า (การแปลหลักของระบบปัจจุบันคือ **เสียง S2S** ผ่าน `translate`)
- มีความสามารถเพิ่มจาก TOR: `request_keyframe`, `renegotiate`/`renegotiate_answer`, `ringing`, `media`, `client_state`, `trunk_push_token`, client diagnostics

---



## 4. พัฒนา WebRTC Gateway Service API และ Interface



### 4.1 คำสั่ง WebSocket ควบคุมสาย (Client → Gateway)


| หัวข้อ TOR             | Message type ปัจจุบัน | Payload หลัก                                            | ผลลัพธ์ที่คาดหวัง                                                           | หลักฐาน          |
| ---------------------- | --------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------- |
| คำสั่ง `offer`         | `offer`               | `sdp`, `sessionId?`                                     | `answer`, `state`                                                           | `ws_call.go`     |
| คำสั่ง `call`          | `call`                | `sessionId`, `destination`, trunk หรือ public SIP creds | `state` (`connecting`/`ringing`/`active`)                                   | `ws_call.go`     |
| คำสั่ง `accept`        | `accept`              | `sessionId`                                             | รับสายฝั่ง SIP + `state`                                                    | `ws_incoming.go` |
| คำสั่ง `reject`        | `reject`              | `sessionId`, `reason?`                                  | ปฏิเสธสาย + `state`                                                         | `ws_incoming.go` |
| คำสั่ง `hangup`        | `hangup`              | `sessionId`                                             | ยุติ session                                                                | `ws_call.go`     |
| คำสั่ง `dtmf`          | `dtmf`                | `sessionId`, `digits`                                   | ส่ง DTMF ไปปลายทาง                                                          | `ws_call.go`     |
| คำสั่ง `send_message`  | `send_message`        | `body`, `destination?`, `sessionId?`                    | `messageSent` หรือ `error`                                                  | `ws_notify.go`   |
| คำสั่ง `resume`        | `resume`              | `sessionId`, `sdp?`                                     | `resumed` / `resume_failed` / `resume_redirect`                             | `ws_resume.go`   |
| คำสั่ง `trunk_resolve` | `trunk_resolve`       | SIP creds หรือระบุผ่าน provision ตอน connect            | `trunk_resolved` / `trunk_not_found` / `trunk_not_ready` / `trunk_redirect` | `ws_trunk.go`    |
| คำสั่ง `ping`          | `ping`                | —                                                       | `pong`                                                                      | `ws_midcall.go`  |


คำสั่งเสริมที่ TOR ไม่ได้ระบุ แต่ใช้จริงในระบบปัจจุบัน:


| Message type                   | หน้าที่                                       |
| ------------------------------ | --------------------------------------------- |
| `ice`                          | ส่ง ICE candidate                             |
| `request_keyframe`             | ขอ keyframe เพื่อกู้วิดีโอ                    |
| `renegotiate_answer`           | ตอบ mid-call renegotiation                    |
| `trunk_push_token`             | ผูก push token กับ trunk                      |
| `client_state`                 | แจ้งสถานะ client (เช่น foreground/background) |
| `translate` / `translate_stop` | เปิด/ปิดแปลเสียง (ดูข้อ 4.3)                  |




### 4.2 เหตุการณ์ WebSocket แจ้งกลับ (Gateway → Client)


| หัวข้อ TOR               | Message type ปัจจุบัน | ความหมาย                                                             | หลักฐาน                      |
| ------------------------ | --------------------- | -------------------------------------------------------------------- | ---------------------------- |
| คำสั่ง `answer`          | `answer`              | SDP Answer กลับหลังประมวลผล `offer`                                  | `ws_call.go`                 |
| คำสั่ง `state`           | `state`               | สถานะสาย: `connecting`, `ringing`, `active`, `ended`, `reconnecting` | `ws_notify.go`, `ws_call.go` |
| คำสั่ง `incoming`        | `incoming`            | มีสายเรียกเข้า                                                       | `ws_incoming.go`             |
| คำสั่ง `message`         | `message`             | รับ SIP MESSAGE จากปลายทาง                                           | `ws_notify.go`               |
| คำสั่ง `messageSent`     | `messageSent`         | ยืนยันส่งข้อความออกจาก Gateway แล้ว                                  | `ws_notify.go`               |
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


| Message type                         | หน้าที่                                   |
| ------------------------------------ | ----------------------------------------- |
| `ringing`                            | สัญญาณ ringing เสริมสำหรับ softphone      |
| `media`                              | แจ้งว่า remote media เริ่ม receiving แล้ว |
| `renegotiate` / `renegotiate_result` | mid-call SDP assistance / agent switch    |




### 4.3 คำสั่ง WebSocket ควบคุมการแปลภาษา


| หัวข้อ TOR                                                            | สิ่งปัจจุบันที่ใกล้เคียง                                     | หมายเหตุความต่าง                                |
| --------------------------------------------------------------------- | ------------------------------------------------------------ | ----------------------------------------------- |
| คำสั่ง `translate` (Source_language, Target_language, tts_voice_name) | `translate` ด้วยฟิลด์ `sourceLang`, `targetLang`, `ttsVoice` | ชื่อฟิลด์เปลี่ยนเป็น camelCase ตามสัญญาปัจจุบัน |
| คำสั่ง `translate_stop`                                               | `translate_stop`                                             | ตรงกัน                                          |


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


ตัวอย่างปัจจุบัน:

```json
{
  "type": "translation_caption",
  "sessionId": "<id>",
  "direction": "sip_to_webrtc",
  "sourceLang": "th-TH",
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


| กลุ่ม                | ตัวอย่าง                                                                           |
| -------------------- | ---------------------------------------------------------------------------------- |
| Call สำรอง           | `POST /api/offer`, `/api/call`, `/api/hangup/{sessionId}`, `/api/dtmf/{sessionId}` |
| Trunk บริหาร         | `GET/POST /api/trunks`, `PUT /api/trunk/{id}`, register/unregister/refresh         |
| Audit / History      | `/api/sessions/history`, `.../events`, `.../payloads`, `.../dialogs`, `.../stats`  |
| Multi-instance / Ops | `/api/gateway/instances`, `/api/session-directory`, `/api/dashboard`               |


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
SIP --> Gateway -- incoming --> Client
Client -- accept/reject --> Gateway --> SIP
Gateway -- state --> Client
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
- [ ] Call-control ผ่าน WebSocket JSON
- [ ] Deploy ได้ผ่าน container
- [ ] รองรับ JWT/Keycloak เมื่อเปิด Auth Mode



### Capability

- [ ] สร้าง session ด้วย `offer`/`answer`
- [ ] โทรออกด้วย `call`
- [ ] รับสายเข้าด้วย `incoming` + `accept`/`reject`
- [ ] วางสายด้วย `hangup` และ cleanup
- [ ] ส่ง/รับ DTMF
- [ ] ส่ง/รับ SIP MESSAGE (`send_message`/`message`/`messageSent`)
- [ ] Resume session (`resume`/`resumed`/`resume_failed`/`resume_redirect`)
- [ ] Resolve trunk (`trunk_resolve`/`trunk_*`)
- [ ] บริหาร trunk ผ่าน REST + soft delete (`enabled=false`)
- [ ] Register/Unregister/Refresh trunk
- [ ] JWT บังคับใช้กับ `/api/*` และ `/ws`
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