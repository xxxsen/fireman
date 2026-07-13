"use client";

import { memo } from "react";
import type { QuickFireResult } from "@/lib/api/quick-fire";
import { formatMoney, formatPercent } from "@/lib/format";
import { HelpLabel } from "@/components/ui/HelpLabel";

function supportLabel(months: number): string {
  return `${Math.floor(months / 12)} 年 ${months % 12} 个月`;
}

export function quickFireOutcomeLabel(result: QuickFireResult): string {
	const depletionAge = result.depletion_age_years == null
		? ""
		: `，约在 ${result.depletion_age_years} 岁 ${result.depletion_age_months ?? 0} 个月发生`;
  switch (result.outcome_status) {
    case "sustainable":
      return `计划可行：可支撑至目标年龄，并保留 ${formatMoney(result.terminal_wealth_minor)}`;
    case "insufficient_funds":
      return `资金不足：可完整支付 ${supportLabel(result.support_months_after_fire)}${depletionAge}`;
    case "wealth_depleted":
      return `资产耗尽：可完整支付 ${supportLabel(result.support_months_after_fire)}${depletionAge}`;
    case "terminal_floor_not_met":
      return `期末目标未达：支出可支付，但比目标少 ${formatMoney(result.terminal_wealth_floor_minor - result.terminal_wealth_minor)}`;
  }
}

export const QuickFireSummary = memo(function QuickFireSummary({ result, compact = false }: { result: QuickFireResult; compact?: boolean }) {
  const earliest =
    result.earliest_fire_age_years == null
      ? "目标年龄前未达到"
      : `${result.earliest_fire_age_years} 岁 ${result.earliest_fire_age_months ?? 0} 个月`;
  const depletion =
    result.depletion_age_years == null
      ? "未耗尽"
      : `${result.depletion_age_years} 岁 ${result.depletion_age_months ?? 0} 个月`;
  if (compact) {
    return <p className="text-sm font-medium text-ink" data-testid="quick-fire-compact-outcome">{quickFireOutcomeLabel(result)}</p>;
  }
  return (
    <section aria-label="FIRE 快算结论" className="space-y-4">
      <p className="text-base font-semibold text-ink" data-testid="quick-fire-outcome">{quickFireOutcomeLabel(result)}</p>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <Metric label="计划 FIRE 时资产" termKey="projected_fire_assets" value={formatMoney(result.projected_assets_at_fire_minor)} />
        <Metric label="所需 FIRE 资本" termKey="required_fire_capital" value={formatMoney(result.required_assets_at_fire_minor)} />
        <Metric
          label="资金富余 / 缺口"
          termKey="fire_funding_gap"
          value={formatMoney(result.fire_funding_gap_minor)}
          tone={result.fire_funding_gap_minor >= 0 ? "positive" : "danger"}
        />
        <Metric label="最早可 FIRE" termKey="earliest_fire_age" value={earliest} />
        <Metric label="可完整支付" termKey="support_duration" value={supportLabel(result.support_months_after_fire)} />
        <Metric label="耗尽年龄" termKey="depletion_age" value={depletion} />
        <Metric label="期末名义资产" termKey="terminal_nominal_wealth" value={formatMoney(result.terminal_wealth_minor)} />
        <Metric label="期末真实资产" termKey="terminal_real_wealth" value={formatMoney(result.real_terminal_wealth_minor)} />
        <Metric label="实际年化收益" termKey="real_annual_return" value={formatPercent(result.real_annual_return_rate)} />
      </div>
      <p className="text-xs text-ink-muted">
        这是固定收益率下的确定性估算，不包含市场波动和收益顺序风险。完整计划的 Monte Carlo 结果才表示概率。
      </p>
    </section>
  );
});

function Metric({ label, termKey, value, tone }: { label: string; termKey: string; value: string; tone?: "positive" | "danger" }) {
  return (
    <dl className="rounded-md border border-line bg-surface px-3 py-3">
      <dt className="text-xs text-ink-muted"><HelpLabel label={label} termKey={termKey} /></dt>
      <dd className={`mt-1 font-mono-numeric text-sm font-medium ${tone === "positive" ? "text-positive" : tone === "danger" ? "text-danger" : "text-ink"}`}>
        {value}
      </dd>
    </dl>
  );
}
