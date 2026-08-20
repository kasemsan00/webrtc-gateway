## Why

The admin UI currently requires TTRS Keycloak SSO (`keycloak-js` plus `VITE_KEYCLOAK_*`) before any operator can open dashboards or manage trunks. That couples a local/self-hosted operations console to an external identity provider. Operators need a single shared password from server env instead, while mobile and `/ws` JWT verification stay on the gateway.

## What Changes

- **BREAKING:** Remove Keycloak from the frontend. No `keycloak-js`, no `VITE_KEYCLOAK_*`, no SSO redirect, no employee JWT obtained in the browser.
- **BREAKING:** Remove the in-browser Gateway Console (`/console`, `features/gateway` softphone, TURN env used only by that page).
- Add a single shared `FRONTEND_PASSWORD` (server env, never a `VITE_` value). The frontend shows a login form; an empty password fails startup rather than leaving the UI open.
- After login, the admin UI sends that same password as `Authorization: Bearer` on `/api/*`. Gateway REST accepts either this password or an existing Keycloak JWT. `/ws` stays JWT-only when `AUTH_ENABLE=true`.
- Keep `AUTH_ENABLE` / JWKS behavior for mobile clients. Push Keycloak client-credentials in the gateway process is unchanged.

## Capabilities

### New Capabilities

- `admin-password-auth`: Operator access to the admin UI and admin REST calls using one shared server-side password, without frontend Keycloak, while gateway JWT auth for `/ws` and mobile REST remains.

### Modified Capabilities

None.

## Impact

- Frontend auth module, root layout, Header login/logout chrome, `http-client` / SSE bearer source.
- Frontend route `/console` and `apps/frontend/src/features/gateway/` (WebRTC console client).
- `keycloak-js` dependency, `VITE_KEYCLOAK_*` and `VITE_TURN_*` frontend env, unified Docker build args.
- Gateway REST `authMiddleware` (additive password check). No `/ws` contract change.
- Deploy env examples, Dockerfile.unified, frontend README.
