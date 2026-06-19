"use client";

import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { AllocationBarChart } from "@/components/charts/AllocationBarChart";
import { RegionAllocationBarChart } from "@/components/charts/RegionAllocationBarChart";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { Alert } from "@/components/ui/Alert";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { useJobStatus } from "@/hooks/useJobStatus";
import { getDashboard } from "@/lib/api/dashboard";
import { formatMoney, formatMoneyScaled, formatPercent } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";

export default function OverviewPage() {
  const planId = useParams().id as string;
  const searchParams = useSearchParams();
  const pendingJobId = searchParams.get("job_id");
  const simulationStartFailed = searchParams.get("simulation_error") === "1";
  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["dashboard", planId],
    queryFn: () => getDashboard(planId),
  });
  const pendingJob = useJobStatus(pendingJobId);

  if (isLoading && !data) {
    return <LoadingState label="加载组合总览…" />;
  }
  if (isError && !data) {
    return (
      <ErrorState
        message="无法加载组合总览。请确认后端服务可用后重试。"
        onRetry={() => void refetch()}
        backHref="/"
        technicalDetail={queryErrorMessage(error)}
      />
    );
  }
  if (!data) return null;

  const enabledHoldings = data.allocation_bars.length > 0;
  const failedChecks = data.weight_checks.checks.filter((check) => !check.passed);
  const simulationSettingsHref = `/plans/${planId}/settings?section=simulation`;
  const activeExecution = data.active_rebalance_execution;
  const rebalanceHref = activeExecution
    ? `/plans/${planId}/rebalance/executions/${activeExecution.id}`
    : `/plans/${planId}/rebalance`;

  const simulationRunning =
    Boolean(pendingJobId) && pendingJob.job?.status !== "succeeded";
  const simulationSucceeded =
    Boolean(pendingJobId) && pendingJob.job?.status === "succeeded";

  // Fold same-screen notices to a single highest-priority banner:
  // blocking error > warning > in-progress > success.
  let topBanner: React.ReactNode = null;
  if (!data.weight_checks.passed) {
    topBanner = (
      <Alert variant="warning">
        {failedChecks.map((check) => check.message).join("；")}
        <Link
          href={`/plans/${planId}/settings?section=plan-targets`}
          className="ml-2 font-medium underline underline-offset-2"
        >
          检查计划目标配置
        </Link>
      </Alert>
    );
  } else if (simulationStartFailed) {
    topBanner = (
      <Alert variant="warning">
        计划已创建，但 FIRE 模拟未能启动。
        <Link href={simulationSettingsHref} className="ml-2 font-medium underline underline-offset-2">
          前往计划设置重新运行
        </Link>
      </Alert>
    );
  } else if (simulationRunning) {
    topBanner = (
      <Alert variant="info">
        FIRE 模拟正在后台运行：{Math.round(pendingJob.progress * 100)}%。
        <Link href={simulationSettingsHref} className="ml-2 font-medium underline underline-offset-2">
          前往计划设置查看
        </Link>
      </Alert>
    );
  } else if (simulationSucceeded) {
    topBanner = (
      <Alert variant="success">
        FIRE 模拟已完成。
        <Link href={simulationSettingsHref} className="ml-2 font-medium underline underline-offset-2">
          在计划设置中查看结果
        </Link>
      </Alert>
    );
  }

  return (
    <div className="content-enter space-y-6">
      {topBanner}

      <section className="rounded-lg bg-surface-muted px-5 py-4">
        <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <dt className="flex items-center text-sm text-ink-muted">
              计划基准规模
              <MetricHelp termKey="configured_total_assets" />
            </dt>
            <dd className="mt-1 text-lg font-semibold text-ink">
              {formatMoneyScaled(data.parameters.total_assets_minor, data.plan.base_currency)}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-ink-muted">
              已投资金
              <MetricHelp termKey="invested_minor" />
            </dt>
            <dd className="mt-1 text-lg font-semibold text-ink">
              {formatMoneyScaled(data.invested_minor ?? 0, data.plan.base_currency)}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-ink-muted">
              已投资金占比
              <MetricHelp termKey="invested_minor" />
            </dt>
            <dd className="mt-1 text-lg font-semibold text-ink">
              {formatPercent(data.invested_ratio ?? 0)}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-ink-muted">
              需调仓标的
              <MetricHelp termKey="actionable_rebalance" />
            </dt>
            <dd className="mt-1 text-lg font-semibold text-ink">
              {data.rebalance_summary.actionable_count > 0 ? (
                <Link
                  href={rebalanceHref}
                  className="text-brand underline underline-offset-2"
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
        <EmptyState
          title="持仓尚未配置"
          description="录入账户真实资产后即可查看大类配置、地区分布与调仓建议。"
          action={{ label: "资产变更", href: `/plans/${planId}/asset-refresh` }}
        />
      ) : (
        <>
          <section className="grid gap-6 md:grid-cols-2">
            <div className="rounded-lg border border-line bg-surface p-4">
              <h2 className="flex items-center text-lg font-medium text-ink">
                大类配置
                <MetricHelp termKey="asset_class_allocation" />
              </h2>
              <AllocationBarChart
                bars={data.allocation_bars}
                currency={data.plan.base_currency}
              />
            </div>
            <div className="rounded-lg border border-line bg-surface p-4">
              <h2 className="flex items-center text-lg font-medium text-ink">
                国内 / 国外配置
                <MetricHelp termKey="region_allocation" />
              </h2>
              <RegionAllocationBarChart bars={data.region_bars} />
            </div>
          </section>

          <section className="rounded-lg border border-line bg-surface p-4">
            <h2 className="flex items-center text-lg font-medium text-ink">
              结构偏离最大
              <MetricHelp termKey="structural_gap_row" />
            </h2>
            {data.top_deviations.length > 0 ? (
              <ul className="mt-3 divide-y divide-line">
                {data.top_deviations.map((deviation) => (
                  <li
                    key={`${deviation.instrument_code}:${deviation.instrument_name}`}
                    className="flex flex-wrap items-center justify-between gap-2 py-3 text-sm"
                  >
                    <span className="text-ink">
                      {deviation.instrument_name}{" "}
                      <span className="text-ink-muted">({deviation.instrument_code})</span>
                    </span>
                    <Link
                      href={rebalanceHref}
                      className="text-right underline decoration-line underline-offset-2 hover:decoration-ink"
                      data-testid="deviation-amount-link"
                    >
                      <strong
                        className={
                          deviation.deviation_amount_minor >= 0
                            ? "text-positive"
                            : "text-danger"
                        }
                      >
                        {deviation.deviation_amount_minor >= 0 ? "还差 " : "超出 "}
                        {formatMoney(Math.abs(deviation.deviation_amount_minor))}
                      </strong>
                      <span className="ml-2 text-xs text-ink-muted">
                        偏离 {formatPercent(Math.abs(deviation.deviation_weight))}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-3 text-sm text-ink-muted">当前持仓与目标配置一致。</p>
            )}
          </section>
        </>
      )}

      {data.data_warnings.length > 0 && (
        <Alert variant="warning" title="数据质量警告">
          <ul className="list-disc pl-5">
            {data.data_warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </Alert>
      )}
    </div>
  );
}
