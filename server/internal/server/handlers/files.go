package handlers

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/kp-cms/server/internal/store"
)

type fileStoreProvider struct {
	stores  map[string]store.Store
	storeMu *sync.RWMutex
}

func (p *fileStoreProvider) Get(name string) (store.Store, bool) {
	p.storeMu.RLock()
	defer p.storeMu.RUnlock()
	st, ok := p.stores[name]
	return st, ok
}

func (p *fileStoreProvider) All() map[string]store.Store {
	p.storeMu.RLock()
	defer p.storeMu.RUnlock()
	return p.stores
}

type FileHandler struct {
	BaseHandler
}

func NewFileHandler(stores map[string]store.Store, storeMu *sync.RWMutex) *FileHandler {
	return &FileHandler{
		BaseHandler: BaseHandler{
			stores: &fileStoreProvider{stores: stores, storeMu: storeMu},
		},
	}
}

func (h *FileHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	recordID := chi.URLParam(r, "id")

	files, err := st.ListRecordFiles(r.Context(), recordID)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgRecordNotFound)
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
		"total": len(files),
	})
}

func (h *FileHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	recordID := chi.URLParam(r, "id")

	file, header, err := r.FormFile("file")
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "file is required")
		return
	}
	defer file.Close()

	filename := header.Filename
	if filename == "" {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "filename is required")
		return
	}

	if strings.HasPrefix(filename, ".") || filename == ".store.json" || filename == ".meta.json" {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "cannot upload file with that name")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "failed to read file")
		return
	}

	if err := st.UploadFile(r.Context(), recordID, filename, data); err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "file uploaded",
		"name":    filename,
		"size":    header.Size,
	})
}

func (h *FileHandler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	recordID := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	data, err := st.DownloadFile(r.Context(), recordID, filename)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, "file not found")
		return
	}

	filename = filepath.Base(filename)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (h *FileHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	recordID := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	if err := st.DeleteFile(r.Context(), recordID, filename); err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, "file not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
