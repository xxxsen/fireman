"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { AllocationBarChart } from "@/components/charts/AllocationBarChart";
import { DonutChart } from "@/components/charts/DonutChart";
import { WealthPathChart } from "@/components/charts/WealthPathChart";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { getDashboard } from "@/lib/api/dashboard";
import { getJob, listPaths } from "@/lib/api/simulations";
import { formatMoney, formatPercent } from "@/lib/format";

export default function DashboardPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["dashboard", planId],
    queryFn: () => getDashboard(planId),
  });

  const sim = data?.latest_simulation;
  const jobQ = useQuery({
    queryKey: ["job", sim?.job_id],
    queryFn: () => getJob(sim!.job_id),
    enabled: !!sim?.job_id,
  });
  const pathsQ = useQuery({
    queryKey: ["paths", sim?.id],
    queryFn: () => listPaths(sim!.id),
    enabled: !!sim?.id && jobQ.data?.status === "succeeded",
  });

  if (isLoading) return <p className="text-slate-600">加载仪表盘…</p>;
  if (error || !data) {
    return <p className="text-red-600">加载失败：{error instanceof Error ? error.message : "未知错误"}</p>;
  }

  const summary = sim?.summary_json;
  const repPaths =
    pathsQ.data?.paths.filter((p) => p.representative_percentile) ?? [];
  const jobDurationMs =
    jobQ.data?.started_at && jobQ.data?.finished_at
      ? jobQ.data.finished_at - jobQ.data.started_at
      : null;

  return (
    <div className="space-y-8">
      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">配置状态</h2>
        <dl className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <dt className="text-sm text-slate-500">总资产</dt>
            <dd className="font-medium">{formatMoney(data.parameters.total_assets_minor, data.plan.base_currency)}</dd>
          </div>
          <div>
            <dt className="text-sm text-slate-500">当前场景</dt>
            <dd>{data.scenario_name ?? "未选择"}</dd>
          </div>
          <div>
            <dt className="text-sm text-slate-500">权重检查</dt>
            <dd>{data.weight_checks.passed ? "通过" : "未通过"}</dd>
          </div>
          <div>
            <dt className="text-sm text-slate-500">需调仓标的</dt>
            <dd>{data.rebalance_summary.actionable_count}</dd>
          </div>
          <div>
            <dt className="text-sm text-slate-500">持仓合计</dt>
            <dd>{formatMoney(data.holdings_sum_minor, data.plan.base_currency)}</dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-slate-500">
              持仓差额
              <MetricHelp termKey="unallocated_gap" />
            </dt>
            <dd className={data.holdings_gap_minor !== 0 ? "font-medium text-amber-800" : ""}>
              {formatMoney(data.holdings_gap_minor, data.plan.base_currency)}
            </dd>
          </div>
        </dl>
      </section>

      {sim?.result_stale && <StaleBanner />}

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="flex items-center text-lg font-medium">
          FIRE 结果
          <MetricHelp termKey="fire_success_rate" />
        </h2>
        {sim && summary?.success_probability !== undefined ? (
          <dl className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div>
              <dt className="text-sm text-slate-500">成功率</dt>
              <dd className="text-xl font-semibold">
                {formatPercent(summary.success_probability)}
                {summary.success_wilson_low !== undefined && (
                  <span className="ml-2 text-sm font-normal text-slate-500">
                    （95% CI {formatPercent(summary.success_wilson_low)}–
                    {formatPercent(summary.success_wilson_high ?? 0)}）
                  </span>
                )}
              </dd>
            </div>
            {summary.terminal_quantiles && (
              <>
                {(["p00", "p25", "p50", "p75", "p95"] as const).map((k) => (
                  <div key={k}>
                    <dt className="flex items-center text-sm text-slate-500">
                      {k.toUpperCase()}
                      <MetricHelp termKey="p_quantiles" />
                    </dt>
                    <dd>{formatMoney(summary.terminal_quantiles![k] ?? 0, data.plan.base_currency)}</dd>
                  </div>
                ))}
              </>
            )}
            {summary.failure_year_quantiles?.p50 !== undefined && (
              <div>
                <dt className="text-sm text-slate-500">失败路径首次失败年份 P50</dt>
                <dd>{summary.failure_year_quantiles.p50.toFixed(0)} 岁</dd>
              </div>
            )}
            {summary.max_drawdown_quantiles?.p95 !== undefined && (
              <div>
                <dt className="flex items-center text-sm text-slate-500">
                  P95 最大回撤
                  <MetricHelp termKey="max_drawdown" />
                </dt>
                <dd>{formatPercent(summary.max_drawdown_quantiles.p95)}</dd>
              </div>
            )}
          </dl>
        ) : (
          <p className="mt-4 text-sm text-slate-600">
            尚无模拟结果。
            <Link href={`/plans/${planId}/analysis`} className="ml-1 underline">
              前往分析中心运行模拟
            </Link>
          </p>
        )}
        {sim && (
          <div className="mt-3 space-y-1 text-xs text-slate-500">
            <p>
              引擎 {sim.engine_version} · {sim.runs} 次 · 种子 {sim.seed} · 配置版本{" "}
              {data.plan.config_version} · 估值日 {data.plan.valuation_date}
              {jobDurationMs != null && ` · 运行 ${(jobDurationMs / 1000).toFixed(1)}s`}
              {sim.result_stale && " · 结果可能已过期"}
            </p>
            <p>市场快照 {sim.market_snapshot_hash.slice(0, 12)}…</p>
            {summary?.correlation_disclaimer && <p>{summary.correlation_disclaimer}</p>}
            {summary?.model_warnings?.map((w) => (
              <p key={w} className="text-amber-700">
                {w}
              </p>
            ))}
            {sim.asset_participation && sim.asset_participation.length > 0 && (
              <ul className="list-disc pl-4">
                {sim.asset_participation.map((a) => (
                  <li key={a.holding_id}>
                    {a.instrument_id}：参与年度 {a.complete_years.join("、") || "—"}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        {repPaths.length > 0 && (
          <div className="mt-3">
            <h3 className="text-sm font-medium text-slate-600">代表路径</h3>
            <ul className="mt-2 flex flex-wrap gap-2">
              {repPaths.map((p) => (
                <li key={p.path_no}>
                  <button
                    type="button"
                    className="rounded border px-2 py-1 text-sm hover:bg-slate-50"
                    onClick={() =>
                      router.push(`/plans/${planId}/analysis/${sim!.id}/paths/${p.path_no}`)
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
        {sim && sim.success_count > 0 && repPaths.length === 0 && (
          <Link
            href={`/plans/${planId}/analysis`}
            className="mt-2 inline-block text-sm underline"
          >
            查看代表路径与分析中心 →
          </Link>
        )}
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">风险与敏感性</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <div className="rounded-md border border-slate-100 p-3">
            <h3 className="flex items-center text-sm font-medium">
              压力测试
              <MetricHelp termKey="stress_test" />
            </h3>
            {data.stress_test?.available ? (
              <dl className="mt-2 space-y-1 text-sm">
                <div>
                  <dt className="text-slate-500">基准成功率</dt>
                  <dd>{formatPercent(data.stress_test.baseline_success_probability ?? 0)}</dd>
                </div>
                {data.stress_test.worst_scenario_name && (
                  <div>
                    <dt className="text-slate-500">最差场景</dt>
                    <dd>{data.stress_test.worst_scenario_name}</dd>
                  </div>
                )}
                {data.stress_test.result_stale && (
                  <p className="text-amber-700">结果可能已过期</p>
                )}
              </dl>
            ) : (
              <p className="mt-2 text-sm text-slate-600">
                {data.stress_test?.message ?? "尚未运行压力测试"}
              </p>
            )}
          </div>
          <div className="rounded-md border border-slate-100 p-3">
            <h3 className="flex items-center text-sm font-medium">
              敏感性测试
              <MetricHelp termKey="sensitivity_test" />
            </h3>
            {data.sensitivity_test?.available ? (
              <dl className="mt-2 space-y-1 text-sm">
                <div>
                  <dt className="text-slate-500">基准成功率</dt>
                  <dd>{formatPercent(data.sensitivity_test.baseline_success_probability ?? 0)}</dd>
                </div>
                {data.sensitivity_test.top_parameters && data.sensitivity_test.top_parameters.length > 0 && (
                  <div>
                    <dt className="text-slate-500">影响最大参数</dt>
                    <dd>{data.sensitivity_test.top_parameters.join("、")}</dd>
                  </div>
                )}
                {data.sensitivity_test.result_stale && (
                  <p className="text-amber-700">结果可能已过期</p>
                )}
              </dl>
            ) : (
              <p className="mt-2 text-sm text-slate-600">
                {data.sensitivity_test?.message ?? "尚未运行敏感性测试"}
              </p>
            )}
          </div>
        </div>
        <Link href={`/plans/${planId}/analysis`} className="mt-3 inline-block text-sm underline">
          在分析中心运行压力/敏感性测试
        </Link>
      </section>

      <section className="grid gap-6 lg:grid-cols-2">
        <div className="rounded-lg border border-slate-200 p-4">
          <h2 className="text-lg font-medium">资产配置</h2>
          <AllocationBarChart bars={data.allocation_bars} />
        </div>
        <div className="rounded-lg border border-slate-200 p-4">
          <h2 className="text-lg font-medium">当前配置</h2>
          <DonutChart
            slices={data.allocation_bars.map((b) => ({
              name: b.asset_class,
              value: b.current_weight,
            }))}
          />
        </div>
      </section>

      {data.top_deviations.length > 0 && (
        <section className="rounded-lg border border-slate-200 p-4">
          <h2 className="text-lg font-medium">偏离最大的标的</h2>
          <ul className="mt-3 divide-y divide-slate-100">
            {data.top_deviations.map((d) => (
              <li key={d.instrument_code} className="flex justify-between py-2 text-sm">
                <span>
                  {d.instrument_name} ({d.instrument_code})
                </span>
                <span className={d.deviation_weight >= 0 ? "text-emerald-700" : "text-red-700"}>
                  {formatPercent(d.deviation_weight)} / {formatMoney(d.deviation_amount_minor)}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {summary?.monthly_wealth_quantiles && (
        <section className="rounded-lg border border-slate-200 p-4">
          <h2 className="text-lg font-medium">财富路径</h2>
          <WealthPathChart series={summary.monthly_wealth_quantiles} />
        </section>
      )}

      {data.data_warnings.length > 0 && (
        <section className="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <h2 className="font-medium text-amber-900">数据质量警告</h2>
          <ul className="mt-2 list-disc pl-5 text-sm text-amber-800">
            {data.data_warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </section>
      )}

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">下一步</h2>
        <div className="mt-3 flex flex-wrap gap-3">
          <Link href={`/plans/${planId}/rebalance`} className="rounded-md border px-3 py-2 text-sm">
            调仓检查
          </Link>
          <Link href="/assets" className="rounded-md border px-3 py-2 text-sm">
            更新市场数据
          </Link>
          <Link href={`/plans/${planId}/analysis`} className="rounded-md bg-slate-900 px-3 py-2 text-sm text-white">
            重新模拟
          </Link>
          <button
            type="button"
            onClick={() => void refetch()}
            className="rounded-md border px-3 py-2 text-sm"
          >
            刷新仪表盘
          </button>
        </div>
      </section>
    </div>
  );
}
