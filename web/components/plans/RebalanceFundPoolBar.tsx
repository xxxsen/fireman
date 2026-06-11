"use client";

import { formatMoney } from "@/lib/format";
import { isFundPoolBalanced } from "@/lib/rebalance-plan";
import { MetricHelp } from "@/components/ui/MetricHelp";

export function RebalanceFundPoolBar({
  releasedMinor,
  usedMinor,
  netMinor,
  currency,
}: {
  releasedMinor: number;
  usedMinor: number;
  netMinor: number;
  currency?: string;
}) {
  const balanced = isFundPoolBalanced(netMinor);

  return (
    <div
      className={`rounded-lg border px-4 py-3 text-sm ${
        balanced
          ? "border-emerald-200 bg-emerald-50 text-emerald-900"
          : netMinor > 0
            ? "border-sky-200 bg-sky-50 text-sky-900"
            : "border-amber-200 bg-amber-50 text-amber-900"
      }`}
    >
      <p className="flex items-center font-medium">
        调仓资金池
        <MetricHelp termKey="rebalance_fund_pool" />
      </p>
      <p className="mt-1">
        减配释放 {formatMoney(releasedMinor, currency)} · 增配占用{" "}
        {formatMoney(usedMinor, currency)}
      </p>
      <p className="mt-1 font-medium">
        {balanced
          ? "调仓资金已平衡"
          : netMinor > 0
            ? `可用于调仓 ${formatMoney(netMinor, currency)}（还需分配到增配标的）`
            : `待补充调仓资金 ${formatMoney(Math.abs(netMinor), currency)}（增配超出减配释放）`}
      </p>
    </div>
  );
}
