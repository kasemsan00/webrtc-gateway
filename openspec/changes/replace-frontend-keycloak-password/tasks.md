## 1. Gateway REST password auth

- [x] 1.1 Load `FRONTEND_PASSWORD` in gateway config and plumb it to the API server.
- [x] 1.2 Enable REST `authMiddleware` when a JWT verifier exists or `FRONTEND_PASSWORD` is non-empty.
- [x] 1.3 Accept a constant-time match of the bearer to `FRONTEND_PASSWORD` with synthetic admin claims, otherwise keep the existing JWT path, and return 401 when neither applies.
- [x] 1.4 Add gateway tests for password-only REST, JWT REST with password also set, 401 when password is set and bearer is wrong, JWT-required when password is unset, unchanged public GET client-diagnostics, and `/ws` rejecting `FRONTEND_PASSWORD` while still accepting JWT.

## 2. Frontend password login

- [x] 2.1 Replace Keycloak runtime/provider with a password-only login that validates via a server function against `process.env.FRONTEND_PASSWORD` and stores the password in `sessionStorage` plus `token-store`.
- [x] 2.2 Show the login form until authenticated, wire Header logout to clear storage and token-store, and drop Keycloak user chrome.
- [x] 2.3 Fail-fast in `docker-server.mjs` when `FRONTEND_PASSWORD` is missing or empty, and keep the password out of `window.__APP_RUNTIME_ENV__` and all `VITE_*` keys.
- [x] 2.4 Add frontend tests for successful login, wrong password, logout clearing the bearer, and Keycloak env no longer being required.

## 3. Remove console and Keycloak client

- [x] 3.1 Delete `/console`, `features/gateway/` softphone code, Header Console link, and frontend TURN usage.
- [x] 3.2 Remove `keycloak-js`, `VITE_KEYCLOAK_*`, and frontend `VITE_TURN_*` from package.json, env examples, Dockerfile.unified, frontend compose, and docker-server runtime keys.
- [x] 3.3 Update frontend README, gateway config reference, and deploy docs for `FRONTEND_PASSWORD` on both frontend and gateway, and note that `AUTH_ENABLE` is unchanged for mobile.

## 4. Verification

- [x] 4.1 Run `go test ./internal/api ./internal/config` from `apps/gateway`.
- [x] 4.2 Run `pnpm --filter frontend run test` from repo root.
- [x] 4.3 Run `pnpm --filter frontend run check-types` or the workspace type-check for frontend if that script exists.
