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

### local (`environments/local.json`)
For normal local server testing against `localhost:8080`.

### test-isolated (`environments/test-isolated.json`)
For isolated state-changing tests with separate data directory.

## Variable Reference

| Variable | Description | Example Value |
|----------|-------------|---------------|
| `base_url` | Server base URL | `localhost:8080` |
| `data_root` | Server data directory | `./data` (local) or `./data-test-isolated` |
| `case_id` | Case record ID for file operations | `case-007-26` |
| `test_filename` | Filename for file operations | `caffeine.png` |
| `auth_token` | Authentication token (set after login) | `` |
| `backup_name` | Backup filename used for restore endpoint | `cases-2026-02-26T1400.zip` |
| `job_id` | Job ID for job operations | `` |
| `test_store_name` | Name for test stores | `test-store-{{$timestamp}}` |

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

Place test files in `api/test-fixtures/`. The upload request currently points to `api/test-fixtures/caffeine.png`; update `api/files/upload.yml` and/or your fixture filename together.
