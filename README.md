https://webrtc-gateway-ui.app.kasemsan.com/

# WebRTC Gateway Monorepo

Monorepo สำหรับระบบ WebRTC <-> SIP Gateway โดยแยกเป็น frontend สำหรับ operations UI และ backend gateway service สำหรับ signaling/media.

## Project Structure

- `apps/gateway-frontend` : React + TypeScript + Vite UI
- `apps/gateway-sip` : Go service สำหรับ WebRTC <-> SIP bridge
- `packages/ui` : shared UI components
- `packages/eslint-config` : shared ESLint config
- `packages/typescript-config` : shared TypeScript config

## Tech Stack

- Monorepo: `pnpm` workspaces + `turborepo`
- Frontend: React, TypeScript, Vite, TanStack Router
- Backend: Go (`k2-gateway`), Pion WebRTC, SIP stack

## Prerequisites

- Node.js `>= 18`
- `pnpm@9`
- Go `1.25.5` (สำหรับ `apps/gateway-sip`)

## Install Dependencies

รันที่ root ของ repo:

```bash
pnpm install
```

## Root Commands

```bash
pnpm build
pnpm lint
pnpm check-types
pnpm dev
```

## Run Each App

Frontend (จาก root):

```bash
pnpm dev:frontend
```

Backend (จาก root):

```bash
pnpm exec turbo dev --filter=gateway-sip
```

หมายเหตุ: สคริปต์ `pnpm run dev:backend` ใน root ยังชี้ไปที่ `gateway-backend` (ชื่อเก่า) จึงควรใช้คำสั่ง `turbo` ด้านบนแทน.

## App-Level Commands

Frontend scripts: ดูที่ `apps/gateway-frontend/package.json`

Backend targets: ดูที่ `apps/gateway-sip/project.json`

ตัวอย่าง backend แบบตรงโฟลเดอร์:

```bash
cd apps/gateway-sip
go run .
go test ./...
```

## Environment Setup

ตั้งค่า environment แยกตามแอปก่อนรัน:

- Frontend: `apps/gateway-frontend/.env.example`
- Backend: `apps/gateway-sip/.env.example`

ไฟล์อ้างอิงเพิ่มเติม:

- `apps/gateway-frontend/README.md`
- `apps/gateway-sip/AGENTS.md`

## Development Notes

- แก้โค้ดให้ scope อยู่ในแอป/แพ็กเกจที่เกี่ยวข้องเท่านั้น
- หลีกเลี่ยงแก้ไฟล์ generated เช่น `apps/gateway-frontend/src/routeTree.gen.ts`
- การเปลี่ยน media path ใน `apps/gateway-sip` มีความเสี่ยงสูง ควรเลี่ยงหากไม่ได้ตั้งใจแก้พฤติกรรมโปรโตคอล

## Key References

- Monorepo tasks: `turbo.json`
- Workspace config: `pnpm-workspace.yaml`
- Root scripts: `package.json`
- Frontend guide: `apps/gateway-frontend/AGENTS.md`
- Backend guide: `apps/gateway-sip/AGENTS.md`
