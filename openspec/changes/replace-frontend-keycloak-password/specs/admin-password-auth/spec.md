## Purpose

Lets operators open the admin UI and call admin REST endpoints with one shared server-side password, without Keycloak in the browser, while mobile JWT auth on the gateway stays in place.

## ADDED Requirements

### Requirement: Shared password is server-side only
The system SHALL treat `FRONTEND_PASSWORD` as a single shared secret loaded from server process environment. The password MUST NOT be published through client-bundled `VITE_*` values or `window.__APP_RUNTIME_ENV__`.

#### Scenario: Password is not in the browser bundle
- **WHEN** an operator loads the admin UI without having logged in
- **THEN** the served JavaScript and runtime env script do not contain `FRONTEND_PASSWORD` or the password value

### Requirement: Admin UI requires the shared password
The admin UI SHALL present a password-only login (no username) and MUST NOT render operations pages until that password matches `FRONTEND_PASSWORD`. A wrong password MUST be rejected without granting access.

#### Scenario: Successful login
- **WHEN** an operator submits the configured `FRONTEND_PASSWORD`
- **THEN** the admin UI shows the operations pages and subsequent `/api/*` calls from that UI include `Authorization: Bearer` with that password

#### Scenario: Wrong password
- **WHEN** an operator submits a password that does not match `FRONTEND_PASSWORD`
- **THEN** the UI stays on the login form and does not call authenticated admin APIs with that value as a valid session

#### Scenario: Logout
- **WHEN** an operator logs out
- **THEN** the UI returns to the login form and later `/api/*` calls from that tab do not send the previous password

### Requirement: Frontend refuses to serve without a password
When the frontend HTTP server starts, it MUST exit non-zero if `FRONTEND_PASSWORD` is missing or empty. Test processes are exempt.

#### Scenario: Production frontend starts without password
- **WHEN** the frontend server process starts with an empty or unset `FRONTEND_PASSWORD`
- **THEN** the process exits non-zero and does not accept HTTP requests

### Requirement: Gateway REST accepts password or JWT
For `/api/*` routes that already require authentication, the gateway SHALL accept either `Authorization: Bearer` matching `FRONTEND_PASSWORD` (constant-time compare) or a JWT that passes the existing JWKS verifier when `AUTH_ENABLE=true`. Unauthenticated GET client-diagnostics list routes that are already public MUST stay public.

#### Scenario: Admin password on REST
- **WHEN** `FRONTEND_PASSWORD` is set and a client calls an authenticated `/api/*` route with `Authorization: Bearer` equal to that password
- **THEN** the gateway accepts the request without verifying a Keycloak JWT

#### Scenario: Mobile JWT on REST still works
- **WHEN** `AUTH_ENABLE=true` and a client calls an authenticated `/api/*` route with a valid Keycloak JWT
- **THEN** the gateway accepts the request even if the bearer is not `FRONTEND_PASSWORD`

#### Scenario: REST locked when password is set
- **WHEN** `FRONTEND_PASSWORD` is set, `AUTH_ENABLE` is false, and a client calls an authenticated `/api/*` route without the matching bearer
- **THEN** the gateway responds `401 Unauthorized`

#### Scenario: JWT still required when password unset and auth enabled
- **WHEN** `FRONTEND_PASSWORD` is empty, `AUTH_ENABLE=true`, and a client calls an authenticated `/api/*` route without a valid JWT
- **THEN** the gateway responds `401 Unauthorized`

### Requirement: WebSocket auth stays JWT-only
When `AUTH_ENABLE=true`, `/ws` MUST continue to require a valid JWT `access_token`. A bearer or query value equal to `FRONTEND_PASSWORD` MUST NOT grant a `/ws` upgrade.

#### Scenario: Admin password cannot open /ws
- **WHEN** `AUTH_ENABLE=true` and a client opens `/ws?access_token=` with `FRONTEND_PASSWORD`
- **THEN** the gateway rejects the upgrade as unauthorized

#### Scenario: Mobile JWT still opens /ws
- **WHEN** `AUTH_ENABLE=true` and a client opens `/ws` with a valid Keycloak JWT
- **THEN** the gateway upgrades the WebSocket as it does today

### Requirement: Admin UI is not bound to Keycloak
The admin UI MUST NOT initialize a Keycloak browser client, MUST NOT require `VITE_KEYCLOAK_*`, and MUST NOT redirect operators to a Keycloak login.

#### Scenario: Admin UI loads without Keycloak
- **WHEN** an operator opens the admin UI with Keycloak URL/realm/client unset
- **THEN** the UI reaches the password login form instead of failing Keycloak initialization or redirecting to an identity provider

### Requirement: In-browser Gateway Console is removed
The admin UI MUST NOT expose a Gateway Console or in-browser softphone route. Operations pages (dashboard, trunks, sessions, and the other existing admin routes besides `/console`) MUST remain available after login.

#### Scenario: Console route is gone
- **WHEN** an authenticated operator requests `/console`
- **THEN** the admin UI does not render a calling console and treats the path as not found

#### Scenario: Operations pages remain
- **WHEN** an operator logs in with `FRONTEND_PASSWORD`
- **THEN** they can open the dashboard and other remaining admin operations pages
