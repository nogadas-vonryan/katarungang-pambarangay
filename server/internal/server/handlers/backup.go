package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		var req createBackupRequest
		if err := decodeJSONOptional(r, &req); err != nil {
			WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidBody)
			return
		}
		scope = strings.TrimSpace(req.Scope)
	}

	if scope == "" {
		scope = "all"
	}

	if scope != "all" {
		if _, ok := h.provider.Get(scope); !ok {
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
			"scope":      scope,
			"storePaths": storePaths,
			"createdBy":  createdBy,
		},
	}

	if err := h.jobsMgr.Enqueue(job); err != nil {
		if dupErr, ok := jobs.DuplicateJobErrorFrom(err); ok {
			WriteErrorWithDetails(w, r, http.StatusConflict, ErrCodeConflict, ErrMsgDuplicateJob, map[string]interface{}{"jobId": dupErr.ExistingJobID})
			return
		}
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, ErrMsgJobEnqueueFailed)
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":     job.ID,
		"type":      job.Type,
		"statusUrl": fmt.Sprintf("/jobs/%s", job.ID),
		"message":   "backup creation started",
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
	Mode        string `json:"mode,omitempty"`
	DryRun      bool   `json:"dryRun,omitempty"`
	Force       bool   `json:"force,omitempty"`
}

func (h *BackupHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	backupName := chi.URLParam(r, "name")

	backupMeta, err := h.backupMgr.GetBackup(backupName)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeBackupNotFound, ErrMsgBackupNotFound)
		return
	}

	var req restoreBackupRequest
	if err := decodeJSONOptional(r, &req); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidBody)
		return
	}

	if req.TargetStore == "" {
		if backupMeta.Scope != "all" {
			req.TargetStore = backupMeta.Scope
		} else {
			req.TargetStore = "all"
		}
	}
	if req.TargetStore != "all" {
		if _, ok := h.provider.Get(req.TargetStore); !ok {
			WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
			return
		}
	}

	if req.Mode == "" {
		req.Mode = "overwrite"
	}
	if req.Mode != "overwrite" {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidRestoreMode)
		return
	}

	job := &jobs.Job{
		Type: jobs.JobBackupRestore,
		Payload: map[string]interface{}{
			"backupName":  backupName,
			"targetStore": req.TargetStore,
			"mode":        req.Mode,
			"dryRun":      req.DryRun,
			"force":       req.Force,
		},
	}

	if err := h.jobsMgr.Enqueue(job); err != nil {
		if dupErr, ok := jobs.DuplicateJobErrorFrom(err); ok {
			WriteErrorWithDetails(w, r, http.StatusConflict, ErrCodeConflict, ErrMsgDuplicateJob, map[string]interface{}{"jobId": dupErr.ExistingJobID})
			return
		}
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, ErrMsgJobEnqueueFailed)
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":     job.ID,
		"type":      job.Type,
		"statusUrl": fmt.Sprintf("/jobs/%s", job.ID),
		"message":   "restore operation started",
	})
}

func decodeJSONOptional(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return nil
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	return nil
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
