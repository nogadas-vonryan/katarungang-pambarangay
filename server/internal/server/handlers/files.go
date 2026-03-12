package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/kp-cms/server/internal/sanitize"
)

const maxFileSize = 10 * 1024 * 1024

type FileHandler struct {
	BaseHandler
}

func NewFileHandler(stores *StoreAccessor) *FileHandler {
	return &FileHandler{
		BaseHandler: BaseHandler{
			stores: stores,
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

	if !sanitize.FileName(filename) {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, "cannot upload file with that name")
		return
	}

	limitedReader := http.MaxBytesReader(w, file, maxFileSize)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			WriteError(w, r, http.StatusRequestEntityTooLarge, ErrCodeBadRequest, "file size exceeds maximum allowed (10MB)")
			return
		}
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
