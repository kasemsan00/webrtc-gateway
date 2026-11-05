## 2026-11-05

- **แก้ Android→9999 (echo) จอดำ ~60 วินาที**: ปลายทางแบบ echo ส่งกลับเฉพาะภาพที่ gateway ส่งไป และ Asterisk `Echo()` ตอบ picture_fast_update เพียงครั้งเดียวต่อสาย
  - ไม่ส่ง FU-A IDR ที่ไม่มี fragment เริ่มต้น (ไม่มี SPS/PPS และ decode ไม่ได้) ไป SIP อีก และไม่นับเป็น uplink IDR ที่ส่งแล้ว (เดิมทำให้ `uplink_idr_parameter_sets ... missing_parameter_sets=1` และหยุดการขอ keyframe) ทิ้ง AU นั้นแล้วขอ PLI ใหม่ไม่เกินหนึ่งครั้งต่อวินาที (`uplink_idr_fragment_dropped reason=no-start`)
  - IDR ที่มาถึงก่อนรู้ที่อยู่ SIP video ไม่เปิด uplink gate และไม่ใช้สิทธิ์ส่ง SPS/PPS ของ AU นั้น
  - kick หลัง `remote-ssrc-learn`/`sip-rtcp-first` ที่ไม่นับ IDR ก่อน learn ส่ง PLI ติดตามได้สูงสุดสามครั้งห่างกัน 1 วินาที จนกว่าจะมี IDR หลัง kick
  - เมื่อ client ขอ keyframe และยังไม่เคยได้ keyframe ขากลับในสายนี้ ขอ IDR จาก encoder ของ client โดยตรง (`uplink_keyframe_kick reason=return-recovery`) ทุก 2 วินาทีสูงสุด 10 ครั้งต่อสาย หยุดเมื่อได้ keyframe ขากลับ
  - SIP INFO picture_fast_update ใช้ floor 1 วินาทีระหว่างที่ INFO ก่อนหน้ายังไม่ได้รับ keyframe ตอบ และกลับเป็น 2 วินาทีเมื่อได้รับแล้ว (`sip_keyframe_via_info ... recovering=`)
- **แก้ 9999 (echo) ขากลับยังจอดำแม้ uplink IDR ครบ**: Asterisk ส่ง IDR ขากลับมาครั้งเดียวแต่ client decode ไม่ได้ และ gateway หยุดพยายามเพราะถือว่า "มี keyframe แล้ว" (keyframeAge 50 วินาทีขึ้นไป)
  - นับว่าขากลับค้าง (return recovery) เมื่อ client ยังขอ keyframe (PLI/FIR หรือ `request_keyframe` ภายใน 2 วินาที) และไม่มี keyframe ขากลับใหม่มาเกิน 1.5 วินาทีนับจากเริ่มขอ ส่วน keyframe ที่มาแล้วแต่ client ยังขอต่อถือว่ายังไม่ได้รับ
  - kick `return-recovery` ขอ IDR จาก encoder ของ client ต่อได้ เมื่อ client ยังขอต่อ 2 วินาทีหลัง keyframe ขากลับล่าสุด (เดิมหยุดตั้งแต่ keyframe แรก) จำกัด 10 ครั้งต่อรอบ เริ่มรอบใหม่เมื่อ client เงียบ 5 วินาที
  - โหมด `SIP_KEYFRAME_REQUEST_MODE=info` ส่ง RTCP PLI/FIR ไป SIP เพิ่มจาก INFO ระหว่าง return recovery (`sip_rtcp_fb_escalated reason=return-recovery`) โดยยังผ่าน gate เดิมทั้งหมด (IDR in-flight, guard, min interval, รอ marker)
  - INFO recovery floor ลดเป็น 500ms และใช้ระหว่าง return recovery ด้วย
  - keyframe watchdog ไม่ข้ามด้วยเหตุผล `rtp-flowing` เมื่อยังไม่เคยได้ keyframe ขากลับหรือ client ยังรอ keyframe (`keyframe_watchdog_client_waiting`)
- **ลดเวลาจอดำตอนเริ่มสาย 9999 (echo) จาก ~4 วินาที**: IDR แรกจากโทรศัพท์มาถึงพร้อมกับตอนเรียนรู้ SSRC ขากลับ (มิลลิวินาทีเดียวกัน) Echo จึงไม่สะท้อนกลับ และ kick ตัวแรกที่ได้ผลรอถึง learn+2.2 วินาที (KVt9sOuRsPuy)
  - เมื่อเรียนรู้ remote video SSRC ครั้งแรกของสาย และสายรับแล้ว (หลัง 200 OK) ขอ IDR จากโทรศัพท์ (forced FIR + PLI ข้าม cap 1 วินาทีของ forced PLI) ที่ learn+500ms แล้วทุก ~1 วินาที จนกว่าจะได้ IDR ขากลับที่สมบูรณ์ (`uplink_keyframe_kick reason=return-recovery trigger=ssrc-learn`, `return_path_kick_schedule_stop`)
  - ก่อนได้ keyframe ขากลับแรกของสาย kick เว้นระยะ 1 วินาที และรอเพียง 500ms หลัง uplink IDR (เดิม 2 วินาทีทั้งคู่) ทั้งจากตาราง learn และจาก PLI ของ client ใช้ cap 10 ครั้งร่วมกัน ไม่ kick ระหว่าง uplink IDR in-flight หลังได้ keyframe ขากลับแล้วพฤติกรรมเหมือนเดิม
  - keyframe ของสายก่อน (LastKeyframe ไม่ถูก reset) ไม่นับเป็น keyframe ขากลับของสายใหม่

## 2026-10-29

- **ถอด prepare-incoming ออกทั้งหมด**: ยกเลิกการสร้าง PeerConnection ล่วงหน้าระหว่างสายเรียกเข้ากำลังดัง เนื่องจากทำให้วิดีโอ Android ไม่ทำงานเมื่อเปิดใช้ กลับไปใช้ขั้นตอน offer/accept ปกติ โดยคงการแก้ H.264 SPS/PPS ให้รอ cache ครบก่อนส่ง IDR และส่ง SPS/PPS ล่าสุดนำหน้า IDR ทุก AU ทั้ง mode 0 และ 1 โดยไม่ส่งซ้ำ รวมถึง telemetry และ regression tests ที่เกี่ยวข้อง
- **คงการแก้ race ของสายเรียกเข้า**: cancel/timeout/reject ตรวจสถานะ incoming ภายใต้ lock เดียวกัน เพื่อไม่ยกเลิกสายที่รับแล้ว
- **แก้ IDR ฝั่ง browser→SIP ขาด SPS/PPS หลังรับสาย**: รอ parameter sets ให้ครบก่อนเปิด startup/recovery gate และทิ้งทั้ง AU ที่เริ่มด้วย IDR ก่อน SPS/PPS พร้อมขอ keyframe ใหม่ (จำกัดหนึ่งครั้งต่อวินาที) เพื่อไม่ส่งเฉพาะท้ายภาพไปยัง OpenH264
  - ส่ง SPS/PPS ล่าสุดเป็น single-NAL นำหน้า IDR ทุก AU ทั้ง packetization-mode 0 และ mode 1/STAP-A โดยใช้ timestamp เดียวกับ IDR, sequence ต่อเนื่อง และ marker เฉพาะท้ายภาพ ไม่ส่งชุดซ้ำเมื่อ encoder ส่งครบแล้วใน AU นั้น และรองรับ timestamp ซ้ำหลัง marker
  - เพิ่ม `uplink_idr_wait_parameter_sets` และ `uplink_idr_parameter_sets` ซึ่งตรวจการเขียน UDP สำเร็จ พร้อมจำนวน IDR/ชุดที่ขาดสะสมและ sequence ของ SPS/PPS/IDR โดยจำกัด log หนึ่งครั้งต่อวินาทีตลอดสาย
  - แก้ `h264_au_normalized` ฝั่ง SIP→browser: `response_time` เป็นศูนย์สำหรับ IDR ที่ไม่ได้ตอบคำขอ SIP ภายในสองวินาที นับหนึ่ง response ต่อคำขอ และไม่จับคู่กับ FIR ที่ส่งไป browser; `injected_parameter_sets` ยังหมายถึงการเติมชุดข้อมูลฝั่ง SIP→browser

## 2026-02-11

> **Note (superseded defaults/behavior):** The `5s` PLI / `10s` FIR / `3s` check watchdog intervals and the removal of SIP→WebRTC SPS/PPS injection recorded below were later superseded by the current defaults in `internal/config/config.go` (`2s` check / `4s` PLI / `8s` FIR via `SIP_VIDEO_KEYFRAME_WATCHDOG_INTERVAL_MS`, `SIP_VIDEO_KEYFRAME_STALE_MS`, `SIP_VIDEO_KEYFRAME_FIR_STALE_MS`) and by the H.264 Access Unit normalizer (`SIP_VIDEO_AU_NORMALIZE_ENABLE=true`), which injects cached SPS/PPS with rewritten continuous RTP sequence numbers.

- Increased default SIP video watchdog intervals (5s PLI / 10s FIR / 3s check) to prevent FIR/PLI flooding when Linphone only emits on-demand keyframes.
- Removed redundant forced PLI after watchdog FIR, reducing RTCP traffic while letting FIR sequence numbers drive recovery.
- **Fixed 7-minute video freeze**: Removed SIP→WebRTC SPS/PPS injection that used `SequenceNumber=0` — pion does not rewrite seq numbers, so the browser received Seq=0 packets amid a stream at seq ~15000+, corrupting the jitter buffer. SPS/PPS now flows naturally from Linphone as regular RTP packets with correct sequence numbers.
