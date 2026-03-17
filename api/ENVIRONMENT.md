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

## Usage

```bash
# Run against local environment
npm run test:api

# Run against isolated test environment
npm run test:api:isolated

# Run a specific request
cd api && bru run auth/login.yml --env-file environments/local.json
```

## Adding Test Fixtures

Place test files in `api/test-fixtures/`. The upload request currently points to `api/test-fixtures/placeholder.png`; update `api/files/upload.yml` and your fixture filename together.
