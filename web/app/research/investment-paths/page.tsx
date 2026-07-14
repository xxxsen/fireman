"use client";

import Link from "next/link";
import { Suspense, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import {
  createInvestmentPathRun,
  investmentPathReadiness,
  listInvestmentPathRuns,
  type InvestmentPathMode,
  type InvestmentPathRequest,
} from "@/lib/api/investment-paths";
import {
  getMarketAssetDetail,
  syncMarketAssetHistory,
  type MarketAsset,
  type WorkerTask,
} from "@/lib/api/market-assets";
import { isTaskActive } from "@/lib/api/tasks";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import { queryErrorMessage } from "@/lib/query-error";
import { formatDateTimeFromMs, instrumentTypeLabel } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { LoadingState } from "@/components/ui/LoadingState";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { MarketAssetPickerDialog } from "@/components/plans/MarketAssetPickerDialog";

const fieldClass = "mt-1 min-h-10 w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink";
const NO_EXCLUDED_ASSETS = new Set<string>();

export default function InvestmentPathsPage() {
  return <Suspense fallback={<LoadingState label="加载投入路径实验…" />}><InvestmentPathsContent /></Suspense>;
}

function InvestmentPathsContent() {
  const router = useRouter();
  const search = useSearchParams();
  const [mode, setMode] = useState<InvestmentPathMode>("income_dca");
  const [assetKey, setAssetKey] = useState(search.get("asset_key") ?? "");
  const [assetPickerOpen, setAssetPickerOpen] = useState(false);
  const [manualHistoryTask, setManualHistoryTask] = useState<WorkerTask | null>(null);
  const [evaluationStart, setEvaluationStart] = useState("2010-01-01");
  const [evaluationEnd, setEvaluationEnd] = useState("2025-12-31");
  const [horizonMonths, setHorizonMonths] = useState(60);
  const [primaryStart, setPrimaryStart] = useState("");
  const [monthlyDay, setMonthlyDay] = useState(15);
  const [costPercent, setCostPercent] = useState(0.1);
  const [initialInvestment, setInitialInvestment] = useState(0);
  const [monthlyContribution, setMonthlyContribution] = useState(10000);
  const [initialCapital, setInitialCapital] = useState(1000000);
  const [phaseMonths, setPhaseMonths] = useState("6,12,24");
  const [thresholdEnabled, setThresholdEnabled] = useState(false);
  const [targetWeight, setTargetWeight] = useState(60);
  const [threshold, setThreshold] = useState(5);
  const [readinessIdentity, setReadinessIdentity] = useState("");

  const assetDetailQuery = useQuery({
    queryKey: ["market-asset-detail", assetKey],
    queryFn: () => getMarketAssetDetail(assetKey),
    enabled: assetKey !== "",
  });
  const runsQuery = useQuery({
    queryKey: ["research", "investment-path-runs"],
    queryFn: () => listInvestmentPathRuns(20),
    refetchInterval: (query) => query.state.data?.runs.some((run) => isTaskActive(run.task.status)) ? 2000 : false,
  });
  const selected = assetDetailQuery.data?.asset;
  const history = assetDetailQuery.data?.history;
  const serverHistoryTask = history?.task ?? null;
  const trackedHistoryTaskID =
    serverHistoryTask && isTaskActive(serverHistoryTask.status)
      ? serverHistoryTask.id
      : manualHistoryTask?.id;
  const refreshAssetHistoryState = () => {
    setManualHistoryTask(null);
    setReadinessIdentity("");
    void assetDetailQuery.refetch();
  };
  const historyTaskState = useTaskStatus(trackedHistoryTaskID, {
    initialTask:
      serverHistoryTask?.id === trackedHistoryTaskID
        ? serverHistoryTask
        : manualHistoryTask?.id === trackedHistoryTaskID
          ? manualHistoryTask
          : undefined,
    onComplete: refreshAssetHistoryState,
    onFailed: refreshAssetHistoryState,
    onCanceled: refreshAssetHistoryState,
  });
  const historyTask = historyTaskState.task ?? serverHistoryTask ?? manualHistoryTask;
  const historyTaskActive = isTaskActive(historyTask?.status);
  const historyReady = (history?.point_count ?? 0) > 0 && !historyTaskActive;

  const buildRequest = (): InvestmentPathRequest => {
    const common = {
      mode,
      asset: {
        asset_key: assetKey,
        adjust_policy: history?.adjust_policy ?? "",
        point_type: history?.point_type ?? "",
      },
      base_currency: "CNY",
      evaluation_start: evaluationStart,
      evaluation_end: evaluationEnd,
      horizon_months: horizonMonths,
      ...(primaryStart ? { primary_start: primaryStart } : {}),
      monthly_day: monthlyDay,
      transaction_cost_rate: costPercent / 100,
    };
    if (mode === "income_dca") {
      return { ...common, income_dca: { initial_investment_minor: Math.round(initialInvestment * 100), monthly_contribution_minor: Math.round(monthlyContribution * 100) } };
    }
    const phases = phaseMonths.split(",").map((value) => Number(value.trim())).filter(Number.isFinite);
    return {
      ...common,
      existing_capital: {
        initial_capital_minor: Math.round(initialCapital * 100),
        phase_in_months: phases,
        threshold_comparison: { enabled: thresholdEnabled, target_asset_weight: targetWeight / 100, rebalance_threshold: threshold / 100 },
      },
    };
  };
  const currentIdentity = JSON.stringify(buildRequest());
  const readinessMutation = useMutation({
    mutationFn: investmentPathReadiness,
    onSuccess: () => setReadinessIdentity(currentIdentity),
  });
  const createMutation = useMutation({
    mutationFn: (request: InvestmentPathRequest) => createInvestmentPathRun({ ...request, idempotency_key: crypto.randomUUID() }),
    onSuccess: ({ run }) => router.push(`/research/investment-paths/runs/${run.id}`),
  });
  const historySyncMutation = useMutation({
    mutationFn: () =>
      syncMarketAssetHistory({
        asset_key: assetKey,
        adjust_policy: history?.adjust_policy || undefined,
        point_type: history?.point_type || undefined,
        mode: "default_refresh",
      }),
    onSuccess: ({ task }) => {
      setManualHistoryTask(task);
      setReadinessIdentity("");
      void assetDetailQuery.refetch();
    },
  });
  const readiness = readinessMutation.data;
  const creationReady = readiness?.ready && readinessIdentity === currentIdentity;

  return (
    <div className="content-enter space-y-6">
      <PageHeader title="单资产投入路径实验" description="比较工资型定投，或比较一笔已存在资金的一次性与分批入场。结果只解释冻结历史，不生成资产或参数推荐。" />

      <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
        <h2 className="text-base font-semibold text-ink">1. 资金何时存在</h2>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {(["income_dca", "existing_capital"] as const).map((value) => (
            <button key={value} type="button" onClick={() => { setMode(value); setReadinessIdentity(""); }} className={`rounded-lg border p-4 text-left ${mode === value ? "border-brand bg-brand/5" : "border-line"}`}>
              <span className="font-medium text-ink">{value === "income_dca" ? "工资结余逐月产生" : "资金第一天已经存在"}</span>
              <span className="mt-1 block text-xs text-ink-muted">{value === "income_dca" ? "未来工资不会被提前当成可投资本金，只与同现金流的零收益现金比较。" : "全部本金从起点计入账户，尚未投入部分留作零收益现金。"}</span>
            </button>
          ))}
        </div>
      </section>

      <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
        <h2 className="text-base font-semibold text-ink">2. 资产与历史窗口</h2>
        <div className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div className="sm:col-span-2 lg:col-span-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-sm text-ink-muted">资产</span>
              <Button
                variant="secondary"
                onClick={() => setAssetPickerOpen(true)}
                data-testid="choose-investment-path-asset"
              >
                {assetKey ? "更换资产" : "选择资产"}
              </Button>
            </div>
            {!assetKey && (
              <p className="mt-2 rounded-md border border-dashed border-line px-3 py-4 text-sm text-ink-muted">
                从资产目录选择一个非现金资产；没有本地历史也可以选择，随后在这里拉取。
              </p>
            )}
            {assetKey && assetDetailQuery.isLoading && <LoadingState label="加载资产状态…" />}
            {assetKey && assetDetailQuery.isError && (
              <div className="mt-2">
                <Alert variant="danger" title="无法加载资产状态">
                  {queryErrorMessage(assetDetailQuery.error)}
                </Alert>
                <Button variant="secondary" className="mt-2" onClick={() => void assetDetailQuery.refetch()}>
                  重试
                </Button>
              </div>
            )}
            {selected && history && (
              <div className="mt-2 rounded-md border border-line bg-surface-muted/50 p-3" data-testid="selected-investment-path-asset">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p className="font-medium text-ink">{selected.name} · {selected.symbol}</p>
                    <p className="mt-1 text-xs text-ink-muted">
                      {instrumentTypeLabel(selected.instrument_type)} · {selected.market} · {selected.currency}
                    </p>
                    <p className="mt-1 text-xs text-ink-muted">
                      {historyReady
                        ? `历史已就绪：${history.point_count} 个点，数据截至 ${history.data_as_of || "—"}`
                        : historyTaskActive
                          ? "历史数据拉取中，完成后会自动刷新。"
                          : "尚无可用本地历史，拉取完成后才能检查实验。"}
                    </p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    {historyTask && (
                      <TaskStatusBadge
                        status={historyTask.status}
                        labels={{
                          pending: "等待拉取",
                          running: "拉取中",
                          pre_complete: "保存中",
                          complete: "拉取完成",
                          failed: "拉取失败",
                          canceled: "已取消",
                        }}
                      />
                    )}
                    <Button
                      variant="secondary"
                      disabled={historyTaskActive || historySyncMutation.isPending}
                      pending={historySyncMutation.isPending}
                      onClick={() => historySyncMutation.mutate()}
                      data-testid="sync-investment-path-history"
                    >
                      {historyReady ? "刷新历史数据" : "拉取历史数据"}
                    </Button>
                    <Link
                      href={`/assets/market/${encodeURIComponent(assetKey)}`}
                      target="_blank"
                      className="text-xs text-brand underline-offset-2 hover:underline"
                    >
                      查看资产详情
                    </Link>
                  </div>
                </div>
                {(historySyncMutation.isError || historyTaskState.pollError || historyTask?.status === "failed") && (
                  <div className="mt-3">
                    <Alert variant="danger" title="历史数据拉取未完成">
                      {historySyncMutation.isError
                        ? queryErrorMessage(historySyncMutation.error)
                        : historyTask?.status === "failed"
                          ? historyTask.error_message || "历史数据拉取失败，可以重试。"
                          : historyTaskState.pollError}
                    </Alert>
                  </div>
                )}
              </div>
            )}
          </div>
            <label className="text-sm text-ink-muted">评估起点<input className={fieldClass} type="date" value={evaluationStart} onChange={(e) => setEvaluationStart(e.target.value)} /></label>
            <label className="text-sm text-ink-muted">评估终点<input className={fieldClass} type="date" value={evaluationEnd} onChange={(e) => setEvaluationEnd(e.target.value)} /></label>
            <label className="text-sm text-ink-muted">每个窗口（月）<input className={fieldClass} type="number" min={12} max={360} value={horizonMonths} onChange={(e) => setHorizonMonths(Number(e.target.value))} /></label>
            <label className="text-sm text-ink-muted">月内计划日<input className={fieldClass} type="number" min={1} max={28} value={monthlyDay} onChange={(e) => setMonthlyDay(Number(e.target.value))} /></label>
            <label className="text-sm text-ink-muted">主窗口起点（可空）<input className={fieldClass} type="date" value={primaryStart} onChange={(e) => setPrimaryStart(e.target.value)} /></label>
            <label className="text-sm text-ink-muted">单笔交易成本（%）<input className={fieldClass} type="number" min={0} max={10} step={0.01} value={costPercent} onChange={(e) => setCostPercent(Number(e.target.value))} /></label>
        </div>
        <MarketAssetPickerDialog
          open={assetPickerOpen}
          onClose={() => setAssetPickerOpen(false)}
          allowCash={false}
          excludeAssetKeys={NO_EXCLUDED_ASSETS}
          title="选择单资产实验标的"
          inputTestId="investment-path-asset-search"
          resultsTestId="investment-path-asset-results"
          onSelect={(asset: MarketAsset) => {
            setAssetKey(asset.asset_key);
            setAssetPickerOpen(false);
            setManualHistoryTask(null);
            setReadinessIdentity("");
            readinessMutation.reset();
            createMutation.reset();
            historySyncMutation.reset();
          }}
        />
      </section>

      <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
        <h2 className="text-base font-semibold text-ink">3. 投入与对照</h2>
        {mode === "income_dca" ? (
          <div className="mt-3 grid gap-4 sm:grid-cols-2">
            <label className="text-sm text-ink-muted">起点额外本金（元）<input className={fieldClass} type="number" min={0} value={initialInvestment} onChange={(e) => setInitialInvestment(Number(e.target.value))} /></label>
            <label className="text-sm text-ink-muted">每月固定投入（元）<input className={fieldClass} type="number" min={0.01} value={monthlyContribution} onChange={(e) => setMonthlyContribution(Number(e.target.value))} /></label>
          </div>
        ) : (
          <div className="mt-3 grid gap-4 sm:grid-cols-2">
            <label className="text-sm text-ink-muted">起点全部本金（元）<input className={fieldClass} type="number" min={0.01} value={initialCapital} onChange={(e) => setInitialCapital(Number(e.target.value))} /></label>
            <label className="text-sm text-ink-muted">分批月数（最多 3 个，逗号分隔）<input className={fieldClass} value={phaseMonths} onChange={(e) => setPhaseMonths(e.target.value)} /></label>
            <label className="flex items-center gap-2 text-sm text-ink"><input type="checkbox" checked={thresholdEnabled} onChange={(e) => setThresholdEnabled(e.target.checked)} />同时比较静态资产/现金与阈值再平衡</label>
            {thresholdEnabled && <div className="grid grid-cols-2 gap-3"><label className="text-sm text-ink-muted">资产比例 %<input className={fieldClass} type="number" min={5} max={95} value={targetWeight} onChange={(e) => setTargetWeight(Number(e.target.value))} /></label><label className="text-sm text-ink-muted">偏离阈值 %<input className={fieldClass} type="number" min={0.01} max={50} value={threshold} onChange={(e) => setThreshold(Number(e.target.value))} /></label></div>}
          </div>
        )}
      </section>

      <section className="rounded-lg border border-line bg-surface p-4 sm:p-6">
        <div className="flex flex-wrap gap-3"><Button disabled={!historyReady || readinessMutation.isPending} pending={readinessMutation.isPending} onClick={() => readinessMutation.mutate(buildRequest())}>检查数据与计算预算</Button><Button variant="secondary" disabled={!creationReady || createMutation.isPending} pending={createMutation.isPending} onClick={() => createMutation.mutate(buildRequest())}>创建后台实验</Button></div>
        {(readinessMutation.isError || createMutation.isError) && <div className="mt-4"><Alert variant="danger" title="无法继续">{queryErrorMessage(readinessMutation.error ?? createMutation.error)}</Alert></div>}
        {readiness && <div className="mt-4"><Alert variant={readiness.ready ? "success" : "danger"} title={readiness.ready ? "可以运行" : "尚未就绪"}>{readiness.ready && readiness.resolved ? `${readiness.resolved.window_starts.length} 个滚动起点，${readiness.resolved.strategy_keys.length} 条策略，主窗口 ${readiness.resolved.primary_start}。` : readiness.issues.map((item) => item.message).join("；")}</Alert>{readiness.warnings.length > 0 && <p className="mt-2 text-xs text-warning">注意：{readiness.warnings.map((item) => item.message).join("；")}</p>}</div>}
      </section>

      <section>
        <h2 className="mb-3 text-base font-semibold text-ink">历史实验</h2>
        <div className="grid gap-3 md:grid-cols-2">
          {(runsQuery.data?.runs ?? []).map((run) => <Link key={run.id} href={`/research/investment-paths/runs/${run.id}`} className="rounded-lg border border-line bg-surface p-4 hover:border-brand/50"><div className="flex items-center justify-between gap-3"><span className="font-medium text-ink">{run.mode === "income_dca" ? "工资型定投" : "存量资金入场"}</span><TaskStatusBadge status={run.task.status} labels={{ pending: "等待计算", running: "计算中", pre_complete: "保存中", complete: "已完成", failed: "失败" }} /></div><p className="mt-2 text-xs text-ink-muted">{run.primary_start} ~ {run.primary_end} · {run.horizon_months} 个月 · {formatDateTimeFromMs(run.created_at)}</p></Link>)}
          {runsQuery.data?.runs.length === 0 && <p className="text-sm text-ink-muted">还没有投入路径实验。</p>}
        </div>
      </section>
    </div>
  );
}
