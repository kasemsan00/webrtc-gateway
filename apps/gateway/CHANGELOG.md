## 2026-11-17

- **สายออกที่ Asterisk ขอ auth (401/407) ส่ง `ringing` ให้ client แล้ว**: เปลี่ยน INVITE ที่แนบ credentials จาก `DoDigestAuth` (ซึ่งทิ้ง 1xx ทั้งหมด) เป็น `TransactionDigestAuth` และใช้ลูปรับ response เดียวกับ INVITE แรก
  - timeout หลัง auth ส่ง CANCEL ให้ INVITE ตัวที่ถูกต้อง (เดิมไม่ส่ง ปลายทางดังต่อ)
  - CSeq เป็น header แบบ typed ทำให้ค่าบนสาย, CANCEL และ ACK ตรงกัน (เดิม INVITE ส่ง 2 แต่ CANCEL ใช้ 3)
  - 181/182/199 เป็น provisional ไม่ใช่สายล้มเหลว; 2xx ทุกตัวถือว่ารับสาย; 487 ที่ตอบ CANCEL ของเราเองไม่มี `endReason`
  - เก็บ INVITE ที่รับสายแล้วไว้ เพื่อ ACK 200 OK ที่ Asterisk ส่งซ้ำ (เดิมล้างทิ้งทันที บน UDP ทำให้โดน BYE หลัง 32 วินาที)
- **BYE, hold re-INVITE, MESSAGE และ INFO ใช้ transport ของ trunk**: เดิม hardcode TCP ทำให้ trunk แบบ UDP วางสายไม่ได้
- **re-INVITE ทุกตัวได้รับคำตอบ**: สถานะ `reconnecting` จัดการเหมือน active แต่ไม่ส่ง `state: active` ให้ client (เดิมไม่ตอบ Asterisk จึง 408 แล้วตัดสาย), สายที่จบแล้วได้ 481, สายออกที่ยังไม่มี dialog ได้ 491 และ re-INVITE ไม่ลงทะเบียน CANCEL handler ของสายเรียกเข้าอีก
- **re-INVITE/UPDATE ที่ย้าย media ของ Asterisk (c=/m= ใหม่) ส่ง RTP ไปที่อยู่ใหม่** ยกเว้น hold แบบ `c=0.0.0.0` หรือ port 0
- **REGISTER ที่ล้มเหลวถูก retry อัตโนมัติ** จาก lease heartbeat แบบ backoff 5 วินาทีถึง 5 นาที (เดิม trunk ที่ register ไม่ได้ตอน boot ค้างจนกว่าจะ refresh ผ่าน admin และ refresh ที่ล้มเหลวรอ 0.8×Expires)
  - REGISTER ที่สำเร็จหลัง unregister/เสีย lease ไม่ถูกนับว่า registered และส่ง Expires:0 ซ้ำเพื่อลบ binding
  - แก้ panic ปิด channel ของ refresh worker ซ้ำเมื่อ `unregisterTrunk` ชนกับ `markLeaseLost`
- **audio SIP→WebRTC ไม่หยุดทั้งสายเมื่อเขียน track ไม่ได้ครั้งเดียว** (เช่นระหว่างสลับ PeerConnection ตอน resume)
- **INVITE ที่ไม่มี trunk manager (ไม่มี DB) ได้ 503** แทนการเงียบ
- **ปฏิเสธ destination/from ที่มี CR/LF หรืออักขระคั่น URI** กัน SIP header injection จาก WebSocket
- แก้ data race ของ `SIPOpusPT` และ `AudioTrack`
- **media (`internal/session`)**
  - PeerConnection ใหม่หลัง resume/`@switch` จัดการ RTCP จาก browser เหมือนตัวแรก (`browser_rtcp.go`): PLI/FIR ใช้ trigger `browser-pli`/`browser-fir` ที่ recovery logic รู้จัก (เดิม `browser-rtcp`), NACK ส่งซ้ำจาก cache (เดิมทิ้ง) และอ่าน RTCP ของ receiver ด้วย
  - PeerConnection ใหม่เข้า grace `reconnecting` 30 วินาทีเมื่อ ICE disconnected เหมือนตัวแรก
  - timer ของ grace ผูกกับรอบ disconnect ของมันเอง (เดิม disconnect→reconnect→disconnect ภายใน 30 วินาทีทำให้สายถูกตัดหลังรอบที่สองเพียงวินาทีเดียว)
  - ไม่ forward NACK ฝั่ง SIP ไป browser แล้ว: อ้าง SSRC/seq ฝั่ง uplink ที่ browser ไม่รู้จัก และอ่าน `PeerConnection` แบบไม่ lock จน panic ได้
  - ไม่ส่ง NACK ของ browser ต่อไป Asterisk เมื่อเปิด AU normalizer (seq ถูก renumber จึงขอ packet ผิดตัว)
  - reorder buffer ยอมรับเลข sequence ใหม่หลังได้ packet "เก่า" เรียงกัน 16 ตัว (SSRC เดิมแต่ seq ถอยหลัง เช่น handoff คิว→agent) เดิมจอดำจนกว่า seq จะไล่ทัน
  - AU ที่มาหลัง seq gap และไม่เริ่มที่ macroblock 0 (slice แรกหาย) ไม่ถูกส่งเป็นภาพสมบูรณ์ จึงไม่ถูก cache/replay เป็น IDR ที่ใช้ได้
  - อ่าน/สลับ `VideoTrack` ภายใต้ lock เดียวกัน และ resume ถอด track เดิมก่อนปิด PeerConnection เดิมแบบ async (กัน pion deadlock ระหว่าง write กับ Close)
  - การผ่อนเกณฑ์ `SIP_SWITCH_VIDEO_RTP_MIN_PACKET_DELTA` หลัง gate ใช้เฉพาะรอบ switch นั้น (เดิมลดค่าถาวรทั้งสาย)
  - PLI/FIR ฝั่ง SIP ที่รอ marker (mid-frame) ส่งออกได้เมื่อ uplink หยุดเกิน 200ms (เดิมรอตลอดไปถ้า uplink หยุดที่ packet ไม่มี marker)
- **media รอบรีวิวที่สอง**
  - สายที่จบเพราะ ICE (failed หรือหมด grace 30 วินาที) ส่ง BYE ไป Asterisk และแจ้ง client ว่า ended (เดิมขา Asterisk ค้างจน RTP timeout และ client ไม่รู้)
  - SIP video ที่ seq ถอยหลังพร้อม timestamp คนละ timeline ถือเป็นการเริ่ม stream ใหม่ (`sip_video_seq_restart`) แล้วเข้า path stream-switch เดิม (reset reorder/normalizer, เปิด gate, PLI)
  - reorder buffer rebase เฉพาะเมื่อ packet ที่เรียงกันอยู่ห่างจาก nextSeq ≥1024 และปล่อยทั้งชุด 16 ตัว ไม่ทิ้ง; packet สายที่มาช้าใกล้ ๆ ยังถูกทิ้งเป็น old
  - reorder buffer ส่ง batch ตามลำดับเสมอ: batch จาก timeout จะไม่ถูก packet ถัดไปแซง (เดิม normalizer ทิ้ง AU ที่ลำดับสลับ ซึ่งมักเป็น IDR ที่เพิ่งขอ) และ IDR replay เขียนเข้าคิวเดียวกัน
  - RTCP ที่ mux มาบน port audio ไม่ถูกส่งเข้า Opus track อีก (นับเป็น SR/RR)
  - media ย้าย IP กลางสายล้างที่อยู่ RTCP ของ video ที่เรียนรู้ไว้และเปิด trust window ใหม่ PLI/FIR จึงไปที่ปลายทางใหม่
  - resume ระหว่าง `@switch` ที่ยังไม่เสร็จ หรือ `@switch` ซ้อน ปิด PeerConnection ที่ park ไว้ (เดิม leak) และถอด track ของมันก่อนปิด
  - abort `@switch`, commit, resume และ hangup ถอด track audio/video ภายใต้ egress lock ก่อนปิด PeerConnection (กัน pion Write/Close deadlock) และ timer หลัง commit ไม่ล้าง audio track ของ `@switch` รอบใหม่
- **ลบ dead code**: package `internal/webrtc` เหลือเพียง `GetUnicastAddress`, RTP listener แบบ global track เดิม, ICE credential generator, helper และ field ที่ไม่มี production code เรียก/อ่าน, ตัวนับ `incomingCounters` ที่เขียนอย่างเดียว และ config `SIP_VIDEO_PRESERVE_STAPA` ที่ไม่มีผล (ลบจาก `.env.example` และ config reference)
- **โอนสาย (call transfer)**: ข้อความ WS `transfer` ส่ง REFER ใน dialog ไปที่ Asterisk แบบ blind (`destination`) หรือ attended (`targetSessionId` ใส่ `Replaces` ของอีกสาย) ทั้งสองสายต้องเป็นของ client เอง ความคืบหน้าจาก NOTIFY (`message/sipfrag`) ส่งให้ client เป็น `transfer_progress` (`accepted`/`trying`/`completed`/`failed` พร้อม SIP status) gateway รับ NOTIFY แล้ว (Allow มี `NOTIFY`, dialog ที่ไม่รู้จักตอบ 481) REFER ขาเข้ายังตอบ 501
- **สายเรียกซ้อนบน `/ws`**: socket ที่ไม่ใช่ public ส่ง `client_state` `multiCall: true` ได้ สายเข้าระหว่างคุยจะถูกส่งให้ client แทนการตอบ busy (เดิมเฉพาะ agent presence) สถานะ `unavailable` ยังปฏิเสธเหมือนเดิม
- **agent-device ต่อสายต่อหลังเน็ตหลุด**: socket ของ agent-device ปิดแล้ว สายยังอยู่ 30 วินาทีให้ device ที่ register ใหม่ `resume` (ต้องเป็นเจ้าของสาย หรืออยู่ trunk เดียวกัน) เกินเวลาแล้วจึงจบสาย เดิมจบสายทันที การ `resume` ย้ายความเป็นเจ้าของสายไปที่ socket ใหม่ผ่าน `bindClientSession`
- **วางสายที่สองไม่ตัดสายแรก**: การจับคู่สาย WebRTC สองขาที่ Asterisk เชื่อมกัน (ขาผู้โทรกับขา agent ที่รับจากคิว) ใช้แค่เบอร์ผู้โทร ผู้โทรที่มีสองสาย (สายที่สอง/สายเรียกซ้อน) วางสายหนึ่งแล้ว gateway วางขา agent ของอีกสายไปด้วย ตอนนี้จับคู่เฉพาะเมื่อไม่กำกวมทั้งสองทาง หรือเมื่อ agent ของ `@switch` ระบุตรงกัน ใช้ทั้งการวางสายตาม การส่งต่อแชท และการขอ keyframe
- **สายที่พักไม่หลุดเองหลัง 6 วินาที**: ACK ของ 200 OK ที่ตอบ re-INVITE กลางสาย (พักสาย/คืนสาย) ไม่มี Route header เพราะ 200 ของ re-INVITE ไม่มี Record-Route proxy จึงทิ้ง ACK แล้ว Asterisk ส่ง 200 ซ้ำจนหมดเวลาและวางสาย ตอนนี้ ACK ของคำขอในไดอะล็อกใช้ route set ของไดอะล็อก และการ ACK 200 ที่ส่งซ้ำของ re-INVITE สร้างจากไดอะล็อกแทน INVITE แรก (CSeq ไม่ตรง)

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
