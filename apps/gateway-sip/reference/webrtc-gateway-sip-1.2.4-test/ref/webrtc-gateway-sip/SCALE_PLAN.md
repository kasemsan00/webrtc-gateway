# K2 Gateway — Scale Plan (SIP Registration + Incoming/Outgoing Calls)

> เป้าหมาย: รองรับ **500–1000 SIP registrations** พร้อมกัน และ **10–100 concurrent calls** (WebRTC↔SIP) โดยมี **Kamailio เป็น SIP proxy/registrar** อยู่หน้า gateway cluster

---

## 1) สถานะปัจจุบันของโปรเจค (Key Findings)

จากโค้ดใน repo:

- **Dynamic SIP Registration เป็น global 1 ชุด/instance**
  - `internal/sip/registration.go` เก็บ `dynamicConfig/isRegistered/registrationCancel` ใน `sip.Server` ทำให้ “หลายบัญชี” จะทับกัน
- **Session/Call state เป็น in-memory ต่อ instance**
  - `internal/session/manager.go` เก็บ `sessions map[string]*Session`
  - `Session` ถือ `PeerConnection`, UDP RTP sockets, dialog state ฯลฯ ⇒ **1 call ต้อง stick กับ node เดิม**
- **Incoming call flow ผูกกับ ServerTransaction ที่อยู่ใน memory**
  - `internal/sip/handlers.go` เก็บ `IncomingSIPTx`/`IncomingSIPReq` ใน session แล้วรอ browser ส่ง `accept/reject`
  - แปลว่า **SIP INVITE ต้องมาถูก node ที่ user ต่อ WS อยู่** (หรือไม่ก็ต้องออกแบบ cross-node accept/reject เพิ่ม)
- **WebSocket client mapping เป็น in-memory (`wsClients map[sessionId]*WSClient`)**
  - ใช้ได้ใน single-node แต่ถ้าหลาย node ต้องมี **presence/routing directory**

ข้อสรุป: การ scale แบบ multi-node ต้องทำให้

1. การ register เป็น **multi-account + distributed ownership**
2. การ route incoming INVITE ไปยัง node ที่ถูกคน/ถูก session
3. call state “stick” และ resume ได้

---

## 2) เป้าหมาย / Non-Goals

### Goals

- รองรับ **500–1000 registrations** (AoR) พร้อม refresh/expiry อย่างปลอดภัย
- รองรับ **10–100 concurrent calls** ด้วย latency/เสถียรภาพเหมาะกับ real-time
- รองรับ **incoming/outgoing** และ `accept/reject/hangup/dtmf/message/resume`
- รองรับ **horizontal scaling** (หลาย gateway instances) โดยไม่ทำให้ calls “หลุด node”
- ปลอดภัย: จัดการ credentials และ secrets อย่างเหมาะสม

### Non-Goals (ระยะนี้)

- ไม่ทำ HA ระดับ “transparent mid-call migration” (ย้าย media/call state ข้าม node กลางสาย)
  - เนื่องจาก WebRTC PeerConnection + RTP sockets เป็น in-memory/OS resource
- ไม่ทำ transcoding (ยังเป็น Opus passthrough / H.264 เท่านั้นตามเดิม)

---

## 3) Target Architecture (Recommended)

### 3.1 Control Plane vs Data Plane

- **Control plane (state/routing/config):**
  - Postgres: persistent config + credentials + policies + audit
  - Redis: ephemeral state + lease + presence + routing + event pubsub
- **Data plane (real-time media + SIP dialog handling):**
  - Gateway nodes: WebRTC PeerConnection, RTP sockets, SIP client/server transactions
  - Kamailio: SIP routing/registrar/location service, load balancing to gateway nodes

### 3.2 Kamailio Responsibilities

- Registrar + location service (usrloc)
- Route INVITE/BYE/ACK/MESSAGE ไปยัง gateway node ที่ถูกต้อง
- Optional: dispatcher/failover, rate limiting, topology hiding

**หลักการสำคัญ:** incoming INVITE ควรไปถึง node ที่มี user ต่อ WebSocket อยู่ (presence-based routing)

---

## 4) Data Model / Storage Strategy

### 4.1 Postgres (Persistent)

ใช้เก็บ “ความจริงระยะยาว”:

- Tenants / Users / SIP Accounts (AoR)
- SIP credentials (ควรเข้ารหัสที่ระดับแอป หรือใช้ secret manager)
- Policy: อนุญาตให้โทรออกไปไหน, caller ID rules, concurrency limits
- Audit/Logs/Registration history (optional)

**ตารางตัวอย่าง**

- `sip_accounts`:
  - `id`, `tenant_id`, `aor` (เช่น `sip:1001@domain`), `domain`, `username`
  - `password_encrypted` (หรือ secret ref), `port`, `transport` (tcp)
  - `enabled`, `created_at`, `updated_at`
- `call_records` (optional): `call_id`, `session_id`, `from`, `to`, `direction`, `started_at`, `ended_at`, `end_reason`

### 4.2 Redis (Ephemeral / TTL / Coordination)

ใช้เก็บ state ที่ churn สูง + ต้องเร็ว:

- **Lease/Ownership ต่อ AoR**
  - `sipreg:lease:{aor}` = `{nodeId}` (TTL 30–60s, ต่ออายุต่อเนื่อง)
- **Registration status**
  - `sipreg:status:{aor}` = JSON:
    - `registered` (bool)
    - `expiresAt` (unix ms)
    - `lastCode` (int), `lastError` (string)
    - `nodeId` (owner)
- **Presence: user ต่อ WS อยู่ที่ไหน**
  - `ws:presence:{userId}` = `{nodeId, connId, lastSeen}` (TTL 30–120s)
- **Routing: sessionId อยู่ node ไหน**
  - `call:route:{sessionId}` = `{nodeId}` (TTL ตามอายุ call + grace)
- **Event bus**
  - Redis PubSub/Streams topics เช่น:
    - `events:incoming:{userId}`
    - `events:state:{sessionId}`

> หมายเหตุ: หลีกเลี่ยงการเก็บ SIP password ใน Redis แบบ plain text

---

## 5) Scaling SIP Registration (500–1000 AoR)

### 5.1 Lease-based Ownership (Per-AoR)

แนวทาง: AoR ใด ๆ จะถูกดูแลโดย owner node เดียว

**Flow**

1. Scheduler/worker บนทุก node ดึงรายการ AoR ที่ต้อง register (จาก Postgres หรือจาก queue)
2. พยายาม acquire lease:
   - `SET sipreg:lease:{aor} {nodeId} NX EX 60`
3. ถ้าได้ lease ⇒ node นี้:
   - สร้าง/ใช้ `sipgo.Client` ของ account (อย่าใช้ shared dynamicConfig แบบเดิม)
   - ส่ง REGISTER และ refresh ก่อนหมดอายุ (ใช้ expiry จาก response)
   - update `sipreg:status:{aor}` เป็นระยะ
4. ถ้า lease หลุด (node ตาย/ถูก kill) ⇒ node อื่น takeover ได้ภายใน TTL

### 5.2 Concurrency / Rate Control

- จำกัด concurrent REGISTER ต่อ node (เช่น 20–50) กัน burst
- ใช้ jitter ในการ schedule refresh (กระจาย load)
- exponential backoff เมื่อ 401/403/5xx หรือ DNS fail

### 5.3 Contact / Routing Considerations (With Kamailio)

เพื่อให้ incoming INVITE กลับมาถูก node owner:

- REGISTER ต้องตั้ง `Contact` ให้ reachable ผ่าน Kamailio routing policy
- ถ้าใช้ Path/Outbound ที่ Kamailio รองรับ ให้ยึด connection หรือ record-route เพื่อส่งกลับถูกทาง

---

## 6) Scaling Calls (10–100 Concurrent)

### 6.1 Outgoing Call (WebRTC → SIP)

**หลักการ:** call เริ่มจาก node ที่ WS ของ user อยู่แล้ว (sticky)

Flow:

1. Browser ส่ง `offer` สร้าง session บน node A
2. Browser ส่ง `call` ไป node A
3. Node A ส่ง INVITE ผ่าน Kamailio ไป upstream (Asterisk/ปลายทาง)
4. Node A เก็บ dialog state + RTP endpoints + ทำ media forwarding (ตามเดิม)
5. เขียน `call:route:{sessionId} -> nodeA` ใน Redis เพื่อให้ระบบอื่น lookup ได้

### 6.2 Incoming Call (SIP → WebRTC)

**แนะนำวิธีหลัก (เพื่อเข้ากับโค้ดปัจจุบัน): Presence-based routing ที่ Kamailio**

Flow:

1. User online ⇒ WS node A เขียน `ws:presence:{userId} = nodeA`
2. Kamailio รับ INVITE (to userId/AoR) ⇒ lookup presence ⇒ route INVITE ไป nodeA
3. NodeA รับ INVITE ⇒ สร้าง session + เก็บ `IncomingSIPTx/Req` + ส่ง 180 Ringing + แจ้ง browser
4. Browser ส่ง `accept/reject` กลับ nodeA ⇒ nodeA ส่ง 200 OK/486 และเริ่ม media

> Alternative (Phase 2): Cross-node accept/reject ผ่าน Redis PubSub/Streams  
> แต่ต้องออกแบบ transaction correlation เพราะ `ServerTransaction` อยู่เฉพาะ node ที่รับ INVITE

### 6.3 Stickiness / Resume

- LB สำหรับ WebSocket ควร sticky (cookie/ip-hash) **หรือ** ใช้ “gateway router” ที่ lookup `sessionId -> nodeId`
- ใช้ `resume` flow ที่มีอยู่ (`internal/api/server.go` + `Session.RenegotiatePeerConnection`) ร่วมกับ `call:route:{sessionId}`

---

## 7) Required Refactors in This Repo

### 7.1 Refactor SIP Registration to Multi-Account

เปลี่ยนจาก:

- `sip.Server.dynamicConfig` (single global)
  เป็น:
- `RegistrationManager` ที่ถือหลายบัญชี (keyed by `accountID`/`aor`) และรองรับ lease ownership

โครงสร้างแนะนำ:

- `internal/store/redis/` (lease, presence, routing, pubsub)
- `internal/store/postgres/` (sip_accounts CRUD)
- `internal/sip/registrar/`:
  - `Manager` (scheduler + lease + refresh)
  - `AccountClient` (sipgo.Client per account)
  - `Register(ctx, account)` / `Unregister(ctx, account)`

### 7.2 Update API/WebSocket Protocol for Multi-Registration

ตอนนี้ `register` message ส่งแค่ `sipDomain/sipUsername/sipPassword/sipPort` และทำ register แบบ global

ต้องเพิ่ม field เพื่อรองรับ multi-tenant/multi-account:

- `accountId` หรือ `aor` หรือ `userId`
- ตัวอย่าง:
  - `{"type":"register","accountId":"...","sipDomain":"...","sipUsername":"...","sipPassword":"...","sipPort":5060}`
  - `{"type":"unregister","accountId":"..."}`
  - `{"type":"registerStatus","accountId":"...","registered":true,"expiresAt":...}`

### 7.3 Presence & Routing Directory

เพิ่มเมื่อ WS connect/login:

- บันทึก `ws:presence:{userId}` ใน Redis พร้อม TTL + heartbeat

จากนั้น:

- เพิ่ม `call:route:{sessionId}` เมื่อสร้าง session/call

### 7.4 Incoming Call Delivery: “Broadcast → Targeted”

ปัจจุบัน `NotifyIncomingCall` broadcast ไปทุก WS client

ต้องปรับเป็น:

- route ไป user ที่ถูกต้อง (ตาม `toURI`/DID/tenant routing)
- หรือส่งไป “agent group” ตาม policy (ถ้ามี)

### 7.5 Observability / Safety

- เพิ่ม metrics: registrations active, refresh failures, call ASR, call setup time, RTP port utilization
- log correlation id: `sessionId`, `sipCallId`, `aor`, `userId`, `nodeId`
- หลีกเลี่ยงการถือ lock ข้าม network call (ตาม guideline ใน repo)

---

## 8) Kamailio Integration Plan

### 8.1 Routing Strategy (Recommended)

- Kamailio เลือก gateway node จาก **presence** (Redis) หรือ directory service
- ใช้ dispatcher/list ของ gateways + per-user override

### 8.2 Failover Strategy

**Registrations:** takeover ด้วย Redis lease TTL

**Calls:** mid-call failover ทำไม่ได้ใน Phase 1 (ต้อง stick node)

- ถ้า node ตายกลางสาย: call drop (acceptable ใน phase 1)
- ใช้ readiness/liveness + draining เพื่อลด impact

---

## 9) Rollout Phases (Incremental Delivery)

### Phase 0 — Baseline & Guardrails

- เพิ่ม nodeId, basic health endpoints, metrics placeholders
- ทำให้ WS sticky routing พร้อม deploy หลาย instance ได้แบบ “ยัง single-registration”

### Phase 1 — Multi-Registration + Ownership

- เพิ่ม Postgres schema + Redis lease/status
- Implement RegistrationManager (register/refresh/unregister per account)
- API: register/unregister แบบมี `accountId`

### Phase 2 — Presence-based Incoming Routing

- WS presence ใน Redis (userId -> nodeId)
- Kamailio route INVITE ไป node ที่ user online
- ปรับ NotifyIncomingCall ให้ targeted

### Phase 3 — Hardening & Scale Tests

- rate limit + jitter + backoff
- load test: 1000 regs + 100 calls
- chaos: kill node owner แล้วดู takeover

---

## 10) Risks / Pitfalls (Must Address)

- **Security:** การเก็บ SIP password ต้องเข้ารหัส (อย่าไว้ plain ใน Redis/log)
- **REGISTER burst:** restart แล้วแห่ register พร้อมกัน ต้อง throttle
- **TCP-only SIP:** load balancer ทั่วไปอาจทำให้ routing เพี้ยนถ้าไม่มี Kamailio คุม
- **Cross-node signaling:** หลีกเลี่ยงใน Phase 1; ให้ Kamailio route ให้ถูก node
- **Sticky requirement:** ทุก call ต้องอยู่ node เดิมจนจบ

---

## 11) Acceptance Criteria

- [ ] รองรับ 1000 AoR: steady-state refresh ไม่มี spike เกิน threshold ที่กำหนด
- [ ] รองรับ 100 concurrent calls โดย CPU/RTP ports อยู่ในกรอบ
- [ ] incoming call เข้าถูก user (ไม่ broadcast มั่ว)
- [ ] node failure: registrations takeover ภายใน ≤ 60s
- [ ] logging/metrics มีพอ debug ปัญหา (sessionId/sipCallId/aor/nodeId)
