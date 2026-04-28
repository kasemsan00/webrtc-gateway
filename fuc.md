# รายการฟังก์ชันระบบและข้อมูลรายละเอียดสำหรับจัดทำ TOR

เอกสารนี้สรุปขอบเขตงานเชิงฟังก์ชันของระบบในภาพรวม โดยปรับรูปแบบการเรียบเรียงให้อ่านในลักษณะรายการฟังก์ชันและขอบเขตงาน เพื่อใช้เป็นข้อมูลตั้งต้นสำหรับจัดทำ TOR

ภาพรวมระบบ

1. ระบบประกอบด้วย `webrtc-gateway` สำหรับ backend gateway และ `ttrs-vri-webrtc-react-native` สำหรับ mobile softphone ฝั่งผู้ใช้งาน
2. ระบบรองรับการสื่อสารภาพ เสียง และข้อความแบบเรียลไทม์ ระหว่างฝั่งผู้ใช้งานบนมือถือหรือเว็บกับระบบโทรศัพท์ SIP/VoIP
3. ระบบรองรับการเชื่อมต่อระหว่าง Browser, Mobile, SIP endpoint และ SIP core ผ่าน WebRTC, WebSocket, REST API และ SIP signaling
4. ระบบรองรับการโทรภาพและเสียง การส่งข้อความ การส่ง DTMF การทำ RTT และการกู้คืนสายเมื่อเครือข่ายขัดข้อง
5. ระบบรองรับการจัดการ SIP trunk, session, security, logging, statistics และข้อมูลทางเทคนิคที่จำเป็นต่อการให้บริการ

ระบบ `webrtc-gateway` สำหรับ Backend Gateway

1. ฟังก์ชันสร้าง WebRTC Session สำหรับเริ่ม session ระหว่าง client กับ gateway ผ่าน WebSocket หรือ REST โดยรองรับ SDP offer, SDP answer และสถานะ session
2. ฟังก์ชันโทรออกไปยังหมายเลขปลายทางผ่าน gateway ไปยัง SIP endpoint หรือ trunk ที่กำหนด
3. ฟังก์ชันรับสายเรียกเข้าจาก SIP core และแจ้ง client ที่เกี่ยวข้องเพื่อให้รับสายหรือปฏิเสธสาย
4. ฟังก์ชันวางสายและยุติการสนทนาจากฝั่ง browser หรือฝั่ง SIP พร้อม cleanup session และปล่อย resource ที่เกี่ยวข้อง
5. ฟังก์ชันส่ง DTMF ระหว่างสนทนา เพื่อใช้งานกับ IVR หรือระบบตอบรับอัตโนมัติของปลายทาง
6. ฟังก์ชันส่งและรับ SIP MESSAGE ทั้งในระหว่างสายและนอกสาย พร้อมแจ้งสถานะการส่งข้อความ
7. ฟังก์ชันกู้คืนสายด้วยการ resume session หลังเครือข่ายหลุด โดยรองรับ reconnect และฟื้น media path ของสายเดิม
8. ฟังก์ชันค้นหาและ resolve trunk จาก domain, username, password, trunk id หรือ public id เพื่อใช้รับสายและโทรออก
9. ฟังก์ชันบริหาร trunk สำหรับสร้าง แก้ไข เปิดใช้งาน ปิดใช้งาน และเก็บประวัติ trunk แบบ soft delete
10. ฟังก์ชันลงทะเบียนและยกเลิกลงทะเบียน trunk กับ SIP core แบบ on-demand พร้อมฟังก์ชัน refresh สถานะ trunk
11. ฟังก์ชันตรวจสอบสิทธิ์และบังคับใช้ JWT authentication สำหรับ `/api/*` และ `/ws` เมื่อเปิดใช้งาน auth mode
12. ฟังก์ชันรองรับการทำงานหลาย gateway instances พร้อม registry, redirect และ resume ข้าม instance
13. ฟังก์ชันบันทึก event, payload, stats, dialogs และ session history ลงฐานข้อมูลสำหรับ audit และรายงานย้อนหลัง

API และ Interface ของระบบ `webrtc-gateway`

1. ฟังก์ชันรับคำสั่งจาก client ผ่าน WebSocket สำหรับการสร้างสาย ควบคุมสาย ส่งข้อความ ค้นหา trunk และตรวจสอบสถานะการเชื่อมต่อ
   1. `offer` ส่ง SDP offer จาก client ไปยัง gateway เพื่อเริ่มสร้าง WebRTC session และรอรับ SDP answer กลับมา
   2. `call` สั่งให้ gateway เริ่มโทรออกไปยังปลายทาง SIP endpoint หรือ trunk ที่ระบุ หลังจากสร้าง session แล้ว
   3. `accept` ตอบรับสายเรียกเข้าที่ gateway แจ้งมายัง client เพื่อให้ระบบส่งสัญญาณรับสายไปยังฝั่ง SIP
   4. `reject` ปฏิเสธสายเรียกเข้าที่ gateway แจ้งมายัง client โดยสามารถส่งเหตุผลการปฏิเสธได้
   5. `hangup` สั่งวางสายและยุติ session ปัจจุบันจากฝั่ง client
   6. `dtmf` ส่ง digits ระหว่างการสนทนาเพื่อควบคุม IVR หรือระบบปลายทางที่ต้องการสัญญาณปุ่มกด
   7. `send_message` ส่งข้อความจาก client ไปยังปลายทาง โดยรองรับทั้งในระหว่างสายและนอกสายตามบริบทของ session เมื่อ translation เปิดใช้งาน gateway จะแปลข้อความจาก source_language → target_language ก่อนส่งต่อเป็น SIP MESSAGE ไปยังปลายทาง โดยส่งข้อความที่แปลแล้วเท่านั้น (ไม่ส่งต้นฉบับ)
   8. `resume` ขอ reconnect และ resume session เดิมเมื่อการเชื่อมต่อหลุดหรือมีการเปลี่ยนเครือข่าย
   9. `trunk_resolve` ขอให้ gateway ค้นหา trunk ที่ตรงกับข้อมูล SIP credential หรือข้อมูล trunk ที่ระบุ ก่อนนำไปรับสายหรือโทรออก
   10. `ping` ส่ง keepalive เพื่อตรวจสอบว่า WebSocket connection ยังใช้งานได้ตามปกติ
2. ฟังก์ชันส่งผลลัพธ์และสถานะกลับไปยัง client ผ่าน WebSocket สำหรับตอบ SDP แจ้งสถานะสาย แจ้งข้อความ แจ้งผลการ resume และแจ้งผลการค้นหา trunk
   1. `answer` ส่ง SDP answer จาก gateway กลับไปยัง client หลังจากระบบประมวลผล offer แล้ว
   2. `state` แจ้งสถานะของสายหรือ session เช่น ringing, active, ended หรือสถานะสำคัญอื่นของการโทร
   3. `incoming` แจ้งว่ามีสายเรียกเข้าเข้ามายัง client พร้อมข้อมูลที่จำเป็นสำหรับการรับหรือปฏิเสธสาย
   4. `message` ส่งข้อความที่รับเข้ามาจากปลายทางมายัง client เมื่อ translation เปิดใช้งาน gateway จะแปลข้อความ SIP MESSAGE จาก target_language → source_language ก่อนส่งให้ client
   5. `messageSent` ยืนยันผลการส่งข้อความจาก client ไปยังปลายทางว่าได้รับการส่งออกจาก gateway แล้ว
   6. `dtmf` แจ้งสัญญาณ DTMF ที่รับเข้ามาจากอีกฝั่งของการสนทนา
   7. `resumed` แจ้งว่า session เดิมถูกกู้คืนสำเร็จและสามารถกลับมาใช้งานสายเดิมต่อได้
   8. `resume_failed` แจ้งว่าการกู้คืน session ไม่สำเร็จ เช่น ไม่พบ session เดิมหรือ session หมดอายุแล้ว
   9. `resume_redirect` แจ้งให้ client เปลี่ยนไปเชื่อมต่อกับ gateway instance อื่นเพื่อกู้คืน session เดิม
   10. `trunk_resolved` แจ้งผลการค้นหา trunk สำเร็จ พร้อมข้อมูล trunk ที่สามารถนำไปดำเนินการต่อได้
   11. `trunk_redirect` แจ้งให้ client เปลี่ยนไปใช้ gateway instance อื่นที่เป็นเจ้าของ trunk นั้นอยู่
   12. `trunk_not_found` แจ้งว่าไม่พบ trunk ที่ตรงกับเงื่อนไขหรือข้อมูลที่ client ส่งมา
   13. `trunk_not_ready` แจ้งว่า trunk ที่พบยังไม่พร้อมใช้งาน เช่น ยังไม่ register หรืออยู่ในสถานะที่โทรออกไม่ได้
   14. `pong` ตอบกลับคำสั่ง `ping` เพื่อยืนยันว่า WebSocket connection ยัง active อยู่
   15. `error` แจ้งข้อผิดพลาดที่เกิดขึ้นระหว่างการประมวลผลคำสั่งจาก client
3. ฟังก์ชัน Public API สำหรับขอ temporary SIP credential เพื่อใช้กับ public-entry flow ของ mobile softphone
   1. `POST {EXPO_PUBLIC_API_URL}/extension/public` ขอข้อมูล SIP credential ชั่วคราว เช่น domain, extension หรือ secret สำหรับเริ่มสายแบบ public-entry
4. ฟังก์ชัน REST API สำหรับสร้าง session สั่งโทรออก และวางสาย
   1. `POST /api/offer` ส่ง SDP offer ผ่าน REST เพื่อสร้าง WebRTC session และรับ SDP answer กลับมา
   2. `POST /api/call` สั่งให้ gateway โทรออกไปยังหมายเลขหรือปลายทางที่กำหนด โดยอ้างอิง session ที่สร้างไว้แล้ว
   3. `POST /api/hangup/{sessionId}` สั่งวางสายของ session ที่ระบุผ่าน sessionId
5. ฟังก์ชัน REST API สำหรับดูข้อมูล session ปัจจุบันและติดตามการเปลี่ยนแปลงของ session
   1. `GET /api/sessions` ดูรายการ session ที่กำลังมีอยู่ในระบบ ณ ขณะนั้น
   2. `GET /api/sessions/stream` ติดตาม event หรือการเปลี่ยนแปลงของ session แบบต่อเนื่อง
   3. `GET /api/session/{sessionId}` ดูรายละเอียดของ session ใด session หนึ่งตาม sessionId
6. ฟังก์ชัน REST API สำหรับส่ง DTMF ระหว่างการสนทนา
   1. `POST /api/dtmf/{sessionId}` ส่ง digits ไปยัง session ที่กำหนดระหว่างที่สายกำลัง active อยู่
7. ฟังก์ชัน REST API สำหรับดูประวัติ session, event, payload, dialog และ media stats ของแต่ละสาย
   1. `GET /api/sessions/history` ดูประวัติ session ที่สิ้นสุดแล้วหรือข้อมูลย้อนหลังของสายทั้งหมด
   2. `GET /api/sessions/{sessionId}/events` ดูรายการ event ของ session ที่ระบุ เช่น state change หรือเหตุการณ์สำคัญระหว่างสาย
   3. `GET /api/sessions/{sessionId}/payloads` ดู payload ที่เกี่ยวข้องกับ session เช่น signaling payload หรือ SDP ที่ใช้ในการ troubleshoot
   4. `GET /api/sessions/{sessionId}/dialogs` ดูข้อมูล SIP dialog ของ session ที่ระบุ
   5. `GET /api/sessions/{sessionId}/stats` ดู media stats ของ session เช่น packet loss, jitter, latency หรือ bitrate
8. ฟังก์ชัน REST API สำหรับดูข้อมูล infrastructure และสถานะการทำงานของระบบ
   1. `GET /api/gateway/instances` ดูรายการ gateway instances ที่กำลังทำงานและข้อมูลที่เกี่ยวข้องกับแต่ละ instance
   2. `GET /api/session-directory` ดูข้อมูล session directory ที่ช่วยในการ redirect หรือ resume ข้าม instance
   3. `GET /api/public-accounts` ดูรายการ public SIP accounts ที่ระบบจัดการอยู่
   4. `GET /api/ws-clients` ดูรายการ WebSocket clients ที่เชื่อมต่ออยู่กับระบบในขณะนั้น
   5. `GET /api/dashboard` ดึงข้อมูลสรุปภาพรวมของระบบเพื่อแสดงบน dashboard
9. ฟังก์ชัน REST API สำหรับบริหารจัดการ trunk และสถานะการลงทะเบียน trunk
   1. `GET /api/trunks` ดูรายการ trunk ทั้งหมด พร้อมข้อมูลสถานะและข้อมูลประกอบที่เกี่ยวข้อง
   2. `GET /api/trunk/{id}` ดูรายละเอียดของ trunk รายการใดรายการหนึ่งตาม id
   3. `POST /api/trunks` สร้าง trunk ใหม่ในระบบ
   4. `PUT /api/trunk/{id}` แก้ไขข้อมูล trunk ที่มีอยู่ เช่น domain, username, transport หรือสถานะ enabled
   5. `POST /api/trunk/{id}/register` สั่งให้ trunk ที่ระบุทำการ register กับ SIP core
   6. `POST /api/trunk/{id}/unregister` สั่งให้ trunk ที่ระบุยกเลิกการ register กับ SIP core
   7. `POST /api/trunks/refresh` สั่ง refresh สถานะ trunk และโหลดข้อมูล trunk ใหม่จากแหล่งข้อมูลที่ระบบใช้งานอยู่

ระบบ `ttrs-vri-webrtc-react-native` สำหรับ Mobile Softphone

1. ฟังก์ชันเลือกโหมดการติดต่ออย่างน้อย normal และ emergency ก่อนเริ่มการโทร
2. ฟังก์ชันกรอกแบบฟอร์ม public entry เช่น ชื่อ เบอร์โทร หน่วยงาน และข้อมูลประกอบก่อนเริ่มสาย
3. ฟังก์ชันขอ temporary SIP credential จาก Public API เพื่อใช้ในการโทรแบบ public-entry
4. ฟังก์ชันรีเซ็ต runtime state และล้าง resume metadata หรือ call state เดิมก่อนเริ่มสายใหม่
5. ฟังก์ชันเชื่อมต่อ gateway ผ่าน WebSocket แบบ on-demand เฉพาะช่วงเวลาที่ใช้งาน call flow
6. ฟังก์ชันโทรภาพและเสียงผ่าน WebRTC โดยรองรับ local media, PeerConnection, offer/answer และการเชื่อมต่อกับ gateway
7. ฟังก์ชันบังคับใช้ video และ audio พร้อมกัน โดยไม่ใช้ audio-only fallback
8. ฟังก์ชันควบคุมระหว่างสาย เช่น ปิดเสียง เปิดลำโพง ปิดกล้อง และสลับกล้อง
9. ฟังก์ชันส่ง DTMF ระหว่างสายจาก UI ของ mobile softphone ไปยัง gateway
10. ฟังก์ชันส่งข้อความระหว่างการสนทนาและแสดงข้อความที่รับเข้ามาใน in-call UI
11. ฟังก์ชันรองรับ Real-Time Text (RTT) ตามแนวทาง T.140/RTT-XML สำหรับการพิมพ์ข้อความแบบเรียลไทม์
12. ฟังก์ชัน fallback การส่ง RTT ไปใช้ SIP MESSAGE เมื่อ DataChannel ไม่พร้อมใช้งาน
13. ฟังก์ชันตรวจจับการเปลี่ยนเครือข่าย เช่น Wi-Fi ไป cellular เพื่อเตรียม reconnect และ resume
14. ฟังก์ชันกู้คืนสายบนมือถือด้วยการ reconnect และ resume session เดิมภายในช่วงเวลาที่กำหนด
15. ฟังก์ชันกู้คืนวิดีโอด้วยการ request keyframe และ refresh remote video เมื่อภาพค้างหรือ media path เพิ่งฟื้นตัว
16. ฟังก์ชันแสดงหน้าจอ in-call แบบเต็มหน้าจอ สำหรับ video, DTMF, chat, RTT, timer และ debug panel ตามสิทธิ์หรือการตั้งค่า
17. ฟังก์ชันรองรับ PiP หรือ overlay สำหรับวิดีโอระหว่างใช้งานหน้าจออื่นในแอป
18. ฟังก์ชันจัดเก็บค่าตั้งต้น เช่น settings, draft dialer, entry form บางส่วน และข้อมูล session-only ตามการออกแบบ
19. ฟังก์ชันตั้งค่าวิดีโอด้วย resolution และ bitrate จาก environment หรือ settings โดยไม่ hardcode ใน call logic
20. ฟังก์ชันรองรับการทำงานบน Android และ iOS พร้อม handling เฉพาะแพลตฟอร์มสำหรับ media recovery และ UI behavior

ข้อมูลหลักที่ระบบต้องจัดเก็บหรือแสดงผล

1. ข้อมูล Session ได้แก่ sessionId, state, caller, callee, trunk reference, create/update time และ media status
2. ข้อมูล Trunk ได้แก่ id, public id, name, SIP domain, port, username, enabled status, registration status และ active call count
3. ข้อมูล Gateway Instance ได้แก่ instance id, status, last heartbeat, public WS URL และ readiness
4. ข้อมูล Event Log ได้แก่ event type, sessionId, timestamp, message และ error detail
5. ข้อมูล Payload ได้แก่ signaling payload, SDP offer/answer และ protocol payload สำหรับ troubleshooting
6. ข้อมูล Dialog ได้แก่ SIP dialog id, dialog state และ peer information
7. ข้อมูล Media Stats ได้แก่ RTP/RTCP counters, packet loss, jitter, latency, bitrate และ timestamp
8. ข้อมูล Public Account ได้แก่ SIP public account identifier, user mapping และ active status
9. ข้อมูล WebSocket Client ได้แก่ client id, associated session หรือ trunk, connect time และ status
10. ข้อมูล Entry Form ได้แก่ ชื่อผู้ติดต่อ เบอร์โทร หน่วยงาน โหมดการติดต่อ และปลายทางเริ่มต้น
11. ข้อมูล Mobile Settings ได้แก่ TURN/SIP settings, video resolution, bitrate, debug flags และ dialer draft
12. ข้อมูล RTT State ได้แก่ ข้อความ local/remote, cursor state, transport mode และ timestamp
13. ข้อมูล Resume Metadata ได้แก่ sessionId ล่าสุด, network recovery state และ reconnect timing

หน้าจอหรือมุมมองหลักของระบบปัจจุบัน

1. หน้าเลือกโหมดการติดต่อ normal หรือ emergency
2. หน้า entry form สำหรับกรอกข้อมูลผู้ใช้งาน
3. หน้าจอ in-call แบบเต็มหน้าจอ
4. มุมมอง remote video และ local video
5. มุมมองควบคุม mute, speaker, video และ camera
6. มุมมอง DTMF keypad
7. มุมมอง chat และ in-call messaging
8. มุมมอง RTT composer และ RTT reader
9. มุมมอง PiP overlay
10. มุมมอง permission หรือสถานะ media และ network ที่เกี่ยวข้อง

ระบบ `AzureGrpcTranslationServer` สำหรับบริการแปลภาษา Speech-to-Speech (S2S)

1. ฟังก์ชันรับ streaming audio (PCM 16kHz 16-bit mono) จาก gRPC client ผ่าน `Translate()` bidirectional stream และส่งกลับ translated audio ดังนี้
   1. โหมด S2T ส่งข้อความแปลกลับ (translated_text) แบบ partial (recognizing) และ final (recognized)
   2. โหมด S2S ส่งข้อความแปลกลับ (translated_text) พร้อม audio ที่สังเคราะห์ด้วย TTS (PCM 16kHz 16-bit mono)
   3. โหมด T2S รับข้อความเข้าและสังเคราะห์เสียงพูดจากข้อความที่แปลแล้ว
2. ฟังก์ชันระบุภาษาและเสียงที่ใช้ในการสังเคราะห์ ผ่าน field ใน gRPC request
   1. `source_language` ภาษาต้นทาง เช่น "en-US"
   2. `target_language` ภาษาปลายทาง เช่น "th"
   3. `tts_voice_name` เสียง TTS ที่ใช้สังเคราะห์ เช่น "th-TH-PremwadeeNeural"
3. ฟังก์ชันอ่านค่า Azure credentials จาก `.env` file ได้แก่ `AZURE_SPEECH_KEY`, `AZURE_SPEECH_REGION`, `AZURE_TRANSLATOR_KEY`, `AZURE_TRANSLATOR_REGION`
4. ฟังก์ชันแสดงสถานะ health check ที่ port 5001 (HTTP GET /) สำหรับตรวจสอบ service ว่าทำงานปกติ
5. ฟังก์ชันบันทึก log การทำงานในแต่ละ session พร้อม timestamp ทุกรายการลงไฟล์ `Server-YYYY-MM-DD.log`
6. ฟังก์ชันบันทึก audio input ที่ได้รับจาก client เป็นไฟล์ WAV สำหรับ debug

ระบบ `webrtc-gateway` สำหรับ Backend Gateway (ส่วนเพิ่มการแปลภาษา S2S)

1. ฟังก์ชันเชื่อมต่อกับ AzureGrpcTranslationServer ผ่าน gRPC bidirectional stream สำหรับรับส่ง audio ที่จะแปล
2. ฟังก์ชัน fork audio pipeline จาก WebRTC session โดยเมื่อเปิดใช้ translation จะไม่ส่ง Opus audio ต้นฉบับไปยัง Asterisk แต่จะ decode Opus เป็น PCM แล้วส่งเข้า Azure S2S แทน
3. ฟังก์ชัน Opus decode แปลง RTP payload (Opus) จาก WebRTC track เป็น PCM 16kHz 16-bit mono สำหรับส่งเข้า gRPC stream
4. ฟังก์ชัน Opus encode แปลง PCM audio ที่ได้จาก Azure S2S TTS (16kHz 16-bit mono) เป็น RTP packet (Opus) สำหรับส่งต่อไปยัง Asterisk/Linphone
5. ฟังก์ชัน RTP packetization สำหรับ Opus encoded audio โดยกำหนด SSRC, Sequence Number, Timestamp, Payload Type ให้สอดคล้องกับ SIP session เดิม
6. ฟังก์ชันจัดการ audio buffer และ latency โดยรวบรวม PCM chunk จาก gRPC stream แล้ว flush เป็น Opus RTP packets ไปยัง Asterisk ตามจังหวะที่เหมาะสม
7. ฟังก์ชันกำหนดค่า translation ผ่าน environment variables เพิ่มเติมดังนี้
   1. `TRANSLATOR_ENABLE` เปิด/ปิดการทำงานของ translation pipeline
   2. `TRANSLATOR_ADDR` ที่อยู่ของ AzureGrpcTranslationServer (ip:port)
   3. `TRANSLATOR_SOURCE_LANG` ภาษาต้นทาง เช่น en-US
   4. `TRANSLATOR_TARGET_LANG` ภาษาปลายทาง เช่น th
   5. `TRANSLATOR_TTS_VOICE` ชื่อเสียง TTS ที่ใช้ เช่น th-TH-PremwadeeNeural
   6. `TRANSLATOR_END_SILENCE_TIMEOUT_MS` ระยะเวลาหยุดพูดที่ถือว่าจบ phrase (default 1000ms)
   7. `TRANSLATOR_OPUS_BITRATE` อัตรา bitrate สำหรับ Opus encoder (default 32000)
8. ฟังก์ชันจัดการกรณี AzureGrpcTranslationServer ไม่พร้อมใช้งาน โดยไม่阻断 audio pipeline หรือ revert กลับเป็น Opus passthrough ตามปกติ
9. ฟังก์ชันจัดการ gRPC connection lifecycle (reconnect, timeout, error handling) โดยไม่กระทบ session ที่กำลัง active
10. ฟังก์ชันติดตามสถานะ translation ใน session log และ logstore event สำหรับ audit
11. ฟังก์ชันแปลภาษาข้อความ chat (SIP MESSAGE) ที่พิมพ์โต้ตอบกันระหว่างสาย โดยเมื่อ translation เปิดใช้งาน gateway จะ intercept ข้อความที่รับจากฝั่ง ttrs-vri หรือ SIP endpoint (Linphone) ส่งเข้า Azure T2S เพื่อแปล แล้ว forward ข้อความที่แปลแล้วไปยังปลายทาง
    1. รองรับการแปลข้อความที่รับจาก ttrs-vri (WebSocket `send_message`) → แปล → ส่งเป็น SIP MESSAGE ไปยัง Linphone
    2. รองรับการแปลข้อความที่รับจาก Linphone (SIP MESSAGE) → แปล → ส่งกลับผ่าน WebSocket `message` ไปยัง ttrs-vri
12. ฟังก์ชันแปลภาษา Real-Time Text (RTT) ที่พิมพ์โต้ตอบกันระหว่างสาย โดย gateway จะ intercept RTT text ที่ส่งผ่าน DataChannel หรือ SIP MESSAGE RTT XML แล้วส่งเข้า Azure T2S เพื่อแปล และ forward ข้อความที่แปลแล้วไปยังฝั่งตรงข้ามแบบ real-time
    1. รองรับการแปล RTT ที่พิมพ์จาก ttrs-vri → ส่ง RTT XML (แปลแล้ว) ไปยัง Linphone ผ่าน SIP MESSAGE
    2. รองรับการแปล RTT ที่พิมพ์จาก Linphone ผ่าน SIP MESSAGE → แปล → ส่ง RTT text กลับไปยัง ttrs-vri ผ่าน WebSocket

ระบบ `ttrs-vri-webrtc-react-native` สำหรับ Mobile Softphone (ส่วนเพิ่มการแปลภาษา)

1. ฟังก์ชันเลือกภาษาปลายทางที่ต้องการให้แปล (เช่น ไทย, อังกฤษ, จีน, ญี่ปุ่น, เกาหลี) ก่อนเริ่มสายหรือระหว่างสาย
2. ฟังก์ชัน toggle เปิด/ปิดการแปลภาษาจากหน้าจอ in-call โดยเมื่อเปิดใช้ translation จะมีผลกับสายที่ active อยู่เท่านั้น
3. ฟังก์ชันแสดงสถานะ translation บน in-call UI เช่น "กำลังแปล...", "แปลภาษา: EN→TH" หรือสถานะ error เมื่อ translation ไม่พร้อม
4. ฟังก์ชันส่งคำสั่ง translation ไปยัง gateway ผ่าน WebSocket message type `translate` พร้อมพารามิเตอร์ source language, target language, tts voice name
5. ฟังก์ชันรับผลลัพธ์และสถานะ translation จาก gateway ผ่าน WebSocket message type `translation_state`
6. ฟังก์ชันบันทึกการตั้งค่าภาษา translation ใน local settings เพื่อให้จำค่าที่เลือกไว้ครั้งล่าสุด
7. ฟังก์ชันแสดงข้อความ chat และ RTT ที่ผ่านการแปลแล้วใน in-call UI โดยแยกเป็นข้อความต้นฉบับและข้อความที่แปลแล้ว พร้อมระบุภาษาของแต่ละฝั่ง

API และ Interface ของระบบ `webrtc-gateway` (ส่วนเพิ่มการแปลภาษา)

1. ฟังก์ชันรับคำสั่งจาก client ผ่าน WebSocket สำหรับควบคุมการแปลภาษา
   1. `translate` ส่งคำขอเปิด translation จาก client พร้อมระบุ source_language, target_language, tts_voice_name
   2. `translate_stop` ส่งคำขอยกเลิก translation ระหว่าง session
2. ฟังก์ชันส่งผลลัพธ์และสถานะการแปลภาษากลับไปยัง client ผ่าน WebSocket
   1. `translation_started` แจ้งว่า translation pipeline เริ่มทำงานแล้ว พร้อมข้อมูลภาษาที่ใช้
   2. `translation_stopped` แจ้งว่า translation หยุดทำงานและ audio กลับสู่โหมด passthrough
   3. `translation_error` แจ้งข้อผิดพลาดเกี่ยวกับการแปลภาษา เช่น translation service ไม่พร้อม
   4. `translated_text` ส่งข้อความที่แปลแล้วในรูปแบบ plain text (สำหรับแสดง subtitle หรือ status)
   5. `translated_message` ส่งข้อความ chat ที่ผ่านการแปลแล้วไปยัง client พร้อม original text, translated text, source language และ target language
3. ฟังก์ชัน REST API สำหรับขอสถานะ translation service
   1. `GET /api/translator/status` ตรวจสอบว่า AzureGrpcTranslationServer พร้อมใช้งาน
   2. `GET /api/translator/sessions` ดูรายการ session ที่กำลังใช้ translation อยู่

ข้อมูลหลักที่ระบบต้องจัดเก็บหรือแสดงผล (ส่วนเพิ่มการแปลภาษา)

1. ข้อมูล Translation Session ได้แก่ sessionId, source language, target language, tts voice, status (active/inactive/error)
2. ข้อมูล Translation Event ได้แก่ recognized text, translated text, timestamp และ sessionId
3. ข้อมูล Translation Config ได้แก่ translator address, source language, target language, voice name และ timeout
4. ข้อมูล Translated Message ได้แก่ original text, translated text, source language, target language, timestamp, sessionId และ sender (client/SIP)

ข้อกำหนดเชิงเทคนิคที่ควรระบุใน TOR

1. ระบบรองรับ WebSocket signaling และ REST API สำหรับฝั่ง client
2. ระบบรองรับ SIP signaling, RTP/RTCP และการทำงานร่วมกับ Kamailio หรือ Asterisk สำหรับฝั่งโทรศัพท์
3. ระบบรองรับ Opus passthrough เป็นหลักสำหรับเสียง
4. ระบบรองรับ H.264 เป็นหลักสำหรับวิดีโอ และต้องมีกลไก keyframe recovery
5. ระบบ mobile รองรับการใช้งานกล้องและไมโครโฟนพร้อมกัน และไม่ใช้ audio-only fallback
6. ระบบรองรับ public-entry flow โดยขอ temporary SIP credential ผ่าน API ก่อนเริ่มสาย
7. ระบบรองรับ RTT ผ่าน DataChannel และมี SIP MESSAGE fallback
8. ระบบรองรับ resume และ reconnect เมื่อเครือข่ายมีการเปลี่ยนแปลงหรือขัดข้อง
9. ระบบรองรับ TURN สำหรับ NAT traversal ของฝั่ง WebRTC
10. ระบบรองรับ JWT verification ผ่าน JWKS, issuer และ audience
11. ระบบรองรับหลาย gateway instances และ shared session หรือ trunk visibility
12. ระบบ mobile รองรับ Android และ iOS พร้อม platform-specific media recovery
13. ระบบรองรับการบันทึก event, stats, payload และข้อมูลย้อนหลังลงฐานข้อมูล

ข้อเสนอการนำไปใช้ใน TOR

1. งานพัฒนาระบบ WebRTC-SIP Gateway Backend
2. งานพัฒนา Mobile Softphone สำหรับ public-entry video calling และ RTT
3. งานเชื่อมต่อกับ Public API, SIP core และระบบปลายทาง
4. งานด้าน security, authentication และ session protection
5. งานจัดเก็บ log, event, statistics และข้อมูลย้อนหลัง
6. งานทดสอบ call flow, failover, resume และ mobile media recovery

หมายเหตุ

1. เอกสารนี้จัดทำจากโค้ดและเอกสารใน repository ปัจจุบันของทั้ง `webrtc-gateway` และ `ttrs-vri-webrtc-react-native`
2. หากต้องการให้จัดรูปแบบตามตัวอย่างไฟล์ DOCX แบบเดียวกันทุกหัวข้อ ควรวางไฟล์ DOCX ตัวอย่างไว้ใน workspace เพื่อดึงหัวข้อและรูปแบบต้นฉบับได้โดยตรง
