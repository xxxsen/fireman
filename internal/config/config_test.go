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
	if cfg.ResearchOptimizationConcurrency != 4 {
		t.Errorf("expected default research optimization concurrency 4, got %d",
			cfg.ResearchOptimizationConcurrency)
	}
	if cfg.FirePlanImprovementConcurrency != 4 {
		t.Errorf("expected FIRE plan improvement concurrency 4, got %d",
			cfg.FirePlanImprovementConcurrency)
	}
	if cfg.AutoUpdateScanIntervalMinutes != 60 {
		t.Errorf("expected default auto-update interval 60, got %d", cfg.AutoUpdateScanIntervalMinutes)
	}
}

func TestLoad_AutoUpdateScanIntervalJSONAndEnvOverride(t *testing.T) {
	t.Setenv("AUTO_UPDATE_SCAN_INTERVAL_MINUTES", "")
	if err := os.Unsetenv("AUTO_UPDATE_SCAN_INTERVAL_MINUTES"); err != nil {
		t.Fatal(err)
	}
	path := writeConfigFile(t, `{"auto_update_scan_interval_minutes":10}`)
	cfg, err := Load(path)
	if err != nil || cfg.AutoUpdateScanIntervalMinutes != 10 {
		t.Fatalf("JSON interval: cfg=%+v err=%v", cfg, err)
	}
	t.Setenv("AUTO_UPDATE_SCAN_INTERVAL_MINUTES", "90")
	cfg, err = Load(path)
	if err != nil || cfg.AutoUpdateScanIntervalMinutes != 90 {
		t.Fatalf("env interval: cfg=%+v err=%v", cfg, err)
	}
}

func TestLoad_RejectsAutoUpdateScanIntervalOutsideRange(t *testing.T) {
	for _, value := range []string{"4", "1441"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AUTO_UPDATE_SCAN_INTERVAL_MINUTES", value)
			if _, err := Load(writeConfigFile(t, `{}`)); err == nil {
				t.Fatalf("expected interval %s to fail", value)
			}
		})
	}
}

func TestLoad_OverridesAndValidation(t *testing.T) {
	path := writeConfigFile(t, `{
		"addr": ":9000",
		"db_path": "/tmp/fireman.db",
		"market_provider_url": "http://127.0.0.1:18081",
		"timezone": "Asia/Shanghai",
		"log_level": "debug",
		"worker_concurrency": 2,
		"research_optimization_concurrency": 6,
		"fire_plan_improvement_concurrency": 8
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
	if cfg.ResearchOptimizationConcurrency != 6 {
		t.Errorf("expected research optimization concurrency 6, got %d",
			cfg.ResearchOptimizationConcurrency)
	}
	if cfg.FirePlanImprovementConcurrency != 8 {
		t.Errorf("expected FIRE plan improvement concurrency 8, got %d",
			cfg.FirePlanImprovementConcurrency)
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

func TestLoad_RejectsInvalidResearchOptimizationConcurrency(t *testing.T) {
	for _, value := range []string{"0", "33"} {
		t.Run(value, func(t *testing.T) {
			path := writeConfigFile(t, `{"research_optimization_concurrency":`+value+`}`)
			if _, err := Load(path); err == nil {
				t.Errorf("expected research optimization concurrency %s to fail", value)
			}
		})
	}
}

func TestLoadRejectsInvalidFirePlanImprovementConcurrency(t *testing.T) {
	for _, value := range []string{"0", "17"} {
		t.Run(value, func(t *testing.T) {
			path := writeConfigFile(t, `{"fire_plan_improvement_concurrency":`+value+`}`)
			if _, err := Load(path); err == nil {
				t.Errorf("expected FIRE plan improvement concurrency %s to fail", value)
			}
		})
	}
}

func TestLoad_RejectsMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing config file")
	}
}
