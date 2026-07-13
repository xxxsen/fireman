"use client";

import { memo } from "react";
import { QuickFireChart } from "./QuickFireChart";
import { QuickFireSummary } from "./QuickFireSummary";
import { QuickFireYearTable } from "./QuickFireYearTable";
import type { QuickFireResult } from "@/lib/api/quick-fire";
import { CalculationExplanation } from "@/components/ui/CalculationExplanation";
import { HelpLabel } from "@/components/ui/HelpLabel";

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
        <p className="text-sm font-medium text-ink">
          <HelpLabel label="本次计算模型" termKey="quick_fire_fixed_model" />
        </p>
        <CalculationExplanation
          summary="快算按固定收益和固定通胀逐月推进现金流，并从目标年龄反推每个月需要的资本。"
          answer="当前输入下，计划 FIRE 月预计有多少资产、需要多少资本，以及固定现金流能够支持多久。"
          changed="计算只使用左侧当前输入；修改任一年龄、金额或比率后会重新计算。"
          fixed="每个月采用同一固定收益率和确定性通胀，不生成随机市场路径。"
          data="使用当前可投资资产、退休前年储蓄、退休支出、稳定收入、增长率、年龄和期末最低资产，不读取持仓或市场历史。"
          criterion="年化收益和增长率先转换为月度复合率；每月按阶段结算收入、支出与投资收益。所需资本从目标年龄的期末最低资产逐月反推。"
          uncertainty="不包含市场波动、资产相关性、FX、交易费、税务和收益顺序风险，因此不输出成功概率。"
          nextStep="用它快速检查数量级；需要评估不确定性时，创建完整计划并运行 Monte Carlo 模拟。"
          audit={`计算引擎 ${result.engine_version}；币种 ${result.base_currency}`}
        />
        <div className="border-y border-line py-3"><QuickFireChart years={result.years} /></div>
        <QuickFireYearTable years={result.years} />
        <details className="border-t border-line pt-4">
          <summary className="cursor-pointer text-sm font-medium text-ink">
            <HelpLabel label="了解完整模拟与收益顺序风险" termKey="return_sequence_risk" />
          </summary>
          <p className="mt-2 text-sm leading-6 text-ink-muted">完整计划会根据持仓、收益波动、资产相关性和通胀路径运行 Monte Carlo，用概率结果评估收益顺序风险。</p>
        </details>
      </div>
    </div>
  );
});
