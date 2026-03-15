package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/kp-cms/server/internal/auth"
	"github.com/kp-cms/server/internal/backup"
	"github.com/kp-cms/server/internal/config"
	"github.com/kp-cms/server/internal/jobs"
	"github.com/kp-cms/server/internal/logging"
	"github.com/kp-cms/server/internal/server/handlers"
	"github.com/kp-cms/server/internal/setup"
	"github.com/kp-cms/server/internal/store"
)

type Server struct {
	cfg       *config.Config
	logger    *logging.Logger
	router    *chi.Mux
	httpSrv   *http.Server
	stores    map[string]store.Store
	storeMu   sync.RWMutex
	provider  *handlers.StoreAccessor
	stopCh    chan struct{}
	authSvc   *auth.AuthService
	jobsMgr   *jobs.JobManager
	setupSvc  *setup.SetupService
	backupMgr *backup.BackupManager

	storeHandler  *handlers.StoreHandler
	recordHandler *handlers.RecordHandler
	jobHandler    *handlers.JobHandler
	fileHandler   *handlers.FileHandler
	backupHandler *handlers.BackupHandler
}

func New(cfg *config.Config, logger *logging.Logger) (*Server, error) {
	s := &Server{
		cfg:    cfg,
		logger: logger,
		stores: make(map[string]store.Store),
		stopCh: make(chan struct{}),
	}

	s.provider = handlers.NewStoreAccessor(s.stores, &s.storeMu)

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

	if err := s.initBackup(); err != nil {
		return nil, fmt.Errorf("init backup: %w", err)
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

func contextWithUser(ctx context.Context, user interface{}) context.Context {
	return context.WithValue(ctx, ctxKeyUser, user)
}

func contextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, middleware.RequestIDKey, id)
}

func (s *Server) initHandlers() {
	s.storeHandler = handlers.NewStoreHandler(s.provider, s.cfg.DataDir, s.jobsMgr)
	s.recordHandler = handlers.NewRecordHandler(s.provider)
	s.jobHandler = handlers.NewJobHandler(s.jobsMgr)
	s.fileHandler = handlers.NewFileHandler(s.provider)
	s.backupHandler = handlers.NewBackupHandler(s.backupMgr, s.jobsMgr, s.provider)
}
