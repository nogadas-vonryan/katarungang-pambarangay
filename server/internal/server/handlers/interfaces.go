package handlers

import (
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
