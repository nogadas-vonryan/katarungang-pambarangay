package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kp-cms/server/internal/backup"
	"github.com/kp-cms/server/internal/jobs"
)

type BackupHandler struct {
	backupMgr BackupManager
	jobsMgr   *jobs.JobManager
	provider  StoreProvider
}

func NewBackupHandler(backupMgr BackupManager, jobsMgr *jobs.JobManager, provider StoreProvider) *BackupHandler {
	return &BackupHandler{
		backupMgr: backupMgr,
		jobsMgr:   jobsMgr,
		provider:  provider,
	}
}

type createBackupRequest struct {
	Scope string `json:"scope"`
}

func (h *BackupHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	var req createBackupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidBody)
		return
	}

	if req.Scope == "" {
		req.Scope = "all"
	}

	if req.Scope != "all" {
		if _, ok := h.provider.Get(req.Scope); !ok {
			WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
			return
		}
	}

	storePaths := h.getStorePaths()
	createdBy := GetUsername(r)
	if createdBy == "" {
		createdBy = "system"
	}

	job := &jobs.Job{
		Type: jobs.JobBackupCreate,
		Payload: map[string]interface{}{
			"scope":      req.Scope,
			"storePaths": storePaths,
			"createdBy":  createdBy,
		},
	}

	if err := h.jobsMgr.Enqueue(job); err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, ErrMsgJobEnqueueFailed)
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":   job.ID,
		"message": "backup creation initiated",
	})
}

func (h *BackupHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	backups := h.backupMgr.ListBackups()
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"backups": backups,
		"total":   len(backups),
	})
}

type restoreBackupRequest struct {
	TargetStore string `json:"targetStore"`
	DryRun      bool   `json:"dryRun,omitempty"`
	Force       bool   `json:"force,omitempty"`
}

func (h *BackupHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	backupName := chi.URLParam(r, "name")

	_, err := h.backupMgr.GetBackup(backupName)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeBackupNotFound, ErrMsgBackupNotFound)
		return
	}

	var req restoreBackupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidBody)
		return
	}

	if req.TargetStore == "" {
		req.TargetStore = "all"
	}

	job := &jobs.Job{
		Type: jobs.JobBackupRestore,
		Payload: map[string]interface{}{
			"backupName":  backupName,
			"targetStore": req.TargetStore,
			"dryRun":      req.DryRun,
			"force":       req.Force,
		},
	}

	if err := h.jobsMgr.Enqueue(job); err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, ErrMsgJobEnqueueFailed)
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":   job.ID,
		"message": "restore initiated",
	})
}

func (h *BackupHandler) getStorePaths() map[string]backup.StoreInfo {
	stores := h.provider.All()
	result := make(map[string]backup.StoreInfo, len(stores))
	for name, st := range stores {
		meta, err := st.Metadata()
		if err != nil {
			continue
		}
		result[name] = backup.StoreInfo{
			Name: meta.Name,
			Path: meta.Path,
			Type: meta.Type,
		}
	}
	return result
}
