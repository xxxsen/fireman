"use client";

import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
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
import { useJobStatus } from "@/hooks/useJobStatus";
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
  cancelJob,
  createSimulation,
  listPaths,
  listSimulations,
} from "@/lib/api/simulations";
import {
  formatDateTimeFromMs,
  formatMoney,
  formatMoneyWan,
  formatPercent,
  historyDepthLabel,
  sortRepresentativePaths,
} from "@/lib/format";
import type { SimulationRun } from "@/types/api";

type JobKind = "sim" | "stress" | "sensitivity";

function caliberLabel(c: "nominal" | "real"): string {
  return c === "real" ? "起点购买力" : "名义金额";
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
    <div className="mt-4 flex items-center gap-2 text-sm" role="group" aria-label="金额口径">
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
      <span className="text-xs text-ink-muted">起点购买力 = 名义金额 ÷ 路径累计通胀</span>
    </div>
  );
}

function RunAssumptionCard({
  assumption,
}: {
  assumption: NonNullable<SimulationRun["assumption"]>;
}) {
  const modeLabel = RETURN_MODE_LABELS[assumption.mode] ?? assumption.mode;
  const factorLabel =
    FACTOR_MODEL_LABELS[assumption.random_factor_model] ?? assumption.random_factor_model;
  const riskAssets = assumption.assets.filter((a) => !a.is_cash);
  return (
    <section className="mt-4 rounded-lg border border-line bg-surface-muted/40 p-3 text-sm">
      <h3 className="font-medium text-ink">本次模拟的收益假设</h3>
      <p className="mt-1 text-xs text-ink-muted">
        引擎 {assumption.engine_version} · {modeLabel}
        {assumption.profile_id ? ` · ${assumption.profile_id}@${assumption.profile_version}` : ""}
        {assumption.scenario ? ` · 假设情景 ${assumption.scenario}` : ""} · {factorLabel}
      </p>
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
                <th className="pr-3 py-1">历史 CAGR</th>
                <th className="pr-3 py-1">前瞻年化</th>
                <th className="pr-3 py-1">历史权重</th>
                <th className="pr-3 py-1">样本年数</th>
                <th className="pr-3 py-1">波动率</th>
              </tr>
            </thead>
            <tbody>
              {riskAssets.map((a) => (
                <tr key={a.holding_id} className="border-t">
                  <td className="py-1 pr-3">
                    {a.instrument_name || a.holding_id}
                    {a.instrument_code ? `（${a.instrument_code}）` : ""}
                  </td>
                  <td className="py-1 pr-3">{formatPercent(a.historical_annual_geometric_return)}</td>
                  <td className="py-1 pr-3 font-medium">
                    {formatPercent(a.forward_annual_geometric_return)}
                  </td>
                  <td className="py-1 pr-3">{formatPercent(a.historical_weight)}</td>
                  <td className="py-1 pr-3">{a.sample_years || "—"}</td>
                  <td className="py-1 pr-3">{formatPercent(a.annual_volatility_used)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="mt-2 text-xs text-ink-muted">
        历史收益不代表未来；前瞻年化为历史与长期先验在对数空间的收缩结果，仅用于本次模拟。
      </p>
    </section>
  );
}

function simulationOptionLabel(run: SimulationRun): string {
  const date = formatDateTimeFromMs(run.created_at);
  const success = run.summary_json?.success_probability;
  const tail =
    typeof success === "number" ? `成功率 ${formatPercent(success)}` : "进行中";
  return `${date} · ${run.runs} 次 · ${tail}`;
}

function AnalysisJobPanel({
  title,
  termKey,
  activeJobId,
  jobState,
  panelError,
  onRetry,
  onRun,
  running,
  runDisabled,
  runDisabledHint,
  onCancel,
  latest,
  listError,
  onReloadList,
}: {
  title: string;
  termKey: "stress_test" | "sensitivity_test";
  activeJobId: string | null;
  jobState: ReturnType<typeof useJobStatus>;
  panelError?: string | null;
  onRetry?: () => void;
  onRun: () => void;
  running: boolean;
  runDisabled?: boolean;
  runDisabledHint?: string;
  onCancel?: () => void;
  latest?: {
    status: string;
    result_stale?: boolean;
    result_json?: Record<string, unknown>;
  } | null;
  listError?: string | null;
  onReloadList?: () => void;
}) {
  const jobBusy = !!activeJobId;
  const report = latest?.result_json;
  const scenarios = (report?.scenarios as Array<Record<string, unknown>> | undefined) ?? [];
  const tornado = (report?.tornado as Array<Record<string, unknown>> | undefined) ?? [];
  const heatmap = (report?.heatmap as Array<Array<Record<string, unknown>>> | undefined) ?? [];
  const curves = (report?.curves as Array<Record<string, unknown>> | undefined) ?? [];

  const worstId = report?.worst_scenario_id as string | undefined;

  return (
    <section className="rounded-lg border border-line bg-surface p-4">
      <h2 className="flex items-center font-medium text-ink">
        {title}
        <MetricHelp termKey={termKey} />
      </h2>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button disabled={running || jobBusy || runDisabled} onClick={onRun}>
          运行{title}
        </Button>
        {runDisabled && runDisabledHint && (
          <span className="text-sm text-ink-muted">{runDisabledHint}</span>
        )}
        {activeJobId && (
          <>
            <span className="text-sm text-ink-muted">
              {jobState.job?.status ?? "连接中"}… {Math.round(jobState.progress * 100)}%
            </span>
            {onCancel && (
              <Button variant="ghost" className="px-2 py-1 text-danger" onClick={onCancel}>
                取消
              </Button>
            )}
          </>
        )}
      </div>
      {panelError && (
        <Alert variant="danger" className="mt-3">
          <div className="flex flex-wrap items-center gap-3">
            <span>{panelError}</span>
            {onRetry && (
              <Button variant="ghost" className="px-2 py-1" disabled={jobBusy} onClick={onRetry}>
                重试
              </Button>
            )}
          </div>
        </Alert>
      )}
      {listError && (
        <Alert variant="danger" className="mt-3">
          <div className="flex flex-wrap items-center gap-3">
            <span>无法加载{title}结果：{listError}</span>
            {onReloadList && (
              <Button variant="ghost" className="px-2 py-1" onClick={onReloadList}>
                重新加载
              </Button>
            )}
          </div>
        </Alert>
      )}
      {latest?.result_stale && <StaleBanner />}
      {latest?.status === "succeeded" && report && (
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
                    <th className="pr-3 py-1">成功率</th>
                    <th className="pr-3 py-1">相对基准</th>
                    <th className="pr-3 py-1">终值 P25/P50/P95</th>
                    <th className="pr-3 py-1">P95 回撤</th>
                    <th className="pr-3 py-1">失败年份 P50</th>
                    <th className="pr-3 py-1">恢复期 P50</th>
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
                          {isWorst && <span className="ml-1 text-danger">（最差）</span>}
                        </td>
                        <td className="py-1 pr-3">{formatPercent((s.success_probability as number) ?? 0)}</td>
                        <td className="py-1 pr-3">{formatPercent((s.baseline_delta as number) ?? 0)}</td>
                        <td className="py-1 pr-3">
                          {formatMoney((s.terminal_p25_minor as number) ?? 0)} /{" "}
                          {formatMoney((s.terminal_p50_minor as number) ?? 0)} /{" "}
                          {formatMoney((s.terminal_p95_minor as number) ?? 0)}
                        </td>
                        <td className="py-1 pr-3">{formatPercent((s.max_drawdown_p95 as number) ?? 0)}</td>
                        <td className="py-1 pr-3">
                          {s.failure_year_p50 ? String(s.failure_year_p50) : "—"}
                        </td>
                        <td className="py-1 pr-3">
                          {s.recovery_not_within_plan
                            ? "规划期内未恢复"
                            : s.recovery_month_p50 != null
                              ? `${String(s.recovery_month_p50)} 月`
                              : "—"}
                        </td>
                        <td className="py-1 pr-3 max-w-xs">{String(s.description ?? "")}</td>
                        <td className="py-1 pr-3 max-w-xs text-warning">{String(s.risk_hint ?? "")}</td>
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
                low_success: (t.low_success as number) ?? 0,
                high_success: (t.high_success as number) ?? 0,
              }))}
            />
          )}
          {termKey === "sensitivity_test" && curves.length > 0 && (
            <ParameterCurvesChart
              curves={curves.map((c) => ({
                parameter_name: String(c.parameter_name),
                points: ((c.points as Array<Record<string, unknown>>) ?? []).map((p) => ({
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
                  success_probability: (cell.success_probability as number) ?? 0,
                })),
              )}
            />
          )}
          {typeof report.monte_carlo_std_error === "number" && (
            <p className="text-xs text-ink-muted">
              MC 标准误 ±{formatPercent(report.monte_carlo_std_error as number)}
              {report.std_error_hint ? ` · ${String(report.std_error_hint)}` : ""}
            </p>
          )}
        </div>
      )}
      {!latest && !activeJobId && !listError && (
        <p className="mt-3 text-sm text-ink-muted">尚无结果，点击上方按钮运行。</p>
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
  const [activeJobs, setActiveJobs] = useState<Partial<Record<JobKind, string>>>({});
  const [runsDraft, setRunsDraft] = useState<string | null>(null);
  const [jobErrors, setJobErrors] = useState<Partial<Record<JobKind, string>>>({});
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
  const runsText = runsDraft ?? String(serverRuns && serverRuns >= 1000 ? serverRuns : 10000);
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

  // A run is displayable once its summary is persisted: summary_json carries a
  // numeric success_probability only on success. Job status drives the
  // pending/running indicator but never gates results already stored on the run.
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
    setJobErrors((prev) => {
      const next = { ...prev };
      delete next[kind];
      return next;
    });
  };

  // Jobs that already reached a terminal state in this session. The rebuild
  // effect below must skip them, otherwise a failed run in the list would be
  // re-adopted after every refetch and loop forever.
  const settledJobsRef = useRef<Set<string>>(new Set());

  const finishJob = (kind: JobKind, message?: string) => {
    setActiveJobs((prev) => {
      const jobId = prev[kind];
      if (jobId) {
        settledJobsRef.current.add(jobId);
      }
      const next = { ...prev };
      delete next[kind];
      return next;
    });
    if (message) {
      setJobErrors((prev) => ({ ...prev, [kind]: message }));
    }
    invalidateAll();
  };

  // Rebuild activeJobs from persisted records so that a page refresh does not
  // lose the progress bar and cancel button of jobs still running on the
  // backend. Jobs the user just started take precedence: a persisted job is
  // only adopted when no job of that kind is currently tracked.
  const simsData = simsQ.data;
  const stressData = stressQ.data;
  const sensData = sensQ.data;
  useEffect(() => {
    setActiveJobs((prev) => {
      const next = { ...prev };
      let changed = false;
      const adopt = (kind: JobKind, jobId: string | undefined) => {
        if (!jobId || next[kind] || settledJobsRef.current.has(jobId)) return;
        next[kind] = jobId;
        changed = true;
      };
      // A simulation run persists its summary only on success, so the newest
      // run having a job_id but no numeric success_probability means it is
      // still pending (or failed — attaching then surfaces the failure reason
      // once). Only the newest run is considered: creating a new run
      // supersedes older jobs, so an older run without a summary is a settled
      // failure whose banner must not resurface on every page visit.
      const newestSim = simsData?.simulations?.[0];
      adopt(
        "sim",
        newestSim?.job_id &&
          typeof newestSim.summary_json?.success_probability !== "number"
          ? newestSim.job_id
          : undefined,
      );
      adopt(
        "stress",
        (stressData?.stress_tests ?? []).find(
          (t) => t.job_id && (t.status === "queued" || t.status === "running"),
        )?.job_id,
      );
      adopt(
        "sensitivity",
        (sensData?.sensitivity_tests ?? []).find(
          (t) => t.job_id && (t.status === "queued" || t.status === "running"),
        )?.job_id,
      );
      return changed ? next : prev;
    });
  }, [simsData, stressData, sensData]);

  const simJobState = useJobStatus(activeJobs.sim ?? null, {
    onComplete: () => {
      clearJobError("sim");
      finishJob("sim");
    },
    onFailed: (msg) => finishJob("sim", msg),
    onCanceled: () => finishJob("sim"),
  });

  const stressJobState = useJobStatus(activeJobs.stress ?? null, {
    onComplete: async () => {
      clearJobError("stress");
      if (activeJobs.stress) {
        await getStressTest(activeJobs.stress).catch(() => null);
      }
      finishJob("stress");
    },
    onFailed: (msg) => finishJob("stress", msg),
    onCanceled: () => finishJob("stress"),
  });

  const sensJobState = useJobStatus(activeJobs.sensitivity ?? null, {
    onComplete: async () => {
      clearJobError("sensitivity");
      if (activeJobs.sensitivity) {
        await getSensitivityTest(activeJobs.sensitivity).catch(() => null);
      }
      finishJob("sensitivity");
    },
    onFailed: (msg) => finishJob("sensitivity", msg),
    onCanceled: () => finishJob("sensitivity"),
  });

  const startMut = useMutation({
    mutationFn: () => createSimulation(planId, { runs }),
    onSuccess: (res) => {
      clearJobError("sim");
      setActiveJobs((prev) => ({ ...prev, sim: res.job_id }));
      // Surface the new run immediately as a pending entry and select it, so the
      // page tracks this run from the start. The list is refetched on terminal
      // (handleJobTerminal -> invalidateAll), replacing it with the stored run.
      if (res.run_id) {
        const pendingRun: SimulationRun = {
          id: res.run_id,
          job_id: res.job_id,
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
    onError: (e) =>
      setJobErrors((prev) => ({
        ...prev,
        sim: e instanceof Error ? e.message : "启动失败",
      })),
  });

  const stressMut = useMutation({
    mutationFn: () => {
      if (!selectedRun) throw new Error("请先运行 Monte Carlo 模拟");
      return createStressTest(planId, { runs, simulation_run_id: selectedRun.id });
    },
    onSuccess: (res) => {
      clearJobError("stress");
      setActiveJobs((prev) => ({ ...prev, stress: res.job_id }));
    },
    onError: (e) =>
      setJobErrors((prev) => ({
        ...prev,
        stress: e instanceof Error ? e.message : "启动失败",
      })),
  });

  const sensMut = useMutation({
    mutationFn: () => {
      if (!selectedRun) throw new Error("请先运行 Monte Carlo 模拟");
      return createSensitivityTest(planId, { runs, simulation_run_id: selectedRun.id });
    },
    onSuccess: (res) => {
      clearJobError("sensitivity");
      setActiveJobs((prev) => ({ ...prev, sensitivity: res.job_id }));
    },
    onError: (e) =>
      setJobErrors((prev) => ({
        ...prev,
        sensitivity: e instanceof Error ? e.message : "启动失败",
      })),
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

  const simBusy = !!activeJobs.sim;

  const simPanelError = jobErrors.sim ?? simJobState.error;

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
        labels.push(`${name}（${code}）· ${historyDepthLabel(h.snapshot_history_depth)}`);
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

  if (paramsQ.isLoading || holdingsQ.isLoading || !paramsQ.data || !holdingsQ.data) {
    return <PageSkeleton label="加载分析数据…" />;
  }

  return (
    <div className="space-y-8">
      <section className="rounded-lg border border-line bg-surface p-4">
        <h2 className="font-medium text-ink">Monte Carlo 模拟</h2>
        {snapshotWarningLabels.length > 0 && (
          <Alert variant="warning" className="mt-2">
            以下持仓历史样本有限，模拟结果长期不确定性较高：{snapshotWarningLabels.join("；")}
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
            disabled={startMut.isPending || simBusy || !readinessReady || !runsValid}
            title={
              !runsValid
                ? "模拟次数必须是 1000 至 100000 之间的整数"
                : readinessQ.isLoading || readinessQ.isFetching
                  ? "正在检查模拟就绪状态"
                  : readinessQ.isError
                    ? "模拟就绪状态检查失败，请重试"
                    : !readinessReady
                      ? "部分持仓暂时无法用于模拟，请先按提示处理"
                      : undefined
            }
            onClick={() => startMut.mutate()}
          >
            运行模拟
          </Button>
          {activeJobs.sim && (
            <>
              <span className="text-sm text-ink-muted">
                {simJobState.job?.status ?? "连接中"}… {Math.round(simJobState.progress * 100)}%
              </span>
              <Button
                variant="ghost"
                className="px-2 py-1 text-danger"
                onClick={() => void cancelJob(activeJobs.sim!)}
              >
                取消
              </Button>
            </>
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
        {simsQ.isError && !simsQ.data && (
          <Alert variant="danger" className="mt-3">
            <div className="flex flex-wrap items-center gap-3">
              <span>无法加载历史模拟结果：{queryErrorMessage(simsQ.error)}</span>
              <Button variant="ghost" className="px-2 py-1" onClick={() => void simsQ.refetch()}>
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
            <p className="mt-2 text-2xl font-semibold text-ink">
              成功率 {formatPercent(latest.summary_json.success_probability)}
            </p>
          )}

          {latest.assumption && <RunAssumptionCard assumption={latest.assumption} />}

          <CaliberToggle value={caliber} onChange={setCaliber} hasReal={hasReal} />

          {chartSeries && (
            <div className="mt-4">
              <p className="mb-1 text-xs text-ink-muted">
                财富分位走势（{caliberLabel(effectiveCaliber)}，单位：元）
              </p>
              <WealthPathChart series={chartSeries} />
            </div>
          )}
          {((latest.summary_json?.model_warnings as string[] | undefined) ?? []).length > 0 && (
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
              <h3 className="text-sm font-medium text-ink-muted">代表路径</h3>
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

          <ScenarioComparisonCard planId={planId} />
        </section>
      )}

      <AnalysisJobPanel
        title="压力测试"
        termKey="stress_test"
        activeJobId={activeJobs.stress ?? null}
        jobState={stressJobState}
        panelError={jobErrors.stress ?? stressJobState.error}
        onRetry={() => {
          clearJobError("stress");
          stressMut.mutate();
        }}
        onRun={() => stressMut.mutate()}
        running={stressMut.isPending}
        runDisabled={attachDisabled}
        runDisabledHint={attachHint}
        onCancel={
          activeJobs.stress ? () => void cancelJob(activeJobs.stress!) : undefined
        }
        latest={latestStress}
        listError={stressQ.isError && !stressQ.data ? queryErrorMessage(stressQ.error) : null}
        onReloadList={() => void stressQ.refetch()}
      />

      <AnalysisJobPanel
        title="敏感性测试"
        termKey="sensitivity_test"
        activeJobId={activeJobs.sensitivity ?? null}
        jobState={sensJobState}
        panelError={jobErrors.sensitivity ?? sensJobState.error}
        onRetry={() => {
          clearJobError("sensitivity");
          sensMut.mutate();
        }}
        onRun={() => sensMut.mutate()}
        running={sensMut.isPending}
        runDisabled={attachDisabled}
        runDisabledHint={attachHint}
        onCancel={
          activeJobs.sensitivity
            ? () => void cancelJob(activeJobs.sensitivity!)
            : undefined
        }
        latest={latestSens}
        listError={sensQ.isError && !sensQ.data ? queryErrorMessage(sensQ.error) : null}
        onReloadList={() => void sensQ.refetch()}
      />
    </div>
  );
}
