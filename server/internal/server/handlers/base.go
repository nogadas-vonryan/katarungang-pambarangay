package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kp-cms/server/internal/store"
)

type BaseHandler struct {
	stores *StoreAccessor
}

func (h *BaseHandler) GetStore(name string) (store.Store, bool) {
	return h.stores.Get(name)
}

func (h *BaseHandler) GetStoreOrError(w http.ResponseWriter, r *http.Request) (store.Store, bool) {
	storeName := chi.URLParam(r, "store")
	st, ok := h.stores.Get(storeName)
	if !ok {
		WriteError(w, r, http.StatusNotFound, ErrCodeNotFound, ErrMsgStoreNotFound)
		return nil, false
	}
	return st, true
}

func (h *BaseHandler) ValidateETag(ctx context.Context, st store.Store, id string, ifMatch string) (*store.Record, bool, error) {
	if ifMatch == "" {
		return nil, true, nil
	}

	existing, err := st.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}

	if ifMatch != existing.ETag {
		return existing, false, nil
	}

	return existing, true, nil
}

func (h *BaseHandler) ValidateETagFromRequest(r *http.Request, st store.Store, id string) (*store.Record, bool, error) {
	return h.ValidateETag(r.Context(), st, id, r.Header.Get("If-Match"))
}
