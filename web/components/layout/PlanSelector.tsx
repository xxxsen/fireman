"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { listPlans } from "@/lib/api/plans";
import { usePlanEdit } from "@/hooks/usePlanEdit";

export function PlanSelector({ currentPlanId }: { currentPlanId: string }) {
  const { confirmLeave } = usePlanEdit();
  const { data: plans = [] } = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
  });

  const current = plans.find((p) => p.id === currentPlanId);

  return (
    <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2">
      <Link
        href="/"
        onClick={(e) => {
          if (!confirmLeave()) e.preventDefault();
        }}
        className="text-sm text-slate-600 underline hover:text-slate-900"
      >
        ← 全部计划
      </Link>
      <h1 className="text-xl font-semibold text-slate-900">{current?.name ?? "计划"}</h1>
      <div className="ml-auto flex flex-wrap items-center gap-3">
        <Link
          href="/plans/new"
          onClick={(e) => {
            if (!confirmLeave()) e.preventDefault();
          }}
          className="text-sm text-slate-600 underline hover:text-slate-900"
        >
          新建计划
        </Link>
        <Link
          href={`/plans/${currentPlanId}/analysis`}
          onClick={(e) => {
            if (!confirmLeave()) e.preventDefault();
          }}
          className="text-sm font-medium text-slate-900 underline"
        >
          模拟分析中心 →
        </Link>
      </div>
    </div>
  );
}
