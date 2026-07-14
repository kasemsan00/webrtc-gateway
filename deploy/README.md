# K2 Gateway deploy profiles

Two production compose profiles for single-domain deploy at e.g. `k2-gateway.kasemsan.com/admin`.
There is **no compose-level nginx proxy** — routing is either on your external proxy (split) or inside the unified image.

## Choose a profile

| Mode | Compose file | Containers | External proxy |
|------|--------------|------------|----------------|
| **Split** (default) | [`docker-compose.split.yml`](docker-compose.split.yml) or root [`docker-compose.yml`](../docker-compose.yml) | `frontend` + `webrtc-gateway` | Path-based — see [`nginx.external.conf.example`](nginx.external.conf.example) |
| **Unified** | [`docker-compose.unified.yml`](docker-compose.unified.yml) | `k2-stack` (gateway + frontend + internal nginx) | Single upstream → `:8088` |

- **Split** — update or scale frontend and gateway independently.
- **Unified** — simplest server deploy: one container, one external upstream.

## URL map (single domain)

| Public path | Split upstream | Unified upstream |
|-------------|----------------|------------------|
| `/` | `302` → `/admin/` (external nginx) | internal nginx → `302 /admin/` |
| `/admin/` | frontend `:4173` | internal nginx → frontend `:4173` |
| `/api/` | gateway `:8080` | internal nginx → gateway `:8080` |
| `/ws`, `/ws-public` | gateway `:8080` | internal nginx → gateway `:8080` |

Public URLs:

- Admin: `https://k2-gateway.kasemsan.com/admin/`
- REST: `https://k2-gateway.kasemsan.com/api/...`
- WebSocket: `wss://k2-gateway.kasemsan.com/ws`

## Split deploy

From repo root (`.env` required):

```bash
docker compose up -d
# or explicitly:
docker compose -f deploy/docker-compose.split.yml up -d
```

Services use `network_mode: host` (SIP/RTP).

Configure external nginx/Coolify using [`nginx.external.conf.example`](nginx.external.conf.example) — route `/admin/` to `127.0.0.1:4173` and `/api`+`/ws` to `127.0.0.1:8080`.

### Split environment

```env
TAG=1.3.2
VITE_GATEWAY_URL=k2-gateway.kasemsan.com
VITE_BASE_PATH=/admin/
GATEWAY_PUBLIC_WS_URL=wss://k2-gateway.kasemsan.com/ws
API_CORS_ORIGINS=https://k2-gateway.kasemsan.com
API_PORT=8080
FRONTEND_PORT=4173
```

## Unified deploy

Build image (CI or manual):

```bash
docker build -f deploy/Dockerfile.unified -t registry.kasemsan.com/k2-stack:1.3.2 .
```

Run:

```bash
docker compose -f deploy/docker-compose.unified.yml up -d
```

External proxy forwards the **entire host** to `STACK_PROXY_PORT` (default **8088**).

Nginx Proxy Manager (or host nginx) must forward `X-Forwarded-Proto` and `X-Forwarded-Host` (NPM usually does this automatically when SSL is enabled). The internal nginx in `k2-stack` uses those headers so `/` redirects to `https://your-domain/admin/` instead of `http://your-domain:8088/admin/`.

```nginx
location / {
    proxy_pass http://127.0.0.1:8088;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_read_timeout 3600s;
}
```

### Unified environment

Same as split, plus:

```env
STACK_PROXY_PORT=8088
```

## Keycloak

Register redirect / web origins for the admin base path:

- `https://k2-gateway.kasemsan.com/admin/*`
- Web origin: `https://k2-gateway.kasemsan.com`

## Images

| Image | Dockerfile |
|-------|------------|
| `k2-frontend` | [`apps/frontend/Dockerfile`](../apps/frontend/Dockerfile) |
| `k2-gateway` | [`apps/gateway/Dockerfile`](../apps/gateway/Dockerfile) |
| `k2-stack` | [`Dockerfile.unified`](Dockerfile.unified) |

Built via [`docker-ci.ps1`](../docker-ci.ps1).

## Files

- [`nginx.conf.template`](nginx.conf.template) — internal routing (unified image only)
- [`nginx.external.conf.example`](nginx.external.conf.example) — external routing (split deploy)
- [`stack-entrypoint.sh`](stack-entrypoint.sh) — unified container process supervisor
- [`Dockerfile.unified`](Dockerfile.unified) — unified image build
