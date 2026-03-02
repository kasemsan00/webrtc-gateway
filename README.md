# webrtc-gateway (Nx workspace)

This repository now contains multiple apps managed by Nx:

- `gateway-frontend` (React + Vite + TanStack Start)
- `gateway-sip` (Go WebRTC/SIP gateway)

## Nx projects

```bash
npx nx show projects
```

Expected projects:

- `gateway-frontend`
- `gateway-sip`

## Common commands

### Frontend

```bash
npx nx dev gateway-frontend
npx nx build gateway-frontend
npx nx test gateway-frontend
npx nx lint gateway-frontend
```

### SIP Gateway (Go)

```bash
npx nx serve gateway-sip
npx nx build gateway-sip
npx nx test gateway-sip
```

## Root npm scripts

```bash
npm run show:projects
npm run dev:frontend
npm run build:frontend
npm run test:frontend
npm run serve:sip
npm run build:sip
npm run test:sip
npm run test
```

## Project config

Nx targets are defined explicitly in:

- `apps/gateway-frontend/project.json`
- `apps/gateway-sip/project.json`

This avoids fragile plugin inference and keeps each app runnable independently.
