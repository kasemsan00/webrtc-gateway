# Trunk API Documentation

Base URL: `http://<host>:<API_PORT>/api`

> Requires `API_ENABLE_REST=true` (default `true`). Trunk management routes require `SIP_TRUNK_ENABLE=true` with PostgreSQL enabled (`DB_ENABLE=true`), otherwise they return `503 Service Unavailable`.

---

## Authentication

- Whenever `AUTH_ENABLE=true` **or** `FRONTEND_PASSWORD` is non-empty, all routes in this document require an HTTP bearer token:
  - `Authorization: Bearer <jwt>` — verified against the configured user/employee JWKS realms (`30s` clock-skew leeway). Optional realm hints are accepted via `?auth_type=user|users|client|employee|employees|agent` (recommended for browser cross-origin requests because `X-Auth-Type` is not in CORS `Access-Control-Allow-Headers`) or the `X-Auth-Type` header for non-browser clients.
  - `Authorization: Bearer <FRONTEND_PASSWORD>` — exact match against `FRONTEND_PASSWORD` when configured (enforced on REST even when `AUTH_ENABLE=false`).
- Missing or invalid credentials return `401 Unauthorized`.
- `PUT /api/user/trunk` **always** requires authenticated claims in the request context (`401` if REST auth is completely disabled and no claims are present) and a non-empty `preferred_username` claim (`400` if empty; `FRONTEND_PASSWORD` sets `preferred_username` to `"admin"`).

---

## 1. List Trunks (Paginated)

```
GET /api/trunks
```

### Query Parameters

| Parameter       | Type   | Default | Description                                                                                                                                             |
| --------------- | ------ | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `page`          | int    | `1`     | Page number (`1`-based; values `< 1` or non-numeric fall back to `1`)                                                                                   |
| `pageSize`      | int    | `20`    | Items per page (values `< 1` or non-numeric fall back to `20`; values `> 100` clamp to `100`)                                                           |
| `trunkId`       | int64  | —       | Filter by exact trunk ID (`> 0`; takes precedence over `trunkPublicId` when both are provided; non-positive/non-numeric values are ignored)             |
| `trunkPublicId` | UUID   | —       | Filter by trunk public ID (`400 Invalid trunk public ID` if non-empty and not a valid UUID; ignored when `trunkId > 0` is supplied)                     |
| `search`        | string | —       | Case-insensitive substring (`ILIKE`) match on `username` or `name`                                                                                      |
| `createdAfter`  | string | —       | Inclusive lower bound on `created_at` (RFC 3339; malformed timestamps are ignored)                                                                      |
| `createdBefore` | string | —       | Inclusive upper bound on `created_at` (RFC 3339; malformed timestamps are ignored)                                                                      |
| `sortBy`        | string | `id`    | `id`, `name`, `domain`, `port`, `username`, `enabled`, `created_at`, `updated_at`; other values fall back to `id` (`activeCallCount` is not DB-sortable) |
| `sortDir`       | string | `asc`   | `asc` or `desc` (case-insensitive; any value other than `desc` sorts `ASC`)                                                                             |

### Examples

#### Basic pagination

```
GET /api/trunks?page=1&pageSize=10
```

#### Search by username or name

```
GET /api/trunks?search=agent01
```

#### Filter by trunk ID or public UUID

```
GET /api/trunks?trunkId=5
GET /api/trunks?trunkPublicId=0195f3e0-7c2a-7b00-9a10-123456789abc
```

#### Filter by creation time range

```
GET /api/trunks?createdAfter=2025-01-01T00:00:00Z&createdBefore=2026-01-01T00:00:00Z
```

#### Combined filters

```
GET /api/trunks?search=agent&page=1&pageSize=5&createdAfter=2025-06-01T00:00:00Z
```

### Response

Optional fields tagged `omitempty` are omitted when empty rather than returned as `""`.

```json
{
  "items": [
    {
      "id": 1,
      "public_id": "0195f3e0-7c2a-7b00-9a10-123456789abc",
      "publicId": "0195f3e0-7c2a-7b00-9a10-123456789abc",
      "name": "Main Trunk",
      "domain": "sip.example.com",
      "port": 5060,
      "username": "agent01",
      "transport": "udp",
      "enabled": true,
      "isDefault": true,
      "activeCallCount": 2,
      "activeDestinations": ["sip:1669@sip.example.com", "sip:1002@sip.example.com"],
      "leaseOwner": "gw-instance-abc",
      "leaseUntil": "2025-07-01T12:30:00Z",
      "lastRegisteredAt": "2025-07-01T12:00:00Z",
      "isRegistered": true,
      "sipAutoRegister": true,
      "inUseBy": "agent01",
      "pushContactReady": false,
      "fcmTokenReady": false,
      "notifyUserBound": true,
      "createdAt": "2025-01-15T08:00:00Z",
      "updatedAt": "2025-07-01T12:00:00Z"
    },
    {
      "id": 2,
      "public_id": "0195f3e1-4a90-7c12-8b01-abcdef012345",
      "publicId": "0195f3e1-4a90-7c12-8b01-abcdef012345",
      "name": "Backup Trunk",
      "domain": "sip2.example.com",
      "port": 5060,
      "username": "agent02",
      "transport": "tcp",
      "enabled": true,
      "isDefault": false,
      "activeCallCount": 0,
      "lastUnregisteredAt": "2025-06-30T10:00:00Z",
      "isRegistered": false,
      "sipAutoRegister": false,
      "pushContactReady": false,
      "fcmTokenReady": false,
      "notifyUserBound": false,
      "createdAt": "2025-03-20T09:00:00Z",
      "updatedAt": "2025-06-30T10:00:00Z"
    }
  ],
  "total": 2,
  "page": 1,
  "pageSize": 10
}
```

### Response Fields

| Field      | Type  | Description                                 |
| ---------- | ----- | ------------------------------------------- |
| `items`    | array | List of trunk objects for the current page  |
| `total`    | int   | Total number of trunks matching the filters |
| `page`     | int   | Current page number                         |
| `pageSize` | int   | Number of items per page                    |

### Trunk Object Fields (`TrunkResponse`)

| Field                | Type     | Presence    | Description                                                                                                                          |
| -------------------- | -------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `id`                 | int64    | always      | Internal numeric trunk ID                                                                                                            |
| `public_id`          | string   | always      | Public UUID identifier (snake_case)                                                                                                  |
| `publicId`           | string   | always      | Same public UUID identifier (camelCase compatibility alias)                                                                          |
| `name`               | string   | always      | Trunk display name                                                                                                                   |
| `domain`             | string   | always      | SIP domain / host                                                                                                                    |
| `port`               | int      | always      | SIP port                                                                                                                             |
| `username`           | string   | always      | SIP username                                                                                                                         |
| `transport`          | string   | always      | SIP transport protocol (`tcp` or `udp`)                                                                                              |
| `enabled`            | bool     | always      | Whether the trunk is enabled (`false` = soft-deleted)                                                                                |
| `isDefault`          | bool     | always      | Whether this is the default trunk                                                                                                    |
| `activeCallCount`    | int      | always      | Number of non-ended calls on this gateway instance currently using this trunk                                                        |
| `activeDestinations` | []string | `omitempty` | Non-empty `to` URIs/destinations of active calls on this trunk                                                                       |
| `leaseOwner`         | string   | `omitempty` | Gateway `instanceId` that currently holds the registration lease                                                                     |
| `leaseUntil`         | string   | `omitempty` | Lease expiry time (RFC 3339)                                                                                                         |
| `lastRegisteredAt`   | string   | `omitempty` | Last successful SIP REGISTER time (RFC 3339)                                                                                         |
| `lastUnregisteredAt` | string   | `omitempty` | Last SIP UNREGISTER time (RFC 3339)                                                                                                  |
| `isRegistered`       | bool     | always      | Computed as `lastRegisteredAt != nil && (lastError == nil \|\| lastError == "")`                                                     |
| `lastError`          | string   | `omitempty` | Last SIP registration error message                                                                                                  |
| `sipAutoRegister`    | bool     | always      | Restore SIP registration on gateway start/`POST /api/trunks/refresh`; `false` for newly created trunks and after explicit Unregister |
| `inUseBy`            | string   | `omitempty` | Username/subject currently recorded as actively using this trunk                                                                     |
| `lastOnlinePlatform` | string   | `omitempty` | Latest client platform recorded for this trunk (`ios` or `android`)                                                                  |
| `lastOnlineAt`       | string   | `omitempty` | Timestamp when `lastOnlinePlatform` was last updated (RFC 3339)                                                                      |
| `pnAppId`            | string   | `omitempty` | SIP Contact PushKit `app-id` (e.g. `th.or.ttrs.video.prod`)                                                                          |
| `pnType`             | string   | `omitempty` | SIP Contact push provider type (e.g. `apple`)                                                                                        |
| `pnTokenMasked`      | string   | `omitempty` | Masked PushKit token (`first6...last6`, or `****` when length ≤ 12)                                                                  |
| `pnUpdatedAt`        | string   | `omitempty` | Timestamp when PushKit contact fields were last updated (RFC 3339)                                                                   |
| `pushContactReady`   | bool     | always      | `true` when `pnAppId`, `pnType`, and `pnTokenMasked` are all non-empty                                                               |
| `fcmTokenReady`      | bool     | always      | `true` when a stored device FCM token (`fcmTokenMasked`) is present                                                                  |
| `fcmTokenMasked`     | string   | `omitempty` | Masked `/ws-agent-device` FCM token (`first6...last6`, or `****` when length ≤ 12)                                                   |
| `fcmUpdatedAt`       | string   | `omitempty` | Timestamp when the stored FCM token was last updated (RFC 3339)                                                                      |
| `notifyUserBound`    | bool     | always      | `true` when `notify_user_id` is non-empty (bound for incoming push lookup/routing)                                                   |
| `createdAt`          | string   | always      | Trunk creation time (RFC 3339)                                                                                                       |
| `updatedAt`          | string   | always      | Last update time (RFC 3339)                                                                                                          |

> SIP passwords, raw push tokens, and raw `notify_user_id` values are never exposed in `TrunkResponse`.

---

## 2. Get Trunk by ID

```
GET /api/trunk/{id}
```

### Path Parameters

| Parameter | Type  | Description            |
| --------- | ----- | ---------------------- |
| `id`      | int64 | Trunk ID (`> 0`)       |

### Example

```
GET /api/trunk/5
```

### Response (`200 OK`)

```json
{
  "id": 5,
  "public_id": "0195f3e2-1111-7000-8000-000000000005",
  "publicId": "0195f3e2-1111-7000-8000-000000000005",
  "name": "Office Trunk",
  "domain": "pbx.office.com",
  "port": 5060,
  "username": "trunk-office",
  "transport": "udp",
  "enabled": true,
  "isDefault": false,
  "activeCallCount": 1,
  "activeDestinations": ["sip:00025@pbx.office.com"],
  "leaseOwner": "gw-instance-abc",
  "leaseUntil": "2025-07-01T12:30:00Z",
  "lastRegisteredAt": "2025-07-01T12:00:00Z",
  "isRegistered": true,
  "sipAutoRegister": true,
  "pushContactReady": false,
  "fcmTokenReady": false,
  "notifyUserBound": false,
  "createdAt": "2025-02-10T14:00:00Z",
  "updatedAt": "2025-07-01T12:00:00Z"
}
```

### Error Response (`404 Not Found`)

```json
{
  "error": "Trunk not found: trunk 5 not found"
}
```

---

## 3. Create Trunk

Creates a new SIP trunk row in `sip_trunks` (with `sipAutoRegister: false` until registered).

```
POST /api/trunks
```

### Request Body

| Field       | Type   | Required | Default | Description                                                          |
| ----------- | ------ | -------- | ------- | -------------------------------------------------------------------- |
| `name`      | string | yes      | —       | Display name (trimmed, non-empty)                                    |
| `domain`    | string | yes      | —       | SIP registrar/proxy domain or IP (trimmed, non-empty)                |
| `port`      | int    | no       | `5060`  | SIP port (`< 1` or `> 65535` defaults to `5060`)                     |
| `username`  | string | yes      | —       | SIP username (trimmed, non-empty)                                    |
| `password`  | string | yes      | —       | SIP password (trimmed, non-empty)                                    |
| `transport` | string | no       | `"tcp"` | `"tcp"` or `"udp"` (any other/empty value normalizes to `"tcp"`)     |
| `enabled`   | bool   | no       | `true`  | Initial enabled state                                                |
| `isDefault` | bool   | no       | `false` | Whether to mark this trunk as default (clears previous default)      |

```json
{
  "name": "Agent 03",
  "domain": "sip.example.com",
  "port": 5060,
  "username": "agent03",
  "password": "secret",
  "transport": "tcp",
  "enabled": true,
  "isDefault": false
}
```

### Response (`201 Created`)

Returns the newly created `TrunkResponse` object and emits a `created` SSE event on `/api/trunks/stream`. Duplicate trunk identity conflicts return `409 Conflict`; validation failures return `400 Bad Request`.

---

## 4. Update Trunk, Soft Delete (Disable), and Restore

Updates a trunk by ID using partial patch semantics (`nil`/omitted fields keep their existing values). Trunks are **never** hard-deleted from `sip_trunks`; use `{"enabled": false}` to soft-delete and `{"enabled": true}` to restore.

```
PUT /api/trunk/{id}
```

### Request Body (all fields optional)

| Field       | Type   | Validation / Behavior                                                                 |
| ----------- | ------ | ------------------------------------------------------------------------------------- |
| `name`      | string | If present, trimmed and cannot be empty (`400`)                                       |
| `domain`    | string | If present, trimmed and cannot be empty (`400`); rejected with `409` if calls active  |
| `port`      | int    | If present, must be `1..65535` (`400`); rejected with `409` if calls active           |
| `username`  | string | If present, trimmed and cannot be empty (`400`); rejected with `409` if calls active  |
| `password`  | string | If present and blank/whitespace-only, treated as **no password change**               |
| `transport` | string | If present, must be `"tcp"` or `"udp"` (`400`); rejected with `409` if calls active   |
| `enabled`   | bool   | `false` soft-disables (rejected with `409` if calls active); `true` restores          |
| `isDefault` | bool   | Marks/unmarks default trunk                                                           |
| `updatedBy` | string | Optional audit actor recorded in the `rest_trunk_updated` event                       |

### Soft delete (disable)

```json
{
  "enabled": false
}
```

### Restore

```json
{
  "enabled": true
}
```

### Response (`200 OK`)

Returns the updated `TrunkResponse` and broadcasts an `updated` SSE event on `/api/trunks/stream`.

### Notes

- While `activeCallCount > 0` on the trunk, disabling (`enabled: false`) or modifying `domain`, `port`, `username`, or `transport` is rejected with `409 Conflict`.
- On successful disable, the gateway performs best-effort SIP UNREGISTER and lease release.
- Missing trunk returns `404 Not Found`; duplicate identity conflict returns `409 Conflict`.

---

## 5. Refresh Trunks

Reloads all `enabled = true` trunks from the database into memory (`RefreshTrunks()`) and attempts lease acquisition + SIP REGISTER **only** for trunks that have both `enabled = true` and `sip_auto_register = true`.

```
POST /api/trunks/refresh
```

### Request Body

None required.

### Notes

- Trunks with `sip_auto_register = false` (newly created trunks that have never been registered, or trunks explicitly unregistered) are loaded into memory for lookup/resolve but are **skipped** for automatic SIP REGISTER.
- Individual registrar/credential failures during refresh are logged and recorded on the trunk without failing the HTTP `200` reload response.

### Response (`200 OK`)

```json
{
  "status": "refreshed",
  "message": "Trunk list reloaded from database"
}
```

### Response (`500` / `503`)

```json
{
  "error": "Failed to refresh trunks: database not available for trunk manager"
}
```

---

## 6. Register Trunk

Forces lease ownership to this gateway instance, sends SIP REGISTER, and sets `sip_auto_register = true`.

```
POST /api/trunk/{id}/register
```

### Path Parameters

| Parameter | Type  | Description      |
| --------- | ----- | ---------------- |
| `id`      | int64 | Trunk ID (`> 0`) |

### Response (`200 OK`)

```json
{
  "trunkId": 5,
  "status": "registered"
}
```

### Response (`500 Internal Server Error`)

```json
{
  "error": "Failed to register trunk: REGISTER failed: 401 Unauthorized"
}
```

---

## 7. Unregister Trunk

Sends SIP REGISTER with `Expires: 0`, releases the lease, and sets `sip_auto_register = false`.

```
POST /api/trunk/{id}/unregister
```

### Path Parameters

| Parameter | Type  | Description      |
| --------- | ----- | ---------------- |
| `id`      | int64 | Trunk ID (`> 0`) |

### Response (`200 OK`)

```json
{
  "trunkId": 5,
  "status": "unregistered"
}
```

### Response (`500 Internal Server Error`)

```json
{
  "error": "Failed to unregister trunk: trunk 5 not found"
}
```

---

## 8. Stream Trunk Changes (SSE)

Server-Sent Events (`text/event-stream`) stream notifying admin UIs when trunks change.

```
GET /api/trunks/stream
```

### SSE Events

- `event: connected` — sent immediately on connect:
  ```json
  {"type": "connected", "at": "2025-07-01T12:00:00.123456789Z"}
  ```
- `event: trunk` — sent when a trunk is created, updated, refreshed, registered, unregistered, or changes registration state in `TrunkManager`:
  ```json
  {"type": "updated", "trunkId": 5, "at": "2025-07-01T12:01:02.345678901Z"}
  ```
  (`trunkId` is omitted on bulk events such as `refreshed`.)
- `event: heartbeat` — sent every `25s` to keep proxies/connections alive:
  ```json
  {"type": "heartbeat", "at": "2025-07-01T12:00:25.123456789Z"}
  ```

---

## 9. Test Incoming Push

Sends a synthetic `incoming_call` push payload for a trunk without creating a SIP or WebRTC session or emitting WebSocket `incoming` (answering on the device will expire).

```
POST /api/trunk/{id}/test-incoming-push
```

### Request Body (optional)

| Field   | Type   | Default  | Description                                                                                      |
| ------- | ------ | -------- | ------------------------------------------------------------------------------------------------ |
| `style` | string | `"data"` | `"data"` (Android data-only VoIP wake) or `"message"` (also includes visible notification alert) |

```json
{
  "style": "data"
}
```

### Routing & Response (`200 OK`)

If the trunk has a stored `/ws-agent-device` FCM token, sends directly via FCM (`channels: ["fcm"]`); otherwise routes via `selectIncomingPushRoute` using stored APNs PushKit (`"apns"`) and/or TTRS Notification API FCM (`"ttrs_fcm"`).

```json
{
  "trunkId": 5,
  "sessionId": "push-test-8c4f1d2e-9b10-4e55-a123-456789abcdef",
  "channels": ["ttrs_fcm"],
  "style": "data",
  "status": "sent"
}
```

### Error Statuses

- `400 Bad Request` — invalid trunk ID, malformed JSON, or unsupported `style`
- `404 Not Found` — trunk does not exist
- `409 Conflict` — `Trunk has no incoming push target`
- `502 Bad Gateway` — upstream push delivery failed (`Failed to send test push: ...`)
- `503 Service Unavailable` — trunk manager, push service (`PUSH_ENABLE=false`), or required push sender is not configured

---

## 10. User Trunk Heartbeat

Checks that a trunk is currently assigned (`in_use_by`) to the authenticated caller's `preferred_username` and refreshes its `updated_at` timestamp.

```
PUT /api/user/trunk
```

### Request Body

None required. Requires authenticated claims (`401` if absent) containing a non-empty `preferred_username` (`400` if missing).

### Response (`200 OK`)

```json
{
  "trunkId": 1,
  "publicId": "0195f3e0-7c2a-7b00-9a10-123456789abc",
  "name": "Main Trunk",
  "domain": "sip.example.com",
  "port": 5060,
  "username": "agent01",
  "inUseBy": "agent01",
  "updatedAt": "2025-07-01T12:05:00Z"
}
```

Returns `404 Not Found` (`No trunk assigned to user <preferred_username>`) and logs an alert if no trunk has `in_use_by = <preferred_username>`.

---

## Error Format

All error responses use the same structure:

```json
{
  "error": "<error message>"
}
```

Common HTTP status codes:

| Code | Meaning                                                                 |
| ---- | ----------------------------------------------------------------------- |
| 200  | Success                                                                 |
| 201  | Created (`POST /api/trunks`)                                            |
| 400  | Bad request (invalid trunk ID/UUID, missing params, missing claim)      |
| 401  | Unauthorized (missing or invalid bearer token / auth claims)            |
| 404  | Trunk not found (or no trunk assigned to user)                          |
| 409  | Conflict (duplicate trunk, active calls block update/disable, no push target) |
| 500  | Internal server error (DB failure, SIP REGISTER/UNREGISTER failure)     |
| 502  | Bad gateway (upstream FCM/APNs/TTRS test push send failure)             |
| 503  | Service unavailable (trunk manager disabled/no DB, or push disabled)    |
