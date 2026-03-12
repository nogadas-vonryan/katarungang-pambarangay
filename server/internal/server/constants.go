package server

import (
	"time"
)

const (
	DefaultJobWorkers      = 2
	DefaultSessionTTL      = 24 * time.Hour
	DefaultReadTimeout     = 30 * time.Second
	DefaultWriteTimeout    = 30 * time.Second
	DefaultIdleTimeout     = 120 * time.Second
	DefaultShutdownTimeout = 30 * time.Second
	DefaultListLimit       = 100
)
