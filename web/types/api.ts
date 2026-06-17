export interface ApiEnvelope<T> {
  code: string;
  message: string;
  data?: T;
  request_id: string;
}

export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
  request_id: string;
}

export interface Plan {
  id: string;
  name: string;
  base_currency: string;
  valuation_date: string;
  status: string;
  config_version: number;
  config_hash: string;
  created_at: number;
  updated_at: number;
  rebalance_actionable_count?: number;
  holdings_gap_minor?: number;
}

export interface PlanParameters {
  plan_id: string;
  current_age: number;
  retirement_age: number;
  end_age: number;
  total_assets_minor: number;
  annual_savings_minor: number;
  annual_savings_growth_rate: number;
  annual_spending_minor: number;
  terminal_wealth_floor_minor: number;
  selected_scenario_id?: string | null;
  inflation_mode: string;
  fixed_inflation_rate: number;
  inflation_mu: number;
  inflation_phi: number;
  inflation_sigma: number;
  withdrawal_type: string;
  withdrawal_rate: number;
  withdrawal_floor_ratio: number;
  withdrawal_ceiling_ratio: number;
  withdrawal_tax_rate: number;
  taxable_withdrawal_ratio: number;
  rebalance_frequency: string;
  rebalance_threshold: number;
  transaction_cost_rate: number;
  simulation_runs: number;
  student_t_df: number;
  seed?: string | null;
  updated_at: number;
}

export interface PlanCashFlow {
  id: string;
  plan_id: string;
  name: string;
  kind: "income" | "expense";
  amount_minor: number;
  start_month_offset: number;
  end_month_offset: number;
  recurrence: "once" | "monthly" | "annual";
  inflation_linked: boolean;
  annual_growth_rate: number;
  enabled: boolean;
  note: string;
}

export interface AssetClassTarget {
  asset_class: string;
  weight: number;
}

export interface RegionTarget {
  asset_class: string;
  region: string;
  weight_within_class: number;
}

export interface AllocationScenario {
  id: string;
  name: string;
  description: string;
  is_builtin: boolean;
  plan_count: number;
  weights: AssetClassTarget[];
  region_targets: RegionTarget[];
  created_at: number;
  updated_at: number;
}

export interface PlanHolding {
  id: string;
  plan_id: string;
  instrument_id: string;
  enabled: boolean;
  asset_class: string;
  region: string;
  weight_within_group: number;
  current_amount_minor: number;
  simulation_snapshot_id: string;
  simulation_snapshot_created_at?: number;
  snapshot_complete_year_count?: number;
  snapshot_monthly_return_count?: number;
  snapshot_history_depth?: string;
  snapshot_metrics_version?: string;
  snapshot_warnings?: string[];
  sort_order: number;
  instrument_code?: string;
  instrument_name?: string;
}

export interface WeightCheck {
  scope: string;
  key: string;
  actual: number;
  target: number;
  passed: boolean;
  message: string;
}

export interface WeightValidationResult {
  passed: boolean;
  checks: WeightCheck[];
}

export interface HoldingTargetLine {
  holding_id: string;
  instrument_id: string;
  instrument_name?: string;
  instrument_code?: string;
  asset_class: string;
  region: string;
  enabled: boolean;
  asset_class_weight: number;
  region_weight: number;
  weight_within_group: number;
  portfolio_target_weight: number;
  target_amount_minor: number;
  current_amount_minor: number;
  current_weight: number;
  deviation_amount_minor: number;
  deviation_weight: number;
  structural_current_weight: number;
  structural_gap_weight: number;
  structural_gap_amount_minor: number;
  structural_target_amount_minor: number;
  plan_gap_weight: number;
  plan_gap_amount_minor: number;
  simulation_snapshot_id: string;
  sort_order: number;
}

export interface TargetView {
  total_assets_minor: number;
  config_hash: string;
  weight_checks: WeightValidationResult;
  asset_class_targets: AssetClassTarget[];
  region_targets: RegionTarget[];
  holdings: HoldingTargetLine[];
}

export interface RebalanceLine extends HoldingTargetLine {
  action: string;
  suggested_trade_minor: number;
  plan_scale_action: string;
  plan_scale_suggested_trade_minor: number;
}

export interface RebalanceSummary {
  total_assets_minor: number;
  configured_total_minor: number;
  holdings_total_minor: number;
  scale_gap_minor: number;
  target_total_minor: number;
  current_total_minor: number;
  actionable_count: number;
  structural_actionable_count: number;
  plan_scale_actionable_count: number;
  estimated_trade_minor: number;
  estimated_cost_minor: number;
}

export interface RebalanceResult {
  mode: string;
  summary: RebalanceSummary;
  lines: RebalanceLine[];
  weight_checks: WeightValidationResult;
}

export interface RebalanceDraft {
  id: string;
  plan_id: string;
  status: string;
  config_version: number;
  baseline_holdings_total_minor: number;
  created_at: number;
  updated_at: number;
  committed_at?: number;
  note?: string;
}

export interface RebalanceDraftLine {
  id: string;
  draft_id: string;
  holding_id: string;
  instrument_id: string;
  instrument_code?: string;
  instrument_name?: string;
  baseline_current_minor: number;
  planned_current_minor: number;
  frozen_target_minor: number;
  frozen_gap_minor: number;
  frozen_gap_weight: number;
  frozen_action: string;
  frozen_suggested_trade_minor: number;
  recommended_package_delta_minor: number;
  last_saved_at?: number;
}

export interface RebalanceDraftEvent {
  id: string;
  draft_id: string;
  seq: number;
  event_type: string;
  payload_json: string;
  created_at: number;
}

export interface RebalanceDraftDetail {
  draft: RebalanceDraft;
  lines: RebalanceDraftLine[];
  events: RebalanceDraftEvent[];
  fund_pool: {
    released_minor: number;
    used_minor: number;
    net_minor: number;
  };
}

export interface RebalanceExecution {
  id: string;
  plan_id: string;
  status: string;
  created_at: number;
  updated_at: number;
  started_at?: number;
  completed_at?: number;
  baseline_holdings_total_minor: number;
  baseline_config_version: number;
  baseline_snapshot_json: string;
  cash_pool_minor: number;
  note?: string;
}

export interface RebalanceExecutionLine {
  id: string;
  execution_id: string;
  holding_id: string;
  instrument_id: string;
  instrument_code?: string;
  instrument_name?: string;
  baseline_current_minor: number;
  target_delta_minor: number;
  executed_delta_minor: number;
  remaining_delta_minor: number;
  action_direction: string;
  execution_status: string;
  sort_order: number;
}

export interface RebalanceExecutionEvent {
  id: string;
  execution_id: string;
  seq: number;
  event_type: string;
  instrument_id?: string;
  amount_minor: number;
  cash_pool_after_minor: number;
  payload_json: string;
  created_at: number;
}

export interface RebalanceExecutionStats {
  line_count: number;
  done_line_count: number;
  skipped_line_count?: number;
  sold_total_minor: number;
  bought_total_minor: number;
}

export interface RebalanceExecutionDetail {
  execution: RebalanceExecution;
  lines: RebalanceExecutionLine[];
  events: RebalanceExecutionEvent[];
  stats: RebalanceExecutionStats;
}

export interface RebalanceExecutionSummary extends RebalanceExecution {
  line_count: number;
  done_line_count: number;
  last_event_at?: number;
}

export interface ActiveRebalanceExecution {
  id: string;
  status: string;
  cash_pool_minor: number;
  done_line_count: number;
  line_count: number;
}

export interface Instrument {
  id: string;
  code: string;
  name: string;
  market: string;
  instrument_type: string;
  asset_class: string;
  region: string;
  currency: string;
  provider: string;
  is_system: boolean;
  expense_ratio?: number | null;
  expense_ratio_status: string;
  fee_treatment: string;
  status: string;
  quality_status?: string;
  data_as_of?: string;
  data_source_name?: string;
  point_type?: string;
  data_stale: boolean;
  stale_warning?: string;
  referencing_plan_count?: number;
  simulation_eligible?: boolean;
  history_depth?: string;
  complete_year_count?: number;
  monthly_return_count?: number;
  metrics_version?: string;
  warnings?: string[];
  created_at: number;
  updated_at: number;
}

export interface InstrumentImportRequest {
  market: string;
  instrument_type: string;
  code: string;
}

export interface Job {
  id: string;
  plan_id: string;
  type: string;
  status: "queued" | "running" | "succeeded" | "failed" | "canceled";
  input_hash: string;
  progress_current: number;
  progress_total: number;
  phase: string;
  cancel_requested: boolean;
  retry_count: number;
  error_code?: string;
  error_message?: string;
  created_at: number;
  started_at?: number | null;
  finished_at?: number | null;
}

export interface JobEvent {
  job_id: string;
  status: string;
  phase?: string;
  progress_current: number;
  progress_total: number;
  error_code?: string;
  error_message?: string;
  run_id?: string;
}

export interface SimulationRun {
  id: string;
  job_id: string;
  plan_id: string;
  input_hash: string;
  current_config_hash: string;
  result_stale: boolean;
  market_snapshot_hash: string;
  engine_version: string;
  runs: number;
  seed: string;
  horizon_months: number;
  success_count: number;
  failure_count: number;
  summary_json: SimulationSummary;
  created_at: number;
  asset_participation?: {
    holding_id: string;
    instrument_id: string;
    complete_years: number[];
  }[];
}

export interface SimulationSummary {
  success_probability?: number;
  success_wilson_low?: number;
  success_wilson_high?: number;
  terminal_quantiles?: Record<string, number>;
  monthly_wealth_quantiles?: QuantilePoint[];
  failure_year_quantiles?: Record<string, number>;
  max_drawdown_quantiles?: Record<string, number>;
  model_warnings?: string[];
  correlation_disclaimer?: string;
}

export interface QuantilePoint {
  month_offset: number;
  p00_minor: number;
  p05_minor: number;
  p25_minor: number;
  p50_minor: number;
  p75_minor: number;
  p95_minor: number;
}

export interface PathIndexRow {
  run_id: string;
  path_no: number;
  path_seed: string;
  succeeded: boolean;
  failure_month?: number | null;
  terminal_wealth_minor: number;
  max_drawdown: number;
  representative_percentile?: string;
}

export interface PathMonthRecord {
  month_offset: number;
  total_wealth_minor: number;
  spending_minor: number;
  income_minor: number;
  tax_minor: number;
  transaction_cost: number;
  drawdown: number;
  rebalanced: boolean;
}

export interface PathYearRecord {
  year: number;
  start_wealth_minor: number;
  income_minor: number;
  spending_minor: number;
  tax_minor: number;
  transaction_cost: number;
  investment_gain_loss: number;
  end_wealth_minor: number;
  year_end_drawdown: number;
  max_intra_year_dd: number;
  rebalanced: boolean;
  asset_weights?: Record<string, number>;
}

export interface PathDetail {
  path_no: number;
  path_seed: string;
  succeeded: boolean;
  failure_month?: number | null;
  failure_reason?: string;
  monthly: PathMonthRecord[];
  yearly: PathYearRecord[];
}

export interface DashboardAnalysisSummary {
  available: boolean;
  job_id?: string;
  result_stale?: boolean;
  baseline_success_probability?: number;
  worst_scenario_id?: string;
  worst_scenario_name?: string;
  top_parameters?: string[];
  message?: string;
}

export interface DashboardView {
  plan: Plan;
  scenario_name?: string;
  parameters: PlanParameters;
  weight_checks: WeightValidationResult;
  holdings_sum_minor: number;
  invested_minor: number;
  invested_ratio: number;
  holdings_gap_minor: number;
  rebalance_summary: RebalanceSummary;
  active_rebalance_execution?: ActiveRebalanceExecution | null;
  allocation_bars: {
    asset_class: string;
    target_weight: number;
    current_weight: number;
  }[];
  region_bars: {
    region: string;
    target_weight: number;
    current_weight: number;
  }[];
  top_deviations: {
    instrument_name: string;
    instrument_code: string;
    deviation_weight: number;
    deviation_amount_minor: number;
    portfolio_target_weight: number;
    current_weight: number;
  }[];
  data_warnings: string[];
  latest_simulation?: SimulationRun | null;
  stress_test?: DashboardAnalysisSummary;
  sensitivity_test?: DashboardAnalysisSummary;
}
