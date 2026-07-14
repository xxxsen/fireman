import { apiGet, apiPost } from "./client";
import type { ResearchAssetView } from "./research";
import type { Task } from "@/types/api";

export type InvestmentPathMode = "income_dca" | "existing_capital";

export interface InvestmentPathRequest {
  mode: InvestmentPathMode;
  asset: { asset_key: string; adjust_policy: string; point_type: string };
  base_currency: string;
  evaluation_start: string;
  evaluation_end: string;
  horizon_months: number;
  primary_start?: string;
  monthly_day: number;
  transaction_cost_rate: number;
  income_dca?: {
    initial_investment_minor: number;
    monthly_contribution_minor: number;
  };
  existing_capital?: {
    initial_capital_minor: number;
    phase_in_months: number[];
    threshold_comparison?: {
      enabled: boolean;
      target_asset_weight: number;
      rebalance_threshold: number;
    };
  };
  idempotency_key?: string;
}

export interface InvestmentPathResolved {
  source_start: string;
  source_end: string;
  primary_start: string;
  primary_first_execution_date: string;
  primary_end: string;
  window_starts: string[];
  strategy_keys: string[];
  path_day_budget: number;
}

export interface InvestmentPathIssue { code: string; message: string }
export interface InvestmentPathReadiness {
  ready: boolean;
  issues: InvestmentPathIssue[];
  warnings: InvestmentPathIssue[];
  resolved?: InvestmentPathResolved;
}

export interface InvestmentPathWindow {
  strategy_key: string;
  window_start: string;
  window_end: string;
  total_contribution_minor: number;
  terminal_value_minor: number;
  profit_minor: number;
  xirr?: number | null;
  xirr_reason?: string;
  twr_total: number;
  twr_annualized: number;
  max_drawdown: number;
  max_drawdown_start: string;
  max_drawdown_end: string;
  longest_underwater_days: number;
  max_principal_deficit_minor: number;
  max_principal_deficit_ratio: number;
  longest_below_principal_days: number;
  first_recovery_above_principal_date?: string;
  average_cash_weight: number;
  total_transaction_cost_minor: number;
  trade_count: number;
  turnover: number;
  deployment_complete_date?: string;
}

export interface InvestmentPathAggregate {
  strategy_key: string;
  window_count: number;
  terminal_value_minor: { p10: number; p50: number; p90: number };
  xirr: { p10: number; p50: number; p90: number };
  xirr_count: number;
  max_drawdown: { p10: number; p50: number; p90: number };
  best_start: string;
  worst_start: string;
  baseline_key?: string;
  higher_terminal_count?: number;
  paired_window_count?: number;
  higher_terminal_ratio?: number;
}

export interface InvestmentPathPoint {
  strategy_key: string;
  valuation_date: string;
  account_value_minor: number;
  asset_value_minor: number;
  cash_value_minor: number;
  cumulative_external_contribution_minor: number;
  unit_nav: number;
  drawdown: number;
}

export interface InvestmentPathTrade {
  strategy_key: string;
  sequence_no: number;
  trade_date: string;
  side: "buy" | "sell";
  reason: "initial" | "scheduled" | "threshold";
  gross_trade_minor: number;
  fee_minor: number;
  asset_value_delta_minor: number;
  cash_delta_minor: number;
}

export interface InvestmentPathRun {
  id: string;
  task_id: string;
  asset_key: string;
  mode: InvestmentPathMode;
  input_hash: string;
  source_hash: string;
  input_snapshot_json: string;
  engine_version: string;
  base_currency: string;
  evaluation_start: string;
  evaluation_end: string;
  primary_start: string;
  primary_end: string;
  horizon_months: number;
  created_at: number;
  completed_at?: number | null;
  task: Task;
  strategies: string[];
  summary: { primary?: InvestmentPathWindow[]; aggregates?: InvestmentPathAggregate[]; warnings?: string[] };
  data_quality: Record<string, unknown>;
}

export const investmentPathReadiness = (request: InvestmentPathRequest) =>
  apiPost<InvestmentPathReadiness>("/api/v1/research/investment-paths/readiness", request);

export const createInvestmentPathRun = (request: InvestmentPathRequest) =>
  apiPost<{ run: InvestmentPathRun; reused: boolean }>("/api/v1/research/investment-path-runs", request);

export const listInvestmentPathRuns = (limit = 50) =>
  apiGet<{ runs: InvestmentPathRun[] }>(`/api/v1/research/investment-path-runs?limit=${limit}`);

export const getInvestmentPathRun = (id: string) =>
  apiGet<InvestmentPathRun>(`/api/v1/research/investment-path-runs/${id}`);

export const getInvestmentPathPoints = (id: string, strategy: string) =>
  apiGet<{ points: InvestmentPathPoint[] }>(`/api/v1/research/investment-path-runs/${id}/points?strategy_key=${encodeURIComponent(strategy)}`);

export const getInvestmentPathTrades = (id: string, strategy: string) =>
  apiGet<{ trades: InvestmentPathTrade[] }>(`/api/v1/research/investment-path-runs/${id}/trades?strategy_key=${encodeURIComponent(strategy)}`);

export const getInvestmentPathWindows = (id: string, strategy: string) =>
  apiGet<{ windows: InvestmentPathWindow[] }>(`/api/v1/research/investment-path-runs/${id}/windows?limit=1000&strategy_key=${encodeURIComponent(strategy)}`);

export type InvestmentPathAsset = Pick<ResearchAssetView, "asset_key" | "name" | "symbol" | "currency" | "adjust_policy" | "point_type" | "backtest_ready" | "is_cash">;
