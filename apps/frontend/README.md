# Gateway Frontend

React + TypeScript + Vite operations UI for the WebRTC gateway.

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

- `FRONTEND_PASSWORD` (required; server-side only, used for login and as the REST bearer)
- `VITE_GATEWAY_URL`
- `VITE_BASE_PATH` (optional; production single-domain default `/admin/`, see `../../deploy/README.md`)
- `VITE_CONFIG_AUTORECORD`

> สำหรับ deployment ด้วย Docker/Coolify: ค่ากลุ่ม `VITE_*` รองรับทั้งตอน build และตอน runtime ของ container
> (ตั้งใน Coolify Environment Variables ได้โดยไม่ต้อง rebuild image)
>
> `FRONTEND_PASSWORD` is runtime-only. The frontend server and gateway both need it.
>
> Single-domain deploy (`gateway.example.com/admin`): see [`deploy/README.md`](../../deploy/README.md) — **split** (2 containers + external path routing) or **unified** (`webrtc-sip-gateway-stack`, one external upstream → `:8088`).

## References

- Dual-flow architecture: `../gateway/docs/dual-flow.md`
- Backend WS contract: `../gateway/AGENTS.md`
