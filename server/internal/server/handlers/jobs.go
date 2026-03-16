package handlers

import (
	"errors"
	"fmt"
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

func (h *JobHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.jobsMgr.Cancel(id); err != nil {
		switch {
		case errors.Is(err, jobs.ErrJobNotFound):
			WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgJobNotFound)
			return
		case errors.Is(err, jobs.ErrJobNotCancelable):
			WriteError(w, r, http.StatusConflict, ErrCodeConflict, "job is not cancellable")
			return
		default:
			WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to cancel job")
			return
		}
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":     id,
		"statusUrl": fmt.Sprintf("/v1/jobs/%s", id),
		"message":   "job cancellation requested",
	})
}
