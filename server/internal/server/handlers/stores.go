package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/kp-cms/server/internal/jobs"
	"github.com/kp-cms/server/internal/sanitize"
	"github.com/kp-cms/server/internal/store"
)

var validStoreName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type createStoreRequest struct {
	Name          string                 `json:"name"`
	Type          string                 `json:"type"`
	Description   string                 `json:"description,omitempty"`
	Schema        map[string]interface{} `json:"schema,omitempty"`
	NamingPattern string                 `json:"namingPattern,omitempty"`
}

type StoreHandler struct {
	stores  *StoreAccessor
	dataDir string
	jobsMgr *jobs.JobManager
}

func NewStoreHandler(stores *StoreAccessor, dataDir string, jobsMgr *jobs.JobManager) *StoreHandler {
	return &StoreHandler{
		stores:  stores,
		dataDir: dataDir,
		jobsMgr: jobsMgr,
	}
}

func (h *StoreHandler) ListStores(w http.ResponseWriter, r *http.Request) {
	allStores := h.stores.All()
	stores := make([]interface{}, 0, len(allStores))
	for _, st := range allStores {
		meta, _ := st.Metadata()
		stores = append(stores, meta)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"stores": stores,
		"total":  len(stores),
	})
}

func (h *StoreHandler) CreateStore(w http.ResponseWriter, r *http.Request) {
	var req createStoreRequest

	if err := decodeJSON(w, r, &req); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidBody)
		return
	}

	if req.Name == "" {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "name is required")
		return
	}

	if !validStoreName.MatchString(req.Name) {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid store name")
		return
	}

	if !sanitize.Path(req.Name) {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid store name: path traversal detected")
		return
	}

	if req.Type == "" {
		req.Type = "folder"
	}

	if !store.IsValidType(req.Type) {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid store type: "+req.Type)
		return
	}

	storePath := filepath.Join(h.dataDir, req.Name)

	if h.stores.Exists(req.Name) {
		WriteError(w, r, http.StatusConflict, ErrCodeConflict, "store already exists")
		return
	}

	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		WriteError(w, r, http.StatusConflict, ErrCodeConflict, "store directory already exists")
		return
	}

	if err := os.MkdirAll(storePath, 0755); err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to create store directory")
		return
	}

	meta := store.StoreMetadata{
		Name:          req.Name,
		Type:          req.Type,
		Path:          storePath,
		Description:   req.Description,
		Schema:        req.Schema,
		NamingPattern: req.NamingPattern,
		Counter:       0,
		IndexReady:    false,
	}

	metaFile := store.MetadataToMetaFile(&meta, 1)

	metaData, err := store.WriteStoreMeta(metaFile)
	if err != nil {
		os.RemoveAll(storePath)
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to marshal metadata")
		return
	}

	metaPath := filepath.Join(storePath, ".store.json")
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		os.RemoveAll(storePath)
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to write metadata file")
		return
	}

	st, err := store.CreateStore(req.Type, storePath, &meta)
	if err != nil {
		os.RemoveAll(storePath)
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to create store: "+err.Error())
		return
	}

	if h.stores.Exists(req.Name) {
		os.RemoveAll(storePath)
		WriteError(w, r, http.StatusConflict, ErrCodeConflict, "store was created by another request")
		return
	}

	h.stores.Set(req.Name, st)

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "store created",
		"store":   meta,
	})
}

func (h *StoreHandler) ReloadStores(w http.ResponseWriter, r *http.Request) {
	job := &jobs.Job{
		Type: jobs.JobStoreScan,
	}

	if err := h.jobsMgr.Enqueue(job); err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to enqueue job")
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":   job.ID,
		"message": "store reload initiated",
	})
}
