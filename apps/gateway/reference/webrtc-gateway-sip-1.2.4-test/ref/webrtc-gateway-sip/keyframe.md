📊 สรุป: K2 Gateway รองรับ RTCP Feedback อะไรบ้าง
หลังจากวิเคราะห์โค้ดทั้งหมด สรุปได้ดังนี้:
✅ รองรับแล้ว:

1. PLI / FIR (Picture Loss Indication & Full Intra Request)

- Status: ✅ FULLY IMPLEMENTED
- Implementation Details:
  - internal/session/manager.go line 230-236, 392-422, 1114
  - internal/sip/rtp.go line 407-413
  - Bidirectional forwarding: Browser ↔ K2 Gateway ↔ SIP endpoint
  - Logs: 📸 Received PLI, 📸 Received FIR
  - Functions: SendPLI(), ForwardPLIToWebRTC()

2. NACK (Negative Acknowledgment)

- Status: ⚠️ PARTIALLY SUPPORTED (Pion Default)
- Implementation Details:
  - ใช้ webrtc.NewPeerConnection() (line 134) ซึ่ง Pion เปิด NACK โดย default
  - ใน temp_pion/webrtc/mediaengine.go line 90: {"nack", ""}, {"nack", "pli"}
  - BUT: ไม่มี explicit NACK handling ใน custom code
  - No forwarding ของ NACK packets ไปยัง SIP endpoint
  - No custom NACK logic - เชื่อมกับ Pion's default interceptor

3. REMB / TWCC (Receiver Estimated Maximum Bitrate / Transport Wide Congestion Control)

- Status: ⚠️ PARTIALLY SUPPORTED (Pion Default ONLY)
- Implementation Details:
  - Pion WebRTC เปิด TWCC โดย default (ดู temp_pion/webrtc/interceptor.go line 19)
  - ใน temp_pion/webrtc/mediaengine.go line 90: {"goog-remb", ""}
  - BUT: K2 Gateway ไม่ทำ bandwidth adaptation จาก REMB/TWCC
  - No forwarding ของ REMB/TWCC ไปยัง SIP endpoint
  - No bitrate control based on congestion feedback
  - No custom implementation - เชื่อมกับ Pion's default interceptor

---

❌ ไม่รองรับ:

1. NACK Forwarding to SIP

- K2 Gateway ไม่มี code สำหรับ forward NACK packets ไปยัง SIP endpoint
- เฉพาะ PLI/FIR เท่านั้นที่ถูก forward

2. REMB/TWCC Forwarding to SIP

- ไม่มี code สำหรับ forward bandwidth feedback ไปยัง SIP endpoint
- ไม่มี bitrate adaptation สำหรับ SIP side

3. Custom NACK/RTX Implementation

- ไม่มี NACK buffer หรือ retransmission logic
- ไม่มี RTX (Retransmission) stream support

---

🔍 RTCP Handling in K2 Gateway:
→ Read internal\session\manager.go [limit=15, offset=1110]
// internal/session/manager.go - Line 1114
case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
s.SendPLI() // ✅ ONLY PLI/FIR handled
สิ่งที่ไม่ถูก handle:

- \*rtcp.Nack - ❌ ไม่มีใน switch case
- \*rtcp.Remb - ❌ ไม่มีใน switch case
- \*rtcp.TransportLayerCC - ❌ ไม่มีใน switch case

---

📈 RTCP Packet Flow ใน K2 Gateway:
WebRTC Browser K2 Gateway SIP Endpoint
│ │ │
├─→ RTP → PLI ──────────────────→│ │
│ ├─→ RTP → PLI ──────────────────→│
│ │ │
│ ←─ RTP ← PLI ───────────────────┤←─ RTP ← PLI ───────────────────┤
│ │ │
├─→ RTP → NACK ─────────────────→│ ❌ Not forwarded │
│ │ (Pion handles internally) │
│ │ │
├─→ RTP → REMB ────────────────→│ ❌ Not forwarded │
│ │ (Pion handles internally) │
│ │ │
├─→ RTP → TWCC ────────────────→│ ❌ Not forwarded │
│ │ (Pion handles internally) │

---

🎯 สรุป Table:
| Feature | K2 Gateway Custom Code | Pion Default Support | SIP Forwarding |
|---------|----------------------|---------------------|---------------|
| PLI / FIR | ✅ Implemented | ✅ Yes | ✅ Yes |
| NACK | ❌ No custom handling | ✅ Yes (default) | ❌ No |
| REMB | ❌ No custom handling | ✅ Yes (default) | ❌ No |
| TWCC | ❌ No custom handling | ✅ Yes (default) | ❌ No |

---

💡 Recommendations:

1. PLI/FIR: ✅ Work correctly - ใช้ได้เลยสำหรับ keyframe recovery
2. NACK: ⚠️ Limited - Pion handles NACK ระหว่าง WebRTC peers แต่:
   - ไม่ forward NACK ไปยัง SIP endpoint
   - ถ้า SIP endpoint ส่ง NACK มา, K2 Gateway จะไม่ส่งไปยัง browser
   - ปัญหา: เมื่อ packet loss บน SIP side, browser จะไม่ได้รับ NACK
3. REMB/TWCC: ⚠️ Limited - Pion handles TWCC ระหว่าง WebRTC peers แต่:
   - ไม่มี bitrate adaptation สำหรับ SIP side
   - ไม่ forward bandwidth feedback ไปยัง SIP endpoint
   - ปัญหา: ไม่สามารถ control bitrate บน SIP side ได้
     K2 Gateway ทำงานได้ดีสำหรับ PLI/FIR แต่ยังขาด NACK/REMB/TWCC forwarding สำหรับ SIP bridge!
