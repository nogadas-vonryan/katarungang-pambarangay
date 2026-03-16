package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type JobType string

const (
	JobStoreScan     JobType = "store_scan"
	JobIndexRebuild  JobType = "index_rebuild"
	JobBackupCreate  JobType = "backup_create"
	JobBackupRestore JobType = "backup_restore"
)

type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

var (
	ErrJobNotFound      = errors.New("job not found")
	ErrJobNotCancelable = errors.New("job is not cancellable")
)

type Job struct {
	ID          string                 `json:"id"`
	Type        JobType                `json:"type"`
	Status      JobStatus              `json:"status"`
	CancelRequested bool               `json:"cancelRequested,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
	StartedAt   *time.Time             `json:"startedAt,omitempty"`
	CompletedAt *time.Time             `json:"completedAt,omitempty"`
}

type JobHandler func(ctx context.Context, job *Job) error

type DuplicateJobError struct {
	ExistingJobID string
	JobType       JobType
	Scope         string
}

func (e *DuplicateJobError) Error() string {
	return fmt.Sprintf("duplicate job for type=%s scope=%s", e.JobType, e.Scope)
}

func IsDuplicateJobError(err error) bool {
	var dupErr *DuplicateJobError
	return errors.As(err, &dupErr)
}

func DuplicateJobErrorFrom(err error) (*DuplicateJobError, bool) {
	var dupErr *DuplicateJobError
	ok := errors.As(err, &dupErr)
	return dupErr, ok
}

type JobManager struct {
	mu            sync.RWMutex
	jobs          map[string]*Job
	queue         chan *Job
	jobCancels    map[string]context.CancelFunc
	handlers      map[JobType]JobHandler
	logger        *slog.Logger
	workers       int
	closed        bool
	maxJobs       int
	jobTTL        time.Duration
	cleanupPeriod time.Duration
}

type JobManagerConfig struct {
	Workers       int
	Logger        *slog.Logger
	MaxJobs       int
	JobTTL        time.Duration
	CleanupPeriod time.Duration
	QueueSize     int
}

func New(cfg JobManagerConfig) *JobManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MaxJobs == 0 {
		cfg.MaxJobs = 1000
	}
	if cfg.JobTTL == 0 {
		cfg.JobTTL = 24 * time.Hour
	}
	if cfg.CleanupPeriod == 0 {
		cfg.CleanupPeriod = 1 * time.Hour
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 100
	}
	return &JobManager{
		jobs:          make(map[string]*Job),
		queue:         make(chan *Job, cfg.QueueSize),
		jobCancels:    make(map[string]context.CancelFunc),
		handlers:      make(map[JobType]JobHandler),
		logger:        cfg.Logger,
		workers:       cfg.Workers,
		maxJobs:       cfg.MaxJobs,
		jobTTL:        cfg.JobTTL,
		cleanupPeriod: cfg.CleanupPeriod,
	}
}

func (m *JobManager) RegisterHandler(jobType JobType, handler JobHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[jobType] = handler
}

func (m *JobManager) Start(ctx context.Context) {
	m.mu.Lock()
	m.closed = false
	m.mu.Unlock()

	for i := 0; i < m.workers; i++ {
		go m.worker(ctx, i)
	}

	go m.cleanupLoop(ctx)

	m.logger.Info("job manager started", "workers", m.workers, "maxJobs", m.maxJobs, "jobTTL", m.jobTTL)
}

func (m *JobManager) worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-m.queue:
			if !ok {
				return
			}
			m.runJob(ctx, job)
		}
	}
}

func (m *JobManager) runJob(ctx context.Context, job *Job) {
	m.mu.Lock()
	if job.Status == JobStatusCancelled {
		if job.CompletedAt == nil {
			completed := time.Now()
			job.CompletedAt = &completed
		}
		m.jobs[job.ID] = job
		m.mu.Unlock()
		m.logger.Info("job skipped (already cancelled)", "id", job.ID, "type", job.Type)
		return
	}

	now := time.Now()
	job.Status = JobStatusRunning
	job.CancelRequested = false
	job.StartedAt = &now
	jobCtx, cancel := context.WithCancel(ctx)
	m.jobCancels[job.ID] = cancel
	m.jobs[job.ID] = job
	m.mu.Unlock()

	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.jobCancels, job.ID)
		m.mu.Unlock()
	}()

	m.logger.Info("job started", "id", job.ID, "type", job.Type)

	m.mu.RLock()
	handler, ok := m.handlers[job.Type]
	m.mu.RUnlock()

	finalStatus := job.Status
	finalError := job.Error
	finalCancelRequested := job.CancelRequested

	if !ok {
		finalStatus = JobStatusFailed
		finalCancelRequested = false
		finalError = fmt.Sprintf("no handler for job type %s", job.Type)
	} else {
		if err := handler(jobCtx, job); err != nil {
			if errors.Is(err, context.Canceled) {
				finalStatus = JobStatusCancelled
				finalCancelRequested = false
				finalError = "job cancelled"
				m.logger.Info("job cancelled", "id", job.ID)
			} else {
				finalStatus = JobStatusFailed
				finalCancelRequested = false
				finalError = err.Error()
				m.logger.Error("job failed", "id", job.ID, "err", err)
			}
		} else {
			m.mu.RLock()
			cancelRequested := job.CancelRequested
			m.mu.RUnlock()

			if cancelRequested {
				finalStatus = JobStatusCancelled
				finalCancelRequested = false
				finalError = "job cancelled"
				m.logger.Info("job cancelled", "id", job.ID)
			} else {
				finalStatus = JobStatusCompleted
				finalCancelRequested = false
				finalError = ""
				m.logger.Info("job completed", "id", job.ID)
			}
		}
	}

	completed := time.Now()

	m.mu.Lock()
	job.Status = finalStatus
	job.CancelRequested = finalCancelRequested
	job.Error = finalError
	job.CompletedAt = &completed
	m.jobs[job.ID] = job
	m.mu.Unlock()
}

func (m *JobManager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[id]
	if !ok {
		return ErrJobNotFound
	}

	switch job.Status {
	case JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
		return ErrJobNotCancelable
	case JobStatusQueued:
		job.Status = JobStatusCancelled
		job.CancelRequested = false
		job.Error = "job cancelled"
		completed := time.Now()
		job.CompletedAt = &completed
		m.jobs[id] = job
		return nil
	case JobStatusRunning:
		job.CancelRequested = true
		job.Error = "cancellation requested"
		m.jobs[id] = job
		if cancel, ok := m.jobCancels[id]; ok {
			cancel()
		}
		return nil
	default:
		return ErrJobNotCancelable
	}
}

func (m *JobManager) Enqueue(job *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("job manager is closed")
	}

	scope, shouldDedupe := dedupeScope(job)
	if shouldDedupe {
		for _, existing := range m.jobs {
			if existing.Type != job.Type {
				continue
			}
			existingScope, existingShouldDedupe := dedupeScope(existing)
			if !existingShouldDedupe || existingScope != scope {
				continue
			}
			if existing.Status == JobStatusQueued || existing.Status == JobStatusRunning {
				return &DuplicateJobError{
					ExistingJobID: existing.ID,
					JobType:       existing.Type,
					Scope:         scope,
				}
			}
		}
	}

	job.ID = generateJobID()
	job.Status = JobStatusQueued
	job.CreatedAt = time.Now()

	select {
	case m.queue <- job:
		m.jobs[job.ID] = job
		return nil
	default:
		return fmt.Errorf("job queue is full")
	}
}

func dedupeScope(job *Job) (string, bool) {
	if job == nil {
		return "", false
	}

	switch job.Type {
	case JobStoreScan:
		return "global", true
	case JobBackupCreate:
		scope := "all"
		if job.Payload != nil {
			if v, ok := job.Payload["scope"].(string); ok && v != "" {
				scope = v
			}
		}
		return scope, true
	case JobBackupRestore:
		targetStore := "all"
		if job.Payload != nil {
			if v, ok := job.Payload["targetStore"].(string); ok && v != "" {
				targetStore = v
			}
		}
		return targetStore, true
	default:
		return "", false
	}
}

func (m *JobManager) Get(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

func (m *JobManager) List() []*Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (m *JobManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *JobManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var toDelete []string

	for id, job := range m.jobs {
		if job.Status == JobStatusQueued || job.Status == JobStatusRunning {
			continue
		}

		if job.CompletedAt != nil && now.Sub(*job.CompletedAt) > m.jobTTL {
			toDelete = append(toDelete, id)
			continue
		}
	}

	if len(m.jobs)-len(toDelete) > m.maxJobs && len(toDelete) < len(m.jobs) {
		type jobWithTime struct {
			id        string
			completed time.Time
		}
		var completedJobs []jobWithTime
		for id, job := range m.jobs {
			if job.Status != JobStatusQueued && job.Status != JobStatusRunning && job.CompletedAt != nil {
				completedJobs = append(completedJobs, jobWithTime{id, *job.CompletedAt})
			}
		}
		sort.Slice(completedJobs, func(i, j int) bool {
			return completedJobs[i].completed.Before(completedJobs[j].completed)
		})

		excess := len(m.jobs) - m.maxJobs
		for i := 0; i < excess && i < len(completedJobs); i++ {
			toDelete = append(toDelete, completedJobs[i].id)
		}
	}

	for _, id := range toDelete {
		delete(m.jobs, id)
	}

	if len(toDelete) > 0 {
		m.logger.Info("cleaned up jobs", "count", len(toDelete), "remaining", len(m.jobs))
	}
}

func (m *JobManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, cancel := range m.jobCancels {
		cancel()
	}
	m.jobCancels = make(map[string]context.CancelFunc)
	close(m.queue)
	return nil
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().Unix(), time.Now().Nanosecond()%1000)
}

type JobResponse struct {
	ID          string                 `json:"id"`
	Type        JobType                `json:"type"`
	Status      JobStatus              `json:"status"`
	CancelRequested bool               `json:"cancelRequested,omitempty"`
	CreatedAt   string                 `json:"createdAt"`
	StartedAt   *string                `json:"startedAt,omitempty"`
	CompletedAt *string                `json:"completedAt,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Result      map[string]interface{} `json:"result,omitempty"`
}

func JobToResponse(job *Job) *JobResponse {
	resp := &JobResponse{
		ID:              job.ID,
		Type:            job.Type,
		Status:          job.Status,
		CancelRequested: job.CancelRequested,
		CreatedAt:       job.CreatedAt.Format(time.RFC3339),
		Result:          job.Result,
		Error:           job.Error,
	}
	if job.StartedAt != nil {
		t := job.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &t
	}
	if job.CompletedAt != nil {
		t := job.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &t
	}
	return resp
}
