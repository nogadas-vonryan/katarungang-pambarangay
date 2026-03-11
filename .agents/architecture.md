# Architecture & Project Structure
## Go Filesystem Database Server + Wails Desktop Application

---

## 1. Monorepo Layout

The project is a monorepo containing two Go modules linked via a Go workspace file.

```
/
├── server/          # Go module — filesystem database server
├── desktop/         # Go module — Wails desktop application
├── go.work          # Go workspace linking both modules for local development
└── README.md
```

The two modules shall have no circular dependencies. `desktop` may import exported packages from `server`. `server` shall never import anything from `desktop`.

They are linked via `go.work` so `desktop` can import `server` locally without requiring a published module registry.

---

## 2. Server Module (`server/`)

```
server/
├── cmd/
│   └── server/
│       └── main.go        # entrypoint: flag parsing, run mode, wiring
├── internal/
│   ├── store/             # Store interface + shared types
│   │   ├── store.go
│   │   ├── folder/        # Folder store implementation
│   │   ├── json/          # JSON store implementation
│   │   ├── csv/           # CSV store implementation
│   │   └── xlsx/          # XLSX store implementation
│   ├── index/             # In-memory index + fsnotify watcher
│   ├── jobs/              # Background job system
│   ├── hooks/             # Lifecycle hook registry
│   ├── auth/              # Authentication + RBAC + auth.db
│   ├── backup/            # Backup creation + restore logic
│   ├── naming/            # Record naming patterns + ID generation
│   ├── config/            # Config loading + environment variable overrides
│   └── logging/           # Structured log writers (app, access, tx, audit, etc.)
├── plugins/
│   ├── llama/             # llama.cpp subprocess management + inference API
│   └── google/            # OAuth 2.0, Google Calendar, Google Drive sync
├── setup/                 # Setup wizard state machine + setup API handlers
├── api/
│   └── router.go          # HTTP route registration
└── go.mod
```

### Package Visibility Rules

All packages under `server/internal/` are private to the `server` module. No package outside `server/` may import them directly. The only importable surface from `desktop` are packages explicitly placed outside `internal/`.

Each store type is implemented in its own sub-package under `server/internal/store/`, all satisfying the common `Store` interface defined in `server/internal/store/store.go`.

### Plugin Package Placement

Plugins (`llama`, `google`) live under `server/plugins/` rather than `internal/` so they can be compiled out or extracted into separate modules in future without restructuring the core server.

The `server/setup/` package is outside `internal/` because plugins need to register setup steps against it during initialisation. Placing it inside `internal/` would prevent plugins from accessing it.

---

## 3. Desktop Module (`desktop/`)

```
desktop/
├── frontend/              # Vue 3 application (Wails frontend)
│   ├── src/
│   │   ├── views/         # Page-level Vue components (panel, setup wizard, etc.)
│   │   ├── components/    # Reusable UI components
│   │   ├── stores/        # Pinia state stores
│   │   ├── composables/   # Vue composables
│   │   ├── router/        # Vue Router configuration
│   │   └── main.ts        # Frontend entrypoint
│   └── package.json
├── internal/
│   └── bridge/            # Server subprocess lifecycle management
│       ├── spawn.go       # Spawns server binary, reads stdout ready signal
│       ├── proxy.go       # HTTP proxy from Wails frontend to server
│       └── shutdown.go    # Graceful subprocess shutdown on app exit
├── app.go                 # Wails app struct + bound methods
├── main.go                # Wails entrypoint
└── go.mod
```

### Frontend

The Wails frontend is a Vue 3 application located at `desktop/frontend/`. It communicates with the server exclusively over HTTP via the Wails proxy. No direct Go bindings are used for data operations; all data flows through the server's REST API.

The setup wizard and the main application panel are implemented as distinct routes within the Vue application (`/setup` and `/panel` respectively), not as separate build targets or top-level directories.

### Bridge Package

`desktop/internal/bridge/` owns all subprocess lifecycle logic: spawning the server binary, reading and parsing the stdout ready signal (`{"status":"ready","port":<port>}`), configuring the HTTP proxy to route frontend requests to the server's bound port, and shutting the subprocess down cleanly when the Wails app exits.

### Wails Bindings

Wails-bound Go methods in `app.go` are limited to desktop-specific concerns only (e.g. opening a native file picker, reading the local OS theme, requesting notification permissions). All business logic remains in the server and is accessed via HTTP.

---

## 4. Store Implementation Notes

### Single-File Stores (JSON, CSV, XLSX)

JSON, CSV, and XLSX stores each store all records in a single file. This means every write operation — regardless of which record is affected — requires a full file rewrite. Consequently, a file-level mutex serialises all writes to these store types. These stores are suitable for small-to-medium collections. The write queue depth is bounded to prevent backlogged rewrites from accumulating in memory.

### XLSX Constraints

XLSX write operations require loading the full workbook into memory, modifying the target sheet, and writing the file back atomically. Other sheets and cell formatting are preserved on a best-effort basis, subject to the limitations of the underlying pure-Go XLSX library. Complex formatting (pivot tables, macros, embedded objects) is explicitly out of scope.

### Mutex Registry

The mutex registry uses two lock scopes:
- **Record-level** — folder stores, keyed by record directory path
- **File-level** — JSON, CSV, XLSX stores, keyed by store file path

Stale entries are evicted when a record is deleted, when a store is unregistered, and periodically via a TTL sweep.

### Index Design

The in-memory index is designed for moderate dataset sizes. A configurable LRU cap per store evicts the least-recently-used entries when the cap is reached; cache misses fall back to a direct filesystem read. The index is kept warm via filesystem watchers with a debounce coalescing window to handle bulk write events.

### ID Generation: scan Strategy

The `scan` increment strategy performs an O(N) filesystem scan on every record creation to find the highest existing ID. It is provided only for stores where records may be created or deleted outside the server and counter drift is a concern. For all other cases, the `counter` strategy is preferred.

---

## 5. Plugin Architecture

Plugins (llama.cpp, Google Cloud) are optional, opt-in components that register themselves with the setup system. They have no coupling to core server functionality. A server with a plugin disabled or uninstalled behaves identically to one without it.

Each plugin:
- Registers a setup step definition (key, display name, required/optional, validation function)
- Exposes its own API route group (e.g. `/llama/*`, `/google/*`)
- Returns `501 Not Implemented` on all its endpoints if not configured

New plugins can be added without modifying the core setup flow or any core server package.

---

## 6. API Versioning Policy

All API routes are prefixed with `/v1/`. When breaking changes are introduced in future, a `/v2/` prefix is introduced alongside `/v1/` with a defined deprecation period, rather than modifying existing routes in place. Non-breaking additions (new endpoints, new optional fields) may be made within an existing version.

---

## 7. Configuration Locations

The server uses two distinct configuration locations:

| Location | Purpose |
|---|---|
| `{root}/.config/config.json` | Data config — store behaviour, runtime settings, port, timeouts, CORS. Travels with the data root. |
| `%APPDATA%\{appname}\` (Windows) / `~/.config/{appname}/` (Linux) | Platform config — `auth.db`, log files, setup state, bootstrap credentials. Never co-located with user data. |

No sensitive data (credentials, tokens, secrets) shall be stored in `.config/config.json`.
