# Backup Package

Backup and restore functionality for kp-cms stores.

## Overview

Provides atomic backup/restore for individual stores or complete data directory. Backups are stored as ZIP archives containing store data and a manifest file.

## Key Components

| File | Purpose |
|------|---------|
| `types.go` | Data structures: BackupMeta, RestoreOptions, BackupResult, Manifest |
| `errors.go` | Sentinel errors for backup operations |
| `constants.go` | Paths and exclusion lists |
| `locker.go` | Store-level locking for concurrent operation safety |
| `index.go` | Backup catalog management (`.backups/index.json`) |
| `manifest.go` | Manifest read/write inside ZIP archives |
| `archive.go` | ZIP creation with staging directory, extraction |
| `backup.go` | BackupManager - create, list, get, delete backups |
| `restore.go` | RestoreBackup with dry-run and rollback support |

## Usage

```go
// Create backup manager
mgr, err := backup.NewBackupManager(dataDir, logger)

// Create backup for a single store
result, err := mgr.CreateBackup(ctx, "cases", storePaths, "admin")

// Create backup for all stores
result, err := mgr.CreateBackup(ctx, "all", storePaths, "admin")

// List all backups
backups := mgr.ListBackups()

// Get specific backup
meta, err := mgr.GetBackup("cases-2026-03-15.zip")

// Restore backup (dry-run)
dryRun, err := mgr.RestoreBackup(ctx, "cases-2026-03-15.zip", "cases", 
    backup.RestoreOptions{DryRun: true})

// Restore backup (actual)
result, err := mgr.RestoreBackup(ctx, "cases-2026-03-15.zip", "cases", 
    backup.RestoreOptions{DryRun: false})

// Delete backup
err = mgr.Delete("cases-2026-03-15.zip")
```

## Backup Structure

```
.backups/
├── index.json
├── cases-2026-03-15.zip
│   ├── _manifest.json
│   └── cases/
│       ├── .store.json
│       ├── case-001/
│       │   ├── .meta.json
│       │   └── files/
│       └── case-002/
│           └── ...
├── notes-2026-03-15.zip
└── complete-backup-2026-03-15.zip
    ├── _manifest.json
    ├── cases/
    ├── notes/
    └── inhabitants/
```

## Manifest

Each backup contains `_manifest.json`:

```json
{
  "scope": "cases",
  "scopeType": "store",
  "timestamp": "2026-03-15T14:00:00Z",
  "storeType": "folder",
  "version": 1
}
```

- `scope`: Store name or "all"
- `scopeType`: "store" for single store, "complete" for all stores

## Locking

`StoreLocker` prevents concurrent backup/restore operations on the same store. Does not interfere with normal store read/write operations.

## Exclusions

The following directories are excluded from backups:
- `.backups/`
- `.config/`

## Dry-Run

Returns diff of what would change without applying changes:
- `WouldDelete`: Records that would be removed
- `WouldCreate`: Records that would be created
- `WouldUpdate`: Records that would be modified

## Rollback

On failed restore, current state is copied to `.backups/.restore-temp/{timestamp}/` for manual recovery.
