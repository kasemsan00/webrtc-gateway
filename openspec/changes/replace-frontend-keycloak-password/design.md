## Context

Today the frontend blocks the whole tree on `KeycloakAuthProvider` (`keycloak-js`, `VITE_KEYCLOAK_*`). The access token is stored in `token-store` and attached by `http-client.ts` / SSE as `Authorization: Bearer`. Unified nginx sends `/admin/` to the Node frontend and `/api/` plus `/ws` to the gateway. `AUTH_ENABLE` lives only in the gateway process and verifies JWTs for both REST and `/ws`. GET `/api/client-diagnostics*` list routes are registered outside that middleware on purpose.

The in-browser Gateway Console at `/console` is the only consumer of `apps/frontend/src/features/gateway/` (WebRTC store, TURN `VITE_TURN_*`). Other admin pages live in separate feature folders.

See `proposal.md` for motivation and `specs/admin-password-auth/spec.md` for required behavior.

## Goals / Non-Goals

**Goals:**

- Replace browser Keycloak with a password-only login using one server env secret.
- Keep mobile `/ws` and JWT REST clients working when `AUTH_ENABLE=true`.
- Lock authenticated REST when `FRONTEND_PASSWORD` is set even if `AUTH_ENABLE=false`.
- Delete the in-browser softphone and its frontend TURN/Keycloak build wiring.

**Non-Goals:**

- Per-user accounts, username, SSO, or password hashing/rotation UI.
- BFF / nginx proxy of `/api` through Node.
- Changing `/ws`, `/ws-public`, `/ws-agent`, SIP, media, or TTRS push Keycloak client-credentials.
- Closing the existing unauthenticated GET client-diagnostics read routes.
- Putting `FRONTEND_PASSWORD` in `VITE_*` or `window.__APP_RUNTIME_ENV__`.

## Decisions

### 1. One env var, two processes

Use `FRONTEND_PASSWORD` in both the frontend Node/Vite process and the gateway process (unified `env_file` already shared). Frontend uses it to validate login. Gateway uses it as an alternate REST bearer. Split deploys must set the same value on both.

Alternative considered: separate `ADMIN_API_TOKEN`. Rejected because the operator asked for one password.

Alternative considered: `VITE_FRONTEND_PASSWORD`. Rejected because Vite and `docker-server.mjs` would leak it to the browser.

### 2. Login in the SPA, password held in sessionStorage

Replace `KeycloakAuthProvider` with a password provider:

1. Show a password-only form until login succeeds.
2. `createServerFn` (or equivalent server handler) compares the submitted password to `process.env.FRONTEND_PASSWORD` with a constant-time compare and returns success/failure. The password never goes into client env.
3. On success the client keeps the password in `sessionStorage` and `token-store`, so `http-client` / SSE keep sending `Authorization: Bearer <password>`.
4. Logout clears storage and the token store.

`sessionStorage` survives refresh in the same tab and is cleared when the tab closes. A cookie-only UI gate cannot authorize `/api` because nginx sends `/api` to the gateway, not Node.

Alternative considered: BFF that proxies `/api` with an httpOnly cookie. Stronger XSS posture, but needs nginx and `http-client` routing changes and still leaves host-mapped `:8080` reachable in unified `network_mode: host`.

### 3. Additive REST auth, JWT-only WebSocket

Gateway `authMiddleware` runs when `tokenVerifier != nil` **or** `FRONTEND_PASSWORD` is non-empty.

Order:

1. OPTIONS pass-through (unchanged).
2. If bearer matches `FRONTEND_PASSWORD` (constant-time), accept and attach synthetic claims (`subject=admin`, `preferred_username=admin`) so existing trunk `in_use_by` logging still has a name.
3. Else if a JWT verifier exists, verify as today.
4. Else `401`.

`/ws` must not take this password. `handleWebSocketConn` stays JWT-only when `AUTH_ENABLE=true`.

When `FRONTEND_PASSWORD` is empty, gateway behavior matches today (JWT if enabled, otherwise open REST). Only the frontend server fail-fasts on an empty password so a gateway-only process can still run.

### 4. Frontend fail-fast in the HTTP server, not in Vitest

`docker-server.mjs` (and the Vite/Node production path) exits if `FRONTEND_PASSWORD` is missing or blank. Vitest does not boot that server. Server functions used in tests read a stubbed env.

### 5. Delete the console module instead of hiding the nav link

Remove `routes/console.tsx`, `features/gateway/` (softphone store/WebRTC/TURN), Header Console link, `keycloak-js`, `VITE_KEYCLOAK_*`, and frontend `VITE_TURN_*`. Keep `features/gateway-instances`, `gateway-logs`, and `gateway-config`. Do not hand-edit `routeTree.gen.ts`; regeneration happens on frontend build.

Gateway `TURN_SERVER` stays for real WebRTC clients.

## Risks / Trade-offs

- [XSS can read sessionStorage and replay the password on `/api`] → Accept for a shared ops secret; never put the secret in the JS bundle; HTTPS at the TLS terminator. A later BFF can tighten this without changing the spec.
- [Same password on every REST call] → Same exposure class as today's employee JWT in memory; password is not a Keycloak session.
- [Unified host networking still exposes gateway `:8080`] → REST is locked when `FRONTEND_PASSWORD` is set even with `AUTH_ENABLE=false`. Operators still need a firewall if that port is public.
- [Operators forget to set `FRONTEND_PASSWORD` on the gateway] → UI logs in, `/api` returns 401. Document that both processes need the same value.

## Migration Plan

1. Set `FRONTEND_PASSWORD` on the stack env (and gateway env for split deploy) before rolling the new image.
2. Remove frontend Keycloak and TURN client env from compose/Docker build args.
3. Leave `AUTH_ENABLE` and JWKS vars as they are for mobile.
4. Rollback: previous image still expects Keycloak in the browser; keep Keycloak client config until the new image is confirmed.

No data migration.

## Open Questions

None. Username, BFF, and JWT-only REST were decided against during explore.
