# API Reference
## Go Filesystem Database Server — v1

All routes are prefixed with `/v1/` unless otherwise noted. Routes without a `/v1/` prefix are infrastructure endpoints (health, setup, auth).

---

## Error Response Envelope

All error responses share a consistent JSON shape regardless of which subsystem produced the error:

```json
{
  "error": {
    "code": "RECORD_NOT_FOUND",
    "message": "Human-readable description of the error",
    "details": {},
    "correlation_id": "abc-123"
  }
}
```

| Field | Description |
|---|---|
| `code` | Stable machine-readable string. Never changes between patch versions. |
| `message` | Human-readable explanation. May change between versions. |
| `details` | Structured context. For `422` validation errors, contains per-field violations. |
| `correlation_id` | Matches the `X-Correlation-ID` response header for log tracing. |

---

## HTTP Status Codes

| Code | Meaning |
|---|---|
| `200` | Successful read |
| `201` | Successful create |
| `202` | Accepted — long-running operation started as a background job |
| `204` | Successful delete |
| `206` | Partial content — cross-store query returned results from some stores only |
| `400` | Bad request — malformed input or cancelled by `before-*` hook |
| `401` | Unauthorized — missing or invalid session |
| `403` | Forbidden — authenticated but insufficient role |
| `404` | Record or resource not found |
| `405` | Method not allowed — endpoint not supported by this store type |
| `409` | Conflict — duplicate record, lock timeout, or schema mismatch |
| `413` | Content too large — XLSX file exceeds configured size limit |
| `422` | Unprocessable entity — schema validation failed |
| `501` | Not implemented — plugin not configured |
| `502` | Bad gateway — hook timed out |
| `503` | Service unavailable — write queue full, store registering, or setup incomplete |

---

## Infrastructure Endpoints

### Health
`GET /health`
Returns server status, run mode, discovered stores, and index readiness.

### Setup
`GET /setup/status` — current state of all setup steps
`GET /setup/steps` — registered setup steps and their states
`POST /setup/llama` — configure llama.cpp integration
`POST /setup/google` — configure Google Cloud integration
`POST /setup/complete` — mark setup as finished
`POST /setup/reset` — reset setup state (admin only)

### Auth
`POST /auth/login` — issue session cookie
`POST /auth/logout` — invalidate session
`GET /auth/users` — list users (admin)
`POST /auth/users` — create user (admin)
`PATCH /auth/users/{id}` — update user (admin)
`DELETE /auth/users/{id}` — delete user (admin)
`GET /auth/users/{id}/stores` — list per-store role overrides
`POST /auth/users/{id}/stores/{store}` — assign per-store role
`DELETE /auth/users/{id}/stores/{store}` — remove per-store role override

### Configuration
`GET /config` — current resolved configuration (sensitive values redacted)

### Logs
`GET /logs` — list log files by type, date, size (admin)
`DELETE /logs/audit` — purge audit log (admin, itself produces an audit entry)

### Jobs
`GET /jobs` — list active and recent jobs
`GET /jobs/{job-id}` — job status and progress
`DELETE /jobs/{job-id}` — cancel job

### Backups
`GET /backups` — list backup archives
`POST /backups` — trigger backup (`scope={store-name}` or `scope=all`)
`POST /backups/{backup-name}/restore` — initiate restore

### Stores
`GET /v1/stores` — list registered stores
`POST /v1/stores/reload` — trigger store discovery scan

---

## Store Endpoints

### Records
`GET /v1/{store}/records` — list records (supports `limit`, `offset`, filter, sort, search params)
`POST /v1/{store}/records` — create record
`GET /v1/{store}/records/{id}` — read record
`PUT /v1/{store}/records/{id}` — full replace
`PATCH /v1/{store}/records/{id}` — partial merge
`DELETE /v1/{store}/records/{id}` — delete record

### Files (Folder Store only)
`GET /v1/{store}/records/{id}/files` — list files
`POST /v1/{store}/records/{id}/files` — upload files (multipart)
`GET /v1/{store}/records/{id}/files/{filename}` — download file (supports `Range` header)
`DELETE /v1/{store}/records/{id}/files/{filename}` — delete file

### Cross-Store Query
`GET /v1/query` — query across multiple stores (index-only, supports timeout and result cap)

---

## Plugin Endpoints

### llama.cpp
`GET /llama/models` — list registered models
`POST /llama/models` — register a model
`POST /llama/models/{name}/activate` — set active model
`DELETE /llama/models/{name}` — deregister model
`POST /llama/generate` — text generation (streaming)
`POST /llama/chat` — chat completion (streaming)

### Google Cloud
`GET /google/auth` — initiate OAuth flow
`GET /google/auth/callback` — OAuth callback
`DELETE /google/auth` — revoke Google authorisation
`GET /google/status` — connection status
`GET /google/calendars` — list user's calendars
`GET /google/calendar/events` — list events (time range)
`POST /google/calendar/events` — create event
`PATCH /google/calendar/events/{event-id}` — update event
`DELETE /google/calendar/events/{event-id}` — delete event
`GET /google/drive/sync/status` — Drive sync status per store
`POST /google/drive/sync` — trigger Drive sync
