package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/kp-cms/server/internal/auth"
	"github.com/kp-cms/server/internal/config"
	"github.com/kp-cms/server/internal/jobs"
	"github.com/kp-cms/server/internal/logging"
	"github.com/kp-cms/server/internal/server/handlers"
	"github.com/kp-cms/server/internal/setup"
	"github.com/kp-cms/server/internal/store"
)

type Server struct {
	cfg      *config.Config
	logger   *logging.Logger
	router   *chi.Mux
	httpSrv  *http.Server
	stores   map[string]store.Store
	storeMu  sync.RWMutex
	stopCh   chan struct{}
	authSvc  *auth.AuthService
	jobsMgr  *jobs.JobManager
	setupSvc *setup.SetupService

	storeHandler  *handlers.StoreHandler
	recordHandler *handlers.RecordHandler
	jobHandler    *handlers.JobHandler
	fileHandler   *handlers.FileHandler
}

func New(cfg *config.Config, logger *logging.Logger) (*Server, error) {
	s := &Server{
		cfg:    cfg,
		logger: logger,
		stores: make(map[string]store.Store),
		stopCh: make(chan struct{}),
	}

	if err := s.initAuth(); err != nil {
		return nil, fmt.Errorf("init auth: %w", err)
	}

	if err := s.initJobs(); err != nil {
		return nil, fmt.Errorf("init jobs: %w", err)
	}

	if err := s.initSetup(); err != nil {
		return nil, fmt.Errorf("init setup: %w", err)
	}

	if err := s.initStores(); err != nil {
		return nil, fmt.Errorf("init stores: %w", err)
	}

	s.initHandlers()

	s.setupRouter()
	s.setupHTTPServer()

	return s, nil
}

func (s *Server) Start() error {
	s.logger.App().Info("starting HTTP server", "addr", s.httpSrv.Addr)

	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.App().Error("HTTP server error", "err", err)
		}
	}()

	s.logger.App().Info("HTTP server started")
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	s.logger.App().Info("stopping HTTP server")

	if s.jobsMgr != nil {
		s.jobsMgr.Close()
	}

	if s.authSvc != nil {
		s.authSvc.Close()
	}

	if err := s.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	for name, st := range s.stores {
		if err := st.Close(); err != nil {
			s.logger.App().Warn("failed to close store", "name", name, "err", err)
		}
	}

	s.logger.App().Info("HTTP server stopped")
	return nil
}

func (s *Server) getStore(name string) (store.Store, bool) {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	st, ok := s.stores[name]
	return st, ok
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func (s *Server) validateETag(r *http.Request, st store.Store, id string) (*store.Record, bool, error) {
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		return nil, true, nil
	}

	existing, err := st.Get(r.Context(), id)
	if err != nil {
		return nil, false, err
	}

	if ifMatch != existing.ETag {
		return existing, false, nil
	}

	return existing, true, nil
}

func getUserFromContext(ctx context.Context) interface{} {
	return ctx.Value(ctxKeyUser)
}

func contextWithUser(ctx context.Context, user interface{}) context.Context {
	return context.WithValue(ctx, ctxKeyUser, user)
}

func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, middleware.RequestIDKey, id)
}

func (s *Server) initHandlers() {
	s.storeHandler = handlers.NewStoreHandler(s.stores, &s.storeMu, s.cfg.DataDir, s.jobsMgr)
	s.recordHandler = handlers.NewRecordHandler(s.stores, &s.storeMu)
	s.jobHandler = handlers.NewJobHandler(s.jobsMgr)
	s.fileHandler = handlers.NewFileHandler(s.stores, &s.storeMu)
}
