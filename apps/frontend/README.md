# Gateway Frontend

React + TypeScript + Vite frontend สำหรับควบคุม WebRTC call ผ่าน `gateway`.

## Run

```bash
pnpm install
pnpm dev
```

จาก root monorepo สามารถใช้:

```bash
pnpm dev:frontend
```

## Build And Test

```bash
pnpm build
pnpm lint
pnpm test
```

## Environment

ตั้งค่าใน `.env` (หรือคัดลอกจาก `.env.example`):

- `VITE_GATEWAY_URL`
- `VITE_TURN_URL`
- `VITE_TURN_USERNAME`
- `VITE_TURN_CREDENTIAL`
- `VITE_KEYCLOAK_URL`
- `VITE_KEYCLOAK_REALM`
- `VITE_KEYCLOAK_CLIENT`
- `VITE_CONFIG_AUTORECORD`

> สำหรับ deployment ด้วย Docker/Coolify: ค่ากลุ่ม `VITE_*` รองรับทั้งตอน build และตอน runtime ของ container
> (ตั้งใน Coolify Environment Variables ได้โดยไม่ต้อง rebuild image)

## Supported Operation Flows

1. Browser Frontend -> Gateway -> Kamailio/Asterisk -> Linphone Desktop
2. Browser Frontend A -> Gateway -> Kamailio/Asterisk -> Gateway -> Browser Frontend B

## Runtime Behavior (Current)

- ค่า default mode เป็น `siptrunk`
- เมื่อ WebSocket connected/reconnected ระบบจะพยายาม `trunk_resolve` อัตโนมัติ (ถ้ามี trunk id/credentials ที่ resolve ได้)
- เมื่อมี `incoming` แล้ว local media ยังไม่พร้อม ระบบจะสร้าง media session อัตโนมัติก่อนส่ง `accept`
- รองรับ `resume` สำหรับ reconnect และ redirect ตาม backend contract

## Key WebSocket Messages

Client -> Server:

- `offer`
- `trunk_resolve`
- `call`
- `accept`
- `reject`
- `hangup`
- `resume`

Server -> Client:

- `answer`
- `incoming`
- `state`
- `trunk_resolved`
- `trunk_redirect`
- `resume_redirect`
- `resumed`
- `resume_failed`
- `error`

## References

- Dual-flow architecture: `../gateway/docs/dual-flow.md`
- Backend WS contract: `../gateway/AGENTS.md`
