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

---

## 2. Get Trunk by ID

```
GET /api/trunk/{id}
```

### Path Parameters

| Parameter | Type  | Description |
| --------- | ----- | ----------- |
| `id`      | int64 | Trunk ID    |

---

## 3. Refresh Trunks

```
POST /api/trunks/refresh
```

### Response (success)

```json
{
  "status": "refreshed"
}
```

---

## 4. Register Trunk

```
POST /api/trunk/{id}/register
```

### Response (success)

```json
{
  "trunkId": 5,
  "status": "registered"
}
```

---

## 5. Unregister Trunk

```
POST /api/trunk/{id}/unregister
```

### Response (success)

```json
{
  "trunkId": 5,
  "status": "unregistered"
}
```

---

## 6. Soft Delete Trunk (Disable)

Soft delete keeps trunk records in `sip_trunks` and marks trunk as disabled.

```
PUT /api/trunk/{id}
```

### Request Body

```json
{
  "enabled": false
}
```

### Response (success)

```json
{
  "id": 5,
  "enabled": false
}
```

---

## 7. Restore Trunk

Restoring is done by enabling the trunk again.

```
PUT /api/trunk/{id}
```

### Request Body

```json
{
  "enabled": true
}
```

### Response (success)

```json
{
  "id": 5,
  "enabled": true
}
```

---

## Error Format

All error responses use this structure:

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
| 409  | Conflict (e.g. disable blocked by active calls) |
| 500  | Internal server error (DB failure, SIP error)   |
| 503  | Trunk manager not available (disabled or no DB) |
