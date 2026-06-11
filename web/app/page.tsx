"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { listPlans } from "@/lib/api/plans";
import { formatMoney } from "@/lib/format";
import { isSignificantScaleGap } from "@/lib/scale-gap";

function formatPlanDate(ts: number): string {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleDateString("zh-CN");
}

export default function HomePage() {
  const { data: plans, isLoading, error } = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
  });

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
        <h1 className="text-2xl font-semibold">我的 FIRE 计划</h1>
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
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {plans.map((plan) => {
            const scaleGap = plan.holdings_gap_minor ?? 0;
            return (
              <Link
                key={plan.id}
                href={`/plans/${plan.id}/overview`}
                className="group flex flex-col rounded-lg border border-slate-200 bg-white p-5 shadow-sm transition hover:border-slate-300 hover:shadow-md"
              >
                <h2 className="text-lg font-semibold text-slate-900 group-hover:text-slate-700">
                  {plan.name}
                </h2>
                <dl className="mt-3 space-y-1.5 text-sm text-slate-600">
                  <div className="flex justify-between gap-2">
                    <dt>估值日</dt>
                    <dd className="text-slate-900">{plan.valuation_date}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>基准货币</dt>
                    <dd className="text-slate-900">{plan.base_currency}</dd>
                  </div>
                  <div className="flex justify-between gap-2">
                    <dt>需调仓</dt>
                    <dd className="font-medium text-slate-900">
                      {plan.rebalance_actionable_count ?? 0} 个标的
                    </dd>
                  </div>
                  {isSignificantScaleGap(scaleGap) && (
                    <div className="flex justify-between gap-2 text-amber-700">
                      <dt>{scaleGap > 0 ? "规模超出" : "规模缺口"}</dt>
                      <dd>{formatMoney(Math.abs(scaleGap), plan.base_currency)}</dd>
                    </div>
                  )}
                  <div className="flex justify-between gap-2">
                    <dt>更新于</dt>
                    <dd className="text-slate-900">{formatPlanDate(plan.updated_at)}</dd>
                  </div>
                </dl>
                <span className="mt-4 text-sm font-medium text-slate-500 group-hover:text-slate-900">
                  查看详情 →
                </span>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}
