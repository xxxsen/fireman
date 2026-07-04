"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { listPlans } from "@/lib/api/plans";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { cn } from "@/lib/cn";
import { LoadingState } from "@/components/ui/LoadingState";
import { Button } from "@/components/ui/Button";

/**
 * Sub-page breadcrumb labels for plan pages deeper than the three tabs. Tab
 * level pages (overview / rebalance / settings) are located by the tab bar
 * itself and get no third segment.
 */
const SUB_PAGE_CRUMBS: ReadonlyArray<{ prefix: string; label: string }> = [
  { prefix: "/asset-refresh", label: "持仓校正" },
  { prefix: "/rebalance/executions", label: "调仓执行" },
  { prefix: "/rebalance/plan/", label: "调仓计划" },
  { prefix: "/analysis/", label: "模拟路径详情" },
];

function subPageCrumb(pathname: string, base: string): string | null {
  if (!pathname.startsWith(base)) return null;
  const rest = pathname.slice(base.length);
  for (const { prefix, label } of SUB_PAGE_CRUMBS) {
    if (rest === prefix || rest.startsWith(prefix)) return label;
  }
  return null;
}

export function PlanContextBar({ currentPlanId }: { currentPlanId: string }) {
  const { confirmLeave } = usePlanEdit();
  const pathname = usePathname();
  const { data: plans, isLoading, isError, refetch } = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
  });

  const current = plans?.find((p) => p.id === currentPlanId);
  const planName = current?.name ?? (isError ? "计划加载失败" : isLoading ? undefined : "未知计划");
  const crumb = subPageCrumb(pathname ?? "", `/plans/${currentPlanId}`);

  return (
    <div
      className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line pb-3"
      data-testid="plan-context-bar"
    >
      <Link
        href="/"
        onClick={(e) => {
          if (!confirmLeave()) e.preventDefault();
        }}
        className="text-sm text-ink-muted underline-offset-2 hover:text-ink hover:underline"
      >
        全部计划
      </Link>

      <span className="text-ink-muted" aria-hidden="true">
        /
      </span>

      {isLoading && !current ? (
        <LoadingState label="加载计划…" />
      ) : isError && plans == null ? (
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          <span className="text-sm text-danger" data-testid="plan-context-error">
            计划加载失败
          </span>
          <Button variant="ghost" onClick={() => void refetch()} data-testid="plan-context-retry">
            重试
          </Button>
        </div>
      ) : crumb ? (
        <Link
          href={`/plans/${currentPlanId}/overview`}
          onClick={(e) => {
            if (!confirmLeave()) e.preventDefault();
          }}
          className={cn(
            "min-w-0 text-sm text-ink-muted underline-offset-2 hover:text-ink hover:underline",
            !planName && "text-ink-muted",
          )}
          data-testid="plan-context-name"
        >
          {planName ?? "计划"}
        </Link>
      ) : (
        <span
          className={cn(
            "min-w-0 text-sm font-medium text-ink",
            !planName && "text-ink-muted",
          )}
          data-testid="plan-context-name"
        >
          {planName ?? "计划"}
        </span>
      )}

      {crumb && (
        <>
          <span className="text-ink-muted" aria-hidden="true">
            /
          </span>
          <span
            className="min-w-0 text-sm font-medium text-ink"
            data-testid="plan-context-subpage"
          >
            {crumb}
          </span>
        </>
      )}
    </div>
  );
}

/** @deprecated Use PlanContextBar */
export { PlanContextBar as PlanSelector };
