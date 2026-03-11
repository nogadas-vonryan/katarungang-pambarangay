package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/kp-cms/server/internal/auth"
	"github.com/kp-cms/server/internal/config"
	"github.com/kp-cms/server/internal/jobs"
	"github.com/kp-cms/server/internal/logging"
	"github.com/kp-cms/server/internal/setup"
	"github.com/kp-cms/server/internal/store"

	_ "github.com/kp-cms/server/internal/store/folder"
)

type Server struct {
	cfg     *config.Config
	logger  *logging.Logger
	router  *chi.Mux
	httpSrv *http.Server
	stores  map[string]store.Store
	storeMu sync.RWMutex
	stopCh  chan struct{}

	authSvc  *auth.AuthService
	jobsMgr  *jobs.JobManager
	setupSvc *setup.SetupService
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
)

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

	s.setupRouter()
	s.setupHTTPServer()

	return s, nil
}

func (s *Server) initAuth() error {
	authDBPath, err := auth.EnsureAuthDB(s.cfg.SystemDir, s.logger.Default())
	if err != nil {
		return fmt.Errorf("ensure auth db: %w", err)
	}

	authSvc, err := auth.NewService(authDBPath, 24*time.Hour, s.logger.Default())
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
	s.jobsMgr = jobs.New(2, s.logger.Jobs())

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

		var meta struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
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
			Name:       meta.Name,
			Type:       meta.Type,
			Path:       storePath,
			IndexReady: false,
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

	s.router.Get("/health", s.handleHealth)

	s.router.Route("/auth", func(r chi.Router) {
		r.Post("/login", s.handleLogin)
		r.Post("/logout", s.handleLogout)
	})

	s.router.Route("/setup", func(r chi.Router) {
		r.Get("/status", s.handleSetupStatus)
		r.Post("/complete", s.handleSetupComplete)
	})

	s.router.Route("/jobs", func(r chi.Router) {
		r.Get("/", s.handleListJobs)
		r.Get("/{id}", s.handleGetJob)
	})

	s.router.Route("/v1", func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Route("/stores", func(r chi.Router) {
			r.Get("/", s.handleListStores)
			r.Post("/", s.handleCreateStore)
			r.Post("/reload", s.handleReloadStores)
		})

		r.Route("/{store}", func(r chi.Router) {
			r.Route("/records", func(r chi.Router) {
				r.Get("/", s.handleListRecords)
				r.Post("/", s.handleCreateRecord)
				r.Get("/{id}", s.handleGetRecord)
				r.Put("/{id}", s.handleReplaceRecord)
				r.Patch("/{id}", s.handleUpdateRecord)
				r.Delete("/{id}", s.handleDeleteRecord)
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
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
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
		if err := s.closeStore(name, st); err != nil {
			s.logger.App().Warn("failed to close store", "name", name, "err", err)
		}
	}

	s.logger.App().Info("HTTP server stopped")
	return nil
}

func (s *Server) closeStore(name string, st store.Store) error {
	if closer, ok := st.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (s *Server) accessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		s.logger.Access().Info("access",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration", time.Since(start).String(),
			"ip", r.RemoteAddr,
		)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Mode == config.ModeStandalone {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, If-Match, If-None-Match")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(middleware.RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			if cookie, err := r.Cookie("session"); err == nil {
				token = cookie.Value
			}
		}

		if token != "" {
			token = strings.TrimPrefix(token, "Bearer ")
			user, err := s.authSvc.ValidateSession(r.Context(), token)
			if err == nil {
				ctx := context.WithValue(r.Context(), "user", user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		s.writeErrorWithCode(w, r, http.StatusUnauthorized, ErrCodeUnauthorized, "unauthorized")
	})
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

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}

	session, err := s.authSvc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		s.writeErrorWithCode(w, r, http.StatusUnauthorized, ErrCodeInvalidCredentials, "invalid credentials")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		Expires:  session.ExpiresAt,
	})

	s.logger.Audit().Info("user logged in", "username", req.Username)

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":   session.Token,
		"expires": session.ExpiresAt.Format(time.RFC3339),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if token != "" {
		token = strings.TrimPrefix(token, "Bearer ")
		s.authSvc.Logout(r.Context(), token)
	}

	if cookie, err := r.Cookie("session"); err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:   "session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		_ = cookie
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "logged out",
	})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.setupSvc.GetStatus())
}

func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if err := s.setupSvc.Complete(); err != nil {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "setup completed",
	})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobsList := s.jobsMgr.List()
	resp := make([]*jobs.JobResponse, len(jobsList))
	for i, job := range jobsList {
		resp[i] = jobs.JobToResponse(job)
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": resp,
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, ok := s.jobsMgr.Get(id)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "job not found")
		return
	}
	s.writeJSON(w, http.StatusOK, jobs.JobToResponse(job))
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "resource not found")
}

func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.writeErrorWithCode(w, r, http.StatusMethodNotAllowed, ErrCodeMethodNotAllowed, "method not allowed")
}

func (s *Server) writeErrorWithCode(w http.ResponseWriter, r *http.Request, statusCode int, errorCode string, message string) {
	correlationID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:          errorCode,
			Message:       message,
			Details:       nil,
			CorrelationID: correlationID,
		},
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func (s *Server) getStore(name string) (store.Store, bool) {
	s.storeMu.RLock()
	defer s.storeMu.RUnlock()
	st, ok := s.stores[name]
	return st, ok
}

func (s *Server) handleListStores(w http.ResponseWriter, r *http.Request) {
	s.storeMu.RLock()
	stores := make([]interface{}, 0, len(s.stores))
	for _, st := range s.stores {
		meta, _ := st.Metadata()
		stores = append(stores, meta)
	}
	s.storeMu.RUnlock()

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"stores": stores,
		"total":  len(stores),
	})
}

func (s *Server) handleCreateStore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string                 `json:"name"`
		Type          string                 `json:"type"`
		Description   string                 `json:"description,omitempty"`
		Schema        map[string]interface{} `json:"schema,omitempty"`
		NamingPattern string                 `json:"namingPattern,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "name is required")
		return
	}

	if req.Type == "" {
		req.Type = "folder"
	}

	validTypes := map[string]bool{
		"folder": true,
		"json":   true,
		"csv":    true,
		"xlsx":   true,
	}
	if !validTypes[req.Type] {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid store type: "+req.Type)
		return
	}

	storePath := filepath.Join(s.cfg.DataDir, req.Name)

	s.storeMu.RLock()
	_, exists := s.stores[req.Name]
	s.storeMu.RUnlock()
	if exists {
		s.writeErrorWithCode(w, r, http.StatusConflict, ErrCodeConflict, "store already exists")
		return
	}

	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		s.writeErrorWithCode(w, r, http.StatusConflict, ErrCodeConflict, "store directory already exists")
		return
	}

	if err := os.MkdirAll(storePath, 0755); err != nil {
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to create store directory")
		return
	}

	meta := store.StoreMetadata{
		Name:          req.Name,
		Type:          req.Type,
		Path:          storePath,
		Description:   req.Description,
		Schema:        req.Schema,
		NamingPattern: req.NamingPattern,
		Counter:       0,
		IndexReady:    false,
	}

	metaFile := struct {
		Version       int                    `json:"version"`
		Name          string                 `json:"name"`
		Type          string                 `json:"type"`
		Path          string                 `json:"path"`
		Schema        map[string]interface{} `json:"schema,omitempty"`
		NamingPattern string                 `json:"namingPattern,omitempty"`
		Counter       int                    `json:"counter"`
		CreatedAt     time.Time              `json:"createdAt"`
		UpdatedAt     time.Time              `json:"updatedAt"`
	}{
		Version:       1,
		Name:          req.Name,
		Type:          req.Type,
		Path:          storePath,
		Schema:        req.Schema,
		NamingPattern: req.NamingPattern,
		Counter:       0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	metaData, err := json.MarshalIndent(metaFile, "", "  ")
	if err != nil {
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to marshal metadata")
		return
	}

	metaPath := filepath.Join(storePath, ".store.json")
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to write metadata file")
		return
	}

	st, err := store.CreateStore(req.Type, storePath, &meta)
	if err != nil {
		os.RemoveAll(storePath)
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to create store: "+err.Error())
		return
	}

	s.storeMu.Lock()
	s.stores[req.Name] = st
	s.storeMu.Unlock()

	s.logger.App().Info("store created", "name", req.Name, "type", req.Type)

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "store created",
		"store":   meta,
	})
}

func (s *Server) handleReloadStores(w http.ResponseWriter, r *http.Request) {
	job := &jobs.Job{
		Type: jobs.JobStoreScan,
	}

	if err := s.jobsMgr.Enqueue(job); err != nil {
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, "failed to enqueue job")
		return
	}

	s.logger.App().Info("store reload job enqueued", "job", job.ID)

	s.writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":   job.ID,
		"message": "store reload initiated",
	})
}

func (s *Server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := s.getStore(storeName)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "store not found")
		return
	}

	opts := store.ListOptions{
		Limit:    100,
		Offset:   0,
		SortBy:   "id",
		SortDesc: false,
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &opts.Limit)
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		fmt.Sscanf(v, "%d", &opts.Offset)
	}
	if v := r.URL.Query().Get("sort"); v != "" {
		opts.SortDesc = strings.HasPrefix(v, "-")
		opts.SortBy = strings.TrimPrefix(v, "-")
	}

	records, err := st.List(r.Context(), opts)
	if err != nil {
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"records": records,
		"total":   len(records),
	})
}

func (s *Server) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	st, ok := s.getStore(storeName)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "store not found")
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON")
		return
	}

	record, err := st.Create(r.Context(), data)
	if err != nil {
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	s.logger.Transaction().Info("record created",
		"store", storeName,
		"id", record.ID,
	)

	w.Header().Set("Location", fmt.Sprintf("/v1/%s/records/%s", storeName, record.ID))
	s.writeJSON(w, http.StatusCreated, record)
}

func (s *Server) handleGetRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	id := chi.URLParam(r, "id")

	st, ok := s.getStore(storeName)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "store not found")
		return
	}

	record, err := st.Get(r.Context(), id)
	if err != nil {
		if err == store.ErrRecordNotFound {
			s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "record not found")
			return
		}
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	w.Header().Set("ETag", record.ETag)
	s.writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleReplaceRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	id := chi.URLParam(r, "id")

	st, ok := s.getStore(storeName)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "store not found")
		return
	}

	ifMatch := r.Header.Get("If-Match")
	if ifMatch != "" {
		existing, err := st.Get(r.Context(), id)
		if err != nil {
			if err == store.ErrRecordNotFound {
				s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "record not found")
				return
			}
			s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
			return
		}
		if ifMatch != existing.ETag {
			s.writeErrorWithCode(w, r, http.StatusPreconditionFailed, ErrCodePreconditionFailed, "ETag mismatch")
			return
		}
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON")
		return
	}

	record, err := st.Replace(r.Context(), id, data)
	if err != nil {
		if err == store.ErrRecordNotFound {
			s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "record not found")
			return
		}
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	s.logger.Transaction().Info("record replaced",
		"store", storeName,
		"id", id,
	)

	w.Header().Set("ETag", record.ETag)
	s.writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	id := chi.URLParam(r, "id")

	st, ok := s.getStore(storeName)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "store not found")
		return
	}

	ifMatch := r.Header.Get("If-Match")
	if ifMatch != "" {
		existing, err := st.Get(r.Context(), id)
		if err != nil {
			if err == store.ErrRecordNotFound {
				s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "record not found")
				return
			}
			s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
			return
		}
		if ifMatch != existing.ETag {
			s.writeErrorWithCode(w, r, http.StatusPreconditionFailed, ErrCodePreconditionFailed, "ETag mismatch")
			return
		}
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		s.writeErrorWithCode(w, r, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON")
		return
	}

	record, err := st.Update(r.Context(), id, data)
	if err != nil {
		if err == store.ErrRecordNotFound {
			s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "record not found")
			return
		}
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	s.logger.Transaction().Info("record updated",
		"store", storeName,
		"id", id,
	)

	w.Header().Set("ETag", record.ETag)
	s.writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	storeName := chi.URLParam(r, "store")
	id := chi.URLParam(r, "id")

	st, ok := s.getStore(storeName)
	if !ok {
		s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "store not found")
		return
	}

	if err := st.Delete(r.Context(), id); err != nil {
		if err == store.ErrRecordNotFound {
			s.writeErrorWithCode(w, r, http.StatusNotFound, ErrCodeNotFound, "record not found")
			return
		}
		s.writeErrorWithCode(w, r, http.StatusInternalServerError, ErrCodeInternal, err.Error())
		return
	}

	s.logger.Transaction().Info("record deleted",
		"store", storeName,
		"id", id,
	)

	w.WriteHeader(http.StatusNoContent)
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		return fmt.Errorf("stop server: %w", err)
	}

	logger.App().Info("server stopped")
	return nil
}
