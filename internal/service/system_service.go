package service

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
)

var errInvalidCopyDestination = errors.New("invalid copy destination")

// SystemService handles database backup, restore, and plan exports.
type SystemService struct {
	sql         *sql.DB
	dbPath      string
	plans       *PlanService
	targets     *TargetService
	rebalance   *RebalanceService
	maintenance *MaintenanceGate
}

func NewSystemService(
	sqlDB *sql.DB,
	dbPath string,
	plans *PlanService,
	targets *TargetService,
	rebalance *RebalanceService,
	maintenance *MaintenanceGate,
) *SystemService {
	return &SystemService{
		sql: sqlDB, dbPath: dbPath, plans: plans, targets: targets, rebalance: rebalance,
		maintenance: maintenance,
	}
}

func (s *SystemService) DownloadBackup(ctx context.Context) ([]byte, string, error) {
	data, err := fdb.ReadDatabaseFile(ctx, s.sql, s.dbPath)
	if err != nil {
		return nil, "", wrapRepo("read database backup", err)
	}
	base := filepath.Base(s.dbPath)
	name := fmt.Sprintf("%s.%s.bak", strings.TrimSuffix(base, filepath.Ext(base)),
		time.Now().UTC().Format("20060102T150405Z"))
	return data, name, nil
}

// RestoreBackup validates an uploaded database, backs up the current file, and atomically replaces it.
// The caller should restart the backend process so connections reopen the restored database.
func (s *SystemService) RestoreBackup(ctx context.Context, data []byte) error {
	if s.maintenance != nil {
		s.maintenance.Enter()
		defer s.maintenance.Leave()
	}
	if len(data) == 0 {
		return newErr("invalid_backup", "backup file is empty", nil)
	}
	if len(data) > 100<<20 {
		return newErr("invalid_backup", "backup file exceeds 100MB limit", nil)
	}

	dir := filepath.Dir(s.dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return wrapRepo("mkdir for restore", err)
	}
	tempPath := filepath.Join(dir, ".restore-"+fmt.Sprintf("%d", time.Now().UnixNano())+".db")
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return wrapRepo("write temp restore file", err)
	}
	defer func() { _ = os.Remove(tempPath) }()

	if err := fdb.ValidateDatabaseFile(ctx, tempPath); err != nil {
		return newErr(
			"invalid_backup",
			"backup failed integrity or schema validation",
			map[string]any{"reason": err.Error()},
		)
	}

	if err := fdb.CheckpointWAL(ctx, s.sql); err != nil {
		return wrapRepo("checkpoint wal before restore", err)
	}
	if info, err := os.Stat(s.dbPath); err == nil && info.Size() > 0 {
		ts := time.Now().UTC().Format("20060102T150405Z")
		if err := writePreRestoreBackup(s.dbPath, ts); err != nil {
			return err
		}
	}

	newPath := s.dbPath + ".new"
	if err := os.WriteFile(newPath, data, 0o600); err != nil {
		return wrapRepo("write restored database", err)
	}
	if err := os.Rename(newPath, s.dbPath); err != nil {
		return wrapRepo("rename restored database", err)
	}
	_ = os.Remove(s.dbPath + "-wal")
	_ = os.Remove(s.dbPath + "-shm")
	return nil
}

func writePreRestoreBackup(dbPath, timestamp string) error {
	in, err := os.ReadFile(dbPath)
	if err != nil {
		return wrapRepo("read database for pre-restore backup", err)
	}
	name := filepath.Base(dbPath) + "." + timestamp + ".pre-restore.bak"
	return wrapRepo("write pre-restore backup", writeFileInDir(filepath.Dir(dbPath), name, in, 0o600))
}

// ExportPlanJSON returns a portable plan snapshot.
func (s *SystemService) ExportPlanJSON(ctx context.Context, planID string) (map[string]any, error) {
	plan, err := s.plans.Get(ctx, planID)
	if err != nil {
		return nil, err
	}
	params, err := s.plans.GetParameters(ctx, planID)
	if err != nil {
		return nil, err
	}
	targets, err := s.targets.GetTargets(ctx, planID)
	if err != nil {
		return nil, err
	}
	reb, err := s.rebalance.GetRebalance(ctx, planID, "full", 0)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"plan":        plan,
		"parameters":  params,
		"targets":     targets,
		"rebalance":   reb,
	}, nil
}

func (s *SystemService) ExportTargetsCSV(ctx context.Context, planID string) ([]byte, error) {
	targets, err := s.targets.GetTargets(ctx, planID)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		"asset_key", "asset_class", "region", "weight_within_group", "portfolio_target_weight",
		"target_amount_minor", "current_amount_minor",
	})
	for _, h := range targets.Holdings {
		if !h.Enabled {
			continue
		}
		_ = w.Write([]string{
			h.AssetKey,
			h.AssetClass,
			h.Region,
			fmt.Sprintf("%.6f", h.WeightWithinGroup),
			fmt.Sprintf("%.6f", h.PortfolioTargetWeight),
			fmt.Sprintf("%d", h.TargetAmountMinor),
			fmt.Sprintf("%d", h.CurrentAmountMinor),
		})
	}
	w.Flush()
	return []byte(buf.String()), w.Error()
}

func (s *SystemService) ExportRebalanceCSV(ctx context.Context, planID string) ([]byte, error) {
	reb, err := s.rebalance.GetRebalance(ctx, planID, "full", 0)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{
		"asset_key", "structural_action", "structural_suggested_trade_minor",
		"structural_gap_weight", "portfolio_target_weight", "structural_current_weight",
		"plan_scale_action", "plan_scale_suggested_trade_minor",
	})
	for _, line := range reb.Lines {
		if !line.Enabled {
			continue
		}
		_ = w.Write([]string{
			line.AssetKey,
			line.Action,
			fmt.Sprintf("%d", line.SuggestedTradeMinor),
			fmt.Sprintf("%.6f", line.StructuralGapWeight),
			fmt.Sprintf("%.6f", line.PortfolioTargetWeight),
			fmt.Sprintf("%.6f", line.StructuralCurrentWeight),
			line.PlanScaleAction,
			fmt.Sprintf("%d", line.PlanScaleSuggestedTradeMinor),
		})
	}
	w.Flush()
	return []byte(buf.String()), w.Error()
}

// MarshalPlanExport encodes export payload as JSON bytes.
func MarshalPlanExport(v map[string]any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
