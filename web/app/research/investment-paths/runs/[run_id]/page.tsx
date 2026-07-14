"use client";

import { useMemo, useState } from "react";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import {
  getInvestmentPathPoints,
  getInvestmentPathRun,
  getInvestmentPathTrades,
  getInvestmentPathWindows,
} from "@/lib/api/investment-paths";
import { isTaskActive } from "@/lib/api/tasks";
import { queryErrorMessage } from "@/lib/query-error";
import { formatMoney, formatPercent } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { LoadingState } from "@/components/ui/LoadingState";
import { ErrorState } from "@/components/ui/ErrorState";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { Alert } from "@/components/ui/Alert";
import { HelpLabel } from "@/components/ui/HelpLabel";
import { ChartFrame } from "@/components/charts/ChartFrame";
import { InvestmentPathChart } from "@/components/research/InvestmentPathChart";

const strategyLabel = (key: string) => {
  if (key === "income_dca") return "固定月投";
  if (key === "income_cash_baseline") return "同现金流留存现金";
  if (key === "lump_sum") return "一次性投入";
  if (key.startsWith("phase_in_")) return key.replace("phase_in_", "分批 ").replace("m", " 个月");
  if (key.startsWith("static_")) return `静态资产/现金 ${Number(key.split("_")[1]) * 100}%`;
  if (key.startsWith("threshold_")) return `阈值再平衡 ${Number(key.split("_")[1]) * 100}%`;
  return key;
};

const tradeReasonLabel = (reason: string) => {
  if (reason === "initial") return "首次建仓";
  if (reason === "scheduled") return "计划投入";
  if (reason === "threshold") return "阈值再平衡";
  return reason;
};

function MetricCard({ label, value, termKey }: { label: string; value: string; termKey: string }) {
  return (
    <div className="rounded-md bg-surface-muted p-3">
      <p className="text-xs text-ink-muted"><HelpLabel label={label} termKey={termKey} /></p>
      <p className="mt-1 font-medium text-ink">{value}</p>
    </div>
  );
}

export default function InvestmentPathRunPage() {
  const id = useParams().run_id as string;
  const [strategy, setStrategy] = useState("");
  const runQuery = useQuery({
    queryKey: ["research", "investment-path-run", id],
    queryFn: () => getInvestmentPathRun(id),
    refetchInterval: (query) => isTaskActive(query.state.data?.task.status) ? 1500 : false,
  });
  const run = runQuery.data;
  const activeStrategy = strategy || run?.strategies[0] || "";
  const complete = run?.task.status === "complete";
  const pointsQuery = useQuery({
    queryKey: ["research", "investment-path-points", id, activeStrategy],
    queryFn: () => getInvestmentPathPoints(id, activeStrategy),
    enabled: complete && !!activeStrategy,
  });
  const tradesQuery = useQuery({
    queryKey: ["research", "investment-path-trades", id, activeStrategy],
    queryFn: () => getInvestmentPathTrades(id, activeStrategy),
    enabled: complete && !!activeStrategy,
  });
  const windowsQuery = useQuery({
    queryKey: ["research", "investment-path-windows", id, activeStrategy],
    queryFn: () => getInvestmentPathWindows(id, activeStrategy),
    enabled: complete && !!activeStrategy,
  });
  const primary = useMemo(
    () => run?.summary.primary?.find((row) => row.strategy_key === activeStrategy),
    [run, activeStrategy],
  );
  const aggregate = useMemo(
    () => run?.summary.aggregates?.find((row) => row.strategy_key === activeStrategy),
    [run, activeStrategy],
  );
  const sourceStart = typeof run?.data_quality.source_start === "string" ? run.data_quality.source_start : "";
  const preHistoryWindowCount = useMemo(
    () => sourceStart
      ? (windowsQuery.data?.windows ?? []).filter((row) => row.window_start < sourceStart).length
      : 0,
    [sourceStart, windowsQuery.data?.windows],
  );

  if (runQuery.isLoading) return <div className="content-enter"><LoadingState label="加载投入路径实验…" /></div>;
  if (runQuery.isError || !run) {
    return (
      <div className="content-enter">
        <ErrorState
          message="加载实验失败。"
          technicalDetail={queryErrorMessage(runQuery.error)}
          onRetry={() => void runQuery.refetch()}
          backHref="/research/investment-paths"
        />
      </div>
    );
  }

  const taskProgress = complete
    ? "计算完成"
    : `${run.task.phase || "等待 Worker"} · ${run.task.progress_current}/${run.task.progress_total}`;

  return (
    <div className="content-enter space-y-6">
      <PageHeader
        backHref="/research/investment-paths"
        backLabel="投入路径实验"
        title={run.mode === "income_dca" ? "工资型定投结果" : "存量资金入场结果"}
        description={`${run.primary_start} ~ ${run.primary_end}，固定 ${run.horizon_months} 个月。历史窗口用于理解过程和代价，不代表未来概率。`}
        secondaryActions={<a className="text-sm text-brand underline" href={`/api/v1/research/investment-path-runs/${id}/export.csv`}>导出 CSV</a>}
      />

      <section className="rounded-lg border border-line bg-surface p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <TaskStatusBadge
              status={run.task.status}
              labels={{ pending: "等待计算", running: "计算中", pre_complete: "保存中", complete: "已完成", failed: "失败" }}
            />
            <span className="text-sm text-ink-muted">{taskProgress}</span>
          </div>
          <TaskCancelButton task={run.task} onCanceled={async () => { await runQuery.refetch(); }} />
        </div>
        {run.task.status === "failed" ? (
          <div className="mt-3"><Alert variant="danger" title={run.task.error_code || "实验失败"}>{run.task.error_message || "后台计算未完成。"}</Alert></div>
        ) : null}
        {run.task.status === "canceled" ? (
          <div className="mt-3"><Alert variant="warning">实验已取消，没有发布部分结果。</Alert></div>
        ) : null}
      </section>

      {complete ? (
        <>
          <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="font-semibold text-ink">主窗口</h2>
              <select
                className="rounded-md border border-line bg-surface px-3 py-2 text-sm"
                value={activeStrategy}
                onChange={(event) => setStrategy(event.target.value)}
              >
                {run.strategies.map((key) => <option key={key} value={key}>{strategyLabel(key)}</option>)}
              </select>
            </div>
            {primary ? (
              <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <MetricCard label="累计投入" value={formatMoney(primary.total_contribution_minor, run.base_currency)} termKey="investment_path_total_contribution" />
                <MetricCard label="期末资产" value={formatMoney(primary.terminal_value_minor, run.base_currency)} termKey="investment_path_terminal_value" />
                <MetricCard label="投资损益" value={formatMoney(primary.profit_minor, run.base_currency)} termKey="investment_path_profit" />
                <MetricCard
                  label="资金加权年化 XIRR"
                  value={primary.xirr == null ? `不可计算${primary.xirr_reason ? `（${primary.xirr_reason}）` : ""}` : formatPercent(primary.xirr)}
                  termKey="investment_path_xirr"
                />
                <MetricCard label="时间加权年化" value={formatPercent(primary.twr_annualized)} termKey="investment_path_twr_annualized" />
                <MetricCard label="单位净值最大回撤" value={formatPercent(primary.max_drawdown)} termKey="investment_path_max_drawdown" />
                <MetricCard label="低于本金最长时间" value={`${primary.longest_below_principal_days} 天`} termKey="investment_path_below_principal" />
                <MetricCard label="交易成本" value={formatMoney(primary.total_transaction_cost_minor, run.base_currency)} termKey="investment_path_transaction_cost" />
              </div>
            ) : null}
          </section>

          <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
            <ChartFrame
              title="账户价值与本金路径"
              termKey="investment_path_account_path"
              xAxis="估值日期"
              yAxis="账户金额"
              unit={run.base_currency}
              legend={<span>实线为账户价值，虚线为累计外部投入；移动鼠标、触摸或使用左右方向键可查看日期点详情。</span>}
              interpretation="账户价值高于累计投入表示当日整体盈利，低于累计投入表示当日仍有本金浮亏；回撤另按剔除新增本金影响的单位净值计算。"
            >
              <InvestmentPathChart points={pointsQuery.data?.points ?? []} currency={run.base_currency} />
            </ChartFrame>
          </section>

          {aggregate ? (
            <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
              <h2 className="font-semibold text-ink"><HelpLabel label="滚动起点分布" termKey="investment_path_rolling_windows" /></h2>
              <p className="mt-1 text-xs text-ink-muted">本次冻结历史的 {aggregate.window_count} 个按月起点；不是未来胜率。</p>
              {preHistoryWindowCount > 0 ? (
                <Alert variant="warning" title="部分滚动窗口早于资产历史" className="mt-3">
                  当前结果有 {preHistoryWindowCount} 个起点早于首个可交易日期 {sourceStart}，这些窗口会先把计划投入留在现金中，因此滚动分布不适合作为有效历史比较。主窗口不受影响；请重新创建实验后再解读滚动分布。
                </Alert>
              ) : null}
              <div className="mt-4 grid gap-3 sm:grid-cols-3">
                <MetricCard
                  label="期末资产 P10 / P50 / P90"
                  value={`${formatMoney(aggregate.terminal_value_minor.p10, run.base_currency)} / ${formatMoney(aggregate.terminal_value_minor.p50, run.base_currency)} / ${formatMoney(aggregate.terminal_value_minor.p90, run.base_currency)}`}
                  termKey="investment_path_quantiles"
                />
                <MetricCard
                  label="最大回撤 P10 / P50 / P90"
                  value={`${formatPercent(aggregate.max_drawdown.p10)} / ${formatPercent(aggregate.max_drawdown.p50)} / ${formatPercent(aggregate.max_drawdown.p90)}`}
                  termKey="investment_path_drawdown_quantiles"
                />
                <MetricCard
                  label="最差 / 最佳起点"
                  value={`${aggregate.worst_start} / ${aggregate.best_start}`}
                  termKey="investment_path_best_worst_start"
                />
              </div>
              {aggregate.baseline_key ? (
                <p className="mt-3 text-sm text-ink-muted">
                  相对 {strategyLabel(aggregate.baseline_key)}：{aggregate.higher_terminal_count}/{aggregate.paired_window_count} 个配对窗口期末值更高（{formatPercent(aggregate.higher_terminal_ratio ?? 0)}）。
                </p>
              ) : null}
            </section>
          ) : null}

          <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
            <h2 className="font-semibold text-ink">逐个滚动窗口</h2>
            <div className="mt-3 hidden overflow-x-auto md:block">
              <table className="w-full min-w-[720px] text-sm">
                <thead>
                  <tr className="border-b border-line text-left text-xs text-ink-muted">
                    <th className="py-2"><HelpLabel label="起点" termKey="investment_path_rolling_windows" /></th>
                    <th><HelpLabel label="终值" termKey="investment_path_terminal_value" /></th>
                    <th><HelpLabel label="XIRR" termKey="investment_path_xirr" /></th>
                    <th><HelpLabel label="最大回撤" termKey="investment_path_max_drawdown" /></th>
                    <th><HelpLabel label="低于本金" termKey="investment_path_below_principal" /></th>
                    <th><HelpLabel label="费用" termKey="investment_path_transaction_cost" /></th>
                  </tr>
                </thead>
                <tbody>
                  {(windowsQuery.data?.windows ?? []).map((row) => (
                    <tr key={row.window_start} className="border-b border-line/60">
                      <td className="py-2">{row.window_start}</td>
                      <td>{formatMoney(row.terminal_value_minor, run.base_currency)}</td>
                      <td>{row.xirr == null ? "—" : formatPercent(row.xirr)}</td>
                      <td>{formatPercent(row.max_drawdown)}</td>
                      <td>{row.longest_below_principal_days} 天</td>
                      <td>{formatMoney(row.total_transaction_cost_minor, run.base_currency)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-3 grid gap-2 md:hidden">
              {(windowsQuery.data?.windows ?? []).map((row) => (
                <article key={row.window_start} className="rounded-md border border-line p-3 text-sm">
                  <div className="flex justify-between"><strong>{row.window_start}</strong><span>{formatMoney(row.terminal_value_minor, run.base_currency)}</span></div>
                  <p className="mt-1 text-xs text-ink-muted">
                    XIRR {row.xirr == null ? "不可计算" : formatPercent(row.xirr)} · 回撤 {formatPercent(row.max_drawdown)} · 低于本金 {row.longest_below_principal_days} 天
                  </p>
                </article>
              ))}
            </div>
          </section>

          <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
            <h2 className="font-semibold text-ink">主窗口交易</h2>
            <div className="mt-3 space-y-2">
              {(tradesQuery.data?.trades ?? []).map((trade) => (
                <div key={trade.sequence_no} className="flex flex-wrap justify-between gap-2 rounded-md border border-line px-3 py-2 text-sm">
                  <span>{trade.trade_date} · {trade.side === "buy" ? "买入" : "卖出"} · {tradeReasonLabel(trade.reason)}</span>
                  <span>{formatMoney(trade.gross_trade_minor, run.base_currency)}，费用 {formatMoney(trade.fee_minor, run.base_currency)}</span>
                </div>
              ))}
              {tradesQuery.data?.trades.length === 0 ? <p className="text-sm text-ink-muted">该策略没有交易。</p> : null}
            </div>
          </section>

          <section className="rounded-lg border border-line bg-surface p-4 text-sm text-ink-muted">
            <h2 className="font-medium text-ink">计算口径</h2>
            <p className="mt-2">交易只在资产真实历史点发生；非交易日计划预算保留为零收益现金。所有买入（含首次建仓）计费。XIRR 使用实际外部现金流日期，时间加权收益和回撤使用现金流发生前单位净值。复权历史仅生成合成单位，不表示真实可成交份额。</p>
            <details className="mt-3">
              <summary className="cursor-pointer">审计信息</summary>
              <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 break-all text-xs">
                <dt>引擎</dt><dd>{run.engine_version}</dd>
                <dt>input hash</dt><dd>{run.input_hash}</dd>
                <dt>source hash</dt><dd>{run.source_hash}</dd>
                <dt>task</dt><dd>{run.task_id}</dd>
              </dl>
            </details>
          </section>
        </>
      ) : null}
    </div>
  );
}
