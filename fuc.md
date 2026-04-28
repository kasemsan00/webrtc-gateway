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

รายการ API และ Interface สำคัญของระบบ

1. WebSocket messages ฝั่ง client
   1. `offer`
   2. `call`
   3. `accept`
   4. `reject`
   5. `hangup`
   6. `dtmf`
   7. `send_message`
   8. `resume`
   9. `trunk_resolve`
   10. `ping`
2. WebSocket messages ฝั่ง server
   1. `answer`
   2. `state`
   3. `incoming`
   4. `message`
   5. `messageSent`
   6. `dtmf`
   7. `resumed`
   8. `resume_failed`
   9. `resume_redirect`
   10. `trunk_resolved`
   11. `trunk_redirect`
   12. `trunk_not_found`
   13. `trunk_not_ready`
   14. `pong`
   15. `error`
3. REST API สำคัญ
   1. `POST {EXPO_PUBLIC_API_URL}/extension/public`
   2. `POST /api/offer`
   3. `POST /api/call`
   4. `POST /api/hangup/{sessionId}`
   5. `GET /api/sessions`
   6. `GET /api/sessions/stream`
   7. `GET /api/session/{sessionId}`
   8. `POST /api/dtmf/{sessionId}`
   9. `GET /api/sessions/history`
   10. `GET /api/sessions/{sessionId}/events`
   11. `GET /api/sessions/{sessionId}/payloads`
   12. `GET /api/sessions/{sessionId}/dialogs`
   13. `GET /api/sessions/{sessionId}/stats`
   14. `GET /api/gateway/instances`
   15. `GET /api/session-directory`
   16. `GET /api/public-accounts`
   17. `GET /api/ws-clients`
   18. `GET /api/dashboard`
   19. `GET /api/trunks`
   20. `GET /api/trunk/{id}`
   21. `POST /api/trunks`
   22. `PUT /api/trunk/{id}`
   23. `POST /api/trunk/{id}/register`
   24. `POST /api/trunk/{id}/unregister`
   25. `POST /api/trunks/refresh`

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
