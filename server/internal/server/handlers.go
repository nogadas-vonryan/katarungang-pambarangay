package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/kp-cms/server/internal/server/handlers"
	"github.com/kp-cms/server/internal/store"
)

func (s *Server) handleStoreError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if err == store.ErrRecordNotFound {
		handlers.WriteError(w, r, http.StatusNotFound, handlers.ErrCodeNotFound, handlers.ErrMsgRecordNotFound)
		return true
	}
	handlers.WriteError(w, r, http.StatusInternalServerError, handlers.ErrCodeInternal, err.Error())
	return true
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.storeMu.RLock()
	storeCount := len(s.stores)
	s.storeMu.RUnlock()

	setupStatus := s.setupSvc.GetStatus()

	status := map[string]interface{}{
		"status":      "healthy",
		"mode":        s.cfg.Mode,
		"appName":     s.cfg.AppName,
		"startTime":   time.Now().Format(time.RFC3339),
		"stores":      storeCount,
		"dataDir":     s.cfg.DataDir,
		"setupStatus": setupStatus,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.setupSvc.GetStatus())
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if err := s.setupSvc.Complete(); err != nil {
		handlers.WriteError(w, r, http.StatusBadRequest, handlers.ErrCodeBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "setup completed",
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	handlers.WriteError(w, r, http.StatusNotFound, handlers.ErrCodeNotFound, handlers.ErrMsgResourceNotFound)
}

func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	handlers.WriteError(w, r, http.StatusMethodNotAllowed, handlers.ErrCodeMethodNotAllowed, handlers.ErrMsgMethodNotAllowed)
}
