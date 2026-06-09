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

  return (
    <div className="mb-4 flex flex-wrap items-center gap-3">
      <label className="text-sm text-slate-600">
        当前计划
        <select
          className="ml-2 rounded-md border border-slate-300 px-2 py-1 text-sm"
          value={currentPlanId}
          onChange={(e) => {
            const id = e.target.value;
            if (id && confirmLeave()) {
              window.location.href = `/plans/${id}/dashboard`;
            }
          }}
        >
          {plans.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
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
        className="ml-auto text-sm font-medium text-slate-900 underline"
      >
        模拟分析中心 →
      </Link>
    </div>
  );
}
