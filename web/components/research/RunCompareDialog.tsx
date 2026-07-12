"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import ReactECharts from "echarts-for-react";
import {
  getRunPoints,
  listCollections,
  listRuns,
  type ResearchRunView,
} from "@/lib/api/research";
import { formatDateTimeFromMs, formatNullablePercent, formatPercent } from "@/lib/format";
import { Dialog } from "@/components/ui/Dialog";
import { LoadingState } from "@/components/ui/LoadingState";
import { runStatusLabel } from "@/components/research/runStatus";

function metricRow(
  label: string,
  a: string,
  b: string,
): { label: string; a: string; b: string } {
  return { label, a, b };
}

export interface RunCompareDialogProps {
  open: boolean;
  onClose: () => void;
  collectionId: string;
  baseRun: ResearchRunView;
}

/**
 * Compare the current run with another succeeded run — from this collection
 * by default, or from any other collection.
 */
export function RunCompareDialog({
  open,
  onClose,
  collectionId,
  baseRun,
}: RunCompareDialogProps) {
  const [otherCollectionId, setOtherCollectionId] = useState(collectionId);
  const [otherRunId, setOtherRunId] = useState("");

  const collectionsQuery = useQuery({
    queryKey: ["research", "collections"],
    queryFn: () => listCollections(),
    enabled: open,
  });

  const runsQuery = useQuery({
    queryKey: ["research", "runs", otherCollectionId, "full"],
    queryFn: () => listRuns(otherCollectionId, 100),
    enabled: open,
  });

  const candidates = useMemo(
    () =>
      (runsQuery.data?.runs ?? []).filter(
        (r) => r.id !== baseRun.id && r.status === "complete",
      ),
    [runsQuery.data, baseRun.id],
  );

  const otherRun = candidates.find((r) => r.id === otherRunId);

  const basePointsQuery = useQuery({
    queryKey: ["research", "run-points", baseRun.id, "compare"],
    queryFn: () => getRunPoints(baseRun.id),
    enabled: open && otherRunId !== "",
  });
  const otherPointsQuery = useQuery({
    queryKey: ["research", "run-points", otherRunId, "compare"],
    queryFn: () => getRunPoints(otherRunId),
    enabled: open && otherRunId !== "",
  });

  const chartOption = useMemo(() => {
    const base = basePointsQuery.data?.points ?? [];
    const other = otherPointsQuery.data?.points ?? [];
    if (base.length === 0 || other.length === 0) return null;
    const dateSet = new Set<string>();
    for (const p of base) dateSet.add(p.date);
    for (const p of other) dateSet.add(p.date);
    const dates = Array.from(dateSet).sort();
    const baseMap = new Map(base.map((p) => [p.date, p.nav]));
    const otherMap = new Map(other.map((p) => [p.date, p.nav]));
    return {
      tooltip: { trigger: "axis" as const },
      legend: { top: 0 },
      grid: { left: 56, right: 16, bottom: 32, top: 32 },
      xAxis: { type: "category" as const, data: dates, boundaryGap: false },
      yAxis: { type: "value" as const, scale: true },
      series: [
        {
          name: "当前 run",
          type: "line" as const,
          data: dates.map((d) => baseMap.get(d) ?? null),
          symbol: "none",
          lineStyle: { width: 2, color: "#0f172a" },
        },
        {
          name: "对比 run",
          type: "line" as const,
          data: dates.map((d) => otherMap.get(d) ?? null),
          symbol: "none",
          lineStyle: { width: 2, color: "#2563eb" },
        },
      ],
    };
  }, [basePointsQuery.data, otherPointsQuery.data]);

  const rows = useMemo(() => {
    if (!otherRun) return [];
    const a = baseRun.summary;
    const b = otherRun.summary;
    const pct = (v: number | null | undefined) => formatNullablePercent(v ?? null);
    const ratio = (v: number | null | undefined) => (v != null ? v.toFixed(2) : "—");
    return [
      metricRow("区间", `${baseRun.window_start} ~ ${baseRun.window_end}`, `${otherRun.window_start} ~ ${otherRun.window_end}`),
      metricRow("再平衡", baseRun.rebalance_policy, otherRun.rebalance_policy),
      metricRow("累计收益", pct(a?.cumulative_return), pct(b?.cumulative_return)),
      metricRow("CAGR", pct(a?.cagr), pct(b?.cagr)),
      metricRow("年化波动率", pct(a?.annual_volatility), pct(b?.annual_volatility)),
      metricRow("最大回撤", pct(a?.max_drawdown), pct(b?.max_drawdown)),
      metricRow("Sharpe", ratio(a?.sharpe), ratio(b?.sharpe)),
      metricRow("Calmar", ratio(a?.calmar), ratio(b?.calmar)),
      metricRow(
        "正收益月份占比",
        pct(a?.positive_month_ratio),
        pct(b?.positive_month_ratio),
      ),
    ];
  }, [baseRun, otherRun]);

  return (
    <Dialog open={open} onClose={onClose} title="与另一次运行对比" className="max-w-3xl">
      <div className="space-y-4">
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-ink-muted">对比集合</span>
          <select
            value={otherCollectionId}
            onChange={(e) => {
              setOtherCollectionId(e.target.value);
              setOtherRunId("");
            }}
            className="w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
            data-testid="compare-collection-select"
          >
            {!(collectionsQuery.data?.collections ?? []).some((c) => c.id === collectionId) && (
              <option value={collectionId}>当前集合</option>
            )}
            {(collectionsQuery.data?.collections ?? []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
                {c.id === collectionId ? "（当前）" : ""}
              </option>
            ))}
          </select>
        </label>

        <label className="block">
          <span className="mb-1 block text-xs font-medium text-ink-muted">选择对比运行</span>
          <select
            value={otherRunId}
            onChange={(e) => setOtherRunId(e.target.value)}
            className="w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
            data-testid="compare-run-select"
          >
            <option value="">请选择…</option>
            {candidates.map((r) => (
              <option key={r.id} value={r.id}>
                {r.window_start} ~ {r.window_end} · {r.rebalance_policy} ·{" "}
                {formatDateTimeFromMs(r.created_at)} · {runStatusLabel(r.status)}
                {r.summary ? ` · CAGR ${formatPercent(r.summary.cagr)}` : ""}
              </option>
            ))}
          </select>
        </label>

        {runsQuery.isLoading && <LoadingState label="加载运行列表…" />}
        {!runsQuery.isLoading && candidates.length === 0 && (
          <p className="text-sm text-ink-muted">
            该集合没有其他成功的运行可对比，可切换其他集合。
          </p>
        )}

        {otherRun && (
          <>
            <div className="overflow-x-auto">
              <table className="w-full text-sm" data-testid="compare-run-table">
                <thead>
                  <tr className="border-b border-line text-left text-xs text-ink-muted">
                    <th className="px-3 py-2 font-medium">指标</th>
                    <th className="px-3 py-2 font-medium">当前 run</th>
                    <th className="px-3 py-2 font-medium">对比 run</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={row.label} className="border-b border-line/60 last:border-0">
                      <td className="px-3 py-1.5 text-xs text-ink-muted">{row.label}</td>
                      <td className="px-3 py-1.5 font-mono-numeric text-xs">{row.a}</td>
                      <td className="px-3 py-1.5 font-mono-numeric text-xs">{row.b}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {basePointsQuery.isLoading || otherPointsQuery.isLoading ? (
              <LoadingState label="加载净值曲线…" />
            ) : chartOption ? (
              <ReactECharts option={chartOption} style={{ height: 300 }} notMerge />
            ) : null}
          </>
        )}
      </div>
    </Dialog>
  );
}
