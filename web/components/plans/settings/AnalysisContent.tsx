"use client";

import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { HelpLabel } from "@/components/ui/HelpLabel";
import { CalculationExplanation } from "@/components/ui/CalculationExplanation";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { queryErrorMessage } from "@/lib/query-error";
import { WealthPathChart } from "@/components/charts/WealthPathChart";
import { ScenarioComparisonCard } from "@/components/analysis/ScenarioComparisonCard";
import {
  ParameterCurvesChart,
  SensitivityHeatmap,
  TornadoChart,
} from "@/components/charts/SensitivityCharts";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import { useActiveTaskRestore } from "@/hooks/useActiveTaskRestore";
import {
  SimulationReadinessPanel,
  useSimulationReadiness,
} from "@/components/plans/SimulationReadinessPanel";
import { getParameters } from "@/lib/api/plans";
import {
  createSensitivityTest,
  createStressTest,
  getSensitivityTest,
  getStressTest,
  listSensitivityTests,
  listStressTests,
} from "@/lib/api/analysis";
import { getHoldings } from "@/lib/api/holdings";
import {
  createSimulation,
  listPaths,
  listSimulations,
} from "@/lib/api/simulations";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { activeTaskConflictRef, isTaskActive } from "@/lib/api/tasks";
import {
  formatDateTimeFromMs,
  formatMoney,
  formatMoneyWan,
  formatPercent,
  historyDepthLabel,
  regionLabel,
  sortRepresentativePaths,
} from "@/lib/format";
import type { SimulationRun } from "@/types/api";

type JobKind = "sim" | "stress" | "sensitivity";

function caliberLabel(c: "nominal" | "real"): string {
  return c === "real" ? "起点购买力" : "名义金额";
}

function formatFailureAge(value: unknown): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  const totalMonths = Math.round(value * 12);
  const years = Math.floor(totalMonths / 12);
  const months = totalMonths % 12;
  return months === 0 ? `${years} 岁` : `${years} 岁 ${months} 个月`;
}

const RETURN_MODE_LABELS: Record<string, string> = {
  blended_prior: "前瞻收益（历史向长期先验收缩）",
  historical_cagr: "历史 CAGR（旧模式）",
  custom: "自定义前瞻收益",
};

const FACTOR_MODEL_LABELS: Record<string, string> = {
  multivariate_student_t: "联合厚尾因子模型（资产/FX 相关）",
  independent_student_t: "独立因子模型（旧）",
};

function CaliberToggle({
  value,
  onChange,
  hasReal,
}: {
  value: "nominal" | "real";
  onChange: (v: "nominal" | "real") => void;
  hasReal: boolean;
}) {
  if (!hasReal) {
    return null;
  }
  return (
    <div
      className="mt-4 flex items-center gap-2 text-sm"
      role="group"
      aria-label="金额口径"
    >
      <span className="text-ink-muted">金额口径</span>
      {(["nominal", "real"] as const).map((opt) => (
        <button
          key={opt}
          type="button"
          aria-pressed={value === opt}
          onClick={() => onChange(opt)}
          className={
            value === opt
              ? "rounded border border-brand bg-brand/10 px-2 py-1 font-medium text-brand-strong"
              : "rounded border border-line px-2 py-1 text-ink-muted hover:text-ink"
          }
        >
          {caliberLabel(opt)}
        </button>
      ))}
      <span className="text-xs text-ink-muted">
        起点购买力 = 名义金额 ÷ 路径累计通胀
      </span>
    </div>
  );
}

function RunAssumptionCard({
  assumption,
}: {
  assumption: NonNullable<SimulationRun["assumption"]>;
}) {
  const modeLabel = assumption.mode
    ? (RETURN_MODE_LABELS[assumption.mode] ?? assumption.mode)
    : "旧版未冻结运行级模式";
  const factorLabel =
    FACTOR_MODEL_LABELS[assumption.random_factor_model] ??
    assumption.random_factor_model;
  const riskAssets = assumption.assets.filter((a) => !a.is_cash);
  return (
    <section className="mt-4 rounded-lg border border-line bg-surface-muted/40 p-3 text-sm">
      <h3 className="font-medium text-ink"><HelpLabel label="本次模拟的收益假设" termKey="return_assumption_mode" /></h3>
      <p className="mt-1 text-xs text-ink-muted">
        {modeLabel}{assumption.scenario ? ` · 假设情景 ${assumption.scenario}` : ""}
      </p>
      <details className="mt-2 text-xs text-ink-muted">
        <summary className="cursor-pointer font-medium">高级假设版本</summary>
        <p className="mt-1 break-words">
          引擎 {assumption.engine_version}
          {assumption.profile_id ? ` · Profile ${assumption.profile_id}@${assumption.profile_version}` : ""}
          {` · 随机因子模型 ${factorLabel}`}
        </p>
      </details>
      {assumption.correlation_prior_only && (
        <p className="mt-1 text-xs text-warning">
          相关性主要依赖先验（历史共同月份不足），分散化结果偏保守。
        </p>
      )}
      {riskAssets.length > 0 && (
        <div className="mt-2 overflow-x-auto">
          <table className="min-w-full text-left text-xs">
            <thead>
              <tr className="text-ink-muted">
                <th className="pr-3 py-1">资产</th>
                <th className="pr-3 py-1">模拟地域</th>
                <th className="pr-3 py-1"><HelpLabel label="历史 CAGR" termKey="annual_return" /></th>
                <th className="pr-3 py-1"><HelpLabel label="本地前瞻收益" termKey="forward_return" /></th>
                <th className="pr-3 py-1"><HelpLabel label="FX 前瞻收益" termKey="fx_forward_return" /></th>
                <th className="pr-3 py-1"><HelpLabel label="费用 / FX 口径" termKey="fee_included" /></th>
                <th className="pr-3 py-1"><HelpLabel label="基准币种合成收益" termKey="base_currency_return" /></th>
                <th className="pr-3 py-1"><HelpLabel label="历史权重" termKey="historical_weight" /></th>
                <th className="pr-3 py-1"><HelpLabel label="样本年数" termKey="sample_years" /></th>
                <th className="pr-3 py-1"><HelpLabel label="波动率" termKey="annual_volatility" /></th>
              </tr>
            </thead>
            <tbody>
              {riskAssets.map((a) => (
                <tr key={a.holding_id} className="border-t">
                  <td className="py-1 pr-3">
                    {a.instrument_name || a.holding_id}
                    {a.instrument_code ? `（${a.instrument_code}）` : ""}
                  </td>
                  <td className="py-1 pr-3">
                    {a.region ? regionLabel(a.region) : "—"}
                  </td>
                  <td className="py-1 pr-3">
                    {formatPercent(a.historical_annual_geometric_return)}
                  </td>
                  <td className="py-1 pr-3 font-medium">
                    {formatPercent(a.forward_annual_geometric_return)}
                  </td>
                  <td className="py-1 pr-3">
                    {a.has_fx ? formatPercent(a.fx_forward_return) : "—"}
                  </td>
                  <td className="py-1 pr-3 text-ink-muted">
                    {a.fee_treatment === "embedded"
                      ? "持续费用已内含"
                      : "无持续费用处理"}{" "}
                    /{" "}
                    {a.fx_treatment === "embedded_in_asset_nav"
                      ? "汇率已含于净值"
                      : a.fx_treatment === "separate_factor"
                        ? "独立汇率因子"
                        : "无汇率因子"}
                  </td>
                  <td className="py-1 pr-3 font-medium">
                    {formatPercent(a.base_currency_forward_return)}
                  </td>
                  <td className="py-1 pr-3">
                    {formatPercent(a.historical_weight)}
                  </td>
                  <td className="py-1 pr-3">{a.sample_years || "—"}</td>
                  <td className="py-1 pr-3">
                    {formatPercent(a.annual_volatility_used)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="mt-2 text-xs text-ink-muted">
        历史收益不代表未来；前瞻年化为历史与长期先验在对数空间的收缩结果。基金净值已反映管理费、托管费等持续费用，系统不会按
        expense ratio 重复扣除。
      </p>
    </section>
  );
}

function simulationOptionLabel(run: SimulationRun): string {
  const date = formatDateTimeFromMs(run.created_at);
  const success = run.summary_json?.success_probability;
  const status =
    run.task_status ?? (typeof success === "number" ? "complete" : "unknown");
  const statusLabels: Record<string, string> = {
    pending: "排队中",
    running: "运行中",
    pre_complete: "正在保存结果",
    failed: "失败",
    canceled: "已取消",
    unknown: "状态未知",
  };
  const tail =
    status === "complete" && typeof success === "number"
      ? `成功率 ${formatPercent(success)}`
      : (statusLabels[status] ?? status);
  return `${date} · ${run.runs} 次 · ${tail}`;
}

function AnalysisJobPanel({
  title,
  termKey,
  activeTaskId,
  taskState,
  panelError,
  onRetry,
  onRun,
  running,
  restoring,
  restoreError,
  onRetryRestore,
  runDisabled,
  runDisabledHint,
  latest,
  listError,
  onReloadList,
}: {
  title: string;
  termKey: "stress_test" | "sensitivity_test";
  activeTaskId: string | null;
  taskState: ReturnType<typeof useTaskStatus>;
  panelError?: string | null;
  onRetry?: () => void;
  onRun: () => void;
  running: boolean;
  restoring?: boolean;
  restoreError?: unknown;
  onRetryRestore?: () => void;
  runDisabled?: boolean;
  runDisabledHint?: string;
  latest?: {
    status: string;
    result_stale?: boolean;
    result_json?: Record<string, unknown>;
  } | null;
  listError?: string | null;
  onReloadList?: () => void;
}) {
  const jobBusy = !!activeTaskId;
  const report = latest?.result_json;
  const scenarios =
    (report?.scenarios as Array<Record<string, unknown>> | undefined) ?? [];
  const tornado =
    (report?.tornado as Array<Record<string, unknown>> | undefined) ?? [];
  const heatmap =
    (report?.heatmap as Array<Array<Record<string, unknown>>> | undefined) ??
    [];
  const curves =
    (report?.curves as Array<Record<string, unknown>> | undefined) ?? [];

  const worstId = report?.worst_scenario_id as string | undefined;

  return (
    <section className="rounded-lg border border-line bg-surface p-4">
      <h2 className="flex items-center font-medium text-ink">
        {title}
        <MetricHelp termKey={termKey} />
      </h2>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button disabled={running || jobBusy || restoring || Boolean(restoreError) || runDisabled} onClick={onRun}>
          运行{title}
        </Button>
        {restoring && <span className="text-sm text-ink-muted">正在恢复任务状态...</span>}
        {Boolean(restoreError) && onRetryRestore && (
          <Button variant="ghost" className="px-2 py-1" onClick={onRetryRestore}>
            重试状态检查
          </Button>
        )}
        {runDisabled && runDisabledHint && (
          <span className="text-sm text-ink-muted">{runDisabledHint}</span>
        )}
        {activeTaskId && (
          <>
            <span className="text-sm text-ink-muted">
              {taskState.task?.status === "pre_complete"
                ? "正在保存结果"
                : (taskState.task?.phase || taskState.task?.status || "连接中")}…{" "}
              {Math.round(taskState.progress * 100)}%
            </span>
            <TaskCancelButton
              task={taskState.task}
              className="min-h-8 px-2 py-1 text-xs"
              onCanceled={() => taskState.refetch().then(() => undefined)}
            />
          </>
        )}
        {taskState.pollError && (
          <span className="flex items-center gap-2 text-sm text-warning">
            状态更新暂时失败，正在重试
            <Button variant="ghost" className="px-2 py-1" onClick={() => void taskState.refetch()}>
              立即重试
            </Button>
          </span>
        )}
      </div>
      {panelError && (
        <Alert variant="danger" className="mt-3">
          <div className="flex flex-wrap items-center gap-3">
            <span>{panelError}</span>
            {onRetry && (
              <Button
                variant="ghost"
                className="px-2 py-1"
                disabled={jobBusy}
                onClick={onRetry}
              >
                重试
              </Button>
            )}
          </div>
        </Alert>
      )}
      {listError && (
        <Alert variant="danger" className="mt-3">
          <div className="flex flex-wrap items-center gap-3">
            <span>
              无法加载{title}结果：{listError}
            </span>
            {onReloadList && (
              <Button
                variant="ghost"
                className="px-2 py-1"
                onClick={onReloadList}
              >
                重新加载
              </Button>
            )}
          </div>
        </Alert>
      )}
      {latest?.result_stale && <StaleBanner />}
      {latest?.status === "complete" && report && (
        <div className="mt-4 space-y-3 text-sm">
          {typeof report.baseline_success_probability === "number" && (
            <p>
              基准成功率{" "}
              <span className="font-medium">
                {formatPercent(report.baseline_success_probability as number)}
              </span>
            </p>
          )}
          {termKey === "stress_test" && scenarios.length > 0 && (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-xs">
                <thead>
                  <tr className="text-ink-muted">
                    <th className="pr-3 py-1">压力场景</th>
                    <th className="pr-3 py-1"><HelpLabel label="成功率" termKey="fire_success_rate" /></th>
                    <th className="pr-3 py-1">相对基准</th>
                    <th className="pr-3 py-1"><HelpLabel label="终值 P25/P50/P95" termKey="p_quantiles" /></th>
                    <th className="pr-3 py-1"><HelpLabel label="P95 回撤" termKey="p95_drawdown" /></th>
                    <th className="pr-3 py-1"><HelpLabel label="首次资金不足年龄 P50" termKey="failure_age" /></th>
                    <th className="pr-3 py-1"><HelpLabel label="恢复期 P50" termKey="recovery_period" /></th>
                    <th className="pr-3 py-1">说明</th>
                    <th className="pr-3 py-1">风险提示</th>
                  </tr>
                </thead>
                <tbody>
                  {scenarios.map((s) => {
                    const isWorst = String(s.scenario_id) === worstId;
                    return (
                      <tr
                        key={String(s.scenario_id)}
                        className={`border-t ${isWorst ? "bg-danger/5" : ""}`}
                      >
                        <td className="py-1 pr-3 font-medium">
                          {String(s.scenario_name ?? s.scenario_id)}
                          {isWorst && (
                            <span className="ml-1 text-danger">（最差）</span>
                          )}
                        </td>
                        <td className="py-1 pr-3">
                          {formatPercent(
                            (s.success_probability as number) ?? 0,
                          )}
                        </td>
                        <td className="py-1 pr-3">
                          {formatPercent((s.baseline_delta as number) ?? 0)}
                        </td>
                        <td className="py-1 pr-3">
                          {formatMoney((s.terminal_p25_minor as number) ?? 0)} /{" "}
                          {formatMoney((s.terminal_p50_minor as number) ?? 0)} /{" "}
                          {formatMoney((s.terminal_p95_minor as number) ?? 0)}
                        </td>
                        <td className="py-1 pr-3">
                          {formatPercent((s.max_drawdown_p95 as number) ?? 0)}
                        </td>
                        <td className="py-1 pr-3">
                          {s.failure_age_p50 != null
                            ? formatFailureAge(s.failure_age_p50)
                            : s.failure_year_p50 != null
                              ? `${formatFailureAge(s.failure_year_p50)}（旧版仅精确到整岁）`
                              : "—"}
                        </td>
                        <td className="py-1 pr-3">
                          {s.recovery_not_within_plan
                            ? "规划期内未恢复"
                            : s.recovery_month_p50 != null
                              ? `${String(s.recovery_month_p50)} 月`
                              : "—"}
                        </td>
                        <td className="py-1 pr-3 max-w-xs">
                          {String(s.description ?? "")}
                        </td>
                        <td className="py-1 pr-3 max-w-xs text-warning">
                          {String(s.risk_hint ?? "")}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
          {termKey === "sensitivity_test" && tornado.length > 0 && (
            <TornadoChart
              items={tornado.map((t) => ({
                parameter_name: String(t.parameter_name),
                low_label: String(t.low_label ?? "低值"),
                high_label: String(t.high_label ?? "高值"),
                low_success: (t.low_success as number) ?? 0,
                high_success: (t.high_success as number) ?? 0,
              }))}
            />
          )}
          {termKey === "sensitivity_test" && curves.length > 0 && (
            <ParameterCurvesChart
              curves={curves.map((c) => ({
                parameter_name: String(c.parameter_name),
                points: (
                  (c.points as Array<Record<string, unknown>>) ?? []
                ).map((p) => ({
                  label: String(p.label ?? ""),
                  success_probability: (p.success_probability as number) ?? 0,
                })),
              }))}
            />
          )}
          {termKey === "sensitivity_test" && heatmap.length > 0 && (
            <SensitivityHeatmap
              heatmap={heatmap.map((row) =>
                row.map((cell) => ({
                  spending_label: String(cell.spending_label ?? ""),
                  return_label: String(cell.return_label ?? ""),
                  success_probability:
                    (cell.success_probability as number) ?? 0,
                })),
              )}
            />
          )}
          {typeof report.monte_carlo_std_error === "number" && (
            <p className="text-xs text-ink-muted">
              MC 标准误 ±{formatPercent(report.monte_carlo_std_error as number)}
              {report.std_error_hint
                ? ` · ${String(report.std_error_hint)}`
                : ""}
            </p>
          )}
        </div>
      )}
      {!latest && !activeTaskId && !listError && (
        <p className="mt-3 text-sm text-ink-muted">
          尚无结果，点击上方按钮运行。
        </p>
      )}
    </section>
  );
}

export function AnalysisContent() {
  const planId = useParams().id as string;
  const router = useRouter();
  const qc = useQueryClient();
  // Each job kind tracks its own busy state; running Monte Carlo no longer
  // disables the stress/sensitivity buttons (and vice versa).
  const [activeTasks, setActiveTasks] = useState<
    Partial<Record<JobKind, string>>
  >({});
  const [runsDraft, setRunsDraft] = useState<string | null>(null);
  const [taskErrors, setTaskErrors] = useState<
    Partial<Record<JobKind, string>>
  >({});
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [caliber, setCaliber] = useState<"nominal" | "real">("nominal");

  const paramsQ = useQuery({
    queryKey: ["parameters", planId],
    queryFn: () => getParameters(planId),
  });
  const holdingsQ = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });

  const serverRuns = paramsQ.data?.parameters.simulation_runs;
  const runsText =
    runsDraft ?? String(serverRuns && serverRuns >= 1000 ? serverRuns : 10000);
  const runs = /^\d+$/.test(runsText) ? Number(runsText) : 0;
  const runsValid = Number.isInteger(runs) && runs >= 1000 && runs <= 100000;

  const simsQ = useQuery({
    queryKey: ["simulations", planId],
    queryFn: () => listSimulations(planId),
  });

  const readinessQ = useSimulationReadiness(planId);
  const readinessReady =
    !readinessQ.isLoading &&
    !readinessQ.isFetching &&
    !readinessQ.isError &&
    readinessQ.data?.ready === true;

  const simulations = simsQ.data?.simulations ?? [];
  const selectedRun =
    simulations.find((run) => run.id === selectedRunId) ?? simulations[0];

  const stressQ = useQuery({
    queryKey: ["stress-tests", planId, selectedRun?.id],
    queryFn: () => listStressTests(planId, selectedRun!.id),
    enabled: !!selectedRun?.id,
  });
  const sensQ = useQuery({
    queryKey: ["sensitivity-tests", planId, selectedRun?.id],
    queryFn: () => listSensitivityTests(planId, selectedRun!.id),
    enabled: !!selectedRun?.id,
  });

  const latest = selectedRun;
  const latestStress = stressQ.data?.stress_tests[0];
  const latestSens = sensQ.data?.sensitivity_tests[0];
  const persistedSimTaskID = simulations.find((run) =>
    isTaskActive(run.task_status),
  )?.task_id;
  const persistedStressTaskID = stressQ.data?.stress_tests.find((item) =>
    isTaskActive(item.status),
  )?.task_id;
  const persistedSensitivityTaskID = sensQ.data?.sensitivity_tests.find((item) =>
    isTaskActive(item.status),
  )?.task_id;
  const simRestore = useActiveTaskRestore({
    workerType: "go_worker",
    taskType: "simulation",
    scopeType: "plan",
    scopeId: planId,
    businessTaskId: persistedSimTaskID,
    preferredTaskId: activeTasks.sim,
  });
  const stressRestore = useActiveTaskRestore({
    workerType: "go_worker",
    taskType: "stress",
    scopeType: "plan",
    scopeId: planId,
    businessTaskId: persistedStressTaskID,
    preferredTaskId: activeTasks.stress,
  });
  const sensitivityRestore = useActiveTaskRestore({
    workerType: "go_worker",
    taskType: "sensitivity",
    scopeType: "plan",
    scopeId: planId,
    businessTaskId: persistedSensitivityTaskID,
    preferredTaskId: activeTasks.sensitivity,
  });

  // A run is displayable once its summary is persisted: summary_json carries a
  // numeric success_probability only on success. Job status drives the
  // Active-task indicator never gates results already stored on the run.
  const simCompleted =
    !!latest && typeof latest.summary_json?.success_probability === "number";

  const pathsQ = useQuery({
    queryKey: ["paths", latest?.id],
    queryFn: () => listPaths(latest!.id),
    enabled: !!latest?.id && simCompleted,
  });

  const invalidateAll = () => {
    void qc.invalidateQueries({ queryKey: ["simulations", planId] });
    void qc.invalidateQueries({ queryKey: ["stress-tests", planId] });
    void qc.invalidateQueries({ queryKey: ["sensitivity-tests", planId] });
    void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
  };

  const clearJobError = (kind: JobKind) => {
    setTaskErrors((prev) => {
      const next = { ...prev };
      delete next[kind];
      return next;
    });
  };

  // Tasks that already reached a terminal state in this session. The rebuild
  // effect below must skip them, otherwise a failed run in the list would be
  // re-adopted after every refetch and loop forever.
  const settledTasksRef = useRef<Set<string>>(new Set());

  const finishJob = (kind: JobKind, message?: string) => {
    setActiveTasks((prev) => {
      const taskId = prev[kind];
      if (taskId) {
        settledTasksRef.current.add(taskId);
      }
      const next = { ...prev };
      delete next[kind];
      return next;
    });
    if (message) {
      setTaskErrors((prev) => ({ ...prev, [kind]: message }));
    }
    invalidateAll();
    void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
  };

  // Rebuild activeTasks from persisted records so that a page refresh does not
  // lose the progress bar and cancel button of tasks still running on the
  // backend. Tasks the user just started take precedence: a persisted job is
  // only adopted when no job of that kind is currently tracked.
  const simsData = simsQ.data;
  const stressData = stressQ.data;
  const sensData = sensQ.data;
  useEffect(() => {
    setActiveTasks((prev) => {
      const next = { ...prev };
      let changed = false;
      const adopt = (kind: JobKind, taskId: string | undefined) => {
        if (!taskId || next[kind] || settledTasksRef.current.has(taskId))
          return;
        next[kind] = taskId;
        changed = true;
      };
      // Only live persisted tasks are adopted after refresh. Failed/canceled
      // runs are terminal records and render their stored error directly.
      const newestSim = simsData?.simulations?.[0];
      adopt(
        "sim",
        newestSim?.task_id &&
          isTaskActive(newestSim.task_status)
          ? newestSim.task_id
          : undefined,
      );
      adopt(
        "stress",
        (stressData?.stress_tests ?? []).find(
          (t) =>
            t.task_id && isTaskActive(t.status),
        )?.task_id,
      );
      adopt(
        "sensitivity",
        (sensData?.sensitivity_tests ?? []).find(
          (t) =>
            t.task_id && isTaskActive(t.status),
        )?.task_id,
      );
      adopt("sim", simRestore.taskId ?? undefined);
      adopt("stress", stressRestore.taskId ?? undefined);
      adopt("sensitivity", sensitivityRestore.taskId ?? undefined);
      return changed ? next : prev;
    });
  }, [
    simsData,
    stressData,
    sensData,
    simRestore.taskId,
    stressRestore.taskId,
    sensitivityRestore.taskId,
  ]);

  const trackedSimTaskID =
    activeTasks.sim ?? simRestore.taskId ?? persistedSimTaskID ?? null;
  const trackedStressTaskID =
    activeTasks.stress ?? stressRestore.taskId ?? persistedStressTaskID ?? null;
  const trackedSensitivityTaskID =
    activeTasks.sensitivity ??
    sensitivityRestore.taskId ??
    persistedSensitivityTaskID ??
    null;

  const simTaskState = useTaskStatus(trackedSimTaskID, {
    initialTask: simRestore.task,
    onComplete: () => {
      clearJobError("sim");
      finishJob("sim");
    },
    onFailed: (task) => finishJob("sim", task.error_message || "任务失败"),
    onCanceled: () => finishJob("sim"),
  });

  const stressTaskState = useTaskStatus(trackedStressTaskID, {
    initialTask: stressRestore.task,
    onComplete: async () => {
      clearJobError("stress");
      if (trackedStressTaskID) {
        await getStressTest(trackedStressTaskID).catch(() => null);
      }
      finishJob("stress");
    },
    onFailed: (task) => finishJob("stress", task.error_message || "任务失败"),
    onCanceled: () => finishJob("stress"),
  });

  const sensTaskState = useTaskStatus(trackedSensitivityTaskID, {
    initialTask: sensitivityRestore.task,
    onComplete: async () => {
      clearJobError("sensitivity");
      if (trackedSensitivityTaskID) {
        await getSensitivityTest(trackedSensitivityTaskID).catch(() => null);
      }
      finishJob("sensitivity");
    },
    onFailed: (task) =>
      finishJob("sensitivity", task.error_message || "任务失败"),
    onCanceled: () => finishJob("sensitivity"),
  });

  const availableTaskID = (
    localTaskID: string | undefined,
    notFound: boolean,
    restoring: boolean,
    restoreError: unknown,
    restoredTaskID: string | null,
    persistedTaskID: string | undefined,
  ) =>
    localTaskID &&
    !(
      notFound &&
      !restoring &&
      !restoreError &&
      !restoredTaskID &&
      !persistedTaskID
    )
      ? localTaskID
      : null;
  const activeSimTaskID = availableTaskID(
    trackedSimTaskID ?? undefined,
    simTaskState.notFound,
    simRestore.restoring,
    simRestore.restoreError,
    simRestore.taskId,
    persistedSimTaskID,
  );
  const activeStressTaskID = availableTaskID(
    trackedStressTaskID ?? undefined,
    stressTaskState.notFound,
    stressRestore.restoring,
    stressRestore.restoreError,
    stressRestore.taskId,
    persistedStressTaskID,
  );
  const activeSensitivityTaskID = availableTaskID(
    trackedSensitivityTaskID ?? undefined,
    sensTaskState.notFound,
    sensitivityRestore.restoring,
    sensitivityRestore.restoreError,
    sensitivityRestore.taskId,
    persistedSensitivityTaskID,
  );

  const startMut = useMutation({
    mutationFn: () => createSimulation(planId, { runs }),
    onSuccess: (res) => {
      clearJobError("sim");
      setActiveTasks((prev) => ({ ...prev, sim: res.task_id }));
      // Surface the new run immediately as a pending entry and select it, so the
      // page tracks this run from the start. The list is refetched on terminal
      // (handleJobTerminal -> invalidateAll), replacing it with the stored run.
      if (res.run_id) {
        const pendingRun: SimulationRun = {
          id: res.run_id,
          task_id: res.task_id,
          plan_id: planId,
          input_hash: "",
          current_config_hash: "",
          result_stale: false,
          market_snapshot_hash: "",
          engine_version: "",
          runs,
          seed: "",
          horizon_months: 0,
          success_count: 0,
          failure_count: 0,
          summary_json: {},
          created_at: Date.now(),
          task_status: "pending",
        };
        qc.setQueryData<{ simulations: SimulationRun[] }>(
          ["simulations", planId],
          (old) => {
            const list = old?.simulations ?? [];
            if (list.some((run) => run.id === pendingRun.id)) {
              return { simulations: list };
            }
            return { simulations: [pendingRun, ...list] };
          },
        );
        setSelectedRunId(res.run_id);
      }
    },
    onError: (error) => {
      const conflict = activeTaskConflictRef(error);
      if (conflict) {
        clearJobError("sim");
        setActiveTasks((prev) => ({ ...prev, sim: conflict.taskId }));
        if (conflict.resourceId) setSelectedRunId(conflict.resourceId);
        void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
        return;
      }
      setTaskErrors((prev) => ({
        ...prev,
        sim: error instanceof Error ? error.message : "启动失败",
      }));
    },
  });

  const stressMut = useMutation({
    mutationFn: () => {
      if (!selectedRun) throw new Error("请先运行 Monte Carlo 模拟");
      return createStressTest(planId, {
        runs,
        simulation_run_id: selectedRun.id,
      });
    },
    onSuccess: (res) => {
      clearJobError("stress");
      setActiveTasks((prev) => ({ ...prev, stress: res.task_id }));
    },
    onError: (error) => {
      const conflict = activeTaskConflictRef(error);
      if (conflict) {
        clearJobError("stress");
        setActiveTasks((prev) => ({ ...prev, stress: conflict.taskId }));
        void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
        return;
      }
      setTaskErrors((prev) => ({
        ...prev,
        stress: error instanceof Error ? error.message : "启动失败",
      }));
    },
  });

  const sensMut = useMutation({
    mutationFn: () => {
      if (!selectedRun) throw new Error("请先运行 Monte Carlo 模拟");
      return createSensitivityTest(planId, {
        runs,
        simulation_run_id: selectedRun.id,
      });
    },
    onSuccess: (res) => {
      clearJobError("sensitivity");
      setActiveTasks((prev) => ({ ...prev, sensitivity: res.task_id }));
    },
    onError: (error) => {
      const conflict = activeTaskConflictRef(error);
      if (conflict) {
        clearJobError("sensitivity");
        setActiveTasks((prev) => ({ ...prev, sensitivity: conflict.taskId }));
        void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
        return;
      }
      setTaskErrors((prev) => ({
        ...prev,
        sensitivity: error instanceof Error ? error.message : "启动失败",
      }));
    },
  });

  const attachDisabled = !selectedRun || !simCompleted;
  const attachHint = !selectedRun
    ? "请先运行 Monte Carlo 模拟"
    : !simCompleted
      ? "当前模拟尚未完成，无法运行附属分析"
      : undefined;

  const repPaths = sortRepresentativePaths(
    pathsQ.data?.paths.filter((p) => p.representative_percentile) ?? [],
  );

  const hasReal =
    (latest?.summary_json?.real_terminal_quantiles &&
      Object.keys(latest.summary_json.real_terminal_quantiles).length > 0) ||
    (latest?.summary_json?.real_monthly_wealth_quantiles?.length ?? 0) > 0;
  const effectiveCaliber: "nominal" | "real" = hasReal ? caliber : "nominal";
  const chartSeries =
    effectiveCaliber === "real"
      ? latest?.summary_json?.real_monthly_wealth_quantiles
      : latest?.summary_json?.monthly_wealth_quantiles;

  const simBusy = !!activeSimTaskID;
  const simRestoring = simsQ.isPending || simRestore.restoring;
  const simRestoreBlocked = simRestoring || Boolean(simRestore.restoreError);
  const stressRestoring = stressQ.isPending || stressRestore.restoring;
  const stressRestoreBlocked = stressRestoring || Boolean(stressRestore.restoreError);
  const sensitivityRestoring = sensQ.isPending || sensitivityRestore.restoring;
  const sensitivityRestoreBlocked =
    sensitivityRestoring || Boolean(sensitivityRestore.restoreError);

  const simPanelError = taskErrors.sim ?? simTaskState.error;

  const snapshotWarningLabels = (() => {
    const labels: string[] = [];
    for (const h of holdingsQ.data?.holdings ?? []) {
      if (!h.enabled) {
        continue;
      }
      const name = h.instrument_name ?? h.asset_key;
      const code = h.instrument_code ?? "—";
      for (const w of h.snapshot_warnings ?? []) {
        labels.push(`${name}（${code}）· ${w}`);
      }
      if (
        h.snapshot_history_depth === "one_year" &&
        (h.snapshot_warnings ?? []).length === 0
      ) {
        labels.push(
          `${name}（${code}）· ${historyDepthLabel(h.snapshot_history_depth)}`,
        );
      }
    }
    return labels;
  })();

  if (
    (paramsQ.isError || holdingsQ.isError) &&
    (!paramsQ.data || !holdingsQ.data)
  ) {
    return (
      <ErrorState
        message="无法加载分析所需的计划参数或持仓数据。请确认后端服务可用后重试。"
        onRetry={() => {
          if (paramsQ.isError) void paramsQ.refetch();
          if (holdingsQ.isError) void holdingsQ.refetch();
        }}
        backHref={`/plans/${planId}/overview`}
        backLabel="返回组合总览"
        technicalDetail={queryErrorMessage(paramsQ.error ?? holdingsQ.error)}
      />
    );
  }

  if (
    paramsQ.isLoading ||
    holdingsQ.isLoading ||
    !paramsQ.data ||
    !holdingsQ.data
  ) {
    return <PageSkeleton label="加载分析数据…" />;
  }

  return (
    <div className="space-y-8">
      <section className="rounded-lg border border-line bg-surface p-4">
        <h2 className="font-medium text-ink"><HelpLabel label="Monte Carlo 模拟" termKey="monte_carlo" /></h2>
        {snapshotWarningLabels.length > 0 && (
          <Alert variant="warning" className="mt-2">
            以下持仓历史样本有限，模拟结果长期不确定性较高：
            {snapshotWarningLabels.join("；")}
          </Alert>
        )}
        <SimulationReadinessPanel planId={planId} />
        {simulations.length > 0 && (
          <label className="mt-3 block text-sm text-ink">
            历史模拟
            <select
              className="ml-2 rounded border border-line px-2 py-1"
              value={selectedRun?.id ?? ""}
              onChange={(e) => setSelectedRunId(e.target.value)}
              data-testid="simulation-history-select"
            >
              {simulations.map((run) => (
                <option key={run.id} value={run.id}>
                  {simulationOptionLabel(run)}
                </option>
              ))}
            </select>
          </label>
        )}
        <div className="mt-3 flex flex-wrap items-end gap-4">
          <div className="text-sm text-ink">
            <label htmlFor="analysis-simulation-runs">模拟次数</label>
            <MetricHelp termKey="simulation_runs" />
            <input
              id="analysis-simulation-runs"
              type="text"
              inputMode="numeric"
              className="ml-2 rounded border border-line px-2 py-1"
              value={runsText}
              aria-invalid={!runsValid}
              onChange={(e) => setRunsDraft(e.target.value)}
            />
          </div>
          <Button
            disabled={
              startMut.isPending || simBusy || simRestoreBlocked || !readinessReady || !runsValid
            }
            onClick={() => startMut.mutate()}
          >
            运行模拟
          </Button>
          {simRestoring && <span className="text-sm text-ink-muted">正在恢复任务状态...</span>}
          {simRestore.restoreError && (
            <Button variant="ghost" className="px-2 py-1" onClick={() => void simRestore.retryRestore()}>
              重试状态检查
            </Button>
          )}
          {activeSimTaskID && (
            <>
              <span className="text-sm text-ink-muted">
                {simTaskState.task?.status === "pre_complete"
                  ? "正在保存结果"
                  : (simTaskState.task?.phase || simTaskState.task?.status || "连接中")}…{" "}
                {Math.round(simTaskState.progress * 100)}%
              </span>
              <TaskCancelButton
                task={simTaskState.task}
                className="min-h-8 px-2 py-1 text-xs"
                onCanceled={() => simTaskState.refetch().then(() => undefined)}
              />
            </>
          )}
          {simTaskState.pollError && (
            <span className="flex items-center gap-2 text-sm text-warning">
              状态更新暂时失败，正在重试
              <Button variant="ghost" className="px-2 py-1" onClick={() => void simTaskState.refetch()}>
                立即重试
              </Button>
            </span>
          )}
        </div>
        {!runsValid && (
          <p className="mt-2 text-xs text-danger">
            模拟次数必须是 1000 至 100000 之间的整数。
          </p>
        )}
        {simPanelError && (
          <Alert variant="danger" className="mt-3">
            <div className="flex flex-wrap items-center gap-3">
              <span>{simPanelError}</span>
              <Button
                variant="ghost"
                className="px-2 py-1"
                disabled={simBusy}
                onClick={() => {
                  clearJobError("sim");
                  startMut.mutate();
                }}
              >
                重试
              </Button>
            </div>
          </Alert>
        )}
        {!simPanelError && selectedRun?.task_status === "failed" && (
          <Alert variant="danger" className="mt-3">
            模拟失败：
            {selectedRun.task_error_message ||
              selectedRun.task_error_code ||
              "未知错误"}
          </Alert>
        )}
        {selectedRun?.task_status === "canceled" && (
          <Alert variant="warning" className="mt-3">
            该次模拟已取消。
          </Alert>
        )}
        {simsQ.isError && !simsQ.data && (
          <Alert variant="danger" className="mt-3">
            <div className="flex flex-wrap items-center gap-3">
              <span>
                无法加载历史模拟结果：{queryErrorMessage(simsQ.error)}
              </span>
              <Button
                variant="ghost"
                className="px-2 py-1"
                onClick={() => void simsQ.refetch()}
              >
                重新加载
              </Button>
            </div>
          </Alert>
        )}
      </section>

      {latest?.result_stale && <StaleBanner />}

      {latest && simCompleted && (
        <section className="rounded-lg border border-line bg-surface p-4">
          <h2 className="flex items-center font-medium text-ink">
            模拟结果
            <MetricHelp termKey="fire_success_rate" />
          </h2>
          {latest.summary_json?.success_probability !== undefined && (
            <div className="mt-2">
              <p className="text-2xl font-semibold text-ink">
                成功率 {formatPercent(latest.summary_json.success_probability)}
              </p>
              {typeof latest.summary_json.success_wilson_low === "number" && typeof latest.summary_json.success_wilson_high === "number" ? (
                <p className="mt-1 flex items-center text-sm text-ink-muted">
                  Wilson 95% 区间 {formatPercent(latest.summary_json.success_wilson_low)}–{formatPercent(latest.summary_json.success_wilson_high)}
                  <MetricHelp termKey="wilson_interval" />
                </p>
              ) : null}
            </div>
          )}

          <CalculationExplanation
            className="mt-4"
            summary="本次结果用同一份冻结计划与市场快照生成多条收益、通胀和现金流路径，并统计满足 FIRE 条件的样本比例。"
            answer="在当前计划、配置和模型假设下，有限模拟样本中有多少路径在规划期内未耗尽并满足期末最低资产。"
            changed="每条路径的资产收益、汇率与随机通胀会按模型生成不同序列。"
            fixed="计划现金流、目标配置、市场统计快照、假设 Profile、运行路径数和 seed 在本次运行中冻结。"
            data={`来源运行 ${latest.id}；${latest.runs.toLocaleString()} 条路径；规划 ${latest.horizon_months.toLocaleString()} 个月；seed ${latest.seed}。`}
            criterion="成功路径必须在整个规划期内能够支付现金流，并在终点达到期末最低资产目标；成功率是成功路径数除以完成路径数。"
            uncertainty="Wilson 区间只描述有限路径的抽样不确定性，不覆盖模型、市场制度或输入判断错误；历史和前瞻假设都不是未来保证。"
            nextStep="查看财富分位、代表路径和模型提示；计划或市场输入变化后重新运行，再使用改善器或达标前沿。"
            audit={`引擎 ${latest.engine_version}；输入 ${latest.input_hash}；配置 ${latest.current_config_hash}；市场 ${latest.market_snapshot_hash}`}
          />

          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              href={`/plans/${planId}/improvement?simulation_run_id=${encodeURIComponent(latest.id)}`}
              disabled={latest.result_stale}
            >
              改善计划
            </Button>
            <Button
              variant="secondary"
              href={`/plans/${planId}/frontier?simulation_run_id=${encodeURIComponent(latest.id)}`}
              disabled={latest.result_stale}
            >
              达标前沿
            </Button>
          </div>

          {latest.assumption && (
            <RunAssumptionCard assumption={latest.assumption} />
          )}

          <CaliberToggle
            value={caliber}
            onChange={setCaliber}
            hasReal={hasReal}
          />

          {chartSeries && (
            <div className="mt-4">
              <WealthPathChart series={chartSeries} caliber={effectiveCaliber} />
            </div>
          )}
          {((latest.summary_json?.model_warnings as string[] | undefined) ?? [])
            .length > 0 && (
            <Alert variant="warning" title="模型提示" className="mt-4">
              <ul className="list-disc pl-5">
                {(latest.summary_json?.model_warnings as string[]).map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            </Alert>
          )}
          {repPaths.length > 0 && (
            <div className="mt-4">
              <h3 className="text-sm font-medium text-ink-muted"><HelpLabel label="代表路径" termKey="representative_path" /></h3>
              <p className="mt-1 text-xs text-ink-muted">
                每项为期末资产最接近对应分位数的实际模拟路径，可点击查看完整过程。
              </p>
              <ul className="mt-2 flex flex-wrap gap-2">
                {repPaths.map((p) => (
                  <li key={p.path_no}>
                    <Button
                      variant="secondary"
                      className="px-2 py-1"
                      onClick={() =>
                        router.push(
                          `/plans/${planId}/analysis/${latest.id}/paths/${p.path_no}`,
                        )
                      }
                    >
                      {p.representative_percentile?.toUpperCase()} ·{" "}
                      {formatMoneyWan(p.terminal_wealth_minor)}
                    </Button>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <ScenarioComparisonCard
            planId={planId}
            runId={latest.id}
            inputHash={latest.input_hash}
          />
        </section>
      )}

      <AnalysisJobPanel
        title="压力测试"
        termKey="stress_test"
        activeTaskId={activeStressTaskID}
        taskState={stressTaskState}
        panelError={taskErrors.stress ?? stressTaskState.error}
        onRetry={() => {
          clearJobError("stress");
          stressMut.mutate();
        }}
        onRun={() => stressMut.mutate()}
        running={stressMut.isPending}
        restoring={stressRestoring}
        restoreError={stressRestore.restoreError}
        onRetryRestore={() => void stressRestore.retryRestore()}
        runDisabled={attachDisabled || stressRestoreBlocked}
        runDisabledHint={attachHint}
        latest={latestStress}
        listError={
          stressQ.isError && !stressQ.data
            ? queryErrorMessage(stressQ.error)
            : null
        }
        onReloadList={() => void stressQ.refetch()}
      />

      <AnalysisJobPanel
        title="敏感性测试"
        termKey="sensitivity_test"
        activeTaskId={activeSensitivityTaskID}
        taskState={sensTaskState}
        panelError={taskErrors.sensitivity ?? sensTaskState.error}
        onRetry={() => {
          clearJobError("sensitivity");
          sensMut.mutate();
        }}
        onRun={() => sensMut.mutate()}
        running={sensMut.isPending}
        restoring={sensitivityRestoring}
        restoreError={sensitivityRestore.restoreError}
        onRetryRestore={() => void sensitivityRestore.retryRestore()}
        runDisabled={attachDisabled || sensitivityRestoreBlocked}
        runDisabledHint={attachHint}
        latest={latestSens}
        listError={
          sensQ.isError && !sensQ.data ? queryErrorMessage(sensQ.error) : null
        }
        onReloadList={() => void sensQ.refetch()}
      />
    </div>
  );
}
