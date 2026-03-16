# Phase 1: Foundation

**Goal:** Establish the core server infrastructure - a runnable server that can start, discover folder stores, serve CRUD API, handle authentication, and provide basic logging.

---

## Current State
- Only `go.mod` exists at root (module: `kp-cms`)
- Architecture, functional requirements, and API reference fully documented in `.agents/`

---

## Scope
- Server module only (`server/`)
- Standalone run mode only (subprocess mode deferred)
- Folder store type only (JSON, CSV, XLSX deferred)
- Minimal job system (just enough for async store reload)
- No plugins (llama, Google deferred)

---

## Day 1: Project Setup

### 1.1 Create Go Workspace
- Create `go.work` at root linking `server/` and `desktop/` modules

### 1.2 Scaffold Server Module
- Create `server/go.mod` with dependencies:
  - `github.com/go-chi/chi/v5` - HTTP routing
  - `modernc.org/sqlite` - SQLite for auth.db
  - `golang.org/x/sync` - errgroup, singleflight
  - `github.com/fsnotify/fsnotify` - filesystem watching
- Go 1.24.4 (use built-in slog)

### 1.3 Create Entry Point
- Create `server/cmd/server/main.go`
- Flag parsing: `--root`, `--port`, `--mode`, `--appname`
- Load config, initialize logging, start server

### 1.4 Configuration
- Create `server/internal/config/config.go`
- Load from `.config/config.json` in data root
- Environment variable overrides (prefixed with `KP_`)
- Config struct with: port, mode, appname, log level, etc.

---

## Day 1-2: Logging System

### 2.1 Logger Implementation
- Create `server/internal/logging/logger.go`
- Use Go's built-in `slog` for structured JSON logging
- JSON handler writing to files

### 2.2 Log Types
Separate loggers for each type (FR-13):
- `app.log` - lifecycle events
- `access.log` - HTTP requests
- `transaction.log` - mutations
- `audit.log` - security events
- `jobs.log` - background jobs
- `error.log` - warnings and errors

### 2.3 Rotation
- Daily file rotation
- Configurable retention (default 7 days)
- Logs stored in platform config directory (`~/.config/{appname}/logs/`)

---

## Day 2: Store Infrastructure

### 3.1 Store Interface
- Create `server/internal/store/store.go`
- Define `Store` interface with: CRUD methods, ListFiles, UploadFile, DownloadFile, DeleteFile
- Define `Record`, `FileInfo`, `StoreMetadata` types

### 3.2 Mutex Registry
- Create `server/internal/store/mutex.go`
- Record-level locks (folder stores)
- File-level locks (JSON, CSV, XLSX)
- TTL-based eviction for stale locks

### 3.3 Folder Store Implementation
- Create `server/internal/store/folder/store.go`
- Implement Store interface
- Handle `.meta.json` read/write
- Atomic writes using temp file + rename

---

## Day 3: Store Discovery & HTTP Server

### 4.1 Store Discovery
- Scan root directory for `.store.json` files
- Parse store descriptors
- Register stores in memory
- Support nested stores

### 4.2 HTTP Server Setup
- Create `server/internal/server/server.go`
- Use chi router with `/v1/` prefix
- Graceful shutdown handling (SIGINT, SIGTERM)
- CORS configuration for standalone mode

### 4.3 Health Endpoint
- Implement `GET /health`
- Return: startup status, run mode, discovered stores, index readiness

### 4.4 Stores API
- `GET /v1/stores` - list registered stores
- `POST /v1/stores/reload` - trigger store discovery scan (returns 202 with job ID)

---

## Day 4: Record CRUD

### 5.1 Record Endpoints
- `GET /v1/{store}/records` - list with pagination, filters, sort
- `POST /v1/{store}/records` - create record
- `GET /v1/{store}/records/{id}` - read record
- `PUT /v1/{store}/records/{id}` - full replace
- `PATCH /v1/{store}/records/{id}` - partial merge
- `DELETE /v1/{store}/records/{id}` - delete record

### 5.2 Error Handling
- Consistent error envelope format
- Proper HTTP status codes
- Correlation ID tracking

### 5.3 ETag Support
- Add `ETag` header on reads
- Handle `If-Match` for optimistic concurrency

---

## Day 5: Index Layer

### 6.1 In-Memory Index
- Create `server/internal/index/index.go`
- Cache record metadata
- Configurable LRU cap per store

### 6.2 Filesystem Watcher
- Use fsnotify to monitor changes
- Debounce with configurable window (default 500ms)
- Update index on file changes

### 6.3 Index Endpoints
- `GET /v1/stores/{store}/records` uses index for listing
- `POST /v1/index/rebuild` - manual full rebuild

---

## Day 5-6: Minimal Job System

### 7.1 Job Infrastructure
- Create `server/internal/jobs/jobs.go`
- Job types: store_scan, index_rebuild
- Simple in-memory queue
- Job statuses: queued, running, completed, failed

### 7.2 Job Endpoints
- `GET /v1/jobs` - list jobs
- `GET /v1/jobs/{id}` - job status
- `POST /v1/stores/reload` returns 202 with job ID

---

## Day 6-7: Authentication

### 8.1 Auth Database
- Create `server/internal/auth/db.go`
- SQLite database at platform config path
- Tables: users, sessions, roles

### 8.2 Bootstrap Flow
- On first start, if no users exist, create default admin
- Print credentials to stdout in standalone mode
- Store hashed passwords (bcrypt)

### 8.3 Auth Endpoints
- `POST /auth/login` - issue session cookie
- `POST /auth/logout` - invalidate session
- Session validation middleware on protected routes

---

## Day 7: Setup State

### 9.1 Setup State Machine
- Create `server/internal/setup/state.go`
- Track setup completion status
- Persist to platform config directory

### 9.2 Setup Endpoints
- `GET /setup/status` - current state
- `POST /setup/complete` - mark setup finished
- `POST /setup/reset` - reset (admin only)

---

## Dependencies Summary

```
github.com/go-chi/chi/v5       # HTTP routing
github.com/go-chi/cors          # CORS middleware
modernc.org/sqlite             # SQLite (auth.db)
golang.org/x/sync               # errgroup, singleflight
github.com/fsnotify/fsnotify    # Filesystem watching
golang.org/x/crypto/bcrypt       # Password hashing
```

---

## Deliverables

1. Runnable `server` binary that starts with `--root` flag
2. Discovers and registers folder stores from `.store.json`
3. Full CRUD API for folder store records
4. Structured JSON logging with rotation
5. Health check endpoint
6. Basic authentication with session cookies
7. Setup state machine (minimal)
8. Minimal async job support for store reload
