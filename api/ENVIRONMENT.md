# Bruno CLI Environment Variables

## Overview
This document defines the environment variable contract for Bruno API requests.

## CLI Setup

```bash
# Install dependencies
npm install

# Verify CLI is available
npx bru --version
```

## Environments

### local (`environments/local.yml` or `environments/local.json`)
For normal local server testing against `http://localhost:8080`.

### test-isolated (`environments/test-isolated.json`)
For isolated state-changing tests with separate data directory.

## Variable Reference

| Variable | Description | Example Value |
|----------|-------------|---------------|
| `base_url` | Server base URL with scheme | `http://localhost:8080` |
| `auth_username` | Login username | `admin` |
| `auth_password` | Login password | `admin` |
| `data_root` | Server data directory | `./data` (local) or `./data-test-isolated` |
| `backup_scope` | Backup scope query param for create backup | `cases` |
| `case_id` | Case record ID for file operations | `case-007-26` |
| `case_etag` | Optional ETag for PUT/PATCH precondition checks | `` |
| `test_filename` | Filename for file operations | `placeholder.png` |
| `renamed_test_filename` | Destination filename for rename operation | `placeholder-renamed.png` |
| `auth_token` | Authentication token (set after login) | `` |
| `restore_target_store` | Target store for backup restore body | `cases` |
| `restore_mode` | Restore mode for backup restore body | `overwrite` |
| `restore_dry_run` | Restore dry-run flag | `true` |
| `restore_force` | Restore force flag | `false` |
| `backup_name` | Backup filename used for restore endpoint | `cases-2026-02-26T1400.zip` |
| `store_name` | Store name for create store request body | `cases` |
| `job_id` | Job ID for job operations | `` |
| `test_store_name` | Name for isolated test stores | `test-store-{{$timestamp}}` |

## Suite Execution Map

### Smoke Suite (default, non-destructive)
Run order:
1. `system/health`
2. `system/status`
3. `auth/login`
4. protected read endpoints (`stores/get_all`, `cases/get_all`, `files/get_all`, `backups/get_all`, `jobs/get_all`)
5. `auth/logout`

Commands:

```bash
# default API check (maps to smoke)
npm run test:api

# explicit smoke runs
npm run test:api:smoke
npm run test:api:smoke:isolated
```

Smoke requests now include test scripts that fail fast on auth regressions (for protected endpoints, `401` is treated as a failure).

### Mutation Suite (opt-in, state changing)
Run order:
1. prechecks (`system/health`, `system/status`, `system/setup`)
2. `auth/login`
3. mutation flow (`stores/reload`, `stores/create`, `cases/create`, `cases/get_one`, `cases/put`, `cases/patch`, file upload/rename/delete, backup create/list/restore, jobs get/cancel, `cases/delete`)
4. `auth/logout`

Command:

```bash
npm run test:api:mutation
```

Mutation requests now include status assertions and response-chaining scripts.

## Isolation Strategy (required for mutation suite)

Use a dedicated disposable data root before running mutations:

```bash
# terminal 1
npm run serve:api:isolated

# terminal 2
npm run test:api:mutation
```

This keeps mutation traffic out of normal local data (`./data`) by using `./data-test-isolated`.

## Variable Dependencies Between Requests

The mutation flow depends on these environment variables:

- `case_id`: used by `cases/get_one`, `cases/put`, `cases/patch`, `cases/delete`, and all `files/*` requests.
- `test_filename` and `renamed_test_filename`: used by upload/get/rename/delete file requests.
- `backup_scope` and `backup_name`: used by backup create/restore flow.
- `restore_target_store`, `restore_mode`, `restore_dry_run`, `restore_force`: restore request body controls.
- `job_id`: required by `jobs/cancel`.
- `store_name`: used by `stores/create`.

At runtime, these values are now automatically captured when available:

- `auth/login` captures `auth_token` when a token field exists.
- `stores/reload`, `backups/create`, and `backups/restore` capture `job_id` from `jobId`.
- `cases/create` captures `case_id` from response `id`.
- `cases/get_one`, `cases/put`, and `cases/patch` capture `case_etag` from response `ETag` header.
- `backups/get_all` captures `backup_name` from the first backup entry when present.
- `files/rename` rotates `test_filename` and `renamed_test_filename` runtime values so file operations remain coherent after rename.

If you need per-run values, override at runtime, for example:

```bash
npx bru run api/cases/create.yml --env-file api/environments/test-isolated.json --env-var case_id=test-case-$(date +%s)
```

## Usage

```bash
# Run against local environment
npm run test:api

# Run against isolated test environment
npm run test:api:smoke:isolated

# Run a specific request
npx bru run api/auth/login.yml --env-file api/environments/local.json
```

## Adding Test Fixtures

Place test files in `api/test-fixtures/`. The upload request currently points to `api/test-fixtures/placeholder.png`; update `api/files/upload.yml` and your fixture filename together.
