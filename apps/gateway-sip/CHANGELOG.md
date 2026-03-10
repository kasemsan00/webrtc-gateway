## 2026-02-11

- Increased default SIP video watchdog intervals (5s PLI / 10s FIR / 3s check) to prevent FIR/PLI flooding when Linphone only emits on-demand keyframes.
- Removed redundant forced PLI after watchdog FIR, reducing RTCP traffic while letting FIR sequence numbers drive recovery.
- **Fixed 7-minute video freeze**: Removed SIP→WebRTC SPS/PPS injection that used `SequenceNumber=0` — pion does not rewrite seq numbers, so the browser received Seq=0 packets amid a stream at seq ~15000+, corrupting the jitter buffer. SPS/PPS now flows naturally from Linphone as regular RTP packets with correct sequence numbers.
