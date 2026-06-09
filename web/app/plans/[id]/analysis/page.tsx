"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { WealthPathChart } from "@/components/charts/WealthPathChart";
import { useJobStatus } from "@/hooks/useJobStatus";
import {
  createSensitivityTest,
  createStressTest,
  getSensitivityTest,
  getStressTest,
  listSensitivityTests,
  listStressTests,
} from "@/lib/api/analysis";
import {
  cancelJob,
  createSimulation,
  listPaths,
  listSimulations,
} from "@/lib/api/simulations";
import { formatMoney, formatPercent } from "@/lib/format";

function AnalysisJobPanel({
  title,
  termKey,
  activeJobId,
  jobState,
  onRun,
  running,
  onCancel,
  latest,
}: {
  title: string;
  termKey: "stress_test" | "sensitivity_test";
  activeJobId: string | null;
  jobState: ReturnType<typeof useJobStatus>;
  onRun: () => void;
  running: boolean;
  onCancel?: () => void;
  latest?: {
    status: string;
    result_stale?: boolean;
    result_json?: Record<string, unknown>;
  } | null;
}) {
  const report = latest?.result_json;
  const scenarios = (report?.scenarios as Array<Record<string, unknown>> | undefined) ?? [];
  const tornado = (report?.tornado as Array<Record<string, unknown>> | undefined) ?? [];
  const heatmap = (report?.heatmap as Array<Array<Record<string, unknown>>> | undefined) ?? [];
  const curves = (report?.curves as Array<Record<string, unknown>> | undefined) ?? [];

  return (
    <section className="rounded-lg border p-4">
      <h2 className="flex items-center font-medium">
        {title}
        <MetricHelp termKey={termKey} />
      </h2>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <button
          type="button"
          className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
          disabled={running || !!activeJobId}
          onClick={onRun}
        >
          运行{title}
        </button>
        {activeJobId && (
          <>
            <span className="text-sm text-slate-600">
              {jobState.job?.status ?? "连接中"}… {Math.round(jobState.progress * 100)}%
            </span>
            {onCancel && (
              <button type="button" className="text-sm text-red-600 underline" onClick={onCancel}>
                取消
              </button>
            )}
          </>
        )}
      </div>
      {jobState.error && <p className="mt-2 text-sm text-red-600">{jobState.error}</p>}
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
          {scenarios.length > 0 && (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-xs">
                <thead>
                  <tr className="text-slate-500">
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
          {tornado.length > 0 && (
            <ul className="space-y-1">
              {tornado.slice(0, 5).map((t) => (
                <li key={String(t.parameter_id)}>
                  {String(t.parameter_name)}：{formatPercent((t.low_success as number) ?? 0)} –{" "}
                  {formatPercent((t.high_success as number) ?? 0)}
                </li>
              ))}
            </ul>
          )}
          {termKey === "sensitivity_test" && heatmap.length > 0 && (
            <div className="overflow-x-auto">
              <p className="mb-1 text-xs text-slate-500">支出 × 收益敏感性热力图（成功率）</p>
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
          {termKey === "sensitivity_test" && curves.length > 0 && (
            <ul className="space-y-1 text-xs text-slate-600">
              {curves.slice(0, 3).map((c) => (
                <li key={String(c.parameter_id)}>
                  {String(c.parameter_name)}：已计算 {((c.points as unknown[]) ?? []).length} 个扰动点
                </li>
              ))}
            </ul>
          )}
          {typeof report.monte_carlo_std_error === "number" && (
            <p className="text-xs text-slate-500">
              MC 标准误 ±{formatPercent(report.monte_carlo_std_error as number)}
              {report.std_error_hint ? ` · ${String(report.std_error_hint)}` : ""}
            </p>
          )}
        </div>
      )}
      {!latest && !activeJobId && (
        <p className="mt-3 text-sm text-slate-600">尚无结果，点击上方按钮运行。</p>
      )}
    </section>
  );
}

export default function AnalysisPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const qc = useQueryClient();
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [activeJobKind, setActiveJobKind] = useState<"sim" | "stress" | "sensitivity" | null>(
    null,
  );
  const [runs, setRuns] = useState(1000);
  const [jobError, setJobError] = useState<string | null>(null);

  const simsQ = useQuery({
    queryKey: ["simulations", planId],
    queryFn: () => listSimulations(planId),
  });
  const stressQ = useQuery({
    queryKey: ["stress-tests", planId],
    queryFn: () => listStressTests(planId),
  });
  const sensQ = useQuery({
    queryKey: ["sensitivity-tests", planId],
    queryFn: () => listSensitivityTests(planId),
  });

  const latest = simsQ.data?.simulations[0];
  const latestStress = stressQ.data?.stress_tests[0];
  const latestSens = sensQ.data?.sensitivity_tests[0];

  const pathsQ = useQuery({
    queryKey: ["paths", latest?.id],
    queryFn: () => listPaths(latest!.id),
    enabled: !!latest?.id && latest.success_count > 0,
  });

  const invalidateAll = () => {
    void qc.invalidateQueries({ queryKey: ["simulations", planId] });
    void qc.invalidateQueries({ queryKey: ["stress-tests", planId] });
    void qc.invalidateQueries({ queryKey: ["sensitivity-tests", planId] });
    void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
  };

  const jobState = useJobStatus(activeJobId, {
    onComplete: async () => {
      setActiveJobId(null);
      setJobError(null);
      if (activeJobKind === "stress" && activeJobId) {
        await getStressTest(activeJobId).catch(() => null);
      }
      if (activeJobKind === "sensitivity" && activeJobId) {
        await getSensitivityTest(activeJobId).catch(() => null);
      }
      setActiveJobKind(null);
      invalidateAll();
    },
    onFailed: (msg) => setJobError(msg),
  });

  const startMut = useMutation({
    mutationFn: () => createSimulation(planId, { runs }),
    onSuccess: (res) => {
      setJobError(null);
      setActiveJobKind("sim");
      setActiveJobId(res.job_id);
    },
    onError: (e) => setJobError(e instanceof Error ? e.message : "启动失败"),
  });

  const stressMut = useMutation({
    mutationFn: () => createStressTest(planId, { runs }),
    onSuccess: (res) => {
      setJobError(null);
      setActiveJobKind("stress");
      setActiveJobId(res.job_id);
    },
    onError: (e) => setJobError(e instanceof Error ? e.message : "启动失败"),
  });

  const sensMut = useMutation({
    mutationFn: () => createSensitivityTest(planId, { runs }),
    onSuccess: (res) => {
      setJobError(null);
      setActiveJobKind("sensitivity");
      setActiveJobId(res.job_id);
    },
    onError: (e) => setJobError(e instanceof Error ? e.message : "启动失败"),
  });

  const repPaths =
    pathsQ.data?.paths.filter((p) => p.representative_percentile) ?? [];

  const jobBusy = !!activeJobId;

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">模拟分析中心</h1>
        <Link href={`/plans/${planId}/dashboard`} className="text-sm underline">
          返回仪表盘
        </Link>
      </div>

      <section className="rounded-lg border p-4">
        <h2 className="font-medium">Monte Carlo 模拟</h2>
        <div className="mt-3 flex flex-wrap items-end gap-4">
          <label className="text-sm">
            模拟次数
            <input
              type="number"
              min={1000}
              max={100000}
              className="ml-2 rounded border px-2 py-1"
              value={runs}
              onChange={(e) => setRuns(Number(e.target.value))}
            />
          </label>
          <button
            type="button"
            className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
            disabled={startMut.isPending || jobBusy}
            onClick={() => startMut.mutate()}
          >
            运行模拟
          </button>
          {activeJobId && activeJobKind === "sim" && (
            <>
              <span className="text-sm text-slate-600">
                {jobState.job?.status ?? "连接中"}… {Math.round(jobState.progress * 100)}%
              </span>
              <button
                type="button"
                className="text-sm text-red-600 underline"
                onClick={() => void cancelJob(activeJobId)}
              >
                取消
              </button>
            </>
          )}
        </div>
        {(jobState.error || jobError) && activeJobKind === "sim" && (
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <p className="text-sm text-red-600">{jobState.error ?? jobError}</p>
            <button
              type="button"
              className="text-sm underline"
              onClick={() => {
                setJobError(null);
                startMut.mutate();
              }}
            >
              重试
            </button>
          </div>
        )}
      </section>

      {latest?.result_stale && <StaleBanner />}

      {latest && latest.success_count > 0 && (
        <section className="rounded-lg border p-4">
          <h2 className="flex items-center font-medium">
            最新结果
            <MetricHelp termKey="fire_success_rate" />
          </h2>
          {latest.summary_json?.success_probability !== undefined && (
            <p className="mt-2 text-2xl font-semibold">
              成功率 {formatPercent(latest.summary_json.success_probability)}
            </p>
          )}
          {latest.summary_json?.monthly_wealth_quantiles && (
            <div className="mt-4">
              <WealthPathChart series={latest.summary_json.monthly_wealth_quantiles} />
            </div>
          )}
          {repPaths.length > 0 && (
            <div className="mt-4">
              <h3 className="text-sm font-medium text-slate-600">代表路径</h3>
              <ul className="mt-2 flex flex-wrap gap-2">
                {repPaths.map((p) => (
                  <li key={p.path_no}>
                    <button
                      type="button"
                      className="rounded border px-2 py-1 text-sm hover:bg-slate-50"
                      onClick={() =>
                        router.push(
                          `/plans/${planId}/analysis/${latest.id}/paths/${p.path_no}`,
                        )
                      }
                    >
                      {p.representative_percentile?.toUpperCase()} ·{" "}
                      {formatMoney(p.terminal_wealth_minor)}
                    </button>
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
        jobState={jobState}
        onRun={() => stressMut.mutate()}
        running={stressMut.isPending}
        onCancel={activeJobId && activeJobKind === "stress" ? () => void cancelJob(activeJobId) : undefined}
        latest={latestStress}
      />

      <AnalysisJobPanel
        title="敏感性测试"
        termKey="sensitivity_test"
        activeJobId={activeJobKind === "sensitivity" ? activeJobId : null}
        jobState={jobState}
        onRun={() => sensMut.mutate()}
        running={sensMut.isPending}
        onCancel={
          activeJobId && activeJobKind === "sensitivity" ? () => void cancelJob(activeJobId) : undefined
        }
        latest={latestSens}
      />
    </div>
  );
}
