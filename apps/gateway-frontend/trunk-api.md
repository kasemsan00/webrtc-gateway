# Trunk API Documentation

Base URL: `http://<host>:<API_PORT>/api`

---

## 1. List Trunks (Paginated)

```
GET /api/trunks
```

### Query Parameters

| Parameter       | Type   | Default | Description                                       |
| --------------- | ------ | ------- | ------------------------------------------------- |
| `page`          | int    | 1       | Page number (1-based)                             |
| `pageSize`      | int    | 20      | Items per page (max 100)                          |
| `trunkId`       | int64  | —       | Filter by exact trunk ID                          |
| `search`        | string | —       | Search by username or name (case-insensitive)     |
| `createdAfter`  | string | —       | Filter trunks created after this time (RFC 3339)  |
| `createdBefore` | string | —       | Filter trunks created before this time (RFC 3339) |

### Examples

#### Basic pagination

```
GET /api/trunks?page=1&pageSize=10
```

#### Search by username or name

```
GET /api/trunks?search=agent01
```

#### Filter by trunk ID

```
GET /api/trunks?trunkId=5
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

```json
{
  "items": [
    {
      "id": 1,
      "name": "Main Trunk",
      "domain": "sip.example.com",
      "port": 5060,
      "username": "agent01",
      "transport": "udp",
      "enabled": true,
      "isDefault": true,
      "activeCallCount": 2,
      "activeDestinations": ["0891112222", "021234567"],
      "leaseOwner": "gw-instance-abc",
      "leaseUntil": "2025-07-01T12:30:00Z",
      "lastRegisteredAt": "2025-07-01T12:00:00Z",
      "lastError": "",
      "createdAt": "2025-01-15T08:00:00Z",
      "updatedAt": "2025-07-01T12:00:00Z"
    },
    {
      "id": 2,
      "name": "Backup Trunk",
      "domain": "sip2.example.com",
      "port": 5060,
      "username": "agent02",
      "transport": "tcp",
      "enabled": true,
      "isDefault": false,
      "activeCallCount": 0,
      "activeDestinations": [],
      "leaseOwner": "",
      "leaseUntil": "",
      "lastRegisteredAt": "2025-06-30T10:00:00Z",
      "lastError": "",
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

### Trunk Object Fields

| Field              | Type   | Description                                          |
| ------------------ | ------ | ---------------------------------------------------- |
| `id`               | int64  | Trunk ID                                             |
| `name`             | string | Trunk display name                                   |
| `domain`           | string | SIP domain / host                                    |
| `port`             | int    | SIP port                                             |
| `username`         | string | SIP username                                         |
| `transport`        | string | Transport protocol (`udp` or `tcp`)                  |
| `enabled`          | bool   | Whether the trunk is enabled                         |
| `isDefault`        | bool   | Whether this is the default trunk                    |
| `activeCallCount`  | int    | Number of active calls currently using this trunk    |
| `activeDestinations` | array of string | Destination values of active calls on this trunk |
| `leaseOwner`       | string | Gateway instance ID that owns the registration lease |
| `leaseUntil`       | string | Lease expiry time (RFC 3339), empty if no lease      |
| `lastRegisteredAt` | string | Last successful SIP REGISTER time (RFC 3339)         |
| `lastError`        | string | Last registration error message, empty if none       |
| `createdAt`        | string | Trunk creation time (RFC 3339)                       |
| `updatedAt`        | string | Last update time (RFC 3339)                          |

---

## 2. Get Trunk by ID

```
GET /api/trunk/{id}
```

### Path Parameters

| Parameter | Type  | Description |
| --------- | ----- | ----------- |
| `id`      | int64 | Trunk ID    |

### Example

```
GET /api/trunk/5
```

### Response

```json
{
  "id": 5,
  "name": "Office Trunk",
  "domain": "pbx.office.com",
  "port": 5060,
  "username": "trunk-office",
  "transport": "udp",
  "enabled": true,
  "isDefault": false,
  "activeCallCount": 1,
  "leaseOwner": "gw-instance-abc",
  "leaseUntil": "2025-07-01T12:30:00Z",
  "lastRegisteredAt": "2025-07-01T12:00:00Z",
  "lastError": "",
  "createdAt": "2025-02-10T14:00:00Z",
  "updatedAt": "2025-07-01T12:00:00Z"
}
```

### Error Response (trunk not found)

```json
{
  "error": "Trunk not found: trunk 5 not found"
}
```

---

## 3. Refresh Trunks

Triggers a reload of all trunks from the database and re-acquires leases / re-registers.

```
POST /api/trunks/refresh
```

### Request Body

None required.

### Example

```
POST /api/trunks/refresh
```

### Response (success)

```json
{
  "status": "refreshed"
}
```

### Response (error)

```json
{
  "error": "Failed to refresh trunks: database not available for trunk manager"
}
```

---

## 4. Unregister Trunk

Sends SIP REGISTER with `Expires: 0` and releases the lease.

```
POST /api/trunk/{id}/unregister
```

### Path Parameters

| Parameter | Type  | Description |
| --------- | ----- | ----------- |
| `id`      | int64 | Trunk ID    |

### Example

```
POST /api/trunk/5/unregister
```

### Response (success)

```json
{
  "trunkId": 5,
  "status": "unregistered"
}
```

### Response (error)

```json
{
  "error": "Failed to unregister trunk: trunk 5 not found"
}
```

---

## 5. Delete Trunk

Hard-deletes a trunk from the database. Also sends SIP unregister (best-effort).

```
DELETE /api/trunk/{id}
```

### Path Parameters

| Parameter | Type  | Description |
| --------- | ----- | ----------- |
| `id`      | int64 | Trunk ID    |

### Example

```
DELETE /api/trunk/5
```

### Response (success)

```json
{
  "trunkId": 5,
  "status": "deleted"
}
```

### Response (error)

```json
{
  "error": "Failed to delete trunk: trunk 5 not found"
}
```

---

## Error Format

All error responses use the same structure:

```json
{
  "error": "<error message>"
}
```

Common HTTP status codes:

| Code | Meaning                                         |
| ---- | ----------------------------------------------- |
| 200  | Success                                         |
| 400  | Bad request (invalid trunk ID, missing params)  |
| 404  | Trunk not found                                 |
| 500  | Internal server error (DB failure, SIP error)   |
| 503  | Trunk manager not available (disabled or no DB) |
