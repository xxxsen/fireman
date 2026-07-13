package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var (
	errConfigPathRequired              = errors.New("config path is required")
	errAddrEmpty                       = errors.New("addr must not be empty")
	errInternalAddrEmpty               = errors.New("internal_addr must not be empty")
	errDBPathEmpty                     = errors.New("db_path must not be empty")
	errResourceDBPathEmpty             = errors.New("resource_db_path must not be empty")
	errTimezoneEmpty                   = errors.New("timezone must not be empty")
	errLogLevelEmpty                   = errors.New("log_level must not be empty")
	errWorkerConcurrency               = errors.New("worker_concurrency must be >= 1")
	errResearchOptimizationConcurrency = errors.New(
		"research_optimization_concurrency must be within [1, 32]",
	)
	errFirePlanImprovementConcurrency = errors.New(
		"fire_plan_improvement_concurrency must be within [1, 16]",
	)
	errAutoUpdateScanInterval = errors.New("auto_update_scan_interval_minutes must be within 5..1440")
)

// Config holds runtime settings loaded from a JSON config file.
type Config struct {
	Addr string `json:"addr"`
	// InternalAddr serves the sidecar-facing internal API (resource upload,
	// task finalization). It must never be published outside the docker
	// network.
	InternalAddr                    string `json:"internal_addr"`
	DBPath                          string `json:"db_path"`
	ResourceDBPath                  string `json:"resource_db_path"`
	Timezone                        string `json:"timezone"`
	LogLevel                        string `json:"log_level"`
	WorkerConcurrency               int    `json:"worker_concurrency"`
	ResearchOptimizationConcurrency int    `json:"research_optimization_concurrency"`
	FirePlanImprovementConcurrency  int    `json:"fire_plan_improvement_concurrency"`
	AutoUpdateScanIntervalMinutes   int    `json:"auto_update_scan_interval_minutes"`
}

const (
	defaultAddr                            = ":8080"
	defaultInternalAddr                    = ":8081"
	defaultDBPath                          = "/data/fireman.db"
	defaultResourceDBPath                  = "/data/fireman_resource.db"
	defaultTimezone                        = "Asia/Shanghai"
	defaultLogLevel                        = "info"
	defaultWorkerConcurrency               = 1
	defaultResearchOptimizationConcurrency = 4
	defaultFirePlanImprovementConcurrency  = 4
	defaultAutoUpdateScanIntervalMinutes   = 60
)

// Default returns production-oriented defaults before JSON overrides are applied.
func Default() Config {
	return Config{
		Addr:                            defaultAddr,
		InternalAddr:                    defaultInternalAddr,
		DBPath:                          defaultDBPath,
		ResourceDBPath:                  defaultResourceDBPath,
		Timezone:                        defaultTimezone,
		LogLevel:                        defaultLogLevel,
		WorkerConcurrency:               defaultWorkerConcurrency,
		ResearchOptimizationConcurrency: defaultResearchOptimizationConcurrency,
		FirePlanImprovementConcurrency:  defaultFirePlanImprovementConcurrency,
		AutoUpdateScanIntervalMinutes:   defaultAutoUpdateScanIntervalMinutes,
	}
}

// Load reads and validates configuration from a JSON file.
func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, errConfigPathRequired
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if raw, exists := os.LookupEnv("AUTO_UPDATE_SCAN_INTERVAL_MINUTES"); exists {
		value, parseErr := strconv.Atoi(strings.TrimSpace(raw))
		if parseErr != nil {
			return Config{}, fmt.Errorf("parse AUTO_UPDATE_SCAN_INTERVAL_MINUTES: %w", parseErr)
		}
		cfg.AutoUpdateScanIntervalMinutes = value
	}
	return validate(cfg)
}

func validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return Config{}, errAddrEmpty
	}
	if strings.TrimSpace(cfg.InternalAddr) == "" {
		return Config{}, errInternalAddrEmpty
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return Config{}, errDBPathEmpty
	}
	if strings.TrimSpace(cfg.ResourceDBPath) == "" {
		return Config{}, errResourceDBPathEmpty
	}
	if strings.TrimSpace(cfg.Timezone) == "" {
		return Config{}, errTimezoneEmpty
	}
	if strings.TrimSpace(cfg.LogLevel) == "" {
		return Config{}, errLogLevelEmpty
	}
	if cfg.WorkerConcurrency < 1 {
		return Config{}, fmt.Errorf("%w, got %d", errWorkerConcurrency, cfg.WorkerConcurrency)
	}
	if cfg.ResearchOptimizationConcurrency < 1 || cfg.ResearchOptimizationConcurrency > 32 {
		return Config{}, fmt.Errorf("%w, got %d", errResearchOptimizationConcurrency,
			cfg.ResearchOptimizationConcurrency)
	}
	if cfg.FirePlanImprovementConcurrency < 1 || cfg.FirePlanImprovementConcurrency > 16 {
		return Config{}, fmt.Errorf("%w, got %d", errFirePlanImprovementConcurrency,
			cfg.FirePlanImprovementConcurrency)
	}
	if cfg.AutoUpdateScanIntervalMinutes < 5 || cfg.AutoUpdateScanIntervalMinutes > 1440 {
		return Config{}, fmt.Errorf("%w, got %d", errAutoUpdateScanInterval, cfg.AutoUpdateScanIntervalMinutes)
	}
	return cfg, nil
}
