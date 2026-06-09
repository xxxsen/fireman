"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { getPathDetail } from "@/lib/api/simulations";
import { formatMoney, formatPercent } from "@/lib/format";

export default function PathDetailPage() {
  const params = useParams();
  const planId = params.id as string;
  const runId = params.run_id as string;
  const pathNo = Number(params.path_no);

  const { data, isLoading, error } = useQuery({
    queryKey: ["path", runId, pathNo],
    queryFn: () => getPathDetail(runId, pathNo),
  });

  if (isLoading) return <p>加载路径详情…</p>;
  if (error || !data) {
    return <p className="text-red-600">加载失败</p>;
  }

  const terminalWealth =
    data.monthly.length > 0
      ? data.monthly[data.monthly.length - 1].total_wealth_minor
      : 0;
  const maxDrawdown = data.monthly.reduce(
    (max, m) => (m.drawdown > max ? m.drawdown : max),
    0,
  );

  return (
    <div className="space-y-4">
      <Link href={`/plans/${planId}/analysis`} className="text-sm underline">
        ← 返回分析中心
      </Link>
      <h1 className="text-xl font-semibold">路径 #{data.path_no}</h1>
      <dl className="grid gap-3 sm:grid-cols-2">
        <div>
          <dt className="text-sm text-slate-500">路径种子</dt>
          <dd className="font-mono text-sm">{data.path_seed}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">是否成功</dt>
          <dd>{data.succeeded ? "是" : "否"}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">期末资产</dt>
          <dd>{formatMoney(terminalWealth)}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">最大回撤</dt>
          <dd>{formatPercent(maxDrawdown)}</dd>
        </div>
        {data.failure_reason && (
          <div className="sm:col-span-2">
            <dt className="text-sm text-slate-500">失败原因</dt>
            <dd>{data.failure_reason}</dd>
          </div>
        )}
      </dl>
      <div className="overflow-x-auto rounded-lg border">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50">
            <tr>
              <th className="px-3 py-2">月份</th>
              <th className="px-3 py-2 text-right">资产</th>
              <th className="px-3 py-2 text-right">支出</th>
              <th className="px-3 py-2 text-right">回撤</th>
            </tr>
          </thead>
          <tbody>
            {data.monthly.slice(0, 120).map((m) => (
              <tr key={m.month_offset} className="border-t">
                <td className="px-3 py-2">{m.month_offset}</td>
                <td className="px-3 py-2 text-right">{formatMoney(m.total_wealth_minor)}</td>
                <td className="px-3 py-2 text-right">{formatMoney(m.spending_minor)}</td>
                <td className="px-3 py-2 text-right">{formatPercent(m.drawdown)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {data.monthly.length > 120 && (
          <p className="p-2 text-xs text-slate-500">仅显示前 120 个月</p>
        )}
      </div>
      {data.yearly.length > 0 && (
        <div className="overflow-x-auto rounded-lg border">
          <h2 className="border-b px-3 py-2 text-sm font-medium">年度明细</h2>
          <table className="min-w-full text-sm">
            <thead className="bg-slate-50">
              <tr>
                <th className="px-3 py-2">年份</th>
                <th className="px-3 py-2 text-right">收入</th>
                <th className="px-3 py-2 text-right">支出</th>
                <th className="px-3 py-2 text-right">期末资产</th>
              </tr>
            </thead>
            <tbody>
              {data.yearly.map((y) => (
                <tr key={y.year} className="border-t">
                  <td className="px-3 py-2">{y.year}</td>
                  <td className="px-3 py-2 text-right">{formatMoney(y.income_minor)}</td>
                  <td className="px-3 py-2 text-right">{formatMoney(y.spending_minor)}</td>
                  <td className="px-3 py-2 text-right">{formatMoney(y.end_wealth_minor)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
