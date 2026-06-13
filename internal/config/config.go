package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	errConfigPathRequired  = errors.New("config path is required")
	errAddrEmpty           = errors.New("addr must not be empty")
	errDBPathEmpty         = errors.New("db_path must not be empty")
	errMarketProviderEmpty = errors.New("market_provider_url must not be empty")
	errTimezoneEmpty       = errors.New("timezone must not be empty")
	errLogLevelEmpty       = errors.New("log_level must not be empty")
	errWorkerConcurrency   = errors.New("worker_concurrency must be >= 1")
)

// Config holds runtime settings loaded from a JSON config file.
type Config struct {
	Addr              string `json:"addr"`
	DBPath            string `json:"db_path"`
	MarketProviderURL string `json:"market_provider_url"`
	Timezone          string `json:"timezone"`
	LogLevel          string `json:"log_level"`
	WorkerConcurrency int    `json:"worker_concurrency"`
}

const (
	defaultAddr              = ":8080"
	defaultDBPath            = "/data/fireman.db"
	defaultMarketProviderURL = "http://market-provider:18081"
	defaultTimezone          = "Asia/Shanghai"
	defaultLogLevel          = "info"
	defaultWorkerConcurrency = 1
)

// Default returns production-oriented defaults before JSON overrides are applied.
func Default() Config {
	return Config{
		Addr:              defaultAddr,
		DBPath:            defaultDBPath,
		MarketProviderURL: defaultMarketProviderURL,
		Timezone:          defaultTimezone,
		LogLevel:          defaultLogLevel,
		WorkerConcurrency: defaultWorkerConcurrency,
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
	return validate(cfg)
}

func validate(cfg Config) (Config, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return Config{}, errAddrEmpty
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return Config{}, errDBPathEmpty
	}
	if strings.TrimSpace(cfg.MarketProviderURL) == "" {
		return Config{}, errMarketProviderEmpty
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
	return cfg, nil
}
