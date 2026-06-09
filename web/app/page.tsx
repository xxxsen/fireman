"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { listPlans } from "@/lib/api/plans";
import { getRecentPlanId, setRecentPlanId } from "@/lib/recentPlan";

export default function HomePage() {
  const router = useRouter();
  const { data: plans, isLoading, error } = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
  });

  useEffect(() => {
    if (!plans?.length) return;
    if (plans.length === 1) {
      setRecentPlanId(plans[0].id);
      router.replace(`/plans/${plans[0].id}/dashboard`);
      return;
    }
    const recent = getRecentPlanId();
    if (recent && plans.some((p) => p.id === recent)) {
      router.replace(`/plans/${recent}/dashboard`);
    }
  }, [plans, router]);

  if (isLoading) {
    return <p className="text-slate-600">加载计划列表…</p>;
  }

  if (error) {
    return (
      <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800">
        无法连接后端 API。请确认 Go 服务已在 :8080 运行。
      </div>
    );
  }

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">计划列表</h1>
        <Link
          href="/plans/new"
          className="rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white"
        >
          新建计划
        </Link>
      </div>

      {!plans?.length ? (
        <div className="rounded-lg border border-dashed border-slate-300 p-8 text-center">
          <p className="text-slate-600">还没有 FIRE 计划</p>
          <Link href="/plans/new" className="mt-4 inline-block text-sm underline">
            开始四步向导
          </Link>
        </div>
      ) : (
        <ul className="divide-y divide-slate-200 rounded-lg border border-slate-200">
          {plans.map((plan) => (
            <li key={plan.id}>
              <Link
                href={`/plans/${plan.id}/dashboard`}
                className="flex items-center justify-between px-4 py-4 hover:bg-slate-50"
              >
                <div>
                  <div className="font-medium">{plan.name}</div>
                  <div className="text-sm text-slate-500">
                    估值日 {plan.valuation_date} · {plan.base_currency}
                  </div>
                </div>
                <span className="text-sm text-slate-400">→</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
