package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/kp-cms/server/internal/store"
)

type StoreAccessor struct {
	stores  map[string]store.Store
	storeMu *sync.RWMutex
}

func NewStoreAccessor(stores map[string]store.Store, storeMu *sync.RWMutex) *StoreAccessor {
	return &StoreAccessor{
		stores:  stores,
		storeMu: storeMu,
	}
}

func (p *StoreAccessor) Get(name string) (store.Store, bool) {
	p.storeMu.RLock()
	defer p.storeMu.RUnlock()
	st, ok := p.stores[name]
	return st, ok
}

func (p *StoreAccessor) All() map[string]store.Store {
	p.storeMu.RLock()
	defer p.storeMu.RUnlock()
	result := make(map[string]store.Store, len(p.stores))
	for k, v := range p.stores {
		result[k] = v
	}
	return result
}

func (p *StoreAccessor) CreateIfNotExists(name string, fn func() (store.Store, error)) (store.Store, error) {
	p.storeMu.Lock()
	defer p.storeMu.Unlock()

	if st, ok := p.stores[name]; ok {
		return st, os.ErrExist
	}

	st, err := fn()
	if err != nil {
		return nil, err
	}
	p.stores[name] = st
	return st, nil
}

const BearerPrefix = "Bearer "

func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func WriteJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func WriteError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode string, message string) {
	correlationID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:          errorCode,
			Message:       message,
			CorrelationID: correlationID,
		},
	})
}

type ErrorBody struct {
	Code          string                 `json:"code"`
	Message       string                 `json:"message"`
	Details       map[string]interface{} `json:"details,omitempty"`
	CorrelationID string                 `json:"correlation_id"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

const (
	ErrCodeUnauthorized       = "UNAUTHORIZED"
	ErrCodeBadRequest         = "BAD_REQUEST"
	ErrCodeNotFound           = "NOT_FOUND"
	ErrCodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	ErrCodeConflict           = "CONFLICT"
	ErrCodePreconditionFailed = "PRECONDITION_FAILED"
	ErrCodeInternal           = "INTERNAL_ERROR"
	ErrCodeInvalidCredentials = "INVALID_CREDENTIALS"
	ErrCodeForbidden          = "FORBIDDEN"
	ErrCodeBackupNotFound     = "BACKUP_NOT_FOUND"
	ErrCodeBackupCorrupted    = "BACKUP_CORRUPTED"
	ErrCodeScopeMismatch      = "SCOPE_MISMATCH"
	ErrCodeStoreLocked        = "STORE_LOCKED"
)

const (
	ErrMsgInvalidJSON      = "invalid JSON"
	ErrMsgInvalidBody      = "invalid request body"
	ErrMsgUnauthorized     = "unauthorized"
	ErrMsgInvalidCreds     = "invalid credentials"
	ErrMsgForbidden        = "forbidden"
	ErrMsgRecordNotFound   = "record not found"
	ErrMsgStoreNotFound    = "store not found"
	ErrMsgResourceNotFound = "resource not found"
	ErrMsgMethodNotAllowed = "method not allowed"
	ErrMsgNameRequired     = "name is required"
	ErrMsgStoreExists      = "store already exists"
	ErrMsgStoreDirExists   = "store directory already exists"
	ErrMsgJobEnqueueFailed = "failed to enqueue job"
	ErrMsgJobNotFound      = "job not found"
	ErrMsgInvalidStoreName = "invalid store name"
	ErrMsgPathTraversal    = "invalid store name: path traversal detected"
	ErrMsgBackupNotFound   = "backup not found"
	ErrMsgBackupCorrupted  = "backup archive is corrupted"
	ErrMsgScopeMismatch    = "backup scope does not match target store"
	ErrMsgStoreLocked      = "store is locked by another operation"
)
