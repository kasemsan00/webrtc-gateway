# Gateway Frontend

React + TypeScript + TanStack Start (Vite) operations UI for the WebRTC-SIP gateway.

## Run

```bash
pnpm install
pnpm dev
```

จาก root monorepo สามารถใช้:

```bash
pnpm dev:frontend
```

`FRONTEND_PASSWORD` must be set in `apps/frontend/.env` (not a `VITE_` variable). Copy `.env.example` first. Set the same value on the gateway process so `/api/*` accepts the login bearer.

## Build And Test

```bash
pnpm build
pnpm lint
pnpm test
```

## Environment

ตั้งค่าใน `.env` (หรือคัดลอกจาก `.env.example`):

- `FRONTEND_PASSWORD` (required; server-side only, used for login and as the REST bearer; both dev and `docker-server.mjs` exit if empty)
- `VITE_GATEWAY_URL`
- `VITE_BASE_PATH` (optional; production single-domain default `/admin/`, see `../../deploy/README.md`)

ตัวแปรเฉพาะตอนรัน container (`docker-server.mjs`):

- `HOST` (ค่าเริ่มต้น `0.0.0.0`)
- `PORT` (ค่าเริ่มต้น `4173`)
- `BASE_PATH` (fallback ของ prefix ฝั่ง server หากไม่ได้ตั้ง `VITE_BASE_PATH` ตอน runtime)

> สำหรับ deployment ด้วย Docker/Coolify:
> - `VITE_GATEWAY_URL` รองรับการ override ตอน runtime ของ container โดยไม่ต้อง rebuild image (`docker-server.mjs` ฉีด `window.__APP_RUNTIME_ENV__` ลงใน HTML)
> - `VITE_BASE_PATH` ถูกฝังตอน build ลงใน client router (`src/router.tsx`) และ URL ของไฟล์ static (`vite.config.ts`) ดังนั้นค่า `VITE_BASE_PATH` ตอน build ต้องตรงกับค่า `VITE_BASE_PATH` (หรือ `BASE_PATH`) ตอนรัน container เสมอ
> - `FRONTEND_PASSWORD` เป็นค่า runtime-only (ห้ามขึ้นต้นด้วย `VITE_`) และต้องตั้งตรงกันทั้งฝั่ง frontend และ gateway
>
> Single-domain deploy (`gateway.example.com/admin`): see [`deploy/README.md`](../../deploy/README.md) — **split** (2 containers + external path routing) or **unified** (`webrtc-sip-gateway-stack`, one external upstream → `:8088`).

## References

- Backend WS contract: [`../../docs/gateway/ws-contract.md`](../../docs/gateway/ws-contract.md)
- Trunk REST/SSE API: [`../gateway/trunk-api.md`](../gateway/trunk-api.md)
- Gateway guide: [`../gateway/AGENTS.md`](../gateway/AGENTS.md)
