package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type LogType string

const (
	LogApp         LogType = "app"
	LogAccess      LogType = "access"
	LogTransaction LogType = "transaction"
	LogAudit       LogType = "audit"
	LogJobs        LogType = "jobs"
	LogError       LogType = "error"
)

type Logger struct {
	mu        sync.RWMutex
	loggers   map[LogType]*slog.Logger
	handlers  map[LogType]*rotationHandler
	logDir    string
	retention int
	closed    bool
}

type rotationHandler struct {
	logType    LogType
	logDir     string
	retention  int
	mu         sync.Mutex
	currentF   *os.File
	currentDay string
	closed     bool
}

func (h *rotationHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	now := time.Now()
	day := now.Format("2006-01-02")

	if day != h.currentDay {
		if h.currentF != nil {
			h.currentF.Close()
		}
		h.currentDay = day
		filename := filepath.Join(h.logDir, fmt.Sprintf("%s.%s.log", h.logType, day))
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		h.currentF = f
	}

	if h.currentF == nil {
		filename := filepath.Join(h.logDir, fmt.Sprintf("%s.%s.log", h.logType, day))
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		h.currentF = f
		h.currentDay = day
	}

	data, err := json.Marshal(map[string]interface{}{
		"time":   r.Time.Format(time.RFC3339Nano),
		"level":  r.Level.String(),
		"msg":    r.Message,
		"source": r.PC,
	})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = h.currentF.Write(data)
	return err
}

func (h *rotationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *rotationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *rotationHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *rotationHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	if h.currentF != nil {
		return h.currentF.Close()
	}
	return nil
}

func (h *rotationHandler) cleanup() {
	entries, err := os.ReadDir(h.logDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -h.retention)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(h.logDir, e.Name()))
		}
	}
}

func New(logDir string, retention int) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	l := &Logger{
		loggers:   make(map[LogType]*slog.Logger),
		handlers:  make(map[LogType]*rotationHandler),
		logDir:    logDir,
		retention: retention,
	}

	for _, lt := range []LogType{LogApp, LogAccess, LogTransaction, LogAudit, LogJobs, LogError} {
		handler := &rotationHandler{
			logType:   lt,
			logDir:    logDir,
			retention: retention,
		}
		l.handlers[lt] = handler
		l.loggers[lt] = slog.New(handler)
	}

	go l.cleanupLoop()

	return l, nil
}

func (l *Logger) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.RLock()
		for _, h := range l.handlers {
			h.cleanup()
		}
		l.mu.RUnlock()
	}
}

func (l *Logger) App() *slog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loggers[LogApp]
}

func (l *Logger) Access() *slog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loggers[LogAccess]
}

func (l *Logger) Transaction() *slog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loggers[LogTransaction]
}

func (l *Logger) Audit() *slog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loggers[LogAudit]
}

func (l *Logger) Jobs() *slog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loggers[LogJobs]
}

func (l *Logger) Error() *slog.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loggers[LogError]
}

func (l *Logger) Default() *slog.Logger {
	return l.App()
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	for _, h := range l.handlers {
		h.Close()
	}
	return nil
}

type levelHandler struct {
	level    slog.Level
	delegate slog.Handler
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.level {
		return h.delegate.Handle(ctx, r)
	}
	return nil
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{h.level, h.delegate.WithAttrs(attrs)}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{h.level, h.delegate.WithGroup(name)}
}

func NewJSONHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return slog.NewJSONHandler(w, opts)
}

func ParseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
