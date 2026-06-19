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
          ? "border-positive/30 bg-positive/5 text-positive"
          : netMinor > 0
            ? "border-info/25 bg-info/5 text-info"
            : "border-warning/30 bg-warning/5 text-warning"
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
