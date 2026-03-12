package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/kp-cms/server/internal/store"
)

type recordStoreProvider struct {
	stores  map[string]store.Store
	storeMu *sync.RWMutex
}

func (p *recordStoreProvider) Get(name string) (store.Store, bool) {
	p.storeMu.RLock()
	defer p.storeMu.RUnlock()
	st, ok := p.stores[name]
	return st, ok
}

func (p *recordStoreProvider) All() map[string]store.Store {
	p.storeMu.RLock()
	defer p.storeMu.RUnlock()
	return p.stores
}

type RecordHandler struct {
	BaseHandler
}

func NewRecordHandler(stores map[string]store.Store, storeMu *sync.RWMutex) *RecordHandler {
	return &RecordHandler{
		BaseHandler: BaseHandler{
			stores: &recordStoreProvider{stores: stores, storeMu: storeMu},
		},
	}
}

func (h *RecordHandler) ListRecords(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	opts := store.ListOptions{
		Limit:    100,
		Offset:   0,
		SortBy:   "id",
		SortDesc: false,
		Filter:   make(map[string]interface{}),
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}
	if v := r.URL.Query().Get("sort"); v != "" {
		opts.SortDesc = v[0] == '-'
		if opts.SortDesc {
			opts.SortBy = v[1:]
		} else {
			opts.SortBy = v
		}
	}

	filterKeys := []string{"filter", "q"}
	for _, key := range filterKeys {
		if v := r.URL.Query().Get(key); v != "" {
			if key == "q" {
				opts.Filter["$search"] = v
			} else {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					opts.Filter[parts[0]] = parts[1]
				}
			}
		}
	}

	for k, v := range r.URL.Query() {
		if k == "limit" || k == "offset" || k == "sort" || k == "filter" || k == "q" {
			continue
		}
		if len(v) > 0 {
			opts.Filter[k] = v[0]
		}
	}

	records, total, err := st.List(r.Context(), opts)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"records": records,
		"total":   total,
		"limit":   opts.Limit,
		"offset":  opts.Offset,
	})
}

func (h *RecordHandler) CreateRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	var data map[string]interface{}
	if err := decodeJSON(w, r, &data); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidJSON)
		return
	}

	record, err := st.Create(r.Context(), data)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v1/%s/records/%s", storeName, record.ID))
	WriteJSON(w, http.StatusCreated, record)
}

func (h *RecordHandler) GetRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	id := chi.URLParam(r, "id")
	record, err := st.Get(r.Context(), id)
	if err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgRecordNotFound)
		return
	}

	w.Header().Set("ETag", record.ETag)
	WriteJSON(w, http.StatusOK, record)
}

func (h *RecordHandler) ReplaceRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	id := chi.URLParam(r, "id")

	if _, valid, err := h.ValidateETagFromRequest(r, st, id); err != nil || !valid {
		if err != nil {
			WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgRecordNotFound)
			return
		}
		WriteError(w, r, http.StatusPreconditionFailed, ErrCodePreconditionFailed, "ETag mismatch")
		return
	}

	var data map[string]interface{}
	if err := decodeJSON(w, r, &data); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidJSON)
		return
	}

	record, err := st.Replace(r.Context(), id, data)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	w.Header().Set("ETag", record.ETag)
	WriteJSON(w, http.StatusOK, record)
}

func (h *RecordHandler) UpdateRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	id := chi.URLParam(r, "id")

	if _, valid, err := h.ValidateETagFromRequest(r, st, id); err != nil || !valid {
		if err != nil {
			WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgRecordNotFound)
			return
		}
		WriteError(w, r, http.StatusPreconditionFailed, ErrCodePreconditionFailed, "ETag mismatch")
		return
	}

	var data map[string]interface{}
	if err := decodeJSON(w, r, &data); err != nil {
		WriteError(w, r, http.StatusBadRequest, ErrCodeBadRequest, ErrMsgInvalidJSON)
		return
	}

	record, err := st.Update(r.Context(), id, data)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	w.Header().Set("ETag", record.ETag)
	WriteJSON(w, http.StatusOK, record)
}

func (h *RecordHandler) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.GetStore(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return
	}

	id := chi.URLParam(r, "id")

	if err := st.Delete(r.Context(), id); err != nil {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgRecordNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
