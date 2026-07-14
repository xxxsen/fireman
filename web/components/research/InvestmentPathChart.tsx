"use client";

import { useMemo, useState, type PointerEvent as ReactPointerEvent } from "react";
import type { InvestmentPathPoint } from "@/lib/api/investment-paths";
import { formatMoney } from "@/lib/format";

const WIDTH = 840;
const HEIGHT = 320;
const MARGIN = { top: 18, right: 22, bottom: 58, left: 82 };
const PLOT_WIDTH = WIDTH - MARGIN.left - MARGIN.right;
const PLOT_HEIGHT = HEIGHT - MARGIN.top - MARGIN.bottom;

function compactMoney(minor: number, currency: string): string {
  const major = minor / 100;
  const symbol = currency === "CNY" ? "¥" : currency === "USD" ? "$" : currency === "HKD" ? "HK$" : `${currency} `;
  const abs = Math.abs(major);
  if (abs >= 100_000_000) return `${symbol}${(major / 100_000_000).toFixed(1)}亿`;
  if (abs >= 10_000) return `${symbol}${(major / 10_000).toFixed(abs >= 100_000 ? 0 : 1)}万`;
  return `${symbol}${major.toFixed(0)}`;
}

function niceScaleMaximum(value: number): number {
  if (value <= 0) return 1;
  const roughStep = value / 4;
  const magnitude = 10 ** Math.floor(Math.log10(roughStep));
  const fraction = roughStep / magnitude;
  const niceFraction = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
  const step = niceFraction * magnitude;
  return Math.ceil(value / step) * step;
}

function uniqueTickIndexes(length: number): number[] {
  return Array.from(new Set([0, 0.25, 0.5, 0.75, 1].map((ratio) => Math.round((length - 1) * ratio))));
}

export interface InvestmentPathChartProps {
  points: InvestmentPathPoint[];
  currency: string;
}

export function InvestmentPathChart({ points, currency }: InvestmentPathChartProps) {
  const [activeIndex, setActiveIndex] = useState<number | null>(null);
  const scaleMaximum = useMemo(
    () => niceScaleMaximum(points.reduce(
      (maximum, point) => Math.max(maximum, point.account_value_minor, point.cumulative_external_contribution_minor),
      1,
    )),
    [points],
  );
  const x = (index: number) => MARGIN.left + (index / Math.max(points.length - 1, 1)) * PLOT_WIDTH;
  const y = (value: number) => MARGIN.top + PLOT_HEIGHT - (value / scaleMaximum) * PLOT_HEIGHT;
  const path = (pick: (point: InvestmentPathPoint) => number) => points.map((point, index) =>
    `${index === 0 ? "M" : "L"}${x(index).toFixed(1)},${y(pick(point)).toFixed(1)}`,
  ).join(" ");
  const accountPath = path((point) => point.account_value_minor);
  const contributionPath = path((point) => point.cumulative_external_contribution_minor);

  if (points.length < 2) return <p className="text-sm text-ink-muted">没有足够路径点。</p>;

  const xTicks = uniqueTickIndexes(points.length);
  const yTicks = Array.from({ length: 5 }, (_, index) => (scaleMaximum * index) / 4);
  const active = activeIndex == null ? null : points[activeIndex];
  const activeX = activeIndex == null ? 0 : x(activeIndex);
  const tooltipWidth = 238;
  const tooltipX = Math.min(Math.max(activeX + 10, MARGIN.left + 4), WIDTH - MARGIN.right - tooltipWidth);

  const selectPointerPoint = (event: ReactPointerEvent<SVGRectElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const svgX = MARGIN.left + ((event.clientX - rect.left) / rect.width) * PLOT_WIDTH;
    const index = Math.round(((svgX - MARGIN.left) / PLOT_WIDTH) * (points.length - 1));
    setActiveIndex(Math.max(0, Math.min(points.length - 1, index)));
  };

  return (
    <div className="overflow-x-auto">
      <svg
        viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
        className="min-w-[680px]"
        role="img"
        aria-label="账户价值与累计投入的历史路径折线图"
      >
        <title>账户价值与累计投入路径</title>
        <desc>横轴为日期，纵轴为账户金额；实线表示账户价值，虚线表示累计投入。</desc>

        {yTicks.map((value) => {
          const tickY = y(value);
          return (
            <g key={value}>
              <line x1={MARGIN.left} x2={WIDTH - MARGIN.right} y1={tickY} y2={tickY} className="stroke-line" strokeWidth="1" />
              <text x={MARGIN.left - 10} y={tickY + 4} textAnchor="end" className="fill-ink-muted text-[11px]">
                {compactMoney(value, currency)}
              </text>
            </g>
          );
        })}
        {xTicks.map((index) => {
          const tickX = x(index);
          return (
            <g key={index}>
              <line x1={tickX} x2={tickX} y1={MARGIN.top + PLOT_HEIGHT} y2={MARGIN.top + PLOT_HEIGHT + 5} className="stroke-line" />
              <text x={tickX} y={MARGIN.top + PLOT_HEIGHT + 22} textAnchor="middle" className="fill-ink-muted text-[11px]">
                {points[index].valuation_date}
              </text>
            </g>
          );
        })}
        <line x1={MARGIN.left} x2={WIDTH - MARGIN.right} y1={MARGIN.top + PLOT_HEIGHT} y2={MARGIN.top + PLOT_HEIGHT} className="stroke-ink-muted" />
        <line x1={MARGIN.left} x2={MARGIN.left} y1={MARGIN.top} y2={MARGIN.top + PLOT_HEIGHT} className="stroke-ink-muted" />
        <text x={MARGIN.left + PLOT_WIDTH / 2} y={HEIGHT - 8} textAnchor="middle" className="fill-ink-muted text-xs">日期</text>
        <text
          x={16}
          y={MARGIN.top + PLOT_HEIGHT / 2}
          textAnchor="middle"
          transform={`rotate(-90 16 ${MARGIN.top + PLOT_HEIGHT / 2})`}
          className="fill-ink-muted text-xs"
        >
          账户金额（{currency}）
        </text>

        <path d={accountPath} fill="none" stroke="currentColor" strokeWidth="3" className="text-brand" />
        <path d={contributionPath} fill="none" stroke="currentColor" strokeWidth="2" strokeDasharray="7 5" className="text-ink-muted" />

        {active && activeIndex != null ? (
          <g data-testid="investment-path-chart-tooltip" pointerEvents="none">
            <line x1={activeX} x2={activeX} y1={MARGIN.top} y2={MARGIN.top + PLOT_HEIGHT} className="stroke-ink-muted" strokeDasharray="3 3" />
            <circle cx={activeX} cy={y(active.account_value_minor)} r="4" className="fill-brand stroke-surface" strokeWidth="2" />
            <circle cx={activeX} cy={y(active.cumulative_external_contribution_minor)} r="4" className="fill-ink-muted stroke-surface" strokeWidth="2" />
            <g transform={`translate(${tooltipX} ${MARGIN.top + 8})`}>
              <rect width={tooltipWidth} height="72" rx="7" className="fill-surface stroke-line" />
              <text x="10" y="18" className="fill-ink text-[11px] font-medium">{active.valuation_date}</text>
              <text x="10" y="39" className="fill-ink-muted text-[11px]">账户价值</text>
              <text x={tooltipWidth - 10} y="39" textAnchor="end" className="fill-ink text-[11px]">{formatMoney(active.account_value_minor, currency)}</text>
              <text x="10" y="59" className="fill-ink-muted text-[11px]">累计投入</text>
              <text x={tooltipWidth - 10} y="59" textAnchor="end" className="fill-ink text-[11px]">{formatMoney(active.cumulative_external_contribution_minor, currency)}</text>
            </g>
          </g>
        ) : null}

        <rect
          data-testid="investment-path-chart-hit-area"
          x={MARGIN.left}
          y={MARGIN.top}
          width={PLOT_WIDTH}
          height={PLOT_HEIGHT}
          fill="transparent"
          tabIndex={0}
          role="slider"
          aria-label="查看投入路径日期点详情"
          aria-valuemin={0}
          aria-valuemax={points.length - 1}
          aria-valuenow={activeIndex ?? 0}
          aria-valuetext={active ? `${active.valuation_date}，账户价值 ${formatMoney(active.account_value_minor, currency)}，累计投入 ${formatMoney(active.cumulative_external_contribution_minor, currency)}` : "使用左右方向键选择日期"}
          onPointerMove={selectPointerPoint}
          onPointerDown={selectPointerPoint}
          onPointerLeave={() => setActiveIndex(null)}
          onFocus={() => setActiveIndex((index) => index ?? 0)}
          onBlur={() => setActiveIndex(null)}
          onKeyDown={(event) => {
            if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
              event.preventDefault();
              const delta = event.key === "ArrowLeft" ? -1 : 1;
              setActiveIndex((index) => Math.max(0, Math.min(points.length - 1, (index ?? 0) + delta)));
            } else if (event.key === "Home" || event.key === "End") {
              event.preventDefault();
              setActiveIndex(event.key === "Home" ? 0 : points.length - 1);
            }
          }}
        />
      </svg>
    </div>
  );
}
