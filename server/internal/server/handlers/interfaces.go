package handlers

import (
	"context"

	"github.com/kp-cms/server/internal/backup"
	"github.com/kp-cms/server/internal/jobs"
	"github.com/kp-cms/server/internal/store"
)

type StoreProvider interface {
	Get(name string) (store.Store, bool)
	All() map[string]store.Store
}

type JobManager interface {
	Enqueue(job *jobs.Job) error
	Get(id string) (*jobs.Job, bool)
	List() []*jobs.Job
}

type BackupManager interface {
	CreateBackup(ctx context.Context, scope string, storePaths map[string]backup.StoreInfo, createdBy string) (*backup.BackupResult, error)
	ListBackups() []backup.BackupMeta
	GetBackup(name string) (*backup.BackupMeta, error)
	RestoreBackup(ctx context.Context, backupName, targetStore string, opts backup.RestoreOptions) (*backup.DryRunResult, error)
	Delete(name string) error
}
