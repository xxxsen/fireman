"use client";

import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { AllocationBarChart } from "@/components/charts/AllocationBarChart";
import { RegionAllocationBarChart } from "@/components/charts/RegionAllocationBarChart";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { useJobStatus } from "@/hooks/useJobStatus";
import { getDashboard } from "@/lib/api/dashboard";
import { formatMoney, formatPercent } from "@/lib/format";

export default function OverviewPage() {
  const planId = useParams().id as string;
  const searchParams = useSearchParams();
  const pendingJobId = searchParams.get("job_id");
  const simulationStartFailed = searchParams.get("simulation_error") === "1";
  const { data, isLoading, error } = useQuery({
    queryKey: ["dashboard", planId],
    queryFn: () => getDashboard(planId),
  });
  const pendingJob = useJobStatus(pendingJobId);

  if (isLoading) return <p className="text-slate-600">加载组合总览…</p>;
  if (error || !data) {
    return (
      <p className="text-red-600">
        加载失败：{error instanceof Error ? error.message : "未知错误"}
      </p>
    );
  }

  const enabledHoldings = data.allocation_bars.length > 0;
  const failedChecks = data.weight_checks.checks.filter((check) => !check.passed);
  const simulationSettingsHref = `/plans/${planId}/settings?section=simulation`;
  const activeExecution = data.active_rebalance_execution;
  const rebalanceHref = activeExecution
    ? `/plans/${planId}/rebalance/executions/${activeExecution.id}`
    : `/plans/${planId}/rebalance`;

  return (
    <div className="space-y-6">
      {!data.weight_checks.passed && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
          {failedChecks.map((check) => check.message).join("；")}
          <Link
            href={`/plans/${planId}/settings?section=plan-targets`}
            className="ml-2 font-medium underline"
          >
            检查计划目标配置
          </Link>
        </div>
      )}

      {pendingJobId && pendingJob.job?.status !== "succeeded" && (
        <div className="rounded-lg border border-sky-200 bg-sky-50 px-4 py-3 text-sm text-sky-900">
          FIRE 模拟正在后台运行：{Math.round(pendingJob.progress * 100)}%。
          <Link href={simulationSettingsHref} className="ml-2 font-medium underline">
            前往计划设置查看
          </Link>
        </div>
      )}
      {pendingJobId && pendingJob.job?.status === "succeeded" && (
        <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-900">
          FIRE 模拟已完成。
          <Link href={simulationSettingsHref} className="ml-2 font-medium underline">
            在计划设置中查看结果
          </Link>
        </div>
      )}
      {simulationStartFailed && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
          计划已创建，但 FIRE 模拟未能启动。
          <Link href={simulationSettingsHref} className="ml-2 font-medium underline">
            前往计划设置重新运行
          </Link>
        </div>
      )}

      <section className="rounded-lg bg-slate-100 px-5 py-4">
        <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <dt className="flex items-center text-sm text-slate-500">
              计划基准规模
              <MetricHelp termKey="configured_total_assets" />
            </dt>
            <dd className="mt-1 text-lg font-semibold">
              {formatMoney(data.parameters.total_assets_minor, data.plan.base_currency)}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-slate-500">
              已投资金
              <MetricHelp termKey="invested_minor" />
            </dt>
            <dd className="mt-1 text-lg font-semibold">
              {formatMoney(data.invested_minor ?? 0, data.plan.base_currency)}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-slate-500">
              已投资金占比
            </dt>
            <dd className="mt-1 text-lg font-semibold">
              {formatPercent(data.invested_ratio ?? 0)}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-slate-500">
              需调仓标的
              <MetricHelp termKey="actionable_rebalance" />
            </dt>
            <dd className="mt-1 text-lg font-semibold">
              {data.rebalance_summary.actionable_count > 0 ? (
                <Link
                  href={rebalanceHref}
                  className="underline decoration-slate-400 underline-offset-2 hover:decoration-slate-900"
                  data-testid="actionable-rebalance-link"
                >
                  {data.rebalance_summary.actionable_count}
                </Link>
              ) : (
                data.rebalance_summary.actionable_count
              )}
            </dd>
          </div>
        </dl>
      </section>

      {!enabledHoldings ? (
        <section className="rounded-lg border border-dashed border-slate-300 p-8 text-center">
          <h2 className="font-medium">持仓尚未配置</h2>
          <Link
            href={`/plans/${planId}/asset-refresh`}
            className="mt-4 inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm text-white"
          >
            资产变更
          </Link>
        </section>
      ) : (
        <>
          <section className="grid gap-6 md:grid-cols-2">
            <div className="rounded-lg border border-slate-200 p-4">
              <h2 className="flex items-center text-lg font-medium">
                大类配置
                <MetricHelp termKey="asset_class_allocation" />
              </h2>
              <AllocationBarChart bars={data.allocation_bars} />
            </div>
            <div className="rounded-lg border border-slate-200 p-4">
              <h2 className="flex items-center text-lg font-medium">
                国内 / 国外配置
                <MetricHelp termKey="region_allocation" />
              </h2>
              <RegionAllocationBarChart bars={data.region_bars} />
            </div>
          </section>

          <section className="rounded-lg border border-slate-200 p-4">
            <h2 className="flex items-center text-lg font-medium">
              结构偏离最大
              <MetricHelp termKey="structural_gap_row" />
            </h2>
            {data.top_deviations.length > 0 ? (
              <ul className="mt-3 divide-y divide-slate-100">
                {data.top_deviations.map((deviation) => (
                  <li
                    key={`${deviation.instrument_code}:${deviation.instrument_name}`}
                    className="flex flex-wrap items-center justify-between gap-2 py-3 text-sm"
                  >
                    <span>
                      {deviation.instrument_name}{" "}
                      <span className="text-slate-500">({deviation.instrument_code})</span>
                    </span>
                    <Link
                      href={rebalanceHref}
                      className="text-right underline decoration-slate-300 underline-offset-2 hover:decoration-slate-900"
                      data-testid="deviation-amount-link"
                    >
                      <strong
                        className={
                          deviation.deviation_amount_minor >= 0
                            ? "text-emerald-700"
                            : "text-red-700"
                        }
                      >
                        {deviation.deviation_amount_minor >= 0 ? "还差 " : "超出 "}
                        {formatMoney(Math.abs(deviation.deviation_amount_minor))}
                      </strong>
                      <span className="ml-2 text-xs text-slate-500">
                        偏离 {formatPercent(Math.abs(deviation.deviation_weight))}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-3 text-sm text-slate-600">当前持仓与目标配置一致。</p>
            )}
          </section>
        </>
      )}

      {data.data_warnings.length > 0 && (
        <section className="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <h2 className="font-medium text-amber-900">数据质量警告</h2>
          <ul className="mt-2 list-disc pl-5 text-sm text-amber-800">
            {data.data_warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
