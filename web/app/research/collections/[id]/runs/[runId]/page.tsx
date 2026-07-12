"use client";

import { useMemo, useState } from "react";
import { useMutation, useQueries, useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import {
  createCollection,
  getCollection,
  getRun,
  getRunPoints,
  runExportCSVUrl,
  type ResearchRunDetail,
} from "@/lib/api/research";
import { getMarketAssetDetail } from "@/lib/api/market-assets";
import { queryErrorMessage } from "@/lib/query-error";
import {
  formatDateTimeFromMs,
  formatMoneyScaled,
  formatNullablePercent,
  formatPercent,
} from "@/lib/format";
import type { NormalizedSeries } from "@/lib/research/candidate-analysis";
import { annualWeightDeviation } from "@/lib/research/run-analysis";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { runStatusBadge } from "@/components/research/runStatus";
import { RunCharts } from "@/components/research/RunCharts";
import {
  RunAnnualTable,
  RunContributions,
  RunCorrelationMatrix,
  RunDataQuality,
  RunMonthlyHeatmap,
} from "@/components/research/RunTables";
import { RunRollingCharts } from "@/components/research/RunRollingCharts";
import { RunCompareDialog } from "@/components/research/RunCompareDialog";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";
import type { ResearchRebalancePolicy } from "@/lib/api/research";

interface SnapshotAsset {
  asset_key: string;
  name?: string;
  is_cash?: boolean;
  adjust_policy?: string;
  point_type?: string;
  source_name?: string;
  points_hash?: string;
}

function snapshotAssets(run: ResearchRunDetail | undefined): SnapshotAsset[] {
  const raw = run?.input_snapshot?.assets;
  if (!Array.isArray(raw)) return [];
  return raw as SnapshotAsset[];
}

function MetricCard({
  label,
  value,
  help,
  tone,
}: {
  label: string;
  value: string;
  help?: string;
  tone?: "positive" | "danger";
}) {
  return (
    <div className="rounded-md border border-line bg-surface px-3 py-2">
      <p className="flex items-center text-xs text-ink-muted">
        {label}
        {help && <MetricHelp text={help} />}
      </p>
      <p
        className={
          "mt-0.5 font-mono-numeric text-base font-semibold " +
          (tone === "positive" ? "text-positive" : tone === "danger" ? "text-danger" : "text-ink")
        }
      >
        {value}
      </p>
    </div>
  );
}

function downloadJSON(filename: string, data: unknown) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export default function ResearchRunDetailPage() {
  const params = useParams();
  const collectionId = params.id as string;
  const runId = params.runId as string;
  const router = useRouter();
  const [showAssetSeries, setShowAssetSeries] = useState(false);
  const [compareOpen, setCompareOpen] = useState(false);
  const [showSnapshot, setShowSnapshot] = useState(false);

  const runQuery = useQuery({
    queryKey: ["research", "run", runId],
    queryFn: () => getRun(runId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 2000 : false;
    },
  });

  const collectionQuery = useQuery({
    queryKey: ["research", "collection", collectionId],
    queryFn: () => getCollection(collectionId),
  });

  const run = runQuery.data;
  const succeeded = run?.status === "complete";

  const pointsQuery = useQuery({
    queryKey: ["research", "run-points", runId, "weights"],
    queryFn: () => getRunPoints(runId, { includeWeights: true }),
    enabled: succeeded,
  });

  const snapAssets = useMemo(() => snapshotAssets(run), [run]);
  const nonCashAssets = useMemo(
    () => snapAssets.filter((a) => !a.is_cash),
    [snapAssets],
  );

  const assetDetails = useQueries({
    queries: nonCashAssets.map((a) => ({
      queryKey: ["market-asset-detail", a.asset_key, a.adjust_policy, a.point_type],
      queryFn: () =>
        getMarketAssetDetail(a.asset_key, {
          adjustPolicy: a.adjust_policy,
          pointType: a.point_type,
        }),
      enabled: showAssetSeries && succeeded,
      staleTime: 5 * 60 * 1000,
    })),
    combine: (results) => ({
      loading: results.some((r) => r.isLoading),
      data: results.map((r) => r.data),
    }),
  });

  const points = useMemo(() => pointsQuery.data?.points ?? [], [pointsQuery.data]);

  const assetSeries: NormalizedSeries[] = useMemo(() => {
    if (!showAssetSeries || points.length === 0) return [];
    const dates = points.map((p) => p.date);
    const out: NormalizedSeries[] = [];
    for (let i = 0; i < nonCashAssets.length; i++) {
      const detail = assetDetails.data[i];
      const meta = nonCashAssets[i]!;
      if (!detail || detail.points.length === 0) continue;
      const byDate = new Map(detail.points.map((p) => [p.date, p.value]));
      let last: number | null = null;
      for (const p of detail.points) {
        if (p.date < dates[0]!) last = p.value;
      }
      const values: number[] = [];
      for (const d of dates) {
        const v = byDate.get(d);
        if (v !== undefined && v > 0) last = v;
        values.push(last ?? 0);
      }
      const base = values.find((v) => v > 0) ?? 1;
      out.push({
        assetKey: meta.asset_key,
        name: meta.name ?? meta.asset_key,
        dates,
        navs: values.map((v) => v / base),
        drawdowns: [],
      });
    }
    return out;
  }, [showAssetSeries, points, nonCashAssets, assetDetails.data]);

  const assetNames = useMemo(() => {
    const map: Record<string, string> = {};
    for (const a of snapAssets) map[a.asset_key] = a.name ?? a.asset_key;
    for (const c of run?.summary?.contributions ?? []) map[c.asset_key] = c.name;
    return map;
  }, [snapAssets, run]);

  const weightDeviations = useMemo(() => {
    if (points.length === 0 || !run?.summary) return undefined;
    const targets: Record<string, number> = {};
    for (const c of run.summary.contributions ?? []) targets[c.asset_key] = c.target_weight;
    return annualWeightDeviation(points, targets);
  }, [points, run]);

  const cloneMutation = useMutation({
    mutationFn: () =>
      createCollection({
        name: `${collectionQuery.data?.name ?? "集合"} · 参数副本`,
        from_collection_id: collectionId,
      }),
    onSuccess: (detail) => router.push(`/research/collections/${detail.id}`),
  });

  if (runQuery.isLoading) {
    return (
      <div className="content-enter">
        <LoadingState label="加载回测结果…" />
      </div>
    );
  }

  if (runQuery.isError || !run) {
    return (
      <div className="content-enter">
        <ErrorState
          message="加载回测结果失败。"
          onRetry={() => void runQuery.refetch()}
          backHref={`/research/collections/${collectionId}/runs`}
          technicalDetail={runQuery.isError ? queryErrorMessage(runQuery.error) : undefined}
        />
      </div>
    );
  }

  const summary = run.summary;
  const collectionName = collectionQuery.data?.name ?? "研究集合";

  return (
    <div className="content-enter">
      <PageHeader
        backHref={`/research/collections/${collectionId}`}
        backLabel={collectionName}
        title="回测结果"
        status={runStatusBadge(run.status)}
        description={`${run.window_start} ~ ${run.window_end} · ${
          REBALANCE_POLICY_LABELS[run.rebalance_policy as ResearchRebalancePolicy] ??
          run.rebalance_policy
        } · ${run.base_currency} · ${formatDateTimeFromMs(run.created_at)}`}
        secondaryActions={
          run.status === "complete" ? (
            <>
              <Button variant="secondary" onClick={() => setCompareOpen(true)} data-testid="compare-run">
                与其他 run 对比
              </Button>
              <Button
                variant="secondary"
                pending={cloneMutation.isPending}
                onClick={() => cloneMutation.mutate()}
                data-testid="clone-params"
              >
                复制参数生成新集合
              </Button>
              <a
                href={runExportCSVUrl(run.id)}
                download
                className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-line bg-surface px-4 py-2 text-sm font-medium text-ink transition-colors hover:bg-surface-muted"
                data-testid="export-csv"
              >
                导出 CSV
              </a>
              <Button
                variant="secondary"
                onClick={() => downloadJSON(`research-run-${run.id}.json`, run)}
                data-testid="export-json"
              >
                导出 JSON
              </Button>
            </>
          ) : undefined
        }
      />

      {(run.status === "pending" || run.status === "running") && (
        <div className="rounded-lg border border-info/25 bg-info/5 px-4 py-6 text-center" role="status">
          <LoadingState
            label={
              run.task?.phase === "retrying"
                ? `执行进程中断，正在自动重试（${run.task.attempt_count ?? 0}/1）…`
                : run.task?.phase
                ? `${(run.task.attempt_count ?? 0) > 0 ? `中断后自动重试（${run.task.attempt_count}/1），` : ""}回测计算中（${run.task.phase}${
                    run.task.progress_total > 0
                      ? ` ${run.task.progress_current}/${run.task.progress_total}`
                      : ""
                  }）…`
                : "回测排队/计算中，页面将自动刷新…"
            }
            className="justify-center"
          />
        </div>
      )}

      {run.status === "failed" && (
        <ErrorState
          title="回测失败"
          message={
            run.task?.error_code === "worker_interrupted"
              ? "执行进程中断，自动重试仍未完成。请重新发起回测。"
              : run.task?.error_message || "回测计算失败。"
          }
          technicalDetail={run.task?.error_code}
          backHref={`/research/collections/${collectionId}`}
          backLabel="返回集合"
        />
      )}

      {run.status === "canceled" && (
        <p className="rounded-lg border border-line bg-surface px-4 py-6 text-center text-sm text-ink-muted">
          该运行已取消。
        </p>
      )}

      {succeeded && summary && (
        <div className="space-y-6">
          {run.engine_version !== "research_backtest_v4" && (
            <p className="text-sm text-warning" role="note">
              该历史回测版本未计研究交易成本；与新版本结果比较时请先统一引擎版本。
            </p>
          )}
          <div
            className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6"
            data-testid="metric-cards"
          >
            <MetricCard
              label="累计收益"
              value={formatPercent(summary.cumulative_return)}
              tone={summary.cumulative_return >= 0 ? "positive" : "danger"}
            />
            <MetricCard
              label="CAGR"
              value={formatPercent(summary.cagr)}
              help="复合年化增长率，按 365.25 天/年折算。"
              tone={summary.cagr >= 0 ? "positive" : "danger"}
            />
            <MetricCard
              label="年化波动率"
              value={formatNullablePercent(summary.annual_volatility)}
              help="有效估值日收益的样本标准差 × √252。"
            />
            <MetricCard
              label="最大回撤"
              value={formatPercent(summary.max_drawdown)}
              help="净值从峰值到谷底的最大跌幅（负数）。"
              tone="danger"
            />
            <MetricCard
              label="Sharpe"
              value={summary.sharpe != null ? summary.sharpe.toFixed(2) : "—"}
              help="（年化收益 − 无风险利率）/ 年化波动率；波动率不可用时为空。"
            />
            <MetricCard
              label="Calmar"
              value={summary.calmar != null ? summary.calmar.toFixed(2) : "—"}
              help="CAGR / |最大回撤|；回撤为 0 时为空。"
            />
            <MetricCard
              label="最好年份"
              value={
                summary.best_year
                  ? `${summary.best_year.year} (${formatPercent(summary.best_year.return)})`
                  : "—"
              }
            />
            <MetricCard
              label="最差年份"
              value={
                summary.worst_year
                  ? `${summary.worst_year.year} (${formatPercent(summary.worst_year.return)})`
                  : "—"
              }
            />
            <MetricCard
              label="最好月份"
              value={
                summary.best_month
                  ? `${summary.best_month.year}-${String(summary.best_month.month).padStart(2, "0")} (${formatPercent(summary.best_month.return)})`
                  : "—"
              }
            />
            <MetricCard
              label="最差月份"
              value={
                summary.worst_month
                  ? `${summary.worst_month.year}-${String(summary.worst_month.month).padStart(2, "0")} (${formatPercent(summary.worst_month.return)})`
                  : "—"
              }
            />
            <MetricCard
              label="正收益月份占比"
              value={formatNullablePercent(summary.positive_month_ratio)}
            />
            <MetricCard
              label="回撤持续期"
              value={`当前 ${summary.current_drawdown_days} 天 / 最长 ${summary.max_drawdown_duration_days} 天`}
              help="当前回撤已持续天数与历史最长回撤持续期（峰值到收复）。"
            />
            {run.engine_version === "research_backtest_v4" && (
              <>
                <MetricCard
                  label="累计单边换手"
                  value={formatPercent(summary.total_turnover ?? 0)}
                  help="每次再平衡的 0.5 × Σ|扣费前漂移权重 − 目标权重| 之和。"
                />
                <MetricCard
                  label="累计交易成本"
                  value={formatMoneyScaled(summary.total_transaction_cost_minor ?? 0, run.base_currency)}
                  help="按运行输入中的初始资金和交易费率逐次四舍五入后的实际费用合计。"
                />
                <MetricCard
                  label="交易成本拖累"
                  value={formatPercent(summary.transaction_cost_drag ?? 0)}
                  help="同一调仓路径下，不计费用终值与计费后终值的差额，占初始净值的比例。"
                  tone={(summary.transaction_cost_drag ?? 0) > 0 ? "danger" : undefined}
                />
              </>
            )}
            {summary.tail_risk && (
              <>
                <MetricCard
                  label={`${summary.tail_risk.horizon_days} 日 ${summary.tail_risk.confidence * 100}% VaR`}
                  value={formatPercent(summary.tail_risk.var_loss)}
                  help={`基于 ${summary.tail_risk.scenario_count} 个滚动场景的尾部分位损失。正数表示损失，负数表示该尾部边界仍为收益。`}
                  tone={summary.tail_risk.var_loss > 0 ? "danger" : "positive"}
                />
                <MetricCard
                  label={`${summary.tail_risk.horizon_days} 日 ${summary.tail_risk.confidence * 100}% CVaR`}
                  value={formatPercent(summary.tail_risk.cvar_loss)}
                  help={`历史最差尾部场景的平均损失，共 ${summary.tail_risk.scenario_count} 个场景，尾部计数 ${summary.tail_risk.tail_count}。`}
                  tone={summary.tail_risk.cvar_loss > 0 ? "danger" : "positive"}
                />
                <MetricCard
                  label={`最差 ${summary.tail_risk.horizon_days} 日损失`}
                  value={formatPercent(summary.tail_risk.worst_loss)}
                  help="该次回测冻结口径下观测到的最差持有期损失。"
                  tone={summary.tail_risk.worst_loss > 0 ? "danger" : "positive"}
                />
              </>
            )}
          </div>

          {!summary.tail_risk && (
            <p className="text-sm text-ink-muted">该历史回测版本未计算 CVaR</p>
          )}

          {summary.benchmark && (
            <p className="text-sm text-ink-muted">
              基准「{summary.benchmark.name}」：累计{" "}
              {formatPercent(summary.benchmark.cumulative_return)} · CAGR{" "}
              {formatPercent(summary.benchmark.cagr)} · 最大回撤{" "}
              {formatPercent(summary.benchmark.max_drawdown)}
            </p>
          )}

          {pointsQuery.isLoading ? (
            <LoadingState label="加载曲线数据…" />
          ) : (
            <RunCharts
              points={points}
              summary={summary}
              assetNames={assetNames}
              assetSeries={assetSeries}
              showAssetSeries={showAssetSeries}
              onToggleAssetSeries={setShowAssetSeries}
              assetSeriesLoading={showAssetSeries && assetDetails.loading}
              hasBenchmark={!!summary.benchmark}
            />
          )}

          <div className="grid gap-6 xl:grid-cols-2">
            <section className="rounded-lg border border-line bg-surface p-4">
              <h3 className="mb-3 text-sm font-semibold text-ink">年度收益表</h3>
              <RunAnnualTable years={run.years} weightDeviations={weightDeviations} />
            </section>
            <section className="rounded-lg border border-line bg-surface p-4">
              <h3 className="mb-3 text-sm font-semibold text-ink">月度收益热力图</h3>
              <RunMonthlyHeatmap months={run.months} />
            </section>
          </div>

          <section className="rounded-lg border border-line bg-surface p-4">
            <h3 className="mb-3 text-sm font-semibold text-ink">滚动指标</h3>
            <RunRollingCharts points={points} />
          </section>

          <div className="grid gap-6 xl:grid-cols-2">
            <section className="rounded-lg border border-line bg-surface p-4">
              <h3 className="mb-3 text-sm font-semibold text-ink">资产贡献</h3>
              <RunContributions summary={summary} />
            </section>
            <section className="rounded-lg border border-line bg-surface p-4">
              <h3 className="mb-3 text-sm font-semibold text-ink">相关性矩阵</h3>
              <RunCorrelationMatrix summary={summary} />
            </section>
          </div>

          {run.data_quality && (
            <section className="rounded-lg border border-line bg-surface p-4">
              <h3 className="mb-3 text-sm font-semibold text-ink">数据质量</h3>
              <RunDataQuality quality={run.data_quality} sources={snapAssets} />
            </section>
          )}

          <section className="rounded-lg border border-line bg-surface p-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold text-ink">运行输入快照</h3>
              <button
                type="button"
                onClick={() => setShowSnapshot(!showSnapshot)}
                className="text-xs text-ink-muted underline-offset-2 hover:text-ink hover:underline"
                data-testid="toggle-snapshot"
              >
                {showSnapshot ? "收起" : "展开"}
              </button>
            </div>
            <dl className="mt-2 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
              <div>
                <dt className="text-ink-muted">engine</dt>
                <dd className="font-mono-numeric text-ink">{run.engine_version}</dd>
              </div>
              <div>
                <dt className="text-ink-muted">input hash</dt>
                <dd className="truncate font-mono-numeric text-ink" title={run.input_hash}>
                  {run.input_hash.slice(0, 16)}…
                </dd>
              </div>
              <div>
                <dt className="text-ink-muted">source hash</dt>
                <dd className="truncate font-mono-numeric text-ink" title={run.source_hash}>
                  {run.source_hash.slice(0, 16)}…
                </dd>
              </div>
              <div>
                <dt className="text-ink-muted">完成时间</dt>
                <dd className="text-ink">{formatDateTimeFromMs(run.completed_at)}</dd>
              </div>
            </dl>
            {showSnapshot && (
              <pre className="mt-3 max-h-96 overflow-auto rounded-md bg-surface-muted p-3 text-xs">
                {JSON.stringify(run.input_snapshot, null, 2)}
              </pre>
            )}
          </section>
        </div>
      )}

      <RunCompareDialog
        open={compareOpen}
        onClose={() => setCompareOpen(false)}
        collectionId={collectionId}
        baseRun={run}
      />
    </div>
  );
}
