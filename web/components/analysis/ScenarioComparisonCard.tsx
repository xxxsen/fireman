"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { getScenarioComparison } from "@/lib/api/simulations";
import { formatMoneyWan, formatPercent } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type { ScenarioComparisonRow } from "@/types/api";

const SCENARIO_LABELS: Record<string, string> = {
  conservative: "保守",
  baseline: "基准",
  optimistic: "乐观",
};

function scenarioLabel(key: string): string {
  return SCENARIO_LABELS[key] ?? key;
}

/** Signed ¥xx.xxw delta vs the baseline row, blank for the baseline itself. */
function deltaLabel(value: number, baseline: number, isBaseline: boolean): string {
  if (isBaseline) return "—";
  const diff = value - baseline;
  const sign = diff > 0 ? "+" : "";
  return `${sign}${formatMoneyWan(diff)}`;
}

/**
 * On-demand comparison of the same frozen plan input under
 * the three global scenarios with one shared seed. Only the scenario differs, so
 * the rows isolate the effect of the return/volatility shift; the deltas are
 * measured against the baseline row.
 */
export function ScenarioComparisonCard({
  planId,
  runId,
  inputHash,
}: {
  planId: string;
  runId: string;
  inputHash: string;
}) {
  const [enabled, setEnabled] = useState(false);
  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ["scenario-comparison", planId, runId, inputHash],
    queryFn: () => getScenarioComparison(planId, runId),
    enabled,
    staleTime: 0,
  });

  const rows = data?.scenarios ?? [];
  const baseline = rows.find((r) => r.scenario === (data?.baseline_key ?? "baseline"));

  return (
    <div className="mt-6 rounded-lg border border-line p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium text-ink">情景比较（保守 / 基准 / 乐观）</h3>
          <p className="mt-1 text-xs text-ink-muted">
            使用所选历史模拟的冻结输入与同一随机种子，仅切换全局情景并列运行。
          </p>
        </div>
        <Button
          variant="secondary"
          className="px-3 py-1"
          disabled={isFetching}
          onClick={() => {
            if (!enabled) setEnabled(true);
            else void refetch();
          }}
        >
          {isFetching ? "运行中…" : enabled ? "重新运行" : "运行情景比较"}
        </Button>
      </div>

      {isLoading && enabled && (
        <p className="mt-3 text-sm text-ink-muted">正在并列运行三情景…</p>
      )}
      {isError && (
        <p className="mt-3 text-sm text-danger">
          无法运行情景比较：{queryErrorMessage(error)}
        </p>
      )}

      {data && rows.length > 0 && (
        <>
          <div className="mt-3 overflow-auto rounded-lg border border-line">
            <table className="min-w-full text-sm">
              <thead className="bg-surface-muted">
                <tr>
                  <th className="px-3 py-2 text-left font-medium text-ink-muted">情景</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">前瞻收益</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">目标权重下近似年化波动率</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">成功率</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">终值 P50（名义）</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">较基准</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">终值 P50（购买力）</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">最大回撤 P50</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r: ScenarioComparisonRow) => {
                  const isBaseline = r.scenario === data.baseline_key;
                  return (
                    <tr
                      key={r.scenario}
                      className={`border-t border-line ${isBaseline ? "bg-brand/5" : ""}`}
                    >
                      <td className="px-3 py-2 text-ink">{scenarioLabel(r.scenario)}</td>
                      <td className="px-3 py-2 text-right text-ink">{formatPercent(r.forward_return)}</td>
                      <td className="px-3 py-2 text-right text-ink">{formatPercent(r.volatility)}</td>
                      <td className="px-3 py-2 text-right text-ink">{formatPercent(r.success_rate)}</td>
                      <td className="px-3 py-2 text-right text-ink">
                        {formatMoneyWan(r.terminal_p50_minor)}
                      </td>
                      <td className="px-3 py-2 text-right text-ink-muted">
                        {deltaLabel(
                          r.terminal_p50_minor,
                          baseline?.terminal_p50_minor ?? r.terminal_p50_minor,
                          isBaseline,
                        )}
                      </td>
                      <td className="px-3 py-2 text-right text-ink">
                        {formatMoneyWan(r.real_terminal_p50_minor)}
                      </td>
                      <td className="px-3 py-2 text-right text-ink">{formatPercent(r.max_drawdown_p50)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <p className="mt-2 text-xs text-ink-muted">
            采用资料库 {data.profile_id || "系统默认"}
            {data.profile_version ? ` v${data.profile_version}` : ""}，每情景 {data.runs} 条路径，种子{" "}
            <span className="font-mono-numeric">{data.seed}</span>。该比较为方向性预览，不写入历史运行。
          </p>
        </>
      )}
    </div>
  );
}
