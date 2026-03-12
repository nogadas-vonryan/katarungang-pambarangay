package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kp-cms/server/internal/jobs"
)

type JobHandler struct {
	jobsMgr *jobs.JobManager
}

func NewJobHandler(jobsMgr *jobs.JobManager) *JobHandler {
	return &JobHandler{
		jobsMgr: jobsMgr,
	}
}

func (h *JobHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	jobsList := h.jobsMgr.List()
	resp := make([]*jobs.JobResponse, len(jobsList))
	for i, job := range jobsList {
		resp[i] = jobs.JobToResponse(job)
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": resp,
	})
}

func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, ok := h.jobsMgr.Get(id)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, "job not found")
		return
	}
	WriteJSON(w, http.StatusOK, jobs.JobToResponse(job))
}
