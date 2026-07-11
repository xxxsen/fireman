"use client";

import { memo } from "react";
import { QuickFireChart } from "./QuickFireChart";
import { QuickFireSummary } from "./QuickFireSummary";
import { QuickFireYearTable } from "./QuickFireYearTable";
import type { QuickFireResult } from "@/lib/api/quick-fire";

export const QuickFireResults = memo(function QuickFireResults({
  result,
  concealed,
  busy,
}: {
  result: QuickFireResult;
  concealed: boolean;
  busy: boolean;
}) {
  return (
    <div
      className={`min-w-0 ${concealed ? "invisible pointer-events-none select-none" : ""}`}
      aria-hidden={concealed || undefined}
      aria-busy={busy || undefined}
      data-testid="quick-fire-results"
    >
      <div className="space-y-8">
        <QuickFireSummary result={result} />
        <section aria-labelledby="quick-fire-chart-title">
          <h2 id="quick-fire-chart-title" className="text-lg font-medium text-ink">资产与所需资本</h2>
          <div className="mt-3 border-y border-line py-3"><QuickFireChart years={result.years} /></div>
        </section>
        <QuickFireYearTable years={result.years} />
        <details className="border-t border-line pt-4">
          <summary className="cursor-pointer text-sm font-medium text-ink">了解完整模拟差异</summary>
          <p className="mt-2 text-sm leading-6 text-ink-muted">完整计划会根据持仓、收益波动、资产相关性和通胀路径运行 Monte Carlo，用概率结果评估收益顺序风险。</p>
        </details>
      </div>
    </div>
  );
});
