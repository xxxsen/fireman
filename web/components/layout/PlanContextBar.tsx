"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { listPlans } from "@/lib/api/plans";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { cn } from "@/lib/cn";
import { LoadingState } from "@/components/ui/LoadingState";
import { Button } from "@/components/ui/Button";

export function PlanContextBar({ currentPlanId }: { currentPlanId: string }) {
  const { confirmLeave } = usePlanEdit();
  const { data: plans, isLoading, isError, refetch } = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
  });

  const current = plans?.find((p) => p.id === currentPlanId);
  const planName = current?.name ?? (isError ? "计划加载失败" : isLoading ? undefined : "未知计划");

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
    </div>
  );
}

/** @deprecated Use PlanContextBar */
export { PlanContextBar as PlanSelector };
