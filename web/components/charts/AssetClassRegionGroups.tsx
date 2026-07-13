"use client";

import { assetClassLabel, formatMoneyScaled, formatPercent, regionLabel } from "@/lib/format";
import type { AssetClassRegionGroup } from "@/types/api";
import { Tooltip } from "@/components/ui/Tooltip";

const MAX_INLINE_HOLDINGS = 4;

function holdingsLabel(
  holdings: AssetClassRegionGroup["regions"][number]["holdings"],
): { text: string; title: string } {
  if (holdings.length === 0) {
    return { text: "暂无资产", title: "" };
  }
  const names = holdings.slice(0, MAX_INLINE_HOLDINGS).map((h) => h.instrument_name || "—");
  const text =
    names.join("、") +
    (holdings.length > MAX_INLINE_HOLDINGS ? ` 等 ${holdings.length} 个` : "");
  const title = holdings
    .map((h) => {
      const code = h.instrument_code ? `（${h.instrument_code}）` : "";
      return `${h.instrument_name || "—"}${code}`;
    })
    .join("\n");
  return { text, title };
}

export function AssetClassRegionGroups({
  groups,
  currency = "CNY",
}: {
  groups: AssetClassRegionGroup[];
  currency?: string;
}) {
  if (groups.length === 0) {
    return <p className="mt-3 text-sm text-ink-muted">暂无大类内地区配置。</p>;
  }

  return (
    <div className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {groups.map((group) => (
        <div key={group.asset_class} className="rounded-md border border-line/70 p-3">
          <h3 className="text-sm font-medium text-ink">{assetClassLabel(group.asset_class)}</h3>
          <ul className="mt-2 space-y-3">
            {group.regions.map((region) => {
              const { text, title } = holdingsLabel(region.holdings ?? []);
              const currentPct = Math.max(0, Math.min(100, region.current_weight * 100));
              const targetPct = Math.max(0, Math.min(100, region.target_weight * 100));
              return (
                <li key={region.region} className="text-sm">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-ink">{regionLabel(region.region)}</span>
                    <span className="text-xs text-ink-muted">
                      目标 {formatPercent(region.target_weight)} · 当前{" "}
                      {formatPercent(region.current_weight)}
                    </span>
                  </div>
                  <div className="relative mt-1 h-1.5 w-full rounded bg-surface-muted">
                    <div
                      className="absolute inset-y-0 left-0 rounded bg-line"
                      style={{ width: `${targetPct}%` }}
                    />
                    <div
                      className="absolute inset-y-0 left-0 rounded bg-ink"
                      style={{ width: `${currentPct}%` }}
                    />
                  </div>
                  <Tooltip content={title} className="mt-1 max-w-full">
                    <p className="truncate text-xs text-ink-muted">
                      {text}
                      {(region.holdings ?? []).length > 0 && (
                        <span className="ml-1">
                          · 当前 {formatMoneyScaled(region.current_amount_minor, currency)}
                        </span>
                      )}
                    </p>
                  </Tooltip>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </div>
  );
}
