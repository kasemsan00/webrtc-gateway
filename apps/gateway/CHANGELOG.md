## 2026-10-29

- **แก้ IDR ฝั่ง browser→SIP ขาด SPS/PPS หลังรับสาย โดยเฉพาะ prepared PC**: รอ parameter sets ให้ครบก่อนเปิด startup/recovery gate และทิ้งทั้ง AU ที่เริ่มด้วย IDR ก่อน SPS/PPS พร้อมขอ keyframe ใหม่ (จำกัดหนึ่งครั้งต่อวินาที) เพื่อไม่ส่งเฉพาะท้ายภาพไปยัง OpenH264
  - ส่ง SPS/PPS ล่าสุดเป็น single-NAL นำหน้า IDR ทุก AU ทั้ง packetization-mode 0 และ mode 1/STAP-A โดยใช้ timestamp เดียวกับ IDR, sequence ต่อเนื่อง และ marker เฉพาะท้ายภาพ ไม่ส่งชุดซ้ำเมื่อ encoder ส่งครบแล้วใน AU นั้น และรองรับ timestamp ซ้ำหลัง marker
  - prepared answer อ่าน packetization mode จาก pending INVITE SDP ด้วย parser เดียวกับ SIP answer จึงไม่เลือกผิดจากค่า session ที่ยังไม่อัปเดตก่อน Accept
  - เพิ่ม `uplink_idr_wait_parameter_sets` และ `uplink_idr_parameter_sets` ซึ่งตรวจการเขียน UDP สำเร็จ พร้อมจำนวน IDR/ชุดที่ขาดสะสมและ sequence ของ SPS/PPS/IDR โดยจำกัด log หนึ่งครั้งต่อวินาทีตลอดสาย
  - แก้ `h264_au_normalized` ฝั่ง SIP→browser: `response_time` เป็นศูนย์สำหรับ IDR ที่ไม่ได้ตอบคำขอ SIP ภายในสองวินาที นับหนึ่ง response ต่อคำขอ และไม่จับคู่กับ FIR ที่ส่งไป browser; `injected_parameter_sets` ยังหมายถึงการเติมชุดข้อมูลฝั่ง SIP→browser
- **เตรียม WebRTC ล่วงหน้าระหว่างสายเรียกเข้ากำลังดัง (prepared offer)**: agent ส่ง `offer` ที่มี `prepare: true` และ `sessionId` เป็นของสายเรียกเข้า เพื่อเชื่อม ICE + DTLS ไว้ก่อนรับสาย ภาพและเสียงจึงเริ่มแทบทันทีเมื่อกด `accept`
  - gateway ประกาศความสามารถนี้ด้วย `prepareOffer: true` ในทุกข้อความ `trunk_resolved` และตอบ `answer` ที่มี `prepare: true` (ใช้ `sessionId` เดิม)
  - การ prepare **ไม่** claim สาย ไม่ผูก session กับ connection และไม่ส่ง 200 OK ไปฝั่ง SIP ก่อนรับสายจะไม่มีสื่อจาก SIP ไปถึง PeerConnection ที่เตรียมไว้ และสื่อจาก agent จะถูกทิ้งไม่ส่งไปฝั่ง SIP หลาย agent เตรียม PeerConnection ของตัวเองไว้พร้อมกันกับสายเดียวได้
  - `accept` ยังเป็นข้อความเดิม gateway เป็นผู้ตัดสินผู้ชนะเพียงผู้เดียวด้วยการ claim แบบ atomic ภายใต้ session lock (สายต้องยังดังอยู่และยังไม่มีคนอื่น claim) ผู้ชนะได้ PeerConnection ที่เตรียมไว้เป็น PeerConnection ของสาย gateway ส่ง 200 OK ตอบ `state: active` ด้วย `sessionId` เดิม และขอ keyframe ทั้งสองฝั่งทันที ผู้ที่กด accept ทีหลังได้ `cancel` เหตุผล `answered_elsewhere` ตามเดิม
  - เมื่อสายออกจากสถานะ ringing ไม่ว่าด้วยเหตุใด (มีคนรับ, ผู้โทรยกเลิก, หมดเวลาดัง, ถูกปฏิเสธ, SIP ผิดพลาด) gateway ปิด PeerConnection ที่เตรียมไว้แต่ไม่ได้ชนะเองทั้งหมด และส่ง `cancel` ให้ connection นั้นหนึ่งครั้ง (`answered_elsewhere`, `caller_cancelled`, `no_answer` หรือ `ended`)
  - WebSocket ที่หลุดการเชื่อมต่อจะถูกปิด PeerConnection ที่เตรียมไว้โดยไม่กระทบสายที่กำลังดัง และ PeerConnection ที่ไม่มีใครรับจะถูกปิดเมื่อครบ `WEBRTC_PREPARE_TTL` (ค่าเริ่มต้น `60s`) หากกด `accept` หลังจากนั้นจะได้ `error` ให้ส่ง offer `prepare=true` ใหม่
  - `offer` ที่ไม่มี `prepare` ทำงานเหมือนเดิมทุกประการ (claim สายตั้งแต่ตอน offer) เพื่อรองรับ SDK รุ่นเก่า
- **แก้ race ระหว่างผู้โทรยกเลิก/หมดเวลาดัง/reject กับการรับสาย**: เส้นทาง SIP CANCEL, ring timeout และ `reject` ใช้ `TryBeginIncomingTerminalAction` ซึ่งตรวจสถานะ `incoming` ภายใต้ lock เดียวกัน สายที่รับแล้วจึงไม่ถูกยกเลิกย้อนหลังอีก (สถานะเปลี่ยนได้ทางเดียว)
- เพิ่มตัวแปร `WEBRTC_PREPARE_TTL` (Go duration หรือจำนวนวินาที) ใน config reference และ `.env.example`

## 2026-02-11

- Increased default SIP video watchdog intervals (5s PLI / 10s FIR / 3s check) to prevent FIR/PLI flooding when Linphone only emits on-demand keyframes.
- Removed redundant forced PLI after watchdog FIR, reducing RTCP traffic while letting FIR sequence numbers drive recovery.
- **Fixed 7-minute video freeze**: Removed SIP→WebRTC SPS/PPS injection that used `SequenceNumber=0` — pion does not rewrite seq numbers, so the browser received Seq=0 packets amid a stream at seq ~15000+, corrupting the jitter buffer. SPS/PPS now flows naturally from Linphone as regular RTP packets with correct sequence numbers.
