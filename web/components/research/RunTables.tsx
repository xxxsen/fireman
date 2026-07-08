"use client";

import { useMemo } from "react";
import type {
  ResearchBacktestMonth,
  ResearchBacktestYear,
  ResearchDataQuality,
  ResearchRunSummary,
} from "@/lib/api/research";
import { dataSourceLabel, formatNullablePercent, formatPercent, pointTypeLabel } from "@/lib/format";
import { Badge } from "@/components/ui/Badge";

// --- annual table ---

export function RunAnnualTable({
  years,
  weightDeviations,
}: {
  years: ResearchBacktestYear[];
  weightDeviations?: Map<number, { start: number | null; end: number | null }>;
}) {
  if (years.length === 0) {
    return <p className="text-sm text-ink-muted">暂无年度数据。</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-sm" data-testid="annual-table">
        <thead>
          <tr className="border-b border-line text-left text-xs text-ink-muted">
            <th className="px-3 py-2 font-medium">年份</th>
            <th className="px-3 py-2 text-right font-medium">收益</th>
            <th className="px-3 py-2 text-right font-medium">年内波动率</th>
            <th className="px-3 py-2 text-right font-medium">年内最大回撤</th>
            <th className="px-3 py-2 text-right font-medium">年初/年末权重偏离</th>
            <th className="px-3 py-2 font-medium">完整性</th>
          </tr>
        </thead>
        <tbody>
          {years.map((y) => {
            const dev = weightDeviations?.get(y.year);
            return (
              <tr key={y.year} className="border-b border-line/60 last:border-0">
                <td className="px-3 py-2 font-mono-numeric">{y.year}</td>
                <td
                  className={
                    "px-3 py-2 text-right font-mono-numeric " +
                    (y.annual_return >= 0 ? "text-positive" : "text-danger")
                  }
                >
                  {formatPercent(y.annual_return)}
                </td>
                <td className="px-3 py-2 text-right font-mono-numeric text-xs">
                  {formatPercent(y.volatility)}
                </td>
                <td className="px-3 py-2 text-right font-mono-numeric text-xs text-danger">
                  {formatPercent(y.max_drawdown)}
                </td>
                <td className="px-3 py-2 text-right font-mono-numeric text-xs">
                  {dev
                    ? `${formatNullablePercent(dev.start)} / ${formatNullablePercent(dev.end)}`
                    : "—"}
                </td>
                <td className="px-3 py-2">
                  {y.is_partial ? (
                    <Badge variant="warning">不完整年份</Badge>
                  ) : (
                    <Badge variant="neutral">完整</Badge>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// --- monthly heatmap ---

function heatColor(value: number): string {
  // Positive -> green, negative -> red; intensity capped at ±8%.
  const capped = Math.max(-0.08, Math.min(0.08, value));
  const alpha = Math.min(1, Math.abs(capped) / 0.08) * 0.75;
  return value >= 0
    ? `rgba(22,163,74,${alpha.toFixed(3)})`
    : `rgba(220,38,38,${alpha.toFixed(3)})`;
}

export function RunMonthlyHeatmap({ months }: { months: ResearchBacktestMonth[] }) {
  const byYear = useMemo(() => {
    const map = new Map<number, Map<number, number>>();
    for (const m of months) {
      const row = map.get(m.year) ?? new Map<number, number>();
      row.set(m.month, m.monthly_return);
      map.set(m.year, row);
    }
    return Array.from(map.entries()).sort((a, b) => a[0] - b[0]);
  }, [months]);

  if (months.length === 0) {
    return <p className="text-sm text-ink-muted">暂无月度数据。</p>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[760px] text-xs" data-testid="monthly-heatmap">
        <thead>
          <tr className="border-b border-line text-left text-ink-muted">
            <th className="px-2 py-1.5 font-medium">年份</th>
            {Array.from({ length: 12 }, (_, i) => (
              <th key={i + 1} className="px-1 py-1.5 text-center font-medium">
                {i + 1}月
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {byYear.map(([year, row]) => (
            <tr key={year} className="border-b border-line/40 last:border-0">
              <td className="px-2 py-1 font-mono-numeric font-medium">{year}</td>
              {Array.from({ length: 12 }, (_, i) => {
                const v = row.get(i + 1);
                return (
                  <td
                    key={i + 1}
                    className="px-1 py-1 text-center font-mono-numeric"
                    style={v != null ? { backgroundColor: heatColor(v) } : undefined}
                    title={v != null ? `${year}-${i + 1}: ${formatPercent(v)}` : undefined}
                  >
                    {v != null ? formatPercent(v) : ""}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// --- contributions ---

export function RunContributions({ summary }: { summary: ResearchRunSummary }) {
  const contributions = summary.contributions ?? [];
  if (contributions.length === 0) {
    return <p className="text-sm text-ink-muted">暂无贡献数据。</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-sm" data-testid="contributions-table">
        <thead>
          <tr className="border-b border-line text-left text-xs text-ink-muted">
            <th className="px-3 py-2 font-medium">资产</th>
            <th className="px-3 py-2 text-right font-medium">目标权重</th>
            <th className="px-3 py-2 text-right font-medium">期末权重</th>
            <th className="px-3 py-2 text-right font-medium">累计收益贡献</th>
            <th className="px-3 py-2 text-right font-medium">风险贡献</th>
            <th className="px-3 py-2 text-right font-medium">回撤期贡献</th>
          </tr>
        </thead>
        <tbody>
          {contributions.map((c) => (
            <tr key={c.asset_key} className="border-b border-line/60 last:border-0">
              <td className="px-3 py-2">
                <span className="block max-w-48 truncate font-medium text-ink">{c.name}</span>
                <span className="block text-xs text-ink-muted">{c.asset_key}</span>
              </td>
              <td className="px-3 py-2 text-right font-mono-numeric text-xs">
                {formatPercent(c.target_weight)}
              </td>
              <td className="px-3 py-2 text-right font-mono-numeric text-xs">
                {formatPercent(c.end_weight)}
              </td>
              <td
                className={
                  "px-3 py-2 text-right font-mono-numeric text-xs " +
                  (c.cumulative_contribution >= 0 ? "text-positive" : "text-danger")
                }
              >
                {formatPercent(c.cumulative_contribution)}
              </td>
              <td className="px-3 py-2 text-right font-mono-numeric text-xs">
                {formatNullablePercent(c.risk_contribution)}
              </td>
              <td className="px-3 py-2 text-right font-mono-numeric text-xs text-danger">
                {formatPercent(c.drawdown_contribution)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// --- correlation matrix ---

export function RunCorrelationMatrix({ summary }: { summary: ResearchRunSummary }) {
  const corr = summary.correlations;
  if (!corr || corr.asset_keys.length < 2) {
    return <p className="text-sm text-ink-muted">资产数量不足，无相关性矩阵。</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="text-xs" data-testid="correlation-matrix">
        <thead>
          <tr className="text-left text-ink-muted">
            <th className="px-2 py-1.5"></th>
            {corr.names.map((name, i) => (
              <th key={corr.asset_keys[i]} className="max-w-28 truncate px-2 py-1.5 font-medium">
                {name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {corr.names.map((name, i) => (
            <tr key={corr.asset_keys[i]}>
              <td className="max-w-28 truncate px-2 py-1.5 font-medium text-ink">{name}</td>
              {corr.matrix[i]?.map((v, j) => (
                <td
                  key={corr.asset_keys[j]}
                  className="px-2 py-1.5 text-center font-mono-numeric"
                  style={
                    v != null && i !== j
                      ? { backgroundColor: `rgba(15,23,42,${(Math.abs(v) * 0.25).toFixed(3)})` }
                      : undefined
                  }
                >
                  {v == null ? "—" : v.toFixed(2)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// --- data quality ---

function QualityRow({
  label,
  q,
  extra,
}: {
  label: string;
  q: {
    raw_start?: string;
    raw_end?: string;
    raw_point_count: number;
    usable_start?: string;
    usable_end?: string;
    fill_count: number;
    max_fill_gap_days: number;
    fill_tolerance_days: number;
    fill_gap_exceeded: boolean;
    limits_common_start?: boolean;
    limits_common_end?: boolean;
  };
  extra?: string;
}) {
  return (
    <tr className="border-b border-line/60 last:border-0">
      <td className="px-3 py-2">
        <span className="block max-w-52 truncate font-medium text-ink">{label}</span>
        {extra && <span className="block text-xs text-ink-muted">{extra}</span>}
      </td>
      <td className="px-3 py-2 font-mono-numeric text-xs">
        {q.raw_start && q.raw_end ? `${q.raw_start} ~ ${q.raw_end}` : "—"}
        <span className="ml-1 text-ink-muted">({q.raw_point_count})</span>
      </td>
      <td className="px-3 py-2 font-mono-numeric text-xs">
        {q.usable_start && q.usable_end ? `${q.usable_start} ~ ${q.usable_end}` : "—"}
      </td>
      <td className="px-3 py-2 text-right font-mono-numeric text-xs">
        {q.fill_count}
        <span className="ml-1 text-ink-muted">（最长 {q.max_fill_gap_days} 天 / 容忍 {q.fill_tolerance_days} 天）</span>
        {q.fill_gap_exceeded && (
          <Badge variant="warning" className="ml-1">
            超容忍
          </Badge>
        )}
      </td>
      <td className="px-3 py-2 text-xs">
        {q.limits_common_start && <Badge variant="info">决定起点</Badge>}
        {q.limits_common_end && (
          <Badge variant="info" className="ml-1">
            决定终点
          </Badge>
        )}
      </td>
    </tr>
  );
}

export function RunDataQuality({
  quality,
  sources,
}: {
  quality: ResearchDataQuality;
  sources?: { asset_key: string; source_name?: string; point_type?: string; points_hash?: string }[];
}) {
  const assets = Array.isArray(quality.assets) ? quality.assets : [];
  const fx = Array.isArray(quality.fx) ? quality.fx : [];
  const sourceByKey = useMemo(() => {
    const map = new Map<string, { source_name?: string; point_type?: string; points_hash?: string }>();
    for (const s of sources ?? []) map.set(s.asset_key, s);
    return map;
  }, [sources]);

  return (
    <div className="space-y-3" data-testid="data-quality">
      <dl className="grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
        <div>
          <dt className="text-ink-muted">共同区间</dt>
          <dd className="font-mono-numeric font-medium text-ink">
            {quality.common_start} ~ {quality.common_end}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">回测区间</dt>
          <dd className="font-mono-numeric font-medium text-ink">
            {quality.window_start} ~ {quality.window_end}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">区间策略</dt>
          <dd className="font-medium text-ink">
            {quality.common_start_policy === "max_first_usable"
              ? "各资产可用起点取最大"
              : quality.common_start_policy}
            {" / "}
            {quality.common_end_policy === "min_last_usable"
              ? "各资产可用终点取最小"
              : quality.common_end_policy}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">最大前值填充</dt>
          <dd className="font-medium text-ink">{quality.forward_fill_days_max} 天</dd>
        </div>
      </dl>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[820px] text-sm">
          <thead>
            <tr className="border-b border-line text-left text-xs text-ink-muted">
              <th className="px-3 py-2 font-medium">序列</th>
              <th className="px-3 py-2 font-medium">原始区间（点数）</th>
              <th className="px-3 py-2 font-medium">可用区间</th>
              <th className="px-3 py-2 text-right font-medium">填充</th>
              <th className="px-3 py-2 font-medium">共同区间影响</th>
            </tr>
          </thead>
          <tbody>
            {assets.map((q) => {
              const src = sourceByKey.get(q.asset_key ?? "");
              const extras: string[] = [];
              if (q.is_cash) extras.push("现金（恒值 1.0）");
              if (src?.source_name) extras.push(dataSourceLabel(src.source_name));
              if (src?.point_type) extras.push(pointTypeLabel(src.point_type));
              if (q.fx_pair) extras.push(`FX ${q.fx_pair}`);
              if (src?.points_hash) extras.push(`hash ${src.points_hash.slice(0, 12)}…`);
              return (
                <QualityRow
                  key={`asset-${q.asset_key}`}
                  label={q.name || q.asset_key || "资产"}
                  q={q}
                  extra={extras.join(" · ") || undefined}
                />
              );
            })}
            {fx.map((q) => (
              <QualityRow key={`fx-${q.pair}`} label={`汇率 ${q.pair ?? ""}`} q={q} />
            ))}
            {quality.benchmark && (
              <QualityRow label={`基准 ${quality.benchmark.name ?? ""}`} q={quality.benchmark} />
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
