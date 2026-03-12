package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
)
