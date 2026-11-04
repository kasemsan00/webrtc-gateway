## 2026-10-29

- **ถอด prepare-incoming ออกทั้งหมด**: ยกเลิกการสร้าง PeerConnection ล่วงหน้าระหว่างสายเรียกเข้ากำลังดัง เนื่องจากทำให้วิดีโอ Android ไม่ทำงานเมื่อเปิดใช้ กลับไปใช้ขั้นตอน offer/accept ปกติ โดยคงการแก้ H.264 SPS/PPS ให้รอ cache ครบก่อนส่ง IDR และส่ง SPS/PPS ล่าสุดนำหน้า IDR ทุก AU ทั้ง mode 0 และ 1 โดยไม่ส่งซ้ำ รวมถึง telemetry และ regression tests ที่เกี่ยวข้อง
- **คงการแก้ race ของสายเรียกเข้า**: cancel/timeout/reject ตรวจสถานะ incoming ภายใต้ lock เดียวกัน เพื่อไม่ยกเลิกสายที่รับแล้ว
- **แก้ IDR ฝั่ง browser→SIP ขาด SPS/PPS หลังรับสาย**: รอ parameter sets ให้ครบก่อนเปิด startup/recovery gate และทิ้งทั้ง AU ที่เริ่มด้วย IDR ก่อน SPS/PPS พร้อมขอ keyframe ใหม่ (จำกัดหนึ่งครั้งต่อวินาที) เพื่อไม่ส่งเฉพาะท้ายภาพไปยัง OpenH264
  - ส่ง SPS/PPS ล่าสุดเป็น single-NAL นำหน้า IDR ทุก AU ทั้ง packetization-mode 0 และ mode 1/STAP-A โดยใช้ timestamp เดียวกับ IDR, sequence ต่อเนื่อง และ marker เฉพาะท้ายภาพ ไม่ส่งชุดซ้ำเมื่อ encoder ส่งครบแล้วใน AU นั้น และรองรับ timestamp ซ้ำหลัง marker
  - เพิ่ม `uplink_idr_wait_parameter_sets` และ `uplink_idr_parameter_sets` ซึ่งตรวจการเขียน UDP สำเร็จ พร้อมจำนวน IDR/ชุดที่ขาดสะสมและ sequence ของ SPS/PPS/IDR โดยจำกัด log หนึ่งครั้งต่อวินาทีตลอดสาย
  - แก้ `h264_au_normalized` ฝั่ง SIP→browser: `response_time` เป็นศูนย์สำหรับ IDR ที่ไม่ได้ตอบคำขอ SIP ภายในสองวินาที นับหนึ่ง response ต่อคำขอ และไม่จับคู่กับ FIR ที่ส่งไป browser; `injected_parameter_sets` ยังหมายถึงการเติมชุดข้อมูลฝั่ง SIP→browser

## 2026-02-11

- Increased default SIP video watchdog intervals (5s PLI / 10s FIR / 3s check) to prevent FIR/PLI flooding when Linphone only emits on-demand keyframes.
- Removed redundant forced PLI after watchdog FIR, reducing RTCP traffic while letting FIR sequence numbers drive recovery.
- **Fixed 7-minute video freeze**: Removed SIP→WebRTC SPS/PPS injection that used `SequenceNumber=0` — pion does not rewrite seq numbers, so the browser received Seq=0 packets amid a stream at seq ~15000+, corrupting the jitter buffer. SPS/PPS now flows naturally from Linphone as regular RTP packets with correct sequence numbers.
