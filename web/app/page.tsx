"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { listPlans } from "@/lib/api/plans";
import { formatMoney, formatDateFromMs } from "@/lib/format";
import { isSignificantScaleGap } from "@/lib/scale-gap";
import { queryErrorMessage } from "@/lib/query-error";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { PlanCardSkeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/cn";

export default function HomePage() {
  const { data: plans, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
  });

  if (isLoading && !plans) {
    return (
      <div className="content-enter">
        <PageHeader
          title="我的 FIRE 计划"
          description="查看各计划的规模状态与调仓待办，进入组合工作台继续管理。"
          primaryAction={{ label: "新建计划", href: "/plans/new" }}
        />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <PlanCardSkeleton key={i} />
          ))}
        </div>
      </div>
    );
  }

  if (isError && !plans) {
    return (
      <div className="content-enter">
        <PageHeader title="我的 FIRE 计划" />
        <ErrorState
          message="无法连接后端 API。请确认 Go 服务已在 :8080 运行。"
          onRetry={() => void refetch()}
          technicalDetail={queryErrorMessage(error)}
        />
      </div>
    );
  }

  const planList = plans ?? [];

  return (
    <div className="content-enter">
      <PageHeader
        title="我的 FIRE 计划"
        description="查看各计划的规模状态与调仓待办，进入组合工作台继续管理。"
        primaryAction={{ label: "新建计划", href: "/plans/new" }}
      />

      {!planList.length ? (
        <EmptyState
          title="还没有 FIRE 计划"
          description="通过四步向导创建第一个计划，配置目标权重与初始持仓。"
          action={{ label: "新建计划", href: "/plans/new" }}
        />
      ) : (
        <>
          {isFetching && !isLoading && (
            <p className="mb-3 text-xs text-ink-muted" role="status">
              正在刷新…
            </p>
          )}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {planList.map((plan) => {
              const scaleGap = plan.holdings_gap_minor ?? 0;
              const actionable = plan.rebalance_actionable_count ?? 0;
              return (
                <Link
                  key={plan.id}
                  href={`/plans/${plan.id}/overview`}
                  className={cn(
                    "group flex min-h-[220px] flex-col rounded-lg border border-line bg-surface p-5 shadow-sm transition",
                    "hover:border-brand/30 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/40",
                  )}
                  aria-label={plan.name}
                >
                  <h2 className="line-clamp-2 min-h-[3rem] text-lg font-semibold text-ink group-hover:text-brand-strong">
                    {plan.name}
                  </h2>
                  <dl className="mt-3 flex-1 space-y-1.5 text-sm text-ink-muted">
                    <div className="flex justify-between gap-2">
                      <dt>估值日</dt>
                      <dd className="font-mono-numeric text-ink">{plan.valuation_date}</dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>基准货币</dt>
                      <dd className="text-ink">{plan.base_currency}</dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>需调仓</dt>
                      <dd className={cn("font-medium", actionable > 0 ? "text-warning" : "text-ink")}>
                        {actionable} 个标的
                      </dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>规模状态</dt>
                      <dd
                        className={cn(
                          "font-medium",
                          isSignificantScaleGap(scaleGap) ? "text-warning" : "text-ink",
                        )}
                      >
                        {isSignificantScaleGap(scaleGap)
                          ? scaleGap > 0
                            ? `规模超出 ${formatMoney(Math.abs(scaleGap), plan.base_currency)}`
                            : `规模缺口 ${formatMoney(Math.abs(scaleGap), plan.base_currency)}`
                          : "规模一致"}
                      </dd>
                    </div>
                    <div className="flex justify-between gap-2">
                      <dt>更新于</dt>
                      <dd className="font-mono-numeric text-ink">{formatDateFromMs(plan.updated_at)}</dd>
                    </div>
                  </dl>
                </Link>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
