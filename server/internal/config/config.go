package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Mode string

const (
	ModeStandalone Mode = "standalone"
	ModeSubprocess Mode = "subprocess"
)

type Config struct {
	Port              int    `json:"port"`
	Host              string `json:"host"`
	Mode              Mode   `json:"mode"`
	AppName           string `json:"appName"`
	LogLevel          string `json:"logLevel"`
	LogRetention      int    `json:"logRetention"`
	DataDir           string `json:"dataDir"`
	SystemDir         string `json:"systemDir"`
	BootstrapUser     string `json:"bootstrapUser"`
	BootstrapPassword string `json:"bootstrapPassword"`
}

func (c *Config) SetDefaults() {
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Mode == "" {
		c.Mode = ModeStandalone
	}
	if c.AppName == "" {
		c.AppName = "kp-cms"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.LogRetention == 0 {
		c.LogRetention = 7
	}
	if c.BootstrapUser == "" {
		c.BootstrapUser = "admin"
	}
	if c.BootstrapPassword == "" {
		c.BootstrapPassword = "admin"
	}
}

func (c *Config) Validate() error {
	if c.DataDir == "" {
		return errors.New("data directory is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	validModes := map[Mode]bool{
		ModeStandalone: true,
		ModeSubprocess: true,
	}
	if !validModes[c.Mode] {
		return fmt.Errorf("invalid mode: %s (must be 'standalone' or 'subprocess')", c.Mode)
	}
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level: %s", c.LogLevel)
	}
	return nil
}

func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("KP_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Port = port
		}
	}
	if v := os.Getenv("KP_HOST"); v != "" {
		c.Host = v
	}
	if v := os.Getenv("KP_MODE"); v != "" {
		c.Mode = Mode(v)
	}
	if v := os.Getenv("KP_APPNAME"); v != "" {
		c.AppName = v
	}
	if v := os.Getenv("KP_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("KP_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("KP_LOG_RETENTION"); v != "" {
		if retention, err := strconv.Atoi(v); err == nil {
			c.LogRetention = retention
		}
	}
	if v := os.Getenv("KP_BOOTSTRAP_USER"); v != "" {
		c.BootstrapUser = v
	}
	if v := os.Getenv("KP_BOOTSTRAP_PASSWORD"); v != "" {
		c.BootstrapPassword = v
	}
}

func Load(dataDir, appName string) (*Config, error) {
	cfg := &Config{
		AppName: appName,
		DataDir: dataDir,
	}
	cfg.SetDefaults()
	cfg.ApplyEnvOverrides()

	if dataDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		cfg.DataDir = filepath.Join(wd, "data")
	}

	cfg.SystemDir = filepath.Join(cfg.DataDir, ".system")

	configFile := filepath.Join(cfg.DataDir, "config.json")
	if _, err := os.Stat(configFile); err == nil {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		var fileCfg Config
		if err := json.Unmarshal(data, &fileCfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
		if fileCfg.Port != 0 {
			cfg.Port = fileCfg.Port
		}
		if fileCfg.Host != "" {
			cfg.Host = fileCfg.Host
		}
		if fileCfg.Mode != "" {
			cfg.Mode = fileCfg.Mode
		}
		if fileCfg.AppName != "" {
			cfg.AppName = fileCfg.AppName
		}
		if fileCfg.LogLevel != "" {
			cfg.LogLevel = fileCfg.LogLevel
		}
		if fileCfg.LogRetention != 0 {
			cfg.LogRetention = fileCfg.LogRetention
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cfg.SystemDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	return cfg, nil
}

func (c *Config) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	if strings.HasPrefix(path, "$") {
		envVar := strings.TrimPrefix(strings.Split(path, "/")[0], "$")
		if val := os.Getenv(envVar); val != "" {
			return filepath.Join(val, strings.TrimPrefix(path, "$"+envVar))
		}
	}
	return filepath.Join(c.DataDir, path)
}
