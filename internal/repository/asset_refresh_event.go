package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// AssetRefreshEvent records one asset refresh submission.
type AssetRefreshEvent struct {
	ID               string `json:"id"`
	PlanID           string `json:"plan_id"`
	RefreshedAt      int64  `json:"refreshed_at"`
	BeforeTotalMinor int64  `json:"before_total_minor"`
	AfterTotalMinor  int64  `json:"after_total_minor"`
	SyncScale        bool   `json:"sync_scale"`
	ConfigChanged    bool   `json:"config_changed"`
}

// AssetRefreshEventRepo persists asset refresh audit rows.
type AssetRefreshEventRepo struct {
	db *sql.DB
}

func NewAssetRefreshEventRepo(db *sql.DB) *AssetRefreshEventRepo {
	return &AssetRefreshEventRepo{db: db}
}

func (r *AssetRefreshEventRepo) CreateTx(ctx context.Context, tx *sql.Tx, event AssetRefreshEvent) error {
	syncScale := 0
	if event.SyncScale {
		syncScale = 1
	}
	configChanged := 0
	if event.ConfigChanged {
		configChanged = 1
	}
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `
		INSERT INTO asset_refresh_events (
			id, plan_id, refreshed_at, before_total_minor, after_total_minor,
			sync_scale, config_changed
		) VALUES (?,?,?,?,?,?,?)`,
		event.ID, event.PlanID, event.RefreshedAt, event.BeforeTotalMinor, event.AfterTotalMinor,
		syncScale, configChanged)
	if err != nil {
		return fmt.Errorf("insert asset refresh event: %w", err)
	}
	return nil
}

func (r *AssetRefreshEventRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}
