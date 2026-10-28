# WebRTC-SIP Gateway deploy profiles

Two production compose profiles for single-domain deploy at e.g. `gateway.example.com/admin`.
There is **no compose-level nginx proxy** — routing is either on your external proxy (split) or inside the unified image.

## Choose a profile

| Mode | Compose file | Containers | External proxy |
|------|--------------|------------|----------------|
| **Split** | [`docker-compose.split.yml`](docker-compose.split.yml) | `gateway-console` + `gateway` | Path-based — see [`nginx.external.conf.example`](nginx.external.conf.example) |
| **Unified** | [`docker-compose.unified.yml`](docker-compose.unified.yml) | `gateway-stack` (gateway + frontend + internal nginx) | Single upstream → `:8088` |

- **Split** — update or scale frontend and gateway independently.
- **Unified** — simplest server deploy: one container, one external upstream.

## URL map (single domain)

| Public path | Split upstream | Unified upstream |
|-------------|----------------|------------------|
| `/` | `302` → `/admin/` (external nginx) | internal nginx → `302 /admin/` |
| `/admin/` | frontend `:4173` | internal nginx → frontend `:4173` |
| `/api/` | gateway `:8080` | internal nginx → gateway `:8080` |
| `/ws`, `/ws-public`, `/ws-agent`, `/ws-agent-device` | gateway `:8080` | internal nginx → gateway `:8080` |

Public URLs:

- Admin: `https://gateway.example.com/admin/`
- REST: `https://gateway.example.com/api/...`
- WebSocket: `wss://gateway.example.com/ws`

Both nginx files use a prefix `location /ws`, so it also forwards `/ws-agent` and
`/ws-agent-device`; `/ws-public` has its own block. Each WebSocket route exists
only when its `API_ENABLE_*` flag is on.

## Split deploy

From repo root (the compose files read `../.env`, i.e. the repo-root `.env`):

```bash
docker compose -f deploy/docker-compose.split.yml up -d
```

There is no root `docker-compose.yml`.

Services use `network_mode: host` (SIP/RTP).

Before application services start, Compose runs the one-shot `db-bootstrap` service from the same release image. It safely initializes an empty gateway schema or applies pending migrations to an existing schema. If it fails, the rollout is blocked. Keep `DB_BOOTSTRAP_ON_START=false` for production application services; use `DB_BOOTSTRAP_ON_START=true` only when deliberately running a single instance without the compose job.

Configure external nginx/Coolify using [`nginx.external.conf.example`](nginx.external.conf.example) — route `/admin/` to `127.0.0.1:4173` and `/api`+`/ws` to `127.0.0.1:8080`.

### Split environment

```env
TAG=1.4.1
VITE_GATEWAY_URL=gateway.example.com
VITE_BASE_PATH=/admin/
GATEWAY_PUBLIC_WS_URL=wss://gateway.example.com/ws
API_CORS_ORIGINS=https://gateway.example.com
API_PORT=8080
FRONTEND_PORT=4173
```

## Unified deploy

Build image (CI or manual):

```bash
docker build -f deploy/Dockerfile.unified -t registry.kasemsan.com/webrtc-sip-gateway-stack:1.4.1 .
```

Run:

```bash
docker compose -f deploy/docker-compose.unified.yml up -d
```

The unified profile also runs `db-bootstrap` before the `gateway-stack` service (image `webrtc-sip-gateway-stack`). A failed bootstrap or a gateway process that exits during startup makes the stack fail instead of serving an admin frontend without a working gateway.

External proxy forwards the **entire host** to `STACK_PROXY_PORT` (default **8088**).

Nginx Proxy Manager (or host nginx) must forward `X-Forwarded-Proto` and `X-Forwarded-Host` (NPM usually does this automatically when SSL is enabled). The internal nginx in `webrtc-sip-gateway-stack` uses those headers so `/` redirects to `https://your-domain/admin/` instead of `http://your-domain:8088/admin/`.

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
    client_max_body_size 12m;
}
```

### Unified environment

Same as split, plus:

```env
STACK_PROXY_PORT=8088
```

## Admin password

Set `FRONTEND_PASSWORD` on both the frontend and gateway processes (unified `env_file` already shared). The admin UI shows a password-only login. After login it sends that value as `Authorization: Bearer` on `/api/*`.

`AUTH_ENABLE` and Keycloak JWKS stay for mobile `/ws` and JWT REST clients. The admin UI does not use Keycloak.

The frontend process exits if `FRONTEND_PASSWORD` is empty. Do not set `VITE_FRONTEND_PASSWORD`.

## Chat images

`POST /api/chat-images` and `GET /api/chat-images/{id}` serve in-call chat photos from a local disk volume (`CHAT_IMAGE_DIR`, default `/var/lib/webrtc-gateway/chat-images`). Proxies in front of `/api/` need `client_max_body_size 12m` so 10MB uploads succeed. Set `CHAT_IMAGE_PUBLIC_BASE_URL=https://your-domain` so returned URLs are absolute.

## Images

| Image | Dockerfile |
|-------|------------|
| `webrtc-sip-gateway-console` | [`apps/frontend/Dockerfile`](../apps/frontend/Dockerfile) |
| `webrtc-sip-gateway` | [`apps/gateway/Dockerfile`](../apps/gateway/Dockerfile) |
| `webrtc-sip-gateway-stack` | [`Dockerfile.unified`](Dockerfile.unified) |

[`docker-ci.sh`](../docker-ci.sh) and [`docker-ci.ps1`](../docker-ci.ps1) build and push **only the unified stack image** from `deploy/Dockerfile.unified`:

- tags `registry.kasemsan.com/webrtc-sip-gateway-stack:${BRANCH}` and `:latest`; `BRANCH` defaults to `1.4.1`, `REGISTRY` to `registry.kasemsan.com`, `PLATFORMS` to `linux/amd64`;
- `VITE_BASE_PATH` defaults to `/admin/`; `VITE_GATEWAY_URL` and `VITE_CONFIG_AUTORECORD` are passed through;
- `RUN_MIGRATIONS=true` (with `DB_DSN`) runs Goose migrations before the build;
- only `docker-ci.sh` passes the `GIT_*`/`BUILD_TIME` build args that `GET /api/version` reports.

The split images (`webrtc-sip-gateway-console`, `webrtc-sip-gateway`) are built from their own Dockerfiles; no script in this repo builds them. The compose files default `TAG` to `1.4.0`, so set `TAG` explicitly to the release you pushed.

k2 deployment: a release recreates only the `k2-stack` container (the legacy stack name, see `LEGACY_IMAGE_NAMES` below); other containers on that host are left running.

## Rename compatibility window

The canonical identities are `webrtc-sip-gateway`,
`webrtc-sip-gateway-console`, and `webrtc-sip-gateway-stack`. Existing
deployments and released clients can still reference the former `k2-*` image
names or legacy hostname during a short, operator-controlled migration window.

- Publish the same immutable stack digest under a legacy image name by setting
  `LEGACY_IMAGE_NAMES` for `docker-ci.sh`/`docker-ci.ps1` (comma-separated; tagged with `${BRANCH}` only, not `latest`), for example
  `LEGACY_IMAGE_NAMES=k2-stack`.
- Keep the legacy DNS/TLS hostname proxying to the same gateway until the
  inventory of supported mobile, desktop, and deployment clients has moved to
  the new endpoint.
- Do not remove either alias merely because the source rename has merged.
  Retire it only after the operator confirms that supported clients and
  automation no longer use it.

Existing PostgreSQL databases, roles, volumes, and `DB_DSN` values are
compatible with the renamed release. The development Compose defaults are new
resources only; production data is not renamed automatically.

## Files

- [`nginx.conf.template`](nginx.conf.template) — internal routing (unified image only)
- [`nginx.external.conf.example`](nginx.external.conf.example) — external routing (split deploy)
- [`stack-entrypoint.sh`](stack-entrypoint.sh) — unified container process supervisor
- [`Dockerfile.unified`](Dockerfile.unified) — unified image build
