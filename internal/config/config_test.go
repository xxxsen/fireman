package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_DefaultsFromEmptyJSON(t *testing.T) {
	path := writeConfigFile(t, `{}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("expected default addr :8080, got %q", cfg.Addr)
	}
	if cfg.DBPath != "/data/fireman.db" {
		t.Errorf("expected default db path /data/fireman.db, got %q", cfg.DBPath)
	}
	if cfg.WorkerConcurrency != 1 {
		t.Errorf("expected default worker concurrency 1, got %d", cfg.WorkerConcurrency)
	}
}


func TestLoad_OverridesAndValidation(t *testing.T) {
	path := writeConfigFile(t, `{
		"addr": ":9000",
		"db_path": "/tmp/fireman.db",
		"market_provider_url": "http://127.0.0.1:18081",
		"timezone": "Asia/Shanghai",
		"log_level": "debug",
		"worker_concurrency": 2
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Addr != ":9000" {
		t.Errorf("expected addr override :9000, got %q", cfg.Addr)
	}
	if cfg.WorkerConcurrency != 2 {
		t.Errorf("expected worker concurrency 2, got %d", cfg.WorkerConcurrency)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %q", cfg.LogLevel)
	}
}

func TestLoad_RequiresPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for empty config path")
	}
}

func TestLoad_RejectsInvalidWorkerConcurrency(t *testing.T) {
	path := writeConfigFile(t, `{"worker_concurrency": 0}`)
	if _, err := Load(path); err == nil {
		t.Error("expected error for worker concurrency 0")
	}
}

func TestLoad_RejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing config file")
	}
}
