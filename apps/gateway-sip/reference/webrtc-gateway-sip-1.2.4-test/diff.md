สรุปการเทียบกับ ref แล้วมีจุดต่างที่ “สัมพันธ์กับอาการภาพ remote ค้าง + burst สูง” ชัดเจนอยู่หลายจุด

จุดต่างสำคัญเมื่อเทียบกับ ref

ปิดการส่ง FIR/PLI ตอน “เรียนรู้ SSRC”
ใน rtp.go

ref: ตอนเรียนรู้ SSRC จะยิง FIR แล้วตามด้วย PLI burst หลายครั้งเพื่อขอ keyframe เร็ว
current: ปิด logic นี้ไว้ (“DISABLED: SSRC-learn FIR/PLI burst”)
ปิด periodic PLI ไปที่ WebRTC
ใน rtp.go

ref: เริ่ม periodic PLI sender (ช่วยให้ฝั่ง WebRTC ส่ง keyframe ไป SIP)
current: ปิดไว้ (“Periodic PLI sender DISABLED”)
หมายเหตุ: จุดนี้ช่วย “WebRTC → SIP” มากกว่า “SIP → WebRTC” แต่มีผลกับความนิ่งของฝั่งสลับทาง
เพิ่ม throttle PLI/FIR (และ burst window) ใน current
ใน keyframe.go และ session.go

current: มี pliMinInterval, StartPLIBurst, shouldThrottlePLI() และ “Skipping PLI to Asterisk - throttled”
ref: ไม่มี throttle/burst ใช้ส่งตรงไป Asterisk
ผลคือเวลาฝั่ง WebRTC ขอ PLI/NACK จะถูก “หน่วง/ตัดทิ้ง” ได้ ซึ่งทำให้ recovery ช้าลงและเกิดค้างถ้า keyframe หาย
ปิด ICE-connected FIR/PLI burst
ใน session.go

ref: ICE connected จะยิง FIR + PLI เพื่อขอ keyframe
current: ถูกคอมเมนต์ไว้
@switch ปิด FIR loop
ใน handlers.go

ref: ยิง FIR 10 ครั้งก่อน แล้วค่อย PLI
current: FIR ถูกคอมเมนต์ เหลือแต่ PLI
ถ้า @switch เกี่ยวข้องกับจังหวะค้าง ภาพจะกู้ช้ากว่าเดิม
สิ่งที่ log บอก (สัมพันธ์กับข้อต่างข้างบน)

SDP Answer ของ Asterisk ไม่มี a=rtcp-fb → Asterisk อาจไม่รองรับ NACK/PLI/FIR เต็มรูปแบบ
เมื่อ WebRTC ส่ง NACK/PLI เยอะ แต่ Asterisk ไม่ตอบสนอง → WebRTC จะยิ่งร้องขอซ้ำ → burst สูง → ภาพค้าง/กระตุก
log ของคุณมี “⚡ PLI burst enabled …” แต่ในโค้ดปัจจุบัน StartPLIBurst() ไม่มีการถูกเรียกเลย (มีแค่ฟังก์ชัน)
ถ้ายังเห็น log นี้ แปลว่า กำลังรัน binary เก่าหรือ build คนละชุด ไม่ตรงกับ source ปัจจุบัน
ข้อสรุปเชิงสาเหตุ (จากการเทียบ ref)

การปิด FIR/PLI burst + เพิ่ม throttle ทำให้ recovery ของวิดีโอฝั่ง Linphone → WebRTC ช้าลง
เมื่อมี loss เล็กน้อย WebRTC ส่ง NACK/PLI ถี่ แต่ Asterisk ไม่ตอบ → burst พุ่งและภาพค้าง
ref ไม่มี throttle และมี burst ตอน SSRC-learn/ICE-connected ทำให้ keyframe มาเร็วกว่า จึงไม่ค้างแบบปัจจุบัน
แนะนำตรวจสอบทันที (ก่อนแก้โค้ดเพิ่ม)

ยืนยันว่ากำลังรัน binary ล่าสุดจริง
ถ้ายังเห็น “⚡ PLI burst enabled …” ให้รีบuild และ restart เพราะใน source ปัจจุบันไม่มี call แล้ว
ถ้าต้องพิสูจน์ให้ชัด ให้ลองเปิดพฤติกรรมตาม ref ทีละจุด
เปิด FIR+PLI ตอน SSRC-learn
เปิด ICE-connected FIR/PLI burst
เอา throttle ออกชั่วคราว แล้วดูว่าอาการค้างหายหรือไม่
ถ้าต้องการ ผมช่วยทำ patch ให้ “กลับไปเหมือน ref เฉพาะส่วนที่กระทบ remote” หรือทำแบบ progressive (เปิดเฉพาะ FIR/PLI burst แล้วค่อยวัดผล) ได้เลย

ต้องการให้ผมลงมือแก้โค้ดให้เหมือน ref ที่จุดไหนก่อน?

เปิด FIR/PLI ตอน SSRC-learn + ICE connected
เอา throttle PLI/FIR ออกทั้งหมด
เปิด FIR ใน @switch
ทำทั้งหมดพร้อมกัน
