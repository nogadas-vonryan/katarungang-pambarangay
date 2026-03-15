package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

func (s *Server) initAuth() error {
	authDBPath, err := auth.EnsureAuthDB(s.cfg.SystemDir, s.logger.Default())
	if err != nil {
		return fmt.Errorf("ensure auth db: %w", err)
	}

	authSvc, err := auth.NewService(authDBPath, DefaultSessionTTL, s.logger.Default())
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
	}

	if err := authSvc.Bootstrap(s.cfg.BootstrapUser, s.cfg.BootstrapPassword); err != nil {
		s.logger.App().Warn("bootstrap failed (user may exist)", "err", err)
	}

	s.authSvc = authSvc
	s.logger.App().Info("auth initialized", "db", authDBPath)
	return nil
}

func (s *Server) initJobs() error {
	s.jobsMgr = jobs.New(jobs.JobManagerConfig{
		Workers:       DefaultJobWorkers,
		Logger:        s.logger.Jobs(),
		MaxJobs:       1000,
		JobTTL:        24 * time.Hour,
		CleanupPeriod: 1 * time.Hour,
	})

	s.jobsMgr.RegisterHandler(jobs.JobStoreScan, func(ctx context.Context, job *jobs.Job) error {
		s.logger.Jobs().Info("job: running store scan", "job", job.ID)

		s.storeMu.Lock()
		for name, st := range s.stores {
			st.Close()
			delete(s.stores, name)
		}
		s.storeMu.Unlock()

		if err := s.initStores(); err != nil {
			return fmt.Errorf("reload stores: %w", err)
		}

		job.Result = map[string]interface{}{
			"storesReloaded": len(s.stores),
		}
		return nil
	})

	s.jobsMgr.RegisterHandler(jobs.JobBackupCreate, func(ctx context.Context, job *jobs.Job) error {
		s.logger.Jobs().Info("job: creating backup", "job", job.ID)

		scope, _ := job.Payload["scope"].(string)
		createdBy, _ := job.Payload["createdBy"].(string)

		storePathsRaw, ok := job.Payload["storePaths"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid storePaths payload")
		}

		storePaths := make(map[string]backup.StoreInfo, len(storePathsRaw))
		for name, v := range storePathsRaw {
			infoMap, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			storePaths[name] = backup.StoreInfo{
				Name: getString(infoMap, "name"),
				Path: getString(infoMap, "path"),
				Type: getString(infoMap, "type"),
			}
		}

		result, err := s.backupMgr.CreateBackup(ctx, scope, storePaths, createdBy)
		if err != nil {
			return fmt.Errorf("create backup: %w", err)
		}

		job.Result = map[string]interface{}{
			"backupName":     result.BackupName,
			"filesProcessed": result.FilesProcessed,
			"recordCount":    result.RecordCount,
			"bytesWritten":   result.BytesWritten,
		}
		return nil
	})

	s.jobsMgr.RegisterHandler(jobs.JobBackupRestore, func(ctx context.Context, job *jobs.Job) error {
		s.logger.Jobs().Info("job: restoring backup", "job", job.ID)

		backupName, _ := job.Payload["backupName"].(string)
		targetStore, _ := job.Payload["targetStore"].(string)

		optsMap, ok := job.Payload["opts"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid opts payload")
		}

		opts := backup.RestoreOptions{
			DryRun: getBool(optsMap, "dryRun"),
			Force:  getBool(optsMap, "force"),
		}

		result, err := s.backupMgr.RestoreBackup(ctx, backupName, targetStore, opts)
		if err != nil {
			return fmt.Errorf("restore backup: %w", err)
		}

		job.Result = map[string]interface{}{
			"recordCount": result.RecordCount,
			"wouldDelete": result.WouldDelete,
			"wouldCreate": result.WouldCreate,
			"wouldUpdate": result.WouldUpdate,
		}
		return nil
	})

	s.jobsMgr.Start(context.Background())
	s.logger.App().Info("job manager started")
	return nil
}

func (s *Server) initSetup() error {
	state, err := setup.New(s.cfg.SystemDir, s.logger.Default())
	if err != nil {
		return fmt.Errorf("create setup state: %w", err)
	}

	s.setupSvc = setup.NewService(state)
	s.logger.App().Info("setup state initialized")
	return nil
}

func (s *Server) initStores() error {
	s.logger.App().Info("discovering stores", "root", s.cfg.DataDir)

	entries, err := os.ReadDir(s.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		storePath := filepath.Join(s.cfg.DataDir, e.Name())
		storeMetaPath := filepath.Join(storePath, ".store.json")

		if _, err := os.Stat(storeMetaPath); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(storeMetaPath)
		if err != nil {
			s.logger.App().Warn("failed to read store descriptor", "path", storeMetaPath, "err", err)
			continue
		}

		var meta storeMetaJSON
		if err := json.Unmarshal(data, &meta); err != nil {
			s.logger.App().Warn("failed to parse store descriptor", "path", storeMetaPath, "err", err)
			continue
		}

		if meta.Name == "" {
			meta.Name = e.Name()
		}
		if meta.Type == "" {
			meta.Type = "folder"
		}

		storeMeta := &store.StoreMetadata{
			Name:          meta.Name,
			Type:          meta.Type,
			Path:          storePath,
			Schema:        meta.Schema,
			NamingPattern: meta.NamingPattern,
			Counter:       meta.Counter,
			IndexReady:    false,
		}

		st, err := store.CreateStore(meta.Type, storePath, storeMeta)
		if err != nil {
			s.logger.App().Warn("failed to create store", "name", meta.Name, "err", err)
			continue
		}

		s.storeMu.Lock()
		s.stores[meta.Name] = st
		s.storeMu.Unlock()

		s.logger.App().Info("store discovered", "name", meta.Name, "type", meta.Type, "path", storePath)
	}

	s.logger.App().Info("store discovery complete", "count", len(s.stores))
	return nil
}

func (s *Server) setupRouter() {
	s.router = chi.NewRouter()

	s.router.Use(s.requestIDMiddleware)
	s.router.Use(middleware.RealIP)
	s.router.Use(s.accessLogger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(s.corsMiddleware)
	s.router.Use(s.bodyLimitMiddleware)

	s.router.Get("/health", s.handleHealth)

	s.router.Route("/auth", func(r chi.Router) {
		r.Post("/login", handlers.HandleLogin(s.authSvc, s.logger.Audit()))
		r.Post("/logout", handlers.HandleLogout(s.authSvc))
	})

	s.router.Route("/setup", func(r chi.Router) {
		r.Get("/status", s.handleSetupStatus)
		r.Post("/complete", s.handleSetupComplete)
	})

	s.router.Route("/jobs", func(r chi.Router) {
		r.Get("/", s.jobHandler.ListJobs)
		r.Get("/{id}", s.jobHandler.GetJob)
	})

	s.router.Route("/v1", func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Route("/stores", func(r chi.Router) {
			r.Get("/", s.storeHandler.ListStores)
			r.Post("/", s.storeHandler.CreateStore)
			r.Post("/reload", s.storeHandler.ReloadStores)
		})

		r.Route("/backups", func(r chi.Router) {
			r.Get("/", s.backupHandler.ListBackups)
			r.Post("/", s.backupHandler.CreateBackup)
			r.Post("/{name}/restore", s.backupHandler.RestoreBackup)
		})

		r.Route("/{store}", func(r chi.Router) {
			r.Route("/records", func(r chi.Router) {
				r.Get("/", s.recordHandler.ListRecords)
				r.Post("/", s.recordHandler.CreateRecord)
				r.Get("/{id}", s.recordHandler.GetRecord)
				r.Put("/{id}", s.recordHandler.ReplaceRecord)
				r.Patch("/{id}", s.recordHandler.UpdateRecord)
				r.Delete("/{id}", s.recordHandler.DeleteRecord)

				r.Route("/{id}/files", func(r chi.Router) {
					r.Get("/", s.fileHandler.ListFiles)
					r.Post("/", s.fileHandler.UploadFile)
					r.Get("/{filename}", s.fileHandler.DownloadFile)
					r.Delete("/{filename}", s.fileHandler.DeleteFile)
					r.Patch("/{filename}", s.fileHandler.RenameFile)
				})
			})
		})
	})

	s.router.NotFound(s.handleNotFound)
	s.router.MethodNotAllowed(s.handleMethodNotAllowed)
}

func (s *Server) setupHTTPServer() {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  DefaultReadTimeout,
		WriteTimeout: DefaultWriteTimeout,
		IdleTimeout:  DefaultIdleTimeout,
	}
}

func Run(cfg *config.Config) error {
	logDir := filepath.Join(cfg.SystemDir, "logs")
	logger, err := logging.New(logDir, cfg.LogRetention)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer logger.Close()

	logger.App().Info("starting kp-cms server",
		"version", "dev",
		"dataDir", cfg.DataDir,
		"mode", cfg.Mode,
	)

	srv, err := New(cfg, logger)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	if err := srv.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Server running at http://%s:%d\n", cfg.Host, cfg.Port)
	fmt.Fprintf(os.Stdout, "Check logs for details: %s\n", logDir)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	logger.App().Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		return fmt.Errorf("stop server: %w", err)
	}

	logger.App().Info("server stopped")
	return nil
}

func (s *Server) initBackup() error {
	mgr, err := backup.NewBackupManager(s.cfg.DataDir, s.logger.Default())
	if err != nil {
		return fmt.Errorf("create backup manager: %w", err)
	}
	s.backupMgr = mgr
	s.logger.App().Info("backup manager initialized")
	return nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
