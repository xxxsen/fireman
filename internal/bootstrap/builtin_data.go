// Package bootstrap initializes immutable application-owned reference data
// after the database schema has been created.
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
)

// EnsureBuiltinData idempotently publishes the reference rows required by a
// fresh Fireman installation. Business data belongs here rather than in SQL
// migrations, which are restricted to DDL.
func EnsureBuiltinData(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("bootstrap: begin builtin data transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, builtinScenariosSQL); err != nil {
		return fmt.Errorf("bootstrap: ensure allocation scenarios: %w", err)
	}
	if _, err := tx.ExecContext(ctx, builtinScenarioWeightsSQL); err != nil {
		return fmt.Errorf("bootstrap: ensure scenario weights: %w", err)
	}
	if _, err := tx.ExecContext(ctx, builtinScenarioRegionsSQL); err != nil {
		return fmt.Errorf("bootstrap: ensure scenario regions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, builtinInstrumentsSQL); err != nil {
		return fmt.Errorf("bootstrap: ensure system instruments: %w", err)
	}
	if _, err := tx.ExecContext(ctx, builtinCashAssetsSQL); err != nil {
		return fmt.Errorf("bootstrap: ensure system cash assets: %w", err)
	}
	if _, err := tx.ExecContext(ctx, builtinCashSnapshotsSQL); err != nil {
		return fmt.Errorf("bootstrap: ensure system cash snapshots: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bootstrap: commit builtin data transaction: %w", err)
	}
	return nil
}

const builtinScenariosSQL = `
INSERT INTO allocation_scenarios
  (id, name, description, is_builtin, created_at, updated_at)
VALUES
  ('scn_builtin_accumulation', '积累期', '距离 FIRE 较远，风险承受能力较高', 1, 0, 0),
  ('scn_builtin_near_fire', '接近 FIRE', 'Excel 默认 70/30 配置', 1, 0, 0),
  ('scn_builtin_post_fire', '已 FIRE', '降低波动并预留现金缓冲', 1, 0, 0),
  ('scn_builtin_conservative', '保守', '偏防守配置', 1, 0, 0)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  description=excluded.description,
  is_builtin=excluded.is_builtin`

const builtinScenarioWeightsSQL = `
INSERT INTO allocation_scenario_weights (scenario_id, asset_class, weight)
VALUES
  ('scn_builtin_accumulation', 'equity', 0.80),
  ('scn_builtin_accumulation', 'bond', 0.20),
  ('scn_builtin_accumulation', 'cash', 0.00),
  ('scn_builtin_near_fire', 'equity', 0.70),
  ('scn_builtin_near_fire', 'bond', 0.30),
  ('scn_builtin_near_fire', 'cash', 0.00),
  ('scn_builtin_post_fire', 'equity', 0.55),
  ('scn_builtin_post_fire', 'bond', 0.35),
  ('scn_builtin_post_fire', 'cash', 0.10),
  ('scn_builtin_conservative', 'equity', 0.45),
  ('scn_builtin_conservative', 'bond', 0.45),
  ('scn_builtin_conservative', 'cash', 0.10)
ON CONFLICT(scenario_id, asset_class) DO UPDATE SET weight=excluded.weight`

const builtinScenarioRegionsSQL = `
INSERT INTO allocation_scenario_region_targets
  (scenario_id, asset_class, region, weight_within_class)
VALUES
  ('scn_builtin_accumulation', 'equity', 'domestic', 1.0),
  ('scn_builtin_accumulation', 'equity', 'foreign', 0.0),
  ('scn_builtin_accumulation', 'bond', 'domestic', 1.0),
  ('scn_builtin_accumulation', 'bond', 'foreign', 0.0),
  ('scn_builtin_accumulation', 'cash', 'domestic', 1.0),
  ('scn_builtin_accumulation', 'cash', 'foreign', 0.0),
  ('scn_builtin_near_fire', 'equity', 'domestic', 1.0),
  ('scn_builtin_near_fire', 'equity', 'foreign', 0.0),
  ('scn_builtin_near_fire', 'bond', 'domestic', 1.0),
  ('scn_builtin_near_fire', 'bond', 'foreign', 0.0),
  ('scn_builtin_near_fire', 'cash', 'domestic', 1.0),
  ('scn_builtin_near_fire', 'cash', 'foreign', 0.0),
  ('scn_builtin_post_fire', 'equity', 'domestic', 1.0),
  ('scn_builtin_post_fire', 'equity', 'foreign', 0.0),
  ('scn_builtin_post_fire', 'bond', 'domestic', 1.0),
  ('scn_builtin_post_fire', 'bond', 'foreign', 0.0),
  ('scn_builtin_post_fire', 'cash', 'domestic', 1.0),
  ('scn_builtin_post_fire', 'cash', 'foreign', 0.0),
  ('scn_builtin_conservative', 'equity', 'domestic', 1.0),
  ('scn_builtin_conservative', 'equity', 'foreign', 0.0),
  ('scn_builtin_conservative', 'bond', 'domestic', 1.0),
  ('scn_builtin_conservative', 'bond', 'foreign', 0.0),
  ('scn_builtin_conservative', 'cash', 'domestic', 1.0),
  ('scn_builtin_conservative', 'cash', 'foreign', 0.0)
ON CONFLICT(scenario_id, asset_class, region) DO UPDATE SET
  weight_within_class=excluded.weight_within_class`

const builtinInstrumentsSQL = `
INSERT INTO instruments (
  id, code, name, market, instrument_type, asset_class, region, currency,
  provider, provider_symbol, adjust_policy, is_system,
  expense_ratio, expense_ratio_status, fee_treatment, status,
  created_at, updated_at, instrument_kind, asset_key
)
VALUES
  ('system_cash_cny', 'SYSTEM_CASH_CNY', '人民币现金', 'SYSTEM', 'system_cash',
   'cash', 'domestic', 'CNY', 'system', 'SYSTEM_CASH_CNY', 'none', 1,
   NULL, 'not_applicable', 'none', 'active', 0, 0, '', ''),
  ('system_fx_usdcny', 'USDCNY', '美元/人民币', 'SYSTEM', 'fx_rate',
   'fx', 'domestic', 'CNY', 'system', 'USDCNY', 'none', 1,
   NULL, 'not_applicable', 'none', 'active', 0, 0, '', ''),
  ('system_fx_hkdcny', 'HKDCNY', '港币/人民币', 'SYSTEM', 'fx_rate',
   'fx', 'domestic', 'CNY', 'system', 'HKDCNY', 'none', 1,
   NULL, 'not_applicable', 'none', 'active', 0, 0, '', '')
ON CONFLICT(id) DO UPDATE SET
  code=excluded.code,
  name=excluded.name,
  market=excluded.market,
  instrument_type=excluded.instrument_type,
  asset_class=excluded.asset_class,
  region=excluded.region,
  currency=excluded.currency,
  provider=excluded.provider,
  provider_symbol=excluded.provider_symbol,
  adjust_policy=excluded.adjust_policy,
  is_system=excluded.is_system,
  expense_ratio=excluded.expense_ratio,
  expense_ratio_status=excluded.expense_ratio_status,
  fee_treatment=excluded.fee_treatment,
  status=excluded.status`

const builtinCashAssetsSQL = `
INSERT INTO market_assets (
  asset_key, market, instrument_type, region_code, symbol, name,
  exchange, instrument_kind, currency, canonical_symbol, fee_mode,
  active, listing_status, last_seen_at, source_name, source_as_of,
  refreshed_at, created_at, updated_at
)
VALUES
  ('SYS|cash||CNY', 'SYS', 'cash', '', 'CNY', '人民币现金',
   '', 'cash', 'CNY', '', '', 1, 'active', 0, 'system', '', 0, 0, 0),
  ('SYS|cash||USD', 'SYS', 'cash', '', 'USD', '美元现金',
   '', 'cash', 'USD', '', '', 1, 'active', 0, 'system', '', 0, 0, 0),
  ('SYS|cash||HKD', 'SYS', 'cash', '', 'HKD', '港币现金',
   '', 'cash', 'HKD', '', '', 1, 'active', 0, 'system', '', 0, 0, 0)
ON CONFLICT(asset_key) DO UPDATE SET
  market=excluded.market,
  instrument_type=excluded.instrument_type,
  region_code=excluded.region_code,
  symbol=excluded.symbol,
  name=excluded.name,
  exchange=excluded.exchange,
  instrument_kind=excluded.instrument_kind,
  currency=excluded.currency,
  active=excluded.active,
  listing_status=excluded.listing_status,
  source_name=excluded.source_name`

const builtinCashSnapshotsSQL = `
INSERT INTO market_asset_simulation_snapshots (
  id, asset_key, plan_id, inclusion_date, as_of_date,
  window_start, window_end, complete_year_start, complete_year_end,
  complete_year_count, daily_observation_count, monthly_return_count,
  volatility_method, metrics_version, history_depth,
  historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
  expense_ratio, expense_ratio_status, fee_treatment,
  source_mode, quality_status, warnings_json, source_hash, created_at,
  adjust_policy
)
VALUES
  ('sim_snapshot_system_cash_cny', 'SYS|cash||CNY', NULL,
   '1970-01-01', '1970-01-01', NULL, NULL, NULL, NULL,
   0, 0, 0, 'not_applicable', 'system_cash_v1', 'system',
   0, 0, 0, 0, NULL, 'not_applicable', 'none',
   'system_cash', 'available', '[]', 'system_cash_cny', 0, 'none'),
  ('sim_snapshot_system_cash_usd', 'SYS|cash||USD', NULL,
   '1970-01-01', '1970-01-01', NULL, NULL, NULL, NULL,
   0, 0, 0, 'not_applicable', 'system_cash_v1', 'system',
   0, 0, 0, 0, NULL, 'not_applicable', 'none',
   'system_cash', 'available', '[]', 'system_cash_usd', 0, 'none'),
  ('sim_snapshot_system_cash_hkd', 'SYS|cash||HKD', NULL,
   '1970-01-01', '1970-01-01', NULL, NULL, NULL, NULL,
   0, 0, 0, 'not_applicable', 'system_cash_v1', 'system',
   0, 0, 0, 0, NULL, 'not_applicable', 'none',
   'system_cash', 'available', '[]', 'system_cash_hkd', 0, 'none')
ON CONFLICT(id) DO UPDATE SET
  asset_key=excluded.asset_key,
  plan_id=excluded.plan_id,
  inclusion_date=excluded.inclusion_date,
  as_of_date=excluded.as_of_date,
  complete_year_count=excluded.complete_year_count,
  daily_observation_count=excluded.daily_observation_count,
  monthly_return_count=excluded.monthly_return_count,
  volatility_method=excluded.volatility_method,
  metrics_version=excluded.metrics_version,
  history_depth=excluded.history_depth,
  historical_cagr=excluded.historical_cagr,
  modeled_annual_return=excluded.modeled_annual_return,
  annual_volatility=excluded.annual_volatility,
  max_drawdown=excluded.max_drawdown,
  expense_ratio=excluded.expense_ratio,
  expense_ratio_status=excluded.expense_ratio_status,
  fee_treatment=excluded.fee_treatment,
  source_mode=excluded.source_mode,
  quality_status=excluded.quality_status,
  warnings_json=excluded.warnings_json,
  source_hash=excluded.source_hash,
  adjust_policy=excluded.adjust_policy`
