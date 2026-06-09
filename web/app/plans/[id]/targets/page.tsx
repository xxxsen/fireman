"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { getTargets } from "@/lib/api/holdings";
import { listSimulations } from "@/lib/api/simulations";
import { downloadCsv } from "@/lib/csv";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
  regionLabel,
} from "@/lib/format";

type Level = "asset_class" | "region" | "holding";

export default function TargetsPage() {
  const planId = useParams().id as string;
  const [level, setLevel] = useState<Level>("holding");

  const { data, isLoading } = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });
  const simQ = useQuery({
    queryKey: ["simulations", planId],
    queryFn: () => listSimulations(planId),
  });

  const stale = simQ.data?.simulations.some((s) => s.result_stale) ?? false;

  if (isLoading || !data) return <p>加载目标配置…</p>;

  const exportCsv = () => {
    downloadCsv(
      "targets.csv",
      ["标的", "大类", "地区", "组内占比", "全组合目标占比", "目标金额", "当前金额"],
      data.holdings
        .filter((h) => h.enabled)
        .map((h) => [
          h.instrument_id,
          h.asset_class,
          h.region,
          formatPercent(h.weight_within_group),
          formatPercent(h.portfolio_target_weight),
          (h.target_amount_minor / 100).toFixed(2),
          (h.current_amount_minor / 100).toFixed(2),
        ]),
    );
  };

  return (
    <div className="space-y-4">
      {stale && <StaleBanner />}

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex rounded-md border text-sm">
          {(
            [
              ["asset_class", "大类"],
              ["region", "地区"],
              ["holding", "标的"],
            ] as const
          ).map(([v, label]) => (
            <button
              key={v}
              type="button"
              className={`px-3 py-1.5 ${level === v ? "bg-slate-900 text-white" : ""}`}
              onClick={() => setLevel(v)}
            >
              {label}
            </button>
          ))}
        </div>
        <button type="button" className="text-sm underline" onClick={exportCsv}>
          导出 CSV
        </button>
        <Link href={`/plans/${planId}/parameters`} className="text-sm underline">
          返回修改参数
        </Link>
        <Link href={`/plans/${planId}/instruments`} className="text-sm underline">
          返回修改标的
        </Link>
      </div>

      {!data.weight_checks.passed && (
        <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
          {data.weight_checks.checks
            .filter((c) => !c.passed)
            .map((c) => (
              <p key={c.key}>{c.message}</p>
            ))}
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50">
            <tr>
              <th className="px-3 py-2 text-left">名称</th>
              <th className="px-3 py-2 text-right">
                目标占比
                <MetricHelp termKey="portfolio_weight" />
              </th>
              <th className="px-3 py-2 text-right">目标金额</th>
              <th className="px-3 py-2 text-right">当前金额</th>
              <th className="px-3 py-2 text-left">快照</th>
            </tr>
          </thead>
          <tbody>
            {level === "asset_class" &&
              data.asset_class_targets.map((t) => (
                <tr key={t.asset_class} className="border-t">
                  <td className="px-3 py-2">{assetClassLabel(t.asset_class)}</td>
                  <td className="px-3 py-2 text-right">{formatPercent(t.weight)}</td>
                  <td className="px-3 py-2 text-right text-slate-400">—</td>
                  <td className="px-3 py-2 text-right text-slate-400">—</td>
                  <td />
                </tr>
              ))}
            {level === "region" &&
              data.region_targets.map((t) => (
                <tr key={`${t.asset_class}-${t.region}`} className="border-t">
                  <td className="px-3 py-2">
                    {regionLabel(t.region)}
                    {assetClassLabel(t.asset_class)}
                  </td>
                  <td className="px-3 py-2 text-right">{formatPercent(t.weight_within_class)}</td>
                  <td colSpan={3} />
                </tr>
              ))}
            {level === "holding" &&
              data.holdings.map((h) => (
                <tr key={h.holding_id} className="border-t">
                  <td className="px-3 py-2">
                    {assetClassLabel(h.asset_class)} / {regionLabel(h.region)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {formatPercent(h.portfolio_target_weight)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {formatMoney(h.target_amount_minor)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {formatMoney(h.current_amount_minor)}
                  </td>
                  <td className="px-3 py-2 text-xs text-slate-500">
                    {h.simulation_snapshot_id.slice(0, 12)}…
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
