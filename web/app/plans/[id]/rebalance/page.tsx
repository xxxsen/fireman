"use client";

import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { getRebalance } from "@/lib/api/holdings";
import { createPortfolioSnapshot, getPlan } from "@/lib/api/plans";
import {
  formatMoney,
  formatPercent,
  rebalanceActionLabel,
} from "@/lib/format";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { usePlanResultStale } from "@/hooks/usePlanResultStale";

export default function RebalancePage() {
  const planId = useParams().id as string;
  const qc = useQueryClient();
  const { stale } = usePlanResultStale(planId);
  const [mode, setMode] = useState<"full" | "new_cash">("full");
  const [newCashMinor, setNewCashMinor] = useState(0);
  const [actionFilter, setActionFilter] = useState<string>("all");

  const planQ = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const { data, isLoading, refetch } = useQuery({
    queryKey: ["rebalance", planId, mode, newCashMinor],
    queryFn: () =>
      getRebalance(planId, mode, mode === "new_cash" ? newCashMinor : undefined),
  });

  const snapshotMut = useMutation({
    mutationFn: () => {
      if (!planQ.data) throw new Error("plan");
      return createPortfolioSnapshot(planId, {
        config_version: planQ.data.config_version,
        note: "调仓后记录新持仓",
      });
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["holdings", planId] });
      void qc.invalidateQueries({ queryKey: ["plan", planId] });
    },
  });

  if (isLoading || !data) return <p>加载调仓检查…</p>;

  const lines = data.lines.filter(
    (l) => actionFilter === "all" || l.action === actionFilter,
  );

  return (
    <div className="space-y-6">
      {stale && <StaleBanner />}
      <dl className="grid gap-4 sm:grid-cols-3 lg:grid-cols-6">
        <div>
          <dt className="text-sm text-slate-500">当前总资产</dt>
          <dd className="font-medium">{formatMoney(data.summary.total_assets_minor)}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">目标合计</dt>
          <dd>{formatMoney(data.summary.target_total_minor)}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">当前合计</dt>
          <dd>{formatMoney(data.summary.current_total_minor)}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">需行动标的</dt>
          <dd>{data.summary.actionable_count}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">预计交易金额</dt>
          <dd>{formatMoney(data.summary.estimated_trade_minor)}</dd>
        </div>
        <div>
          <dt className="text-sm text-slate-500">预计交易成本</dt>
          <dd>{formatMoney(data.summary.estimated_cost_minor)}</dd>
        </div>
      </dl>

      <div className="flex flex-wrap gap-4">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="radio"
            checked={mode === "full"}
            onChange={() => setMode("full")}
          />
          完整调仓
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="radio"
            checked={mode === "new_cash"}
            onChange={() => setMode("new_cash")}
          />
          仅用新增资金
        </label>
        {mode === "new_cash" && (
          <MoneyInput
            label="本次新增资金"
            valueMinor={newCashMinor}
            onChange={setNewCashMinor}
          />
        )}
        <select
          className="rounded-md border px-2 py-1 text-sm"
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
        >
          <option value="all">全部动作</option>
          <option value="increase">增配</option>
          <option value="decrease">减配</option>
          <option value="hold">不动</option>
        </select>
        <button
          type="button"
          className="rounded-md border px-3 py-1.5 text-sm"
          onClick={() => void refetch()}
        >
          重新计算
        </button>
      </div>

      <div className="overflow-x-auto rounded-lg border">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50">
            <tr>
              <th className="px-3 py-2 text-left">标的</th>
              <th className="px-3 py-2 text-right">目标金额</th>
              <th className="px-3 py-2 text-right">当前金额</th>
              <th className="px-3 py-2 text-right">偏离</th>
              <th className="px-3 py-2 text-left">建议</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l) => (
              <tr key={l.holding_id} className="border-t">
                <td className="px-3 py-2">
                  {l.instrument_name ?? l.instrument_code ?? l.instrument_id}
                </td>
                <td className="px-3 py-2 text-right">{formatMoney(l.target_amount_minor)}</td>
                <td className="px-3 py-2 text-right">{formatMoney(l.current_amount_minor)}</td>
                <td className="px-3 py-2 text-right">
                  {formatPercent(l.deviation_weight)}
                </td>
                <td className="px-3 py-2">
                  {rebalanceActionLabel(l.action)}{" "}
                  {l.suggested_trade_minor !== 0 &&
                    formatMoney(Math.abs(l.suggested_trade_minor))}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p className="text-xs text-slate-500">
        系统只生成建议，不修改当前金额。实际交易后请记录新持仓快照。
      </p>
      <button
        type="button"
        className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white"
        onClick={() => snapshotMut.mutate()}
        disabled={snapshotMut.isPending}
      >
        记录新持仓快照
      </button>
    </div>
  );
}
