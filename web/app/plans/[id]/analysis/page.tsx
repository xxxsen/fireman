"use client";

import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { queryErrorMessage } from "@/lib/query-error";
import { WealthPathChart } from "@/components/charts/WealthPathChart";
import {
  ParameterCurvesChart,
  SensitivityHeatmap,
  TornadoChart,
} from "@/components/charts/SensitivityCharts";
import { useJobStatus } from "@/hooks/useJobStatus";
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
import { formatMoney, formatPercent, historyDepthLabel } from "@/lib/format";
import type { SimulationRun } from "@/types/api";

type JobKind = "sim" | "stress" | "sensitivity";

function simulationOptionLabel(run: SimulationRun): string {
  const date = new Date(run.created_at).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
  const success = run.summary_json?.success_probability;
  const tail =
    typeof success === "number" ? `成功率 ${formatPercent(success)}` : "进行中";
  return `${date} · ${run.runs} 次 · ${tail}`;
}

function AnalysisJobPanel({
  title,
  termKey,
  activeJobId,
  jobBusy,
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
  jobBusy: boolean;
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
                    <th className="pr-3 py-1">场景</th>
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
          {termKey === "stress_test" && scenarios.length === 0 && (
            <div />
          )}
          {termKey !== "stress_test" && scenarios.length > 0 && (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-xs">
                <thead>
                  <tr className="text-ink-muted">
                    <th className="pr-4 py-1">场景</th>
                    <th className="pr-4 py-1">成功率</th>
                    <th className="pr-4 py-1">相对基准</th>
                  </tr>
                </thead>
                <tbody>
                  {scenarios.map((s) => (
                    <tr key={String(s.scenario_id)} className="border-t">
                      <td className="py-1 pr-4">{String(s.scenario_name ?? s.scenario_id)}</td>
                      <td className="py-1 pr-4">
                        {formatPercent((s.success_probability as number) ?? 0)}
                      </td>
                      <td className="py-1 pr-4">
                        {formatPercent((s.baseline_delta as number) ?? 0)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {termKey !== "sensitivity_test" && tornado.length > 0 && (
            <ul className="space-y-1">
              {tornado.slice(0, 5).map((t) => (
                <li key={String(t.parameter_id)}>
                  {String(t.parameter_name)}：{formatPercent((t.low_success as number) ?? 0)} –{" "}
                  {formatPercent((t.high_success as number) ?? 0)}
                </li>
              ))}
            </ul>
          )}
          {termKey !== "sensitivity_test" && heatmap.length > 0 && (
            <div className="overflow-x-auto">
              <p className="mb-1 text-xs text-ink-muted">支出 × 收益敏感性热力图（成功率）</p>
              <table className="min-w-full text-xs">
                <tbody>
                  {heatmap.map((row, ri) => (
                    <tr key={ri}>
                      {row.map((cell, ci) => (
                        <td
                          key={ci}
                          className="border px-2 py-1 text-center"
                          title={`${String(cell.spending_label)} / ${String(cell.return_label)}`}
                        >
                          {formatPercent((cell.success_probability as number) ?? 0)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {termKey !== "sensitivity_test" && curves.length > 0 && (
            <ul className="space-y-1 text-xs text-ink-muted">
              {curves.slice(0, 3).map((c) => (
                <li key={String(c.parameter_id)}>
                  {String(c.parameter_name)}：已计算 {((c.points as unknown[]) ?? []).length} 个扰动点
                </li>
              ))}
            </ul>
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
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [activeJobKind, setActiveJobKind] = useState<"sim" | "stress" | "sensitivity" | null>(
    null,
  );
  const [runsOverride, setRunsOverride] = useState<number | null>(null);
  const [jobErrors, setJobErrors] = useState<Partial<Record<JobKind, string>>>({});
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);

  const paramsQ = useQuery({
    queryKey: ["parameters", planId],
    queryFn: () => getParameters(planId),
  });
  const holdingsQ = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });

  const serverRuns = paramsQ.data?.parameters.simulation_runs;
  const runs = runsOverride ?? (serverRuns && serverRuns >= 1000 ? serverRuns : 10000);

  const simsQ = useQuery({
    queryKey: ["simulations", planId],
    queryFn: () => listSimulations(planId),
  });

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

  const handleJobTerminal = (kind: JobKind | null, message?: string) => {
    setActiveJobId(null);
    setActiveJobKind(null);
    if (kind && message) {
      setJobErrors((prev) => ({ ...prev, [kind]: message }));
    }
    invalidateAll();
  };

  const jobState = useJobStatus(activeJobId, {
    onComplete: async () => {
      const kind = activeJobKind;
      if (kind) {
        clearJobError(kind);
      }
      if (kind === "stress" && activeJobId) {
        await getStressTest(activeJobId).catch(() => null);
      }
      if (kind === "sensitivity" && activeJobId) {
        await getSensitivityTest(activeJobId).catch(() => null);
      }
      handleJobTerminal(null);
    },
    onFailed: (msg) => handleJobTerminal(activeJobKind, msg),
    onCanceled: () => handleJobTerminal(null),
  });

  const startMut = useMutation({
    mutationFn: () => createSimulation(planId, { runs }),
    onSuccess: (res) => {
      clearJobError("sim");
      setActiveJobKind("sim");
      setActiveJobId(res.job_id);
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
      setActiveJobKind("stress");
      setActiveJobId(res.job_id);
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
      setActiveJobKind("sensitivity");
      setActiveJobId(res.job_id);
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

  const repPaths =
    pathsQ.data?.paths.filter((p) => p.representative_percentile) ?? [];

  const jobBusy = !!activeJobId;

  const simPanelError =
    jobErrors.sim ?? (activeJobKind === "sim" ? jobState.error : null);

  const snapshotWarningLabels = (() => {
    const labels: string[] = [];
    for (const h of holdingsQ.data?.holdings ?? []) {
      if (!h.enabled) {
        continue;
      }
      const name = h.instrument_name ?? h.instrument_id;
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
    return <LoadingState label="加载分析数据…" />;
  }

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-ink">模拟分析中心</h1>
        <Button href={`/plans/${planId}/overview`} variant="secondary">
          返回组合总览
        </Button>
      </div>

      <section className="rounded-lg border border-line bg-surface p-4">
        <h2 className="font-medium text-ink">Monte Carlo 模拟</h2>
        {snapshotWarningLabels.length > 0 && (
          <Alert variant="warning" className="mt-2">
            以下持仓历史样本有限，模拟结果长期不确定性较高：{snapshotWarningLabels.join("；")}
          </Alert>
        )}
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
          <label className="text-sm text-ink">
            模拟次数
            <input
              type="number"
              min={1000}
              max={100000}
              className="ml-2 rounded border border-line px-2 py-1"
              value={runs}
              onChange={(e) => setRunsOverride(Number(e.target.value))}
            />
          </label>
          <Button
            disabled={startMut.isPending || jobBusy}
            onClick={() => startMut.mutate()}
          >
            运行模拟
          </Button>
          {activeJobId && activeJobKind === "sim" && (
            <>
              <span className="text-sm text-ink-muted">
                {jobState.job?.status ?? "连接中"}… {Math.round(jobState.progress * 100)}%
              </span>
              <Button
                variant="ghost"
                className="px-2 py-1 text-danger"
                onClick={() => void cancelJob(activeJobId)}
              >
                取消
              </Button>
            </>
          )}
        </div>
        {simPanelError && (
          <Alert variant="danger" className="mt-3">
            <div className="flex flex-wrap items-center gap-3">
              <span>{simPanelError}</span>
              <Button
                variant="ghost"
                className="px-2 py-1"
                disabled={jobBusy}
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
          {latest.summary_json?.terminal_quantiles && (
            <dl className="mt-3 grid grid-cols-2 gap-2 text-sm sm:grid-cols-3">
              {Object.entries(latest.summary_json.terminal_quantiles).map(([k, v]) => (
                <div key={k}>
                  <dt className="text-ink-muted">{k.toUpperCase()}</dt>
                  <dd>{formatMoney(v)}</dd>
                </div>
              ))}
            </dl>
          )}
          {latest.summary_json?.monthly_wealth_quantiles && (
            <div className="mt-4">
              <WealthPathChart series={latest.summary_json.monthly_wealth_quantiles} />
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
                      {formatMoney(p.terminal_wealth_minor)}
                    </Button>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </section>
      )}

      <AnalysisJobPanel
        title="压力测试"
        termKey="stress_test"
        activeJobId={activeJobKind === "stress" ? activeJobId : null}
        jobBusy={jobBusy}
        jobState={jobState}
        panelError={
          jobErrors.stress ?? (activeJobKind === "stress" ? jobState.error : null)
        }
        onRetry={() => {
          clearJobError("stress");
          stressMut.mutate();
        }}
        onRun={() => stressMut.mutate()}
        running={stressMut.isPending}
        runDisabled={attachDisabled}
        runDisabledHint={attachHint}
        onCancel={activeJobId && activeJobKind === "stress" ? () => void cancelJob(activeJobId) : undefined}
        latest={latestStress}
        listError={stressQ.isError && !stressQ.data ? queryErrorMessage(stressQ.error) : null}
        onReloadList={() => void stressQ.refetch()}
      />

      <AnalysisJobPanel
        title="敏感性测试"
        termKey="sensitivity_test"
        activeJobId={activeJobKind === "sensitivity" ? activeJobId : null}
        jobBusy={jobBusy}
        jobState={jobState}
        panelError={
          jobErrors.sensitivity ??
          (activeJobKind === "sensitivity" ? jobState.error : null)
        }
        onRetry={() => {
          clearJobError("sensitivity");
          sensMut.mutate();
        }}
        onRun={() => sensMut.mutate()}
        running={sensMut.isPending}
        runDisabled={attachDisabled}
        runDisabledHint={attachHint}
        onCancel={
          activeJobId && activeJobKind === "sensitivity" ? () => void cancelJob(activeJobId) : undefined
        }
        latest={latestSens}
        listError={sensQ.isError && !sensQ.data ? queryErrorMessage(sensQ.error) : null}
        onReloadList={() => void sensQ.refetch()}
      />
    </div>
  );
}

export default function AnalysisPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  useEffect(() => {
    router.replace(`/plans/${planId}/settings?section=simulation`);
  }, [planId, router]);
  return <p className="text-ink-muted">正在前往计划设置…</p>;
}
