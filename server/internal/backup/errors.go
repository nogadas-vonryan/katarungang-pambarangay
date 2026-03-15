package backup

import "errors"

var (
	ErrBackupNotFound    = errors.New("backup not found")
	ErrBackupCorrupted   = errors.New("backup archive is corrupted")
	ErrScopeMismatch     = errors.New("backup scope does not match target")
	ErrStoreLocked       = errors.New("store is locked")
	ErrStoreNotFound     = errors.New("target store not found")
	ErrInvalidManifest   = errors.New("invalid manifest in archive")
	ErrRestoreFailed     = errors.New("restore operation failed")
	ErrBackupInProgress  = errors.New("backup already in progress for this scope")
	ErrRestoreInProgress = errors.New("restore already in progress for this store")
)
