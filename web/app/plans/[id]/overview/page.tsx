"use client";

import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AllocationBarChart } from "@/components/charts/AllocationBarChart";
import { RegionAllocationBarChart } from "@/components/charts/RegionAllocationBarChart";
import { AssetClassRegionGroups } from "@/components/charts/AssetClassRegionGroups";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import { useActiveTaskRestore } from "@/hooks/useActiveTaskRestore";
import { getDashboard } from "@/lib/api/dashboard";
import { formatMoney, formatMoneyScaled, formatPercent } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import { isTaskActive } from "@/lib/api/tasks";

/**
 * Shared header for the two side-by-side allocation cards: a title row plus a
 * fixed-height (20px) description row so both charts start at the same vertical
 * offset and the card bottoms align on desktop.
 */
function ChartCardHeader({
  title,
  termKey,
  description,
}: {
  title: string;
  termKey: string;
  description: string;
}) {
  return (
    <div>
      <h2 className="flex items-center text-lg font-medium text-ink">
        {title}
        <MetricHelp termKey={termKey} />
      </h2>
      <p className="mt-1 h-5 truncate text-xs leading-5 text-ink-muted">
        {description}
      </p>
    </div>
  );
}

export default function OverviewPage() {
  const planId = useParams().id as string;
  const qc = useQueryClient();
  const searchParams = useSearchParams();
  const pendingTaskId = searchParams.get("task_id");
  const simulationStartFailed = searchParams.get("simulation_error") === "1";
  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["dashboard", planId],
    queryFn: () => getDashboard(planId),
  });
  const businessTaskId = isTaskActive(data?.latest_simulation?.task_status)
    ? data?.latest_simulation?.task_id
    : null;
  const taskRestore = useActiveTaskRestore({
    workerType: "go_worker",
    taskType: "simulation",
    scopeType: "plan",
    scopeId: planId,
    businessTaskId,
    preferredTaskId: pendingTaskId,
  });
  // The restore query intentionally returns active tasks only. Keep the URL
  // task as a terminal-state hint too, so a refresh can still show completion
  // or failure feedback for the task that opened this page.
  const effectiveTaskId = taskRestore.taskId ?? businessTaskId ?? pendingTaskId;
  const pendingTask = useTaskStatus(effectiveTaskId, {
    initialTask: taskRestore.task,
    onComplete: () => {
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
      void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
    },
    onFailed: () => {
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
      void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
    },
    onCanceled: () => {
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
      void qc.invalidateQueries({ queryKey: ["active-task-restore"] });
    },
  });

  if (isLoading && !data) {
    return <PageSkeleton label="加载组合总览…" />;
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
  const failedChecks = data.weight_checks.checks.filter(
    (check) => !check.passed,
  );
  const simulationSettingsHref = `/plans/${planId}/settings?section=simulation`;
  const activeExecution = data.active_rebalance_execution;
  const rebalanceHref = activeExecution
    ? `/plans/${planId}/rebalance/executions/${activeExecution.id}`
    : `/plans/${planId}/rebalance`;

  // Explicit job-status branches: active → info, failed → warning,
  // succeeded → success, canceled → silent.
  const taskStatus = effectiveTaskId
    ? (pendingTask.task?.status ?? "pending")
    : null;
  const simulationRunning = isTaskActive(taskStatus);
  const simulationSucceeded = taskStatus === "complete";
  const simulationFailed = taskStatus === "failed";

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
          检查目标配置
        </Link>
      </Alert>
    );
  } else if (simulationStartFailed) {
    topBanner = (
      <Alert variant="warning">
        计划已创建，但 FIRE 模拟未能启动。
        <Link
          href={simulationSettingsHref}
          className="ml-2 font-medium underline underline-offset-2"
        >
          前往计划设置重新运行
        </Link>
      </Alert>
    );
  } else if (taskRestore.restoreError) {
    topBanner = (
      <Alert variant="warning">
        <span>模拟任务状态恢复失败。</span>
        <Button
          variant="ghost"
          className="ml-2 px-2 py-1"
          onClick={() => void taskRestore.retryRestore()}
        >
          重试状态检查
        </Button>
      </Alert>
    );
  } else if (taskRestore.restoring) {
    topBanner = <Alert variant="info">正在恢复模拟任务状态...</Alert>;
  } else if (simulationFailed) {
    topBanner = (
      <Alert variant="warning">
        FIRE 模拟运行失败{pendingTask.error ? `：${pendingTask.error}` : "。"}
        <Link
          href={simulationSettingsHref}
          className="ml-2 font-medium underline underline-offset-2"
        >
          前往计划设置重试
        </Link>
      </Alert>
    );
  } else if (simulationRunning) {
    topBanner = (
      <Alert variant="info">
        FIRE 模拟正在后台运行：{Math.round(pendingTask.progress * 100)}%。
        <Link
          href={simulationSettingsHref}
          className="ml-2 font-medium underline underline-offset-2"
        >
          前往计划设置查看
        </Link>
        <span className="ml-2 inline-flex">
          <TaskCancelButton
            task={pendingTask.task}
            className="min-h-8 px-2 py-1 text-xs"
            onCanceled={async () => {
              await pendingTask.refetch();
              await qc.invalidateQueries({ queryKey: ["dashboard", planId] });
            }}
          />
        </span>
        {pendingTask.pollError && (
          <Button
            variant="ghost"
            className="ml-2 px-2 py-1"
            onClick={() => void pendingTask.refetch()}
          >
            状态更新失败，立即重试
          </Button>
        )}
      </Alert>
    );
  } else if (simulationSucceeded) {
    topBanner = (
      <Alert variant="success">
        FIRE 模拟已完成。
        <Link
          href={simulationSettingsHref}
          className="ml-2 font-medium underline underline-offset-2"
        >
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
              {formatMoneyScaled(
                data.parameters.total_assets_minor,
                data.plan.base_currency,
              )}
            </dd>
          </div>
          <div>
            <dt className="flex items-center text-sm text-ink-muted">
              已投资金
              <MetricHelp termKey="invested_minor" />
            </dt>
            <dd className="mt-1 text-lg font-semibold text-ink">
              {formatMoneyScaled(
                data.invested_minor ?? 0,
                data.plan.base_currency,
              )}
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

      {data.latest_simulation && (
        <section className="border-b border-line pb-6">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <h2 className="flex items-center text-lg font-medium text-ink">
                FIRE 状态
                <MetricHelp termKey="fire_success_rate" />
              </h2>
              <p className="mt-1 text-sm text-ink-muted">
                最新模拟成功率 {formatPercent(
                  data.latest_simulation.summary_json.success_probability ?? 0,
                )}
                {data.latest_simulation.summary_json.success_wilson_low !== undefined &&
                  data.latest_simulation.summary_json.success_wilson_high !== undefined &&
                  `，95% 区间 ${formatPercent(data.latest_simulation.summary_json.success_wilson_low)} - ${formatPercent(data.latest_simulation.summary_json.success_wilson_high)}`}
              </p>
            </div>
            <Button
              href={`/plans/${planId}/improvement?simulation_run_id=${encodeURIComponent(data.latest_simulation.id)}`}
              disabled={
                data.latest_simulation.result_stale ||
                data.latest_simulation.task_status !== "complete"
              }
              title={
                data.latest_simulation.result_stale ||
                data.latest_simulation.task_status !== "complete"
                  ? "先运行当前计划模拟"
                  : undefined
              }
            >
              改善计划
            </Button>
          </div>
        </section>
      )}

      {!enabledHoldings ? (
        <EmptyState
          title="持仓尚未配置"
          description="录入账户真实资产后即可查看大类配置、地区分布与调仓建议。"
          action={{ label: "持仓校正", href: `/plans/${planId}/asset-refresh` }}
        />
      ) : (
        <>
          <section className="grid gap-6 md:grid-cols-2">
            <div className="rounded-lg border border-line bg-surface p-4">
              <ChartCardHeader
                title="大类配置"
                termKey="asset_class_allocation"
                description="全组合资产大类目标与当前占比"
              />
              <AllocationBarChart
                bars={data.allocation_bars}
                currency={data.plan.base_currency}
              />
            </div>
            <div className="rounded-lg border border-line bg-surface p-4">
              <ChartCardHeader
                title="地区配置"
                termKey="region_allocation"
                description="全组合地区暴露（按全组合权重折算）"
              />
              <RegionAllocationBarChart
                bars={data.region_bars}
                currency={data.plan.base_currency}
              />
            </div>
          </section>

          <section className="rounded-lg border border-line bg-surface p-4">
            <h2 className="flex items-center text-lg font-medium text-ink">
              大类内地区配置
              <MetricHelp termKey="region_allocation" />
            </h2>
            <p className="mt-1 text-xs text-ink-muted">
              各资产大类内部的国内 / 国外目标与当前比例
            </p>
            <AssetClassRegionGroups
              groups={data.asset_class_region_groups}
              currency={data.plan.base_currency}
            />
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
                      <span className="text-ink-muted">
                        ({deviation.instrument_code})
                      </span>
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
                        {deviation.deviation_amount_minor >= 0
                          ? "还差 "
                          : "超出 "}
                        {formatMoney(
                          Math.abs(deviation.deviation_amount_minor),
                        )}
                      </strong>
                      <span className="ml-2 text-xs text-ink-muted">
                        偏离{" "}
                        {formatPercent(Math.abs(deviation.deviation_weight))}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-3 text-sm text-ink-muted">
                当前持仓与目标配置一致。
              </p>
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
