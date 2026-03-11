# Functional Requirements
## Go Filesystem Database Server + Wails Desktop Application

---

## 1. Server Initialization

**FR-1.1** The server shall accept a root directory path as a configuration parameter at startup. The directory does not need to be named any specific name.

**FR-1.2** The server shall validate that the root directory exists and is readable before accepting any requests.

**FR-1.3** The server shall scan the root directory on startup to discover all stores by locating `.store.json` descriptor files.

**FR-1.4** The server shall load global configuration from `.config/config.json` within the root directory if it exists.

**FR-1.5** The server shall start successfully even if no stores are discovered, and allow stores to be added at runtime.

**FR-1.6** The server shall expose a health check endpoint at `GET /health` that reports startup status, run mode, number of discovered stores, and index readiness.

**FR-1.7** The server shall support two run modes, specified at startup via a `--mode` flag or environment variable:

- `subprocess` — the server is spawned and owned by a parent process (e.g. a Wails application). The server signals readiness to the parent via stdout and responds to shutdown signals from the parent.
- `standalone` — the server runs as an independent long-running service, managed by the OS or a process supervisor.

**FR-1.8** In `subprocess` mode, once the server is fully initialised and ready to accept requests, it shall write a single structured JSON line to stdout in the following form: `{"status":"ready","port":<port>}`. The parent process shall use this signal to know the server is available before making any requests. No other output shall be written to stdout in subprocess mode; all logs shall go to log files only.

**FR-1.9** In `subprocess` mode, the server shall listen for a shutdown signal from the parent process. If the parent process exits or closes the server's stdin pipe, the server shall initiate a graceful shutdown automatically, running the same shutdown sequence as FR-18.9.

**FR-1.10** In `standalone` mode, the server shall handle OS signals (`SIGINT`, `SIGTERM`) for graceful shutdown, logging a shutdown event to the application log before exiting.

---

## 2. Store Discovery & Registration

**FR-2.1** A store shall be defined by the presence of a `.store.json` file inside a directory. The server shall treat any directory containing `.store.json` as a store root.

**FR-2.2** The `.store.json` file shall declare at minimum: the store type, a human-readable name, and an optional schema definition.

**FR-2.3** The server shall support the following store types: `folder`, `json`, `csv`, and `xlsx`.

**FR-2.4** The server shall support nested stores, where a folder store may contain sub-directories that are themselves stores with their own `.store.json`.

**FR-2.5** The server shall expose an API endpoint to list all registered stores, including their type, path relative to root, and metadata.

**FR-2.6** The server shall allow a store to be registered at runtime by creating a `.store.json` file in a directory and calling `POST /stores/reload`. The reload endpoint shall immediately return `202 Accepted` with a job ID (see Section 18). The store shall be visible in the store list with a status of `registering` until the background scan completes. Any CRUD request against a store in `registering` status shall return `503 Service Unavailable` with a reference to the pending job ID.

**FR-2.7** The server shall allow a store to be unregistered at runtime by removing its `.store.json` file and calling `POST /stores/reload`.

---

## 3. Store Types

### 3.1 Folder Store

**FR-3.1.1** A folder store shall treat each immediate subdirectory as a record.

**FR-3.1.2** Each record directory shall maintain its structured state in a `.meta.json` sidecar file. The contents of `.meta.json` shall be arbitrary JSON and not enforced by the server unless a schema is declared in `.store.json`.

**FR-3.1.3** The server shall allow arbitrary files to be uploaded to and downloaded from any record directory.

**FR-3.1.4** The server shall track all files within a record directory and expose a file listing per record.

**FR-3.1.5** The server shall support creating a new record by creating a new subdirectory with an empty `.meta.json`.

**FR-3.1.6** The server shall support deleting a record by removing its directory and all contents, with a configurable soft-delete option that renames the directory with a tombstone prefix instead.

### 3.2 JSON Store

**FR-3.2.1** A JSON store shall treat a single `.json` file as a collection of records, where the file contains either a JSON array (each element is a record) or a JSON object (each key is a record ID).

**FR-3.2.2** The server shall support reading, creating, updating, and deleting individual records within a JSON file. The `.store.json` shall support a `max_records` advisory limit beyond which the server logs a warning to discourage use of JSON stores as large-scale databases.

**FR-3.2.3** The server shall write JSON atomically using a write-to-temp-then-rename strategy to prevent corruption.

**FR-3.2.4** The server shall expose a configurable write queue depth per JSON store. If the queue is full (i.e. too many writes are backlogged behind the file-level lock), the server shall return `503 Service Unavailable` rather than allowing unbounded queue growth.

### 3.3 CSV Store

**FR-3.3.1** A CSV store shall treat each row as a record, with the header row defining field names.

**FR-3.3.2** The server shall support reading, creating, updating, and deleting rows in a CSV file.

**FR-3.3.3** The server shall preserve the original column order and header names when writing back to a CSV file.

**FR-3.3.4** The server shall write CSV atomically using a write-to-temp-then-rename strategy.

### 3.4 XLSX Store

**FR-3.4.1** An XLSX store shall treat each row in a designated sheet as a record.

**FR-3.4.2** The `.store.json` shall allow specifying the target sheet name; if not specified, the first sheet shall be used.

**FR-3.4.3** The server shall support reading, creating, updating, and deleting rows in the XLSX file. Other sheets and cell formatting shall be preserved. Complex formatting (pivot tables, macros, embedded objects) is explicitly out of scope.

**FR-3.4.4** The server shall enforce a configurable maximum file size for XLSX stores. Write requests against a store whose file exceeds this limit shall be rejected with `413 Content Too Large`. The server shall also enforce the same write queue depth cap as JSON stores (FR-3.2.4), returning `503` if the queue is full.

---

## 4. CRUD Operations

**FR-4.1** The server shall expose a consistent REST API for CRUD operations across all store types. Endpoints that are not applicable to a store type shall return `405 Method Not Allowed` with a clear explanation, rather than being absent from the API. File management endpoints (Section 5) are exclusive to folder stores and shall return `405` on all other store types.

**FR-4.2** The server shall support creating a record via `POST /{store}/records` with a JSON body representing the record's initial state.

**FR-4.3** The server shall support reading a single record via `GET /{store}/records/{id}`.

**FR-4.4** The server shall support listing all records in a store via `GET /{store}/records`, with support for pagination via `limit` and `offset` query parameters.

**FR-4.5** The server shall support updating a record via `PUT /{store}/records/{id}` (full replace) and `PATCH /{store}/records/{id}` (partial merge).

**FR-4.6** The server shall support deleting a record via `DELETE /{store}/records/{id}`.

**FR-4.7** The server shall return appropriate HTTP status codes for all operations: `200` for successful reads, `201` for successful creates, `204` for successful deletes, `404` for missing records, `409` for conflicts, and `422` for validation failures.

**FR-4.11** All error responses shall use a consistent JSON envelope regardless of which subsystem produced the error. The envelope shall include a stable machine-readable error code, a human-readable message, a structured details object (carrying per-field violations for `422` responses), and the correlation ID. The error envelope schema is defined in the API Reference document.

**FR-4.12** All API routes shall be prefixed with a version segment. The current version is `v1`. The versioning policy is defined in the Architecture document.

**FR-4.8** When creating a record in a store that uses an `{id}` naming pattern, the caller may optionally supply an explicit record name in the request body. If supplied, the server shall use it as-is instead of auto-generating one, but shall reject the request with `409 Conflict` if a record with that name already exists.

**FR-4.9** The server shall allow the caller to override auto-managed fields on create — specifically `created_at`, `updated_at`, and any other fields declared as `auto` in the store schema. Overridden values shall be written as provided without modification.

**FR-4.10** The server shall document which fields are auto-managed as part of the store metadata returned by `GET /{store}`, so callers know what can be overridden.

---

## 5. File Management (Folder Store)

**FR-5.1** The server shall support uploading one or more files to a record via `POST /{store}/records/{id}/files` using multipart form data.

**FR-5.2** The server shall support downloading a specific file from a record via `GET /{store}/records/{id}/files/{filename}`. File downloads shall be streamed directly from disk to the response without buffering the entire file in memory. The server shall support HTTP range requests (`Range` header) for partial content delivery, enabling resumable downloads and efficient seeking in large files.

**FR-5.3** The server shall support listing all files attached to a record via `GET /{store}/records/{id}/files`, returning filename, size, MIME type, and last modified timestamp.

**FR-5.4** The server shall support deleting a specific file from a record via `DELETE /{store}/records/{id}/files/{filename}`.

**FR-5.5** The server shall reject file uploads that would overwrite `.meta.json` or `.store.json` through the files API.

**FR-5.6** The `.store.json` shall allow declaring optional constraints on file uploads: allowed MIME types, maximum file size, and maximum number of files per record.

---

## 6. Querying & Filtering

**FR-6.1** The server shall support filtering records by field values via query parameters on the list endpoint (e.g., `GET /{store}/records?status=active`).

**FR-6.2** The server shall support sorting records by a specified field in ascending or descending order via query parameters.

**FR-6.3** The server shall support a simple search query parameter that performs a case-insensitive substring match across all string fields of a record's metadata.

**FR-6.4** The server shall support cross-store queries via `GET /v1/query`, accepting a list of store names, filter criteria, sort order, and pagination parameters. Cross-store queries shall execute against the in-memory index only and shall not trigger filesystem reads. The endpoint shall enforce a configurable maximum result limit and a per-query timeout; if the timeout is exceeded, the server shall return partial results with a `206 Partial Content` status and a response body indicating which stores were successfully queried and which timed out. Stores whose index is currently being rebuilt shall be excluded from the query with a note in the response rather than causing the entire query to fail.

---

## 7. Index Layer

**FR-7.1** The server shall maintain an in-memory index of all record metadata, built on startup by reading all relevant files in the root directory. The server shall expose a configurable record cap per store; entries beyond the cap shall be evicted, with subsequent reads falling back to a direct filesystem read.

**FR-7.2** The server shall use filesystem watchers to keep the in-memory index current as files are changed on disk, including changes made outside the server. Watcher events shall be debounced with a configurable coalescing window (default: 500ms) so that bursts of rapid filesystem changes are batched into a single index update.

**FR-7.3** The index shall support filtering, sorting, and counting operations without hitting the filesystem on each request.

**FR-7.4** The server shall expose an endpoint to manually trigger a full index rebuild.

**FR-7.5** The server shall report index status (last built, number of indexed records, watcher health) via the health check endpoint.

---

## 8. Concurrency & Safety

**FR-8.1** The server shall prevent concurrent writes using a mutex registry. Folder stores shall use record-level locking, allowing concurrent writes to different records within the same store. JSON, CSV, and XLSX stores shall use file-level locking, serialising all writes to the store file. Lock scope details are defined in the Architecture document.

**FR-8.2** The server shall use atomic write strategies (write to a temporary file, then rename) for all file modifications to prevent partial writes from corrupting data.

**FR-8.3** The server shall return a `409 Conflict` response if a write cannot acquire the record lock within a configurable timeout period.

**FR-8.4** The server shall support optimistic concurrency control via an `ETag` header on read responses, allowing clients to detect mid-air collisions on updates using `If-Match`.

**FR-8.5** The mutex registry shall implement garbage collection for stale entries. Mutexes for deleted records and unregistered stores shall be removed promptly. The registry shall also evict mutexes that have not been accessed within a configurable TTL.

**FR-8.6** All lifecycle hooks (both `before-*` and `after-*`) shall execute under a configurable timeout. If a hook does not complete within the timeout, the server shall cancel it, release the associated lock, and return a `502 Bad Gateway` response indicating which hook timed out.

---

## 9. Schema Validation

**FR-9.1** The `.store.json` shall allow an optional JSON Schema definition. When present, the server shall validate all create and update payloads against the schema before writing.

**FR-9.2** For CSV stores, strict JSON Schema validation is not directly applicable because all CSV values are strings. The `.store.json` for a CSV store shall therefore support a `columns` declaration that maps each column name to a type (`string`, `integer`, `number`, `boolean`). The server shall coerce and validate incoming values against these declared types before writing. If no `columns` declaration is present, all values are treated as strings and no type validation is performed.

**FR-9.3** The server shall return a `422 Unprocessable Entity` response with a detailed error body when validation fails, listing each violated constraint.

**FR-9.4** Schema validation shall be optional and off by default. Stores without a schema shall accept any valid JSON payload.

---

## 10. Extension Hooks

**FR-10.1** The server shall support lifecycle hooks that fire on the following events for any store: `before-create`, `after-create`, `before-update`, `after-update`, `before-delete`, `after-delete`, `after-file-upload`, `after-file-delete`.

**FR-10.2** Hooks shall be registerable per store or globally (applying to all stores).

**FR-10.3** `before-*` hooks shall be able to cancel the operation by returning an error, which the server shall propagate as a `400` or `403` response.

**FR-10.4** The server shall provide built-in hook implementations for: audit logging (append a log entry on every mutation), and timestamp management (auto-set `created_at` and `updated_at` fields on `.meta.json`).

**FR-10.5** The order of operations for a create or update request shall be: (1) run `before-*` hooks, which may mutate or cancel the payload; (2) apply caller-supplied field overrides from the request body, which take precedence over any values set by `before-*` hooks; (3) write to disk; (4) run `after-*` hooks. This ensures that explicit caller values such as a supplied `created_at` always win over auto-managed hook values.

**FR-10.6** The hook system shall be designed so that reporting and monitoring features (e.g., "list all active cases") can be implemented as `after-*` hook consumers without modifying core store logic.

---

## 11. Backup & Restore

**FR-11.1** The server shall own and manage the `.backups` directory within the root directory.

**FR-11.2** The server shall support triggering a backup of a single store via `POST /backups` with a `scope={store-name}` parameter, producing a ZIP archive of the store directory as-is, preserving the exact directory and file structure with no transformation.

**FR-11.3** The server shall support triggering a full backup of the entire root directory via the same endpoint with `scope=all`.

**FR-11.4** Backup filenames shall follow a deterministic format encoding scope and timestamp (e.g., `cases-2026-02-26T1400.zip`).

**FR-11.5** The server shall support a configurable retention policy that automatically deletes backups older than a specified number of days.

**FR-11.6** The server shall list all available backups and their metadata (scope, size, timestamp) via `GET /backups`.

**FR-11.7** The server shall support initiating a restore via `POST /backups/{backup-name}/restore`, where `{backup-name}` identifies a specific archive from the backup list. The request body shall specify the restoration mode and optional flags.

**FR-11.8** The server shall support two restoration modes, specified in the request body:

- `overwrite` — the target store is fully replaced by the contents of the backup archive. Any records or files present on disk but absent from the archive are deleted. For single-file stores (JSON, CSV, XLSX), the entire file is replaced.
- `merge` — the behaviour depends on store type:
  - **Folder store**: record directories from the archive are written into the target. Existing records not present in the archive are left untouched. For records present in both, the backup version of `.meta.json` and all files wins.
  - **JSON store**: records from the backup file are merged entry-by-entry into the live file by record ID. Existing records absent from the backup are retained. For records present in both, the backup version wins.
  - **CSV store**: rows from the backup are merged into the live file by matching on the column declared as the primary key in `.store.json`. Rows absent from the backup are retained. Rows present in both are replaced by the backup version. CSV stores without a declared primary key do not support merge mode and shall return `422` if merge is attempted. If the backup file's column headers do not match the live file's column headers, the server shall abort the merge and return `409 Conflict` with a diff of the mismatched headers, unless `force: true` is provided, in which case the backup's schema is used as authoritative and extra columns in the live file are dropped.
  - **XLSX store**: follows the same row-level merge semantics as CSV, including the same schema mismatch handling.

**FR-11.9** Both restoration modes shall support a `dry_run: true` body parameter that reports the full diff of what would be created, overwritten, or deleted without applying any changes.

**FR-11.10** The server shall reject a restoration request if the backup archive's declared scope does not match the target store, unless a `force: true` flag is explicitly provided in the request body.

---

## 12. Configuration

**FR-12.1** The server uses two distinct configuration locations with different responsibilities:

- **Data config** — `.config/config.json` inside the root directory. Controls store behaviour, server runtime settings (port, timeouts, log level, CORS, etc.), and anything specific to the data root being served. This file travels with the data.
- **Platform config** — the platform-specific directory used by `auth.db` and log files (`%APPDATA%\{appname}\` on Windows, `~/.config/{appname}/` on Linux). Holds security-sensitive data and system-level state that must not be co-located with user data.

No sensitive data (credentials, tokens, secrets) shall be stored in `.config/config.json`.

**FR-12.2** Configuration shall support at minimum: run mode, server port, request timeout, write lock timeout, index rebuild interval, backup retention days, log level, and CORS settings.

**FR-12.3** Configuration values shall be overridable by environment variables, with environment variables taking precedence over the config file.

**FR-12.4** The server shall expose a read-only endpoint that returns the current resolved configuration (with sensitive values redacted).

**FR-12.5** The server shall support binding to a fixed port specified in config or via `--port` flag. If `--port 0` is passed, the server shall bind to a random available port assigned by the OS. In `subprocess` mode, the actual bound port shall be communicated to the parent via the stdout ready signal (FR-1.8), allowing the parent to discover the port dynamically.

**FR-12.6** CORS behaviour shall differ by run mode:

- `subprocess` — CORS shall be disabled entirely by default. Since the Wails application proxies all frontend requests to the server over localhost, no cross-origin headers are needed. Enabling CORS in subprocess mode shall require an explicit opt-in in config.
- `standalone` — CORS shall be configurable via an allowed origins list in config. If no origins are configured, the server shall default to rejecting all cross-origin requests. A wildcard `*` shall be supported for development use only and shall produce a warning in the application log on startup.

---

## 13. Logging

### 13.1 General

**FR-13.1** The server shall write all logs in structured JSON format, with each entry containing at minimum: timestamp (ISO 8601), log level, log type, and a correlation ID linking all entries produced by a single request or job.

**FR-13.2** The correlation ID shall be generated per request and per job, included in every log entry produced during that scope, and returned to the client in an `X-Correlation-ID` response header.

**FR-13.3** Log level shall be configurable (e.g. `debug`, `info`, `warn`, `error`) and shall default to `info`. Changing the log level shall not require a server restart.

**FR-13.4** Each log type shall be written to its own file, rotated daily, and retained for a configurable number of days. All log files shall be stored in a `logs` subdirectory at the platform-specific config path alongside `auth.db`.

**FR-13.5** The server shall expose a `GET /logs` endpoint listing available log files by type, date, and size. Log file download shall be restricted to `admin` users.

### 13.2 Application Logs

**FR-13.6** The server shall write an application log (`app.log`) capturing server lifecycle events: startup (including resolved config and discovered stores), shutdown, store registration and deregistration, configuration reloads, index rebuilds, filesystem watcher events, and any unhandled errors or panics.

**FR-13.7** Each application log entry shall include: timestamp, level, event type, and a human-readable message with relevant context (e.g. store name, file path, error detail).

### 13.3 Access Logs

**FR-13.8** The server shall write an access log (`access.log`) for every inbound HTTP request, regardless of outcome. Each entry shall include: timestamp, correlation ID, authenticated user ID (or `anonymous`), HTTP method, path, query string, response status code, response time in milliseconds, and bytes sent.

**FR-13.9** The access log shall record both successful and failed authentication attempts, including the username supplied on failure (but never the password).

**FR-13.10** Requests to health check and metrics endpoints shall be logged at `debug` level to avoid noise, but shall not be omitted entirely.

### 13.4 Transaction Logs

**FR-13.11** The server shall write a transaction log (`transaction.log`) for every mutation that changes persistent state. Each entry shall include: timestamp, correlation ID, user ID, store name, record ID (if applicable), operation type (`create`, `update`, `delete`, `file-upload`, `file-delete`), and the diff or description of what changed.

**FR-13.12** For record creates and updates, the transaction log entry shall include the full before and after state of `.meta.json` so the change is fully reconstructible from the log alone.

**FR-13.13** Transaction log entries shall be written atomically after the filesystem write succeeds. A transaction that fails before completion shall log the failure with the partial state and reason.

**FR-13.14** Backup and restore operations shall each produce a transaction log entry summarising the scope, mode, archive name, and list of records affected.

### 13.5 Audit Logs

**FR-13.15** The server shall write a dedicated audit log (`audit.log`) covering all security-relevant events. This log shall never be disabled regardless of the configured log level.

**FR-13.16** The following events shall always produce an audit log entry: user login (success and failure), logout, session expiry, password change, user creation, user deletion, role assignment and revocation (global and per-store), any `403 Forbidden` or `401 Unauthorized` response, backup creation, backup restoration, and any change to server configuration.

**FR-13.17** Each audit log entry shall include: timestamp, event type, acting user ID, target user ID or resource (if applicable), IP address, correlation ID, and outcome (`success` or `failure` with reason).

**FR-13.18** Audit log files shall be append-only. The server shall never delete or rotate audit log entries based on retention policy — audit logs shall be retained indefinitely unless an admin explicitly purges them via a dedicated endpoint (`DELETE /logs/audit`) which itself produces an audit log entry.

### 13.6 Job Logs

**FR-13.19** The server shall write a job log (`jobs.log`) for all background job lifecycle events: job enqueued, job started, progress updates, job completed, job failed, and job cancelled. Each entry shall include the job ID, job type, scope, and relevant progress detail.

**FR-13.20** Job log entries shall reference the same correlation ID used for the originating request so the full lifecycle of a job can be traced from initial API call through to completion.

### 13.7 Error Logs

**FR-13.21** The server shall write a dedicated error log (`error.log`) for all `warn` and `error` level events, regardless of which subsystem produced them. This provides a single file for operational alerting without needing to search across all log types.

**FR-13.22** Each error log entry shall include the originating subsystem (e.g. `store`, `auth`, `index`, `backup`, `job`), error code if applicable, full error message, and stack trace for unexpected errors.

---

## 14. Security

**FR-14.1** The server shall not serve or expose the `.backups` or `.config` directories through any record or file API endpoint.

**FR-14.2** The server shall sanitize all record IDs and filenames to prevent path traversal attacks (e.g., `../../etc/passwd`).

**FR-14.3** The server shall reject requests where the resolved file path falls outside the root directory.

**FR-14.4** The server shall protect against symlink attacks. Before reading or writing any path within the root directory, the server shall resolve all symlinks in the path and verify the resolved path still falls within the root directory. Any path that resolves outside the root — including via symlinks created by file uploads or external processes — shall be rejected with `403 Forbidden`. Filesystem watchers shall also skip symlinked paths that resolve outside the root.

---

## 15. Authentication

**FR-15.1** The server shall store all credentials, sessions, roles, and other authentication data in a SQLite database (`auth.db`) located outside the data root, at a platform-specific path:

- Windows: `%APPDATA%\{appname}\auth.db`
- Linux: `~/.config/{appname}/auth.db`

The `appname` value shall be configurable at startup.

**FR-15.2** The server shall support username and password authentication. Passwords shall be stored as hashes using a modern algorithm (e.g. bcrypt or argon2). Plaintext passwords shall never be stored or logged.

**FR-15.3** The server shall issue a session cookie upon successful login via `POST /auth/login`. The session cookie shall be HTTP-only and have a configurable expiry duration.

**FR-15.4** The server shall support logging out via `POST /auth/logout`, which invalidates the session both server-side (removing it from `auth.db`) and instructs the client to clear the cookie.

**FR-15.5** Session tokens shall be stored in `auth.db` and validated on every request. Expired or unrecognised session tokens shall result in a `401 Unauthorized` response.

**FR-15.6** The server shall support a first-run bootstrap flow: if no users exist in `auth.db` on startup, the server shall create a default admin account. In `standalone` mode, the credentials shall be printed once to stdout. In `subprocess` mode, stdout is reserved for the ready signal (FR-1.8); the bootstrap credentials shall instead be written to a `bootstrap.txt` file in the platform config directory and the ready signal payload shall include a `"bootstrap":true` field so the parent process knows to surface the credentials to the user. The bootstrap file shall be deleted automatically after the admin successfully changes their password on first login.

**FR-15.7** The server shall expose user management endpoints restricted to admin users: create user (`POST /auth/users`), list users (`GET /auth/users`), update user (`PATCH /auth/users/{id}`), and delete user (`DELETE /auth/users/{id}`).

---

## 16. Authorization (RBAC)

**FR-16.1** The server shall implement a two-level RBAC model: global roles that apply server-wide, and per-store role overrides that apply to a specific store and take precedence over the global role for operations on that store.

**FR-16.2** The server shall ship with the following built-in global roles:

- `admin` — full access to all stores, all records, all files, backup/restore, job management, and user management
- `editor` — read and write access to all stores and records; no access to backup/restore, jobs, or user management
- `viewer` — read-only access to all stores and records; no write, backup, or admin operations

**FR-16.3** Per-store role overrides shall allow assigning a user a different effective role for a specific store. For example, a global `viewer` may be granted `editor` on one store, or a global `editor` may be restricted to `viewer` on a sensitive store.

**FR-16.4** Per-store role assignments shall be stored in `auth.db` and manageable via `POST /auth/users/{id}/stores/{store}`, `GET /auth/users/{id}/stores`, and `DELETE /auth/users/{id}/stores/{store}`.

**FR-16.5** The server shall evaluate the effective role for every request in the following order: per-store override (if present and applicable) → global role → deny.

**FR-16.6** The server shall return `403 Forbidden` for any request where the authenticated user's effective role does not permit the attempted operation.

**FR-16.7** Role-to-permission mappings shall define access at the operation level, covering: list records, read record, create record, update record, delete record, upload file, download file, delete file, trigger backup, trigger restore, manage jobs, and manage users.

**FR-16.8** Custom roles shall be supported, allowing an admin to define a named role with an explicit set of permitted operations. Custom roles shall be stored in `auth.db` and assignable to users in the same way as built-in roles.

**FR-16.9** All authorization decisions (allow and deny) shall be recorded in the structured request log with the user ID, effective role, and operation attempted.


---

## 17. Record Naming & ID Generation

**FR-17.1** Each folder store shall have a configurable naming pattern defined in `.store.json`. The default pattern is `{prefix}-{id:DDD}-{YY}`.

**FR-17.2** A naming pattern is composed of literal strings and tokens. The supported tokens are:

- `{prefix}` — a static string literal declared in `.store.json`
- `{id:DDD}` — an auto-incrementing integer, zero-padded to the number of `D` characters specified
- `{YY}` / `{YYYY}` — a year component, either 2 or 4 digits
- `{MM}` — a month component, zero-padded
- `{DD}` — a day component, zero-padded

**FR-17.3** If a naming pattern contains no `{id}` token, the store shall not support automatic record creation. Any `POST` to create a new record on such a store shall be rejected with `405 Method Not Allowed` and an explanation that the store requires an explicit record name.

**FR-17.4** If a naming pattern contains an `{id}` token, the server shall auto-generate the next record name on creation. The increment strategy shall be configurable per store as one of:

- `counter` *(default)* — maintain a persisted counter value inside `.store.json`, incremented atomically on each create.
- `scan` — derive the next ID by scanning existing record names for the highest current ID. Suitable only for small stores or stores where records may be created outside the server. Performance characteristics are documented in the Architecture document.

**FR-17.5** When using the `scan` strategy, if no records exist the sequence shall start at `1`.

**FR-17.6** If the next ID value exceeds the digit width declared in the `{id}` token (e.g. ID `1000` with token `{id:DDD}`), the server shall auto-expand the padding to accommodate the value. The declared width is treated as a minimum width, not a maximum.

**FR-17.7** The year component (`{YY}` / `{YYYY}`) shall be configurable per store as either:

- `auto` — resolved to the current year at the time of record creation
- `manual` — the caller must supply the year value in the create request body; the server shall reject the request if it is absent

**FR-17.8** The server shall validate that a newly generated record name does not already exist on disk before creating the directory. Under the `scan` strategy, if a collision is detected the server shall retry with an incremented ID.

**FR-17.9** The server shall expose the resolved naming pattern, increment strategy, and current counter (if applicable) as part of the store metadata returned by `GET /{store}`.

---

## 18. Background Jobs

**FR-18.1** The server shall have a job system for executing long-running operations asynchronously. Operations that shall always run as background jobs include: index scan and rebuild, backup creation, backup restoration, and store registration scans.

**FR-18.2** When a long-running operation is triggered via the API, the server shall immediately return `202 Accepted` with a job descriptor containing a unique job ID, the operation type, and a status URL.

**FR-18.3** Each job shall have one of the following statuses: `queued`, `running`, `completed`, `failed`, or `cancelled`.

**FR-18.4** The server shall expose a job status endpoint at `GET /jobs/{job-id}` that returns the current status, progress information (where applicable, e.g. files scanned out of total), start time, end time, and any error details on failure.

**FR-18.5** The server shall expose a job list endpoint at `GET /jobs` returning all active and recently completed jobs. Completed and failed jobs shall be retained in memory for a configurable duration before being discarded.

**FR-18.6** The server shall support cancelling a queued or running job via `DELETE /jobs/{job-id}`. Cancellation shall be best-effort — jobs that have reached a point of no return (e.g. mid-write during restore) shall complete the current atomic unit before stopping.

**FR-18.7** The server shall enforce a configurable limit on the number of concurrently running jobs. Requests that would exceed this limit shall be queued rather than rejected.

**FR-18.8** The server shall prevent duplicate jobs of the same type and scope from being enqueued simultaneously (e.g. two backup jobs for the same store). A `409 Conflict` shall be returned if a duplicate is attempted, with a reference to the already-running job ID.

**FR-18.9** On server shutdown, the server shall wait for all running jobs to reach a safe stopping point before exiting, up to a configurable grace period. Jobs that cannot finish within the grace period shall be marked `failed` with a reason of `server-shutdown`.

**FR-18.10** Job history (ID, type, scope, status, timestamps, error) shall be written to a structured log so that completed job outcomes are not lost on server restart.

---

## 19. Initial Setup

### 19.1 Setup State

**FR-19.1** The server shall track a global setup state stored in the platform config directory alongside `auth.db`. Setup state records which integrations have been configured and whether initial setup has been completed.

**FR-19.2** On first start, if setup has not been completed, the server shall enter setup mode. In setup mode, all regular API endpoints except the setup API (Section 19.3) and health check shall return `503 Service Unavailable` with a body indicating setup is required.

**FR-19.3** Setup mode shall be exitable by completing the setup flow via either the CLI wizard or the setup API. Once exited, the server shall not re-enter setup mode unless explicitly reset via `POST /setup/reset` (admin only).

**FR-19.4** Setup state shall be fully persistent across server restarts. Each step's state (`pending`, `skipped`, `configured`, `failed`) shall be written to the platform config directory immediately when it changes. A server restart during a partially completed setup shall resume from the last persisted step state, not from the beginning.

### 19.2 CLI Wizard

**FR-19.5** The server shall provide an interactive CLI wizard, invoked automatically on first start in a terminal context, that guides the user through all setup steps in sequence.

**FR-19.6** The CLI wizard shall present each integration as an opt-in step with a yes/no prompt. Skipped steps shall be recorded as `skipped` in setup state and may be configured later via the setup API.

**FR-19.7** The CLI wizard shall support being re-run at any time via a `--setup` flag (e.g. `appname --setup`) to reconfigure or complete previously skipped steps.

**FR-19.8** If the server is started in a non-terminal context (e.g. as a background service or inside a container) and setup is incomplete, the server shall skip the CLI wizard, enter setup mode, and log a warning directing the operator to complete setup via the setup API.

### 19.3 Setup API

**FR-19.9** The server shall expose a setup API at `/setup` that is accessible without authentication only when setup mode is active. Once setup is completed, all `/setup` endpoints shall require admin authentication.

**FR-19.10** The server shall expose `GET /setup/status` returning the current state of each setup step: `pending`, `skipped`, `configured`, or `failed`.

**FR-19.11** Each integration shall expose a dedicated setup endpoint (e.g. `POST /setup/llama`, `POST /setup/google`) that accepts the configuration for that integration, validates it, and persists it to the config store.

**FR-19.12** The server shall expose `POST /setup/complete` to explicitly mark setup as finished. This endpoint shall fail if any step marked as required is still in `pending` state.

### 19.4 Extensibility

**FR-19.13** The setup system shall be designed so that new integration steps can be added in future without modifying the core setup flow. Each integration registers its own setup step definition, including: a unique key, a display name, whether it is required or optional, and a validation function.

**FR-19.14** The server shall expose `GET /setup/steps` listing all registered setup steps and their current state, so a frontend UI can dynamically render the setup wizard without hardcoding the list of integrations.

---

## 20. Local AI Integration (llama.cpp)

The llama.cpp integration is an optional plugin. All `/llama/*` endpoints shall return `501 Not Implemented` if the integration is not configured.

### 20.1 Setup Step

**FR-20.1** The llama.cpp integration shall be presented as an optional setup step (key: `llama`) during initial setup. The user shall be offered a checkbox/prompt to download the llama.cpp release binary for their platform.

**FR-20.2** If the user opts in, the server shall download the latest llama.cpp release binary from the official GitHub releases page for the detected platform (Windows x64, Linux x64, Linux arm64) as a background job. Download progress shall be reported via the job system (Section 18).

**FR-20.3** Downloaded binaries shall be stored at the following platform-specific paths:
- Windows: `%LOCALAPPDATA%\{appname}\bin\`
- Linux: `~/.local/share/{appname}/bin\`

**FR-20.4** If the user already has a llama.cpp binary or llama-server executable on their system, the setup step shall allow them to provide the path manually instead of downloading.

**FR-20.5** The setup step shall verify the downloaded or provided binary is functional by running it with a version flag and capturing the output before marking the step as `configured`.

### 20.2 Subprocess Management

**FR-20.6** The llama-server management mode shall be configurable per the following options:

- `managed` — the server spawns and owns the llama-server process as a child subprocess, starting it on demand and stopping it on server shutdown.
- `external` — the server connects to an already-running llama-server instance at a configured host and port. The server does not manage the process lifecycle.

**FR-20.7** In `managed` mode, the server shall start the llama-server subprocess with a configurable set of arguments (model path, context size, number of threads, GPU layers, host, port) sourced from the integration config.

**FR-20.8** In `managed` mode, the server shall monitor the llama-server subprocess. If it exits unexpectedly, the server shall attempt to restart it up to a configurable number of times before marking the integration as `degraded` and logging an error.

**FR-20.9** In `managed` mode, the llama-server subprocess shall be cleanly terminated during server shutdown before the main process exits.

**FR-20.10** In `external` mode, the server shall perform a health check against the configured llama-server endpoint on startup and expose its reachability as part of `GET /health`.

### 20.3 Model Management

**FR-20.11** The server shall support registering one or more GGUF model files via `POST /llama/models`, specifying the file path or a Hugging Face model URL to download from.

**FR-20.12** Model files downloaded from a URL shall be saved to the platform bin directory alongside the llama.cpp binary and tracked in the integration config.

**FR-20.13** The server shall expose `GET /llama/models` listing all registered models with their name, path, size, and whether they are currently loaded.

**FR-20.14** The server shall support selecting the active model via `POST /llama/models/{name}/activate`. In `managed` mode, activating a different model shall restart the llama-server subprocess with the new model. In `external` mode, this endpoint shall return `405` as model selection is outside the server's control.

**FR-20.15** The server shall expose `DELETE /llama/models/{name}` to deregister a model. If `delete_file: true` is passed in the request body, the model file shall also be deleted from disk.

### 20.4 Inference API

**FR-20.16** The server shall expose a text generation endpoint at `POST /llama/generate` that proxies the request to the running llama-server instance and streams the response back to the caller.

**FR-20.17** The server shall expose a chat completion endpoint at `POST /llama/chat` that accepts a messages array and proxies it to the llama-server OpenAI-compatible chat endpoint.

**FR-20.18** If the llama-server is not running or is unreachable at the time of a generation request, the server shall return `503 Service Unavailable` with a clear message rather than hanging or timing out silently.

**FR-20.19** All inference requests and their metadata (model used, prompt token count, generation token count, duration, user ID) shall be recorded in the transaction log.

---

## 21. Google Cloud Integration

The Google Cloud integration is an optional plugin split into two independently-enableable sub-features: Calendar and Drive. All `/google/*` endpoints shall return `501 Not Implemented` if the plugin is not configured.

### 21.1 Authentication & Setup

**FR-21.1** The Google Cloud plugin shall use OAuth 2.0 for authentication. Each app user who wishes to use Google features shall individually authorise the application via a browser-based OAuth consent flow.

**FR-21.2** The setup step (key: `google`) shall require the operator to provide a Google Cloud project OAuth 2.0 client ID and client secret, obtained from the Google Cloud Console. These credentials shall be stored in `auth.db` and never exposed via any API response.

**FR-21.3** The server shall expose an OAuth initiation endpoint at `GET /google/auth` that redirects the authenticated app user to Google's consent screen, requesting the minimum required scopes for the enabled sub-features (`calendar.events` and/or `drive.file`).

**FR-21.4** The server shall expose an OAuth callback endpoint at `GET /google/auth/callback` that receives the authorisation code from Google, exchanges it for access and refresh tokens, and stores both in `auth.db` associated with the app user's account.

**FR-21.5** Access tokens shall be refreshed automatically using the stored refresh token before expiry. If a refresh fails (e.g. the user has revoked access), the affected user's Google integration status shall be marked `disconnected` and any subsequent Google API calls for that user shall return `401 Unauthorized` with a reconnect URL.

**FR-21.6** The server shall expose `DELETE /google/auth` allowing a user to revoke their Google authorisation, which shall delete their tokens from `auth.db` and call Google's token revocation endpoint.

**FR-21.7** The server shall expose `GET /google/status` returning the current connection status for the authenticated user: `connected`, `disconnected`, or `not_configured`, along with which sub-features are enabled and the token expiry time.

### 21.2 Calendar

**FR-21.8** The server shall expose `POST /google/calendar/events` to create a new Google Calendar event on behalf of the authenticated user. The request body shall accept: title, description, start datetime, end datetime, timezone, calendar ID (defaulting to the user's primary calendar if omitted), attendees list, and recurrence rule.

**FR-21.9** The server shall support recurring events via an RRULE string in the create and update request body. The server shall pass the RRULE through to the Google Calendar API without parsing or validating it, delegating recurrence logic entirely to Google.

**FR-21.10** The server shall expose `PATCH /google/calendar/events/{event-id}` to update an existing event. For recurring events, the request body shall specify the update scope: `this` (only the specified instance), `this_and_following`, or `all` (all instances in the series).

**FR-21.11** The server shall expose `DELETE /google/calendar/events/{event-id}` to delete an event. The same `scope` parameter shall apply for recurring events as in FR-21.10.

**FR-21.12** The server shall expose `GET /google/calendar/events` to list events from the user's Google Calendar within a specified time range, returned in chronological order. This allows the app to display calendar data without storing it locally.

**FR-21.13** The server shall expose `GET /google/calendars` to list all calendars available to the authenticated user, so the caller can present a calendar picker when creating events.

**FR-21.14** All calendar API calls shall be made on behalf of the user whose session is making the request, using that user's stored OAuth tokens. One user's calendar credentials shall never be used for another user's request.

**FR-21.15** All calendar operations (create, update, delete) shall be recorded in the transaction log with the user ID, event ID, and operation type.

### 21.3 Google Drive Backup Sync

**FR-21.16** The Google Drive backup sync feature shall upload completed local backup archives (from Section 11) to a designated folder in the authenticated user's Google Drive.

**FR-21.17** The target Drive folder shall be configurable per store or globally. If the configured folder does not exist, the server shall create it on first sync.

**FR-21.18** The server shall support three sync trigger modes, configurable independently per store:

- `scheduled` — syncs run on a configurable cron schedule (e.g. nightly at 02:00)
- `post-backup` — a Drive upload job is automatically enqueued immediately after a local backup completes successfully
- `manual` — syncs only run when explicitly triggered via `POST /google/drive/sync`

Multiple modes may be enabled simultaneously for a single store.

**FR-21.19** Each sync operation shall run as a background job (Section 18). The job shall upload only backup archives that are not already present in the target Drive folder, determined by matching filename. Re-uploading an existing file shall not occur unless `force: true` is passed.

**FR-21.20** The server shall expose `GET /google/drive/sync/status` returning the last sync time, last sync outcome, number of files uploaded, and any error detail per store.

**FR-21.21** The server shall expose `POST /google/drive/sync` to manually trigger an immediate sync for one store (`scope={store-name}`) or all stores (`scope=all`), returning a job ID per FR-18.2.

**FR-21.22** If a Drive upload fails partway through (e.g. network interruption), the job shall record which files were successfully uploaded and which failed, and shall retry only the failed files on the next sync rather than re-uploading everything.

**FR-21.23** The server shall enforce a configurable maximum number of backup archives to retain in the Drive folder per store. When the limit is exceeded, the oldest archives shall be deleted from Drive (but not locally) to stay within the limit.

**FR-21.24** All Drive sync operations (file uploaded, file deleted, sync started, sync completed, sync failed) shall be recorded in the transaction log with the user ID, filename, Drive file ID, and outcome.


