"use client";

import { memo, useMemo, useState, type ReactNode } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PageHeader } from "@/components/ui/PageHeader";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { CalculationExplanation } from "@/components/ui/CalculationExplanation";
import { HelpLabel } from "@/components/ui/HelpLabel";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { useActiveTaskRestore } from "@/hooks/useActiveTaskRestore";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import {
  applyImprovementProposal,
  createImprovementRun,
  getImprovementReadiness,
  getImprovementRun,
  listImprovementRuns,
  previewImprovementProposal,
} from "@/lib/api/improvements";
import { getPlan } from "@/lib/api/plans";
import { createSimulation, getSimulation } from "@/lib/api/simulations";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import {
  activeTaskConflictRef,
  isTaskActive,
} from "@/lib/api/tasks";
import { formatDateTimeFromMs, formatMoney, formatPercent } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type {
  ImprovementConfig,
  ImprovementPreview,
  ImprovementProposal,
  ImprovementResult,
  ImprovementRun,
} from "@/types/api";

type MoneyLever = { enabled: boolean; maximum: number; step: number };

const RECIPE_LABELS: Record<string, string> = {
  pure_retirement_delay: "仅推迟 FIRE",
  pure_savings_increase: "仅增加储蓄",
  pure_spending_reduction: "仅降低支出",
  pure_retirement_income_increase: "仅增加稳定收入",
  balanced: "平衡方案",
};

function parseTarget(draft: string): number | null {
  if (!/^\d{0,2}(?:\.\d{0,2})?$/.test(draft)) return null;
  const value = Number(draft);
  if (!Number.isFinite(value) || value < 50 || value > 99) return null;
  return value / 100;
}

function LeverToggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: ReactNode;
}) {
  return (
    <label className="flex min-h-10 items-center gap-2 text-sm font-medium text-ink">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="h-4 w-4 accent-brand"
      />
      {label}
    </label>
  );
}

function MoneyLeverFields({
  value,
  onChange,
  maximumLabel,
}: {
  value: MoneyLever;
  onChange: (value: MoneyLever) => void;
  maximumLabel: string;
}) {
  if (!value.enabled) return null;
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <MoneyInput
        plain
        label={<HelpLabel label={maximumLabel} termKey="search_domain" />}
        valueMinor={value.maximum}
        onChange={(maximum) => onChange({ ...value, maximum })}
      />
      <MoneyInput
        plain
        label={<HelpLabel label="每档金额" termKey="discrete_search_step" />}
        valueMinor={value.step}
        onChange={(step) => onChange({ ...value, step })}
      />
    </div>
  );
}

function proposalStatus(proposal: ImprovementProposal, target: number) {
  if (proposal.success_wilson_low >= target) return "稳健达到";
  if (proposal.success_probability >= target) return "估计达到，下界未达到";
  return "未达到";
}

const ImprovementResults = memo(function ImprovementResults({
  run,
  result,
  mode,
  onModeChange,
  onPreview,
}: {
  run: ImprovementRun;
  result: ImprovementResult;
  mode: "pure" | "balanced";
  onModeChange: (mode: "pure" | "balanced") => void;
  onPreview: (proposal: ImprovementProposal) => void;
}) {
  const pureProposals = result.proposals.filter((proposal) => proposal.recipe !== "balanced");
  const balancedProposals = result.proposals.filter((proposal) => proposal.recipe === "balanced");
  const visibleMode = mode === "pure" && pureProposals.length === 0 && balancedProposals.length > 0
    ? "balanced"
    : mode;
  const proposals = visibleMode === "balanced" ? balancedProposals : pureProposals;
  const nearTarget = result.evaluations.filter(
    (evaluation) =>
      evaluation.success_probability >= result.target_probability &&
      evaluation.success_wilson_low < result.target_probability,
  );
  return (
    <section className="space-y-4 border-t border-line pt-6" aria-label="改善结果">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-ink">分析结果</h2>
          <p className="mt-1 text-sm text-ink-muted">
            基准 {formatPercent(result.baseline.success_probability)}（95% 区间 {formatPercent(result.baseline.success_wilson_low)} - {formatPercent(result.baseline.success_wilson_high)}） · 目标下界 {formatPercent(result.target_probability)}
          </p>
        </div>
        <span className="inline-flex items-center text-xs text-ink-muted">
          来源 {run.source_simulation_run_id}
          <MetricHelp termKey="common_random_numbers" />
        </span>
      </div>

      <CalculationExplanation
        summary="本次结果在来源模拟的冻结输入上，搜索四类现金流调整能否让 Wilson 95% 下界达到目标。"
        answer="在你允许的调整范围内，需要怎样改变退休年龄、储蓄、退休支出或稳定收入，才能更稳健地达到目标成功率。"
        changed="仅改变已启用的四个现金流杠杆，并按设置的上限和离散档位生成候选。"
        fixed="持仓、配置权重、收益与风险假设、市场快照、来源模拟 seed 和逐路径随机样本保持不变。"
        data={`来源模拟 ${run.source_simulation_run_id}；每个候选使用 ${result.baseline.runs.toLocaleString()} 条配对路径。`}
        criterion={`候选的 Wilson 95% 下界达到 ${formatPercent(result.target_probability)} 才算稳健达标；估计成功率达到但下界未达到的候选只列为接近目标。`}
        uncertainty="这是有限路径、离散档位和当前搜索约束下的结果；域内无解不代表所有可能调整都无解，也不保证真实未来结果。"
        nextStep="查看候选的具体改变，应用前确认差异；应用只修改计划参数，随后运行正式模拟验证。"
        audit={`算法 ${result.algorithm_version}；来源引擎 ${run.source_engine_version}；输入 ${run.input_hash}`}
      />

      {run.result_stale && (
        <Alert variant="warning">计划或市场输入已变化，需要重新分析后才能应用。</Alert>
      )}

      {!result.target_reached && result.best_attainable && (
        <Alert variant="warning" title="当前约束内未达到目标">
          <p>
            最佳可达下界为 {formatPercent(result.best_attainable.success_wilson_low)}，估计成功率为 {formatPercent(result.best_attainable.success_probability)}。
          </p>
          <p className="mt-1 text-xs">
            对应调整：推迟 FIRE {result.best_attainable.delay_years} 年，年储蓄 +{formatMoney(result.best_attainable.savings_increase_minor)}，年支出 -{formatMoney(result.best_attainable.spending_reduction_minor)}，稳定年收入 +{formatMoney(result.best_attainable.retirement_income_increase_minor)}。该结果已搜索到当前设置的调整边界。
          </p>
        </Alert>
      )}

      {result.target_reached && (
        <div className="flex items-center gap-1">
        <div className="inline-flex rounded-md border border-line p-0.5" role="group" aria-label="方案类型">
          <button
            type="button"
            aria-pressed={visibleMode === "pure"}
            onClick={() => onModeChange("pure")}
            className={visibleMode === "pure" ? "rounded bg-brand px-3 py-1.5 text-sm text-surface" : "rounded px-3 py-1.5 text-sm text-ink-muted"}
          >
            单杠杆
          </button>
          <button
            type="button"
            aria-pressed={visibleMode === "balanced"}
            onClick={() => onModeChange("balanced")}
            className={visibleMode === "balanced" ? "rounded bg-brand px-3 py-1.5 text-sm text-surface" : "rounded px-3 py-1.5 text-sm text-ink-muted"}
          >
            平衡方案
          </button>
        </div>
        <MetricHelp termKey="improvement_recipe" />
        </div>
      )}

      {proposals.length > 0 && (
        <>
          <div className="hidden overflow-x-auto md:block">
            <table className="min-w-[980px] w-full text-left text-sm">
              <thead className="border-b border-line text-xs text-ink-muted">
                <tr>
                  <th className="sticky left-0 bg-surface px-2 py-2">方案</th>
                  <th className="px-2 py-2">FIRE 年龄</th>
                  <th className="px-2 py-2">年储蓄变化</th>
                  <th className="px-2 py-2">年支出变化</th>
                  <th className="px-2 py-2">稳定收入变化</th>
                  <th className="px-2 py-2"><HelpLabel label="成功率区间" termKey="wilson_interval" /></th>
                  <th className="px-2 py-2"><HelpLabel label="路径改善" termKey="paired_path_changes" /></th>
                  <th className="sticky right-0 bg-surface px-2 py-2">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {proposals.map((proposal) => (
                  <tr key={proposal.id}>
                    <td className="sticky left-0 bg-surface px-2 py-3 font-medium">{RECIPE_LABELS[proposal.recipe] ?? proposal.recipe}</td>
                    <td className="px-2 py-3 tabular-nums">{proposal.result_retirement_age}</td>
                    <td className="px-2 py-3 tabular-nums">+{formatMoney(proposal.savings_increase_minor)}</td>
                    <td className="px-2 py-3 tabular-nums">-{formatMoney(proposal.spending_reduction_minor)}</td>
                    <td className="px-2 py-3 tabular-nums">+{formatMoney(proposal.retirement_income_increase_minor)}</td>
                    <td className="px-2 py-3 tabular-nums">
                      {formatPercent(proposal.success_probability)} · {formatPercent(proposal.success_wilson_low)} - {formatPercent(proposal.success_wilson_high)}
                      <span className="mt-0.5 block text-xs text-positive">{proposalStatus(proposal, result.target_probability)}</span>
                    </td>
                    <td className="px-2 py-3 tabular-nums">+{proposal.improved_path_count} / -{proposal.regressed_path_count}</td>
                    <td className="sticky right-0 bg-surface px-2 py-3">
                      <Button variant="secondary" className="px-2 py-1" disabled={run.result_stale || Boolean(run.application)} onClick={() => onPreview(proposal)}>
                        查看并应用
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="space-y-3 md:hidden">
            {proposals.map((proposal) => (
              <div key={proposal.id} className="border-b border-line pb-3">
                <div className="flex items-center justify-between gap-2">
                  <h3 className="text-sm font-medium text-ink">{RECIPE_LABELS[proposal.recipe] ?? proposal.recipe}</h3>
                  <Button variant="secondary" className="px-2 py-1" disabled={run.result_stale || Boolean(run.application)} onClick={() => onPreview(proposal)}>查看并应用</Button>
                </div>
                <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                  <dt className="text-ink-muted">FIRE 年龄</dt><dd>{proposal.result_retirement_age}</dd>
                  <dt className="text-ink-muted">储蓄 / 支出</dt><dd>+{formatMoney(proposal.savings_increase_minor)} / -{formatMoney(proposal.spending_reduction_minor)}</dd>
                  <dt className="text-ink-muted"><HelpLabel label="95% 区间" termKey="wilson_interval" /></dt><dd>{formatPercent(proposal.success_wilson_low)} - {formatPercent(proposal.success_wilson_high)}</dd>
                  <dt className="text-ink-muted"><HelpLabel label="路径改善" termKey="paired_path_changes" /></dt><dd>+{proposal.improved_path_count} / -{proposal.regressed_path_count}</dd>
                </dl>
              </div>
            ))}
          </div>
        </>
      )}

      {nearTarget.length > 0 && (
        <Alert variant="info" title="接近目标">
          有 {nearTarget.length} 个搜索点的估计成功率达到目标，但 Wilson 下界尚未达到，未列为可行方案。
        </Alert>
      )}

      <details className="text-sm">
        <summary className="cursor-pointer font-medium text-ink"><HelpLabel label="搜索边界" termKey="search_boundary" /></summary>
        <div className="mt-2 overflow-x-auto">
          <table className="min-w-[720px] w-full text-left text-xs">
            <thead><tr className="text-ink-muted"><th className="p-2">调整</th><th className="p-2"><HelpLabel label="成功率" termKey="fire_success_rate" /></th><th className="p-2"><HelpLabel label="Wilson 下界" termKey="wilson_lower_bound" /></th><th className="p-2">判定</th></tr></thead>
            <tbody className="divide-y divide-line">
              {result.evaluations.map((evaluation) => (
                <tr key={`${evaluation.adjustments.delay_years}-${evaluation.adjustments.savings_increase_minor}-${evaluation.adjustments.spending_reduction_minor}-${evaluation.adjustments.retirement_income_increase_minor}`}>
                  <td className="p-2">延迟 {evaluation.adjustments.delay_years} 年 · 储蓄 +{formatMoney(evaluation.adjustments.savings_increase_minor)} · 支出 -{formatMoney(evaluation.adjustments.spending_reduction_minor)} · 收入 +{formatMoney(evaluation.adjustments.retirement_income_increase_minor)}</td>
                  <td className="p-2">{formatPercent(evaluation.success_probability)}</td>
                  <td className="p-2">{formatPercent(evaluation.success_wilson_low)}</td>
                  <td className="p-2">{evaluation.meets_target ? "稳健达到" : evaluation.success_probability >= result.target_probability ? "估计达到" : "未达到"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </section>
  );
});

function PreviewDialog({
  preview,
  applying,
  error,
  onClose,
  onApply,
}: {
  preview: ImprovementPreview | null;
  applying: boolean;
  error: string | null;
  onClose: () => void;
  onApply: () => void;
}) {
  return (
    <Dialog
      open={Boolean(preview)}
      onClose={onClose}
      title="查看并应用改善方案"
      className="max-w-2xl"
      footer={<div className="flex justify-end gap-2"><Button variant="ghost" onClick={onClose}>取消</Button><Button pending={applying} onClick={onApply}>应用到计划</Button></div>}
    >
      {preview && (
        <div className="space-y-4">
          <table className="w-full text-left text-sm">
            <thead><tr className="border-b border-line text-ink-muted"><th className="py-2">参数</th><th className="py-2">应用前</th><th className="py-2">应用后</th></tr></thead>
            <tbody className="divide-y divide-line">
              <tr><td className="py-2">FIRE 年龄</td><td>{preview.before.retirement_age}</td><td>{preview.after.retirement_age}</td></tr>
              <tr><td className="py-2">年净储蓄</td><td>{formatMoney(preview.before.annual_savings_minor)}</td><td>{formatMoney(preview.after.annual_savings_minor)}</td></tr>
              <tr><td className="py-2">退休年支出</td><td>{formatMoney(preview.before.annual_spending_minor)}</td><td>{formatMoney(preview.after.annual_spending_minor)}</td></tr>
              <tr><td className="py-2">退休稳定年收入</td><td>{formatMoney(preview.before.annual_retirement_income_minor)}</td><td>{formatMoney(preview.after.annual_retirement_income_minor)}</td></tr>
            </tbody>
          </table>
          <p className="text-sm text-ink-muted">成功率 {formatPercent(preview.success_probability)}，95% 区间 {formatPercent(preview.success_wilson_low)} - {formatPercent(preview.success_wilson_high)}。</p>
          {preview.retirement_income_delayed && <Alert variant="warning">推迟 FIRE 会同时推迟退休稳定收入的开始时间。</Alert>}
          <p className="text-xs leading-relaxed text-ink-muted">只会修改表格中的四项计划参数。保持不变：{preview.unchanged.join("、")}。这不是交易操作；应用后需要重新运行正式模拟。预览有效至 {formatDateTimeFromMs(preview.preview_expires_at)}。</p>
          {error && <Alert variant="danger">{error}</Alert>}
        </div>
      )}
    </Dialog>
  );
}

export function ImprovementPage({ planId }: { planId: string }) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const requestedSimulationRun = searchParams.get("simulation_run_id") ?? undefined;
  const [selectedRunID, setSelectedRunID] = useState<string | null>(null);
  const [trackedTaskID, setTrackedTaskID] = useState<string | null>(null);
  const [targetDraft, setTargetDraft] = useState("90");
  const [delayEnabled, setDelayEnabled] = useState(true);
  const [maxDelay, setMaxDelay] = useState(3);
  const [savings, setSavings] = useState<MoneyLever>({ enabled: true, maximum: 50_000_00, step: 5_000_00 });
  const [spending, setSpending] = useState<MoneyLever>({ enabled: true, maximum: 24_000_00, step: 3_000_00 });
  const [income, setIncome] = useState<MoneyLever>({ enabled: false, maximum: 12_000_00, step: 3_000_00 });
  const [mode, setMode] = useState<"pure" | "balanced">("pure");
  const [preview, setPreview] = useState<ImprovementPreview | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [applied, setApplied] = useState(false);

  const readinessQ = useQuery({
    queryKey: ["improvement-readiness", planId, requestedSimulationRun],
    queryFn: () => getImprovementReadiness(planId, requestedSimulationRun),
  });
  const runsQ = useQuery({
    queryKey: ["improvement-runs", planId],
    queryFn: () => listImprovementRuns(planId),
  });
  const planQ = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });

  const restoredActiveRun = runsQ.data?.runs.find((run) =>
    isTaskActive(run.status),
  );
  const restoredRun = restoredActiveRun ?? runsQ.data?.runs[0];
  const effectiveRunID = selectedRunID ?? restoredRun?.id ?? null;

  const detailQ = useQuery({
    queryKey: ["improvement-run", effectiveRunID],
    queryFn: () => getImprovementRun(effectiveRunID!),
    enabled: Boolean(effectiveRunID),
    refetchInterval: (query) =>
      isTaskActive(query.state.data?.status) ? 2_000 : false,
  });
  const detailTaskID = detailQ.data && isTaskActive(detailQ.data.status)
    ? detailQ.data.task_id
    : null;
  const taskRestore = useActiveTaskRestore({
    workerType: "go_worker",
    taskType: "fire_plan_improvement",
    scopeType: "plan",
    scopeId: planId,
    businessTaskId: restoredActiveRun?.task_id ?? detailTaskID,
    preferredTaskId: trackedTaskID,
  });
  const trackedTaskFallback =
    taskRestore.restoring || taskRestore.restoreError ? trackedTaskID : null;
  const activeTaskID = taskRestore.taskId ?? detailTaskID ?? trackedTaskFallback;
  const invalidateImprovement = () => {
    setTrackedTaskID(null);
    void qc.invalidateQueries({ queryKey: ["improvement-run", effectiveRunID] });
    void qc.invalidateQueries({ queryKey: ["improvement-runs", planId] });
    void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
  };
  const taskState = useTaskStatus(activeTaskID, {
    initialTask: taskRestore.task,
    onComplete: invalidateImprovement,
    onFailed: invalidateImprovement,
    onCanceled: invalidateImprovement,
  });

  const target = parseTarget(targetDraft);
  const maxAllowedDelay = readinessQ.data
    ? Math.min(10, readinessQ.data.current_parameters.end_age - readinessQ.data.current_parameters.retirement_age - 1)
    : 10;
  const effectiveMaxDelay = Math.min(maxDelay, Math.max(0, maxAllowedDelay));

  const configError = useMemo(() => {
    if (target === null) return "目标成功率必须在 50% 至 99% 之间";
    if (!delayEnabled && !savings.enabled && !spending.enabled && !income.enabled) return "至少启用一个可接受调整";
    if (delayEnabled && (effectiveMaxDelay < 1 || effectiveMaxDelay > maxAllowedDelay)) return "FIRE 延迟超出计划年龄范围";
    for (const [name, lever] of [["增加储蓄", savings], ["降低支出", spending], ["增加稳定收入", income]] as const) {
      if (!lever.enabled) continue;
      if (lever.maximum <= 0 || lever.step <= 0 || lever.step > lever.maximum) return `${name}的最大值和档位金额无效`;
      if (Math.ceil(lever.maximum / lever.step) > 100) return `${name}最多允许 100 档`;
    }
    if (spending.enabled && readinessQ.data && spending.maximum >= readinessQ.data.current_parameters.annual_spending_minor) return "退休年支出必须保持大于 0";
    return null;
  }, [target, delayEnabled, effectiveMaxDelay, maxAllowedDelay, savings, spending, income, readinessQ.data]);

  const createM = useMutation({
    mutationFn: async () => {
      if (!readinessQ.data?.source_run || target === null || configError) throw new Error(configError ?? "来源模拟不可用");
      const config: ImprovementConfig & { simulation_run_id: string } = {
        simulation_run_id: readinessQ.data.source_run.id,
        target_success_probability: target,
        retirement_delay: delayEnabled ? { max_delay_years: effectiveMaxDelay } : null,
        savings_increase: savings.enabled ? { max_increase_minor: savings.maximum, step_minor: savings.step } : null,
        spending_reduction: spending.enabled ? { max_reduction_minor: spending.maximum, step_minor: spending.step } : null,
        retirement_income_increase: income.enabled ? { max_increase_minor: income.maximum, step_minor: income.step } : null,
      };
      return createImprovementRun(planId, config);
    },
    onSuccess: (response) => {
      setSelectedRunID(response.run_id);
      setTrackedTaskID(response.task_id);
      setApplied(false);
      void qc.invalidateQueries({ queryKey: ["improvement-runs", planId] });
    },
    onError: (error) => {
      const conflict = activeTaskConflictRef(error);
      if (!conflict) return;
      setTrackedTaskID(conflict.taskId);
      if (conflict.resourceId) setSelectedRunID(conflict.resourceId);
      void qc.invalidateQueries({ queryKey: ["improvement-runs", planId] });
      void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
    },
  });

  const previewM = useMutation({
    mutationFn: (proposal: ImprovementProposal) => {
      if (!detailQ.data || !planQ.data) throw new Error("计划版本不可用");
      return previewImprovementProposal(detailQ.data.id, proposal.id, planQ.data.config_version);
    },
    onSuccess: (value) => { setPreview(value); setPreviewError(null); },
  });
  const applyM = useMutation({
    mutationFn: () => {
      if (!preview) throw new Error("预览不可用");
      return applyImprovementProposal(preview);
    },
    onSuccess: () => {
      setPreview(null);
      setApplied(true);
      for (const key of ["plan", "parameters", "dashboard", "simulations", "improvement-run", "improvement-runs"]) {
        void qc.invalidateQueries({ queryKey: [key] });
      }
    },
    onError: (error) => setPreviewError(queryErrorMessage(error)),
  });
  const verifyM = useMutation({
    mutationFn: async () => {
      if (!detailQ.data) throw new Error("改善结果不可用");
      const source = await getSimulation(detailQ.data.source_simulation_run_id);
      return createSimulation(planId, { runs: source.runs, seed: source.seed });
    },
    onSuccess: (response) => router.push(`/plans/${planId}/settings?section=simulation&task_id=${encodeURIComponent(response.task_id)}`),
  });

  if ((readinessQ.isLoading || planQ.isLoading) && !readinessQ.data) return <PageSkeleton label="加载 FIRE 计划改善器…" />;
  if ((readinessQ.isError || planQ.isError) && (!readinessQ.data || !planQ.data)) {
    return <ErrorState message="无法加载 FIRE 计划改善器。" technicalDetail={queryErrorMessage(readinessQ.error ?? planQ.error)} onRetry={() => { void readinessQ.refetch(); void planQ.refetch(); }} backHref={`/plans/${planId}/settings?section=simulation`} />;
  }
  if (!readinessQ.data || !planQ.data) return null;

  const source = readinessQ.data.source_run;
  const alreadyMeets = source && target !== null && source.success_wilson_low >= target;
  const activeRun = detailQ.data;
  const running = Boolean(activeTaskID) || isTaskActive(activeRun?.status);
  const restorationBlocked =
    runsQ.isPending || taskRestore.restoring || Boolean(taskRestore.restoreError);
  const createConflict = activeTaskConflictRef(createM.error);
  return (
    <div>
      <PageHeader backHref={`/plans/${planId}/settings?section=simulation`} backLabel="返回分析中心" title="FIRE 计划改善器" />
      <div className="space-y-6">
        <p className="flex items-center text-sm leading-relaxed text-ink-muted">
          只搜索你能接受的现金流调整，不改资产配置和收益假设。
          <MetricHelp termKey="improvement_search" />
        </p>
        <section className="border-b border-line pb-6">
          <h2 className="text-lg font-semibold text-ink">来源模拟</h2>
          {source ? (
            <dl className="mt-3 grid gap-3 text-sm sm:grid-cols-4">
              <div><dt className="text-ink-muted"><HelpLabel label="估计成功率" termKey="fire_success_rate" /></dt><dd className="mt-1 font-medium">{formatPercent(source.success_probability)}</dd></div>
              <div><dt className="text-ink-muted"><HelpLabel label="95% Wilson 区间" termKey="wilson_interval" /></dt><dd className="mt-1 font-medium">{formatPercent(source.success_wilson_low)} - {formatPercent(source.success_wilson_high)}</dd></div>
              <div><dt className="text-ink-muted"><HelpLabel label="模拟路径" termKey="simulation_runs" /></dt><dd className="mt-1 font-medium">{source.runs.toLocaleString()} 条</dd></div>
              <div><dt className="text-ink-muted">创建时间</dt><dd className="mt-1 font-medium">{formatDateTimeFromMs(source.created_at)}</dd></div>
            </dl>
          ) : (
            <Alert variant="warning">{readinessQ.data.blocking_reasons.map((reason) => reason.message).join("；") || "先运行当前计划模拟"}</Alert>
          )}
          {(runsQ.data?.runs.length ?? 0) > 1 && (
            <label className="mt-4 block max-w-md text-sm text-ink">
              历史分析
              <select
                aria-label="历史改善分析"
                className="input-base mt-1 w-full"
                value={effectiveRunID ?? ""}
                onChange={(event) => setSelectedRunID(event.target.value)}
              >
                {runsQ.data!.runs.map((item) => (
                  <option key={item.id} value={item.id}>
                    {formatDateTimeFromMs(item.created_at)} · {item.status}
                  </option>
                ))}
              </select>
            </label>
          )}
        </section>

        <section className="border-b border-line pb-6">
          <h2 className="text-lg font-semibold text-ink">目标</h2>
          <div className="mt-3 grid max-w-xl gap-3 sm:grid-cols-[160px_1fr] sm:items-end">
            <label className="block text-sm text-ink"><HelpLabel label="成功率下界" termKey="wilson_lower_bound" />
              <div className="mt-1 flex items-center gap-2"><input aria-label="目标成功率" className="input-base min-w-0" inputMode="decimal" value={targetDraft} onChange={(event) => { const next = event.target.value; if (/^\d{0,2}(?:\.\d{0,2})?$/.test(next)) setTargetDraft(next); }} /><span className="text-sm text-ink-muted">%</span></div>
            </label>
            <input aria-label="目标成功率滑块" type="range" min={50} max={99} step={0.5} value={target ? target * 100 : 90} onChange={(event) => setTargetDraft(Number(event.target.value).toFixed(1).replace(/\.0$/, ""))} className="mb-2 w-full accent-brand" />
          </div>
          {alreadyMeets && <Alert variant="success" className="mt-3">当前计划的成功率下界已经达到该目标。</Alert>}
        </section>

        <section className="border-b border-line pb-6">
          <h2 className="text-lg font-semibold text-ink">可接受调整</h2>
          <div className="mt-3 grid gap-5 lg:grid-cols-2">
            <div className="space-y-3">
              <LeverToggle checked={delayEnabled} onChange={setDelayEnabled} label={<HelpLabel label="推迟 FIRE" termKey="fire_delay" />} />
              {delayEnabled && <label className="block text-sm text-ink"><HelpLabel label="最多推迟" termKey="search_domain" />
                <select aria-label="最多推迟年数" value={effectiveMaxDelay} onChange={(event) => setMaxDelay(Number(event.target.value))} className="input-base mt-1">
                  {Array.from({ length: Math.max(0, maxAllowedDelay) }, (_, index) => index + 1).map((year) => <option key={year} value={year}>{year} 年</option>)}
                </select>
              </label>}
            </div>
            <div className="space-y-3"><LeverToggle checked={savings.enabled} onChange={(enabled) => setSavings({ ...savings, enabled })} label={<HelpLabel label="增加工作期年净储蓄" termKey="annual_savings_wizard" />} /><MoneyLeverFields value={savings} onChange={setSavings} maximumLabel="最多增加" /></div>
            <div className="space-y-3"><LeverToggle checked={spending.enabled} onChange={(enabled) => setSpending({ ...spending, enabled })} label={<HelpLabel label="降低退休后年支出" termKey="retirement_spending" />} /><MoneyLeverFields value={spending} onChange={setSpending} maximumLabel="最多降低" /></div>
            <div className="space-y-3"><LeverToggle checked={income.enabled} onChange={(enabled) => setIncome({ ...income, enabled })} label={<HelpLabel label="增加可确认的退休稳定年收入" termKey="stable_retirement_income" />} /><MoneyLeverFields value={income} onChange={setIncome} maximumLabel="最多增加" /></div>
          </div>
          <div className="mt-5 flex flex-wrap items-center gap-3">
            <Button
              disabled={!source || Boolean(configError) || Boolean(alreadyMeets) || running || restorationBlocked}
              pending={createM.isPending}
              onClick={() => createM.mutate()}
            >开始分析</Button>
            {(runsQ.isPending || taskRestore.restoring) && <span className="text-sm text-ink-muted">正在恢复任务状态...</span>}
            {taskRestore.restoreError && (
              <>
                <span className="text-sm text-danger">任务状态检查失败，请重试后再创建。</span>
                <Button variant="ghost" onClick={() => void taskRestore.retryRestore()}>
                  重试状态检查
                </Button>
              </>
            )}
            {configError && <span className="text-sm text-danger">{configError}</span>}
            {createM.isError && !createConflict && <span className="text-sm text-danger">{queryErrorMessage(createM.error)}</span>}
            {createConflict && <span className="text-sm text-ink-muted">已有任务正在执行，已继续跟踪该任务。</span>}
          </div>
        </section>

        {running && (
          <section aria-live="polite" className="border-b border-line pb-6">
            <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="text-lg font-semibold text-ink">正在分析</h2><p className="mt-1 text-sm text-ink-muted">{taskState.task?.status === "pre_complete" ? "正在保存结果" : taskState.task?.phase || activeRun?.phase || "等待执行"} · {Math.round(taskState.progress * 100)}%</p></div><TaskCancelButton task={taskState.task} onCanceled={() => taskState.refetch().then(() => undefined)} /></div>
            <div className="mt-3 h-2 overflow-hidden rounded bg-surface-muted"><div className="h-full bg-brand transition-[width]" style={{ width: `${Math.max(2, taskState.progress * 100)}%` }} /></div>
            {taskState.pollError && (
              <p className="mt-2 flex flex-wrap items-center gap-2 text-sm text-warning">
                状态更新暂时失败，正在重试
                <Button variant="ghost" onClick={() => void taskState.refetch()}>
                  立即重试
                </Button>
              </p>
            )}
          </section>
        )}
        {activeRun?.status === "failed" && <Alert variant="danger" title="改善分析失败">{activeRun.error_message || "任务失败"}{activeRun.error_code && <span className="mt-1 block text-xs">{activeRun.error_code}</span>}</Alert>}
        {activeRun?.status === "canceled" && <Alert variant="info">改善分析已取消，可使用当前配置重新创建。</Alert>}
        {activeRun?.status === "complete" && activeRun.result && <ImprovementResults run={activeRun} result={activeRun.result} mode={mode} onModeChange={setMode} onPreview={(proposal) => previewM.mutate(proposal)} />}
        {previewM.isError && <Alert variant="danger">{queryErrorMessage(previewM.error)}</Alert>}
        {(applied || activeRun?.application) && (
          <Alert variant="success" title="改善方案已应用">
            <div className="flex flex-wrap items-center gap-3"><span>计划参数已经更新，请运行正式模拟验证。</span><Button pending={verifyM.isPending} onClick={() => verifyM.mutate()}>运行验证模拟</Button><Button variant="secondary" href={`/plans/${planId}/settings?section=parameters`}>返回计划参数</Button></div>
            {verifyM.isError && <span className="mt-2 block text-danger">{queryErrorMessage(verifyM.error)}</span>}
          </Alert>
        )}
      </div>
      <PreviewDialog preview={preview} applying={applyM.isPending} error={previewError} onClose={() => { setPreview(null); setPreviewError(null); }} onApply={() => applyM.mutate()} />
    </div>
  );
}
