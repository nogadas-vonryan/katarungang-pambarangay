package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrInvalidState     = errors.New("invalid setup state")
	ErrAlreadyCompleted = errors.New("setup already completed")
	ErrNotCompleted     = errors.New("setup not completed")
)

type State string

const (
	StatePending   State = "pending"
	StateCompleted State = "completed"
)

type SetupState struct {
	mu          sync.RWMutex
	state       State
	configPath  string
	logger      *slog.Logger
	completedAt *time.Time
}

func New(configDir string, logger *slog.Logger) (*SetupState, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	s := &SetupState{
		state:      StatePending,
		configPath: filepath.Join(configDir, "setup.json"),
		logger:     logger,
	}

	if err := s.load(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	return s, nil
}

type setupFile struct {
	State       State  `json:"state"`
	CompletedAt string `json:"completedAt,omitempty"`
}

func (s *SetupState) load() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}

	var sf setupFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return fmt.Errorf("parse setup file: %w", err)
	}

	s.state = sf.State
	if sf.CompletedAt != "" {
		t, err := time.Parse(time.RFC3339, sf.CompletedAt)
		if err == nil {
			s.completedAt = &t
		}
	}

	return nil
}

func (s *SetupState) save() error {
	sf := setupFile{
		State: s.state,
	}
	if s.completedAt != nil {
		sf.CompletedAt = s.completedAt.Format(time.RFC3339)
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal setup file: %w", err)
	}

	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		return fmt.Errorf("write setup file: %w", err)
	}

	return nil
}

func (s *SetupState) Get() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *SetupState) IsCompleted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == StateCompleted
}

func (s *SetupState) Complete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == StateCompleted {
		return ErrAlreadyCompleted
	}

	now := time.Now()
	s.completedAt = &now
	s.state = StateCompleted

	if err := s.save(); err != nil {
		return err
	}

	s.logger.Info("setup: completed", "completedAt", now.Format(time.RFC3339))
	return nil
}

func (s *SetupState) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = StatePending
	s.completedAt = nil

	if err := s.save(); err != nil {
		return err
	}

	s.logger.Info("setup: reset")
	return nil
}

type SetupService struct {
	state *SetupState
}

func NewService(state *SetupState) *SetupService {
	return &SetupService{state: state}
}

func (svc *SetupService) GetStatus() map[string]interface{} {
	state := svc.state.Get()
	resp := map[string]interface{}{
		"state": state,
	}

	if svc.state.completedAt != nil {
		resp["completedAt"] = svc.state.completedAt.Format(time.RFC3339)
	}

	return resp
}

func (svc *SetupService) Complete() error {
	return svc.state.Complete()
}

func (svc *SetupService) Reset() error {
	return svc.state.Reset()
}
