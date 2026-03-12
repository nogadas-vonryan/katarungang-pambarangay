package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type JobType string

const (
	JobStoreScan    JobType = "store_scan"
	JobIndexRebuild JobType = "index_rebuild"
)

type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

type Job struct {
	ID          string                 `json:"id"`
	Type        JobType                `json:"type"`
	Status      JobStatus              `json:"status"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
	StartedAt   *time.Time             `json:"startedAt,omitempty"`
	CompletedAt *time.Time             `json:"completedAt,omitempty"`
}

type JobHandler func(ctx context.Context, job *Job) error

type JobManager struct {
	mu            sync.RWMutex
	jobs          map[string]*Job
	queue         chan *Job
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
	return &JobManager{
		jobs:          make(map[string]*Job),
		queue:         make(chan *Job, 100),
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
	now := time.Now()
	job.Status = JobStatusRunning
	job.StartedAt = &now

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()

	m.logger.Info("job started", "id", job.ID, "type", job.Type)

	m.mu.RLock()
	handler, ok := m.handlers[job.Type]
	m.mu.RUnlock()

	if !ok {
		job.Status = JobStatusFailed
		job.Error = fmt.Sprintf("no handler for job type %s", job.Type)
	} else {
		if err := handler(ctx, job); err != nil {
			job.Status = JobStatusFailed
			job.Error = err.Error()
			m.logger.Error("job failed", "id", job.ID, "err", err)
		} else {
			job.Status = JobStatusCompleted
			m.logger.Info("job completed", "id", job.ID)
		}
	}

	completed := time.Now()
	job.CompletedAt = &completed

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
}

func (m *JobManager) Enqueue(job *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("job manager is closed")
	}

	job.ID = generateJobID()
	job.Status = JobStatusQueued
	job.CreatedAt = time.Now()

	m.jobs[job.ID] = job

	select {
	case m.queue <- job:
		return nil
	default:
		return fmt.Errorf("job queue is full")
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
	CreatedAt   string                 `json:"createdAt"`
	StartedAt   *string                `json:"startedAt,omitempty"`
	CompletedAt *string                `json:"completedAt,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Result      map[string]interface{} `json:"result,omitempty"`
}

func JobToResponse(job *Job) *JobResponse {
	resp := &JobResponse{
		ID:        job.ID,
		Type:      job.Type,
		Status:    job.Status,
		CreatedAt: job.CreatedAt.Format(time.RFC3339),
		Result:    job.Result,
		Error:     job.Error,
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
