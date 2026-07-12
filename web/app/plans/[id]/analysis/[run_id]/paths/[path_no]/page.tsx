"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { Tooltip } from "@/components/ui/Tooltip";
import { getPathDetail } from "@/lib/api/simulations";
import {
  failureStatusLabel,
  formatMoneyWan,
  formatNullablePercent,
  formatPercent,
} from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type { PathAssetLabel, PathMonthRecord, PathYearRecord } from "@/types/api";

interface Column<T> {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  align?: "left" | "right";
}

/**
 * Plain scrollable table that renders every row. The data sizes here (≤ 420
 * monthly / ≤ 35 yearly) are small enough to render directly; spacer-based
 * virtualization left blank cells in some browsers, so it was removed.
 */
function ScrollTable<T>({
  rows,
  columns,
  height,
  emptyLabel,
  rowKey,
}: {
  rows: T[];
  columns: Column<T>[];
  height: number;
  emptyLabel: string;
  rowKey: (row: T, index: number) => string | number;
}) {
  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-line px-3 py-8 text-center text-sm text-ink-muted">
        {emptyLabel}
      </div>
    );
  }
  return (
    <div className="overflow-auto rounded-lg border border-line" style={{ height }}>
      <table className="min-w-full text-sm">
        <thead className="sticky top-0 z-10 bg-surface-muted">
          <tr>
            {columns.map((c) => (
              <th
                key={c.key}
                className={`px-3 py-2 font-medium text-ink-muted ${
                  c.align === "right" ? "text-right" : "text-left"
                }`}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={rowKey(row, i)} className="border-t border-line">
              {columns.map((c) => (
                <td
                  key={c.key}
                  className={`px-3 py-2 text-ink ${c.align === "right" ? "text-right" : ""}`}
                >
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <p className="p-2 text-xs text-ink-muted">共 {rows.length} 行</p>
    </div>
  );
}

/** Render a frozen-snapshot weight entry as `名称（代码）: 70.00%`, never a holding UUID. */
function assetWeightLabel(
  holdingId: string,
  weight: number,
  labels: Record<string, PathAssetLabel> | undefined,
): string {
  const pct = `${(weight * 100).toFixed(2)}%`;
  const label = labels?.[holdingId];
  if (!label) {
    return `未知资产: ${pct}`;
  }
  if (label.is_cash) {
    return `现金/其他: ${pct}`;
  }
  const code = label.instrument_code ? `（${label.instrument_code}）` : "";
  const name = label.instrument_name || "未知资产";
  return `${name}${code}: ${pct}`;
}

/**
 * Year-end allocation cell: keeps the table narrow by hiding the per-asset weight
 * breakdown behind a hover/focus/click `查看` control whose tooltip lists
 * `名称（代码）: xx.xx%` (cash and unknown assets degrade gracefully, never a
 * holding ID). Rows without weights render `—` and create no tooltip.
 */
function YearEndWeightsCell({
  year,
  weights,
  labels,
}: {
  year: number;
  weights: Record<string, number> | undefined;
  labels: Record<string, PathAssetLabel> | undefined;
}) {
  const entries = weights ? Object.entries(weights) : [];
  if (entries.length === 0) {
    return <span className="text-ink-muted">—</span>;
  }
  return (
    <Tooltip
      content={
        <ul className="space-y-0.5">
          {entries.map(([k, v]) => (
            <li key={k}>{assetWeightLabel(k, v, labels)}</li>
          ))}
        </ul>
      }
      align="center"
      clickToggle
      contentTestId={`year-weights-tooltip-${year}`}
      contentClassName="max-w-xs"
    >
      <button
        type="button"
        aria-label={`查看 ${year} 年末资产配置`}
        className="text-brand underline-offset-2 hover:underline"
      >
        查看
      </button>
    </Tooltip>
  );
}

export default function PathDetailPage() {
  const params = useParams();
  const planId = params.id as string;
  const runId = params.run_id as string;
  const pathNo = Number(params.path_no);
  const [view, setView] = useState<"monthly" | "yearly">("monthly");
  const [caliber, setCaliber] = useState<"nominal" | "real">("nominal");

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["path", runId, pathNo],
    queryFn: () => getPathDetail(runId, pathNo),
  });

  const maxDrawdown = useMemo(
    () => data?.monthly.reduce((max, m) => (m.drawdown > max ? m.drawdown : max), 0) ?? 0,
    [data],
  );

  const settingsHref = `/plans/${planId}/settings?section=simulation`;

  if (isLoading && !data) return <LoadingState label="加载路径详情…" />;
  if (isError && !data) {
    return (
      <ErrorState
        message="无法加载路径详情。请确认后端服务可用后重试。"
        onRetry={() => void refetch()}
        backHref={settingsHref}
        backLabel="返回分析中心"
        technicalDetail={queryErrorMessage(error)}
      />
    );
  }
  if (!data) return null;

  const assetLabels = data.asset_labels;
  const real = caliber === "real";
  const caliberLabel = real ? "起点购买力" : "名义金额";
  const lastMonth = data.monthly.length > 0 ? data.monthly[data.monthly.length - 1] : null;
  const terminalWealth = lastMonth
    ? real
      ? lastMonth.real_total_wealth_minor
      : lastMonth.total_wealth_minor
    : 0;

  const monthCols: Column<PathMonthRecord>[] = [
    { key: "m", header: "月份", render: (m) => m.month_offset },
    {
      key: "w",
      header: `资产（${caliberLabel}）`,
      align: "right",
      render: (m) => formatMoneyWan(real ? m.real_total_wealth_minor : m.total_wealth_minor),
    },
    { key: "i", header: "收入", align: "right", render: (m) => formatMoneyWan(m.income_minor) },
    {
      key: "sr",
      header: "请求支出",
      align: "right",
      render: (m) => formatMoneyWan(m.spending_requested_minor ?? m.spending_minor),
    },
    { key: "s", header: "实际支出", align: "right", render: (m) => formatMoneyWan(m.spending_minor) },
    {
      key: "u",
      header: "未满足支出",
      align: "right",
      render: (m) => formatMoneyWan(m.unfunded_spending_minor ?? 0),
    },
    { key: "t", header: "税费", align: "right", render: (m) => formatMoneyWan(m.tax_minor) },
    { key: "c", header: "交易成本", align: "right", render: (m) => formatMoneyWan(m.transaction_cost) },
    { key: "d", header: "回撤", align: "right", render: (m) => formatPercent(m.drawdown) },
    { key: "r", header: "调仓", render: (m) => (m.rebalanced ? "是" : "否") },
  ];

  const yearCols: Column<PathYearRecord>[] = [
    { key: "y", header: "年份", render: (y) => y.year },
    {
      key: "sw",
      header: `年初资产（${caliberLabel}）`,
      align: "right",
      render: (y) => formatMoneyWan(real ? y.real_start_wealth_minor : y.start_wealth_minor),
    },
    { key: "i", header: "收入", align: "right", render: (y) => formatMoneyWan(y.income_minor) },
    { key: "s", header: "支出", align: "right", render: (y) => formatMoneyWan(y.spending_minor) },
    { key: "t", header: "税费", align: "right", render: (y) => formatMoneyWan(y.tax_minor) },
    { key: "c", header: "交易成本", align: "right", render: (y) => formatMoneyWan(y.transaction_cost) },
    { key: "g", header: "投资损益", align: "right", render: (y) => formatMoneyWan(y.investment_gain_loss) },
    {
      key: "ar",
      header: "年末收益率",
      align: "right",
      render: (y) => formatNullablePercent(y.annual_return),
    },
    {
      key: "ew",
      header: `期末资产（${caliberLabel}）`,
      align: "right",
      render: (y) => formatMoneyWan(real ? y.real_end_wealth_minor : y.end_wealth_minor),
    },
    {
      key: "ydd",
      header: (
        <span className="inline-flex items-center justify-end">
          年末回撤
          <MetricHelp text="年末资产相对路径历史峰值的回撤幅度，不是该年度的投资收益率。" />
        </span>
      ),
      align: "right",
      render: (y) => formatPercent(y.year_end_drawdown),
    },
    { key: "idd", header: "年内最大回撤", align: "right", render: (y) => formatPercent(y.max_intra_year_dd) },
    { key: "r", header: "调仓", render: (y) => (y.rebalanced ? "是" : "否") },
    {
      key: "wt",
      header: "年末配置",
      render: (y) => (
        <YearEndWeightsCell year={y.year} weights={y.asset_weights} labels={assetLabels} />
      ),
    },
  ];

  return (
    <div className="content-enter space-y-4">
      <Link
        href={settingsHref}
        className="inline-flex text-sm text-ink-muted underline-offset-2 hover:text-ink hover:underline"
      >
        ← 返回分析中心
      </Link>
      <h1 className="text-xl font-semibold text-ink">路径 #{data.path_no}</h1>
      <dl className="grid gap-3 sm:grid-cols-2">
        <div>
          <dt className="text-sm text-ink-muted">路径种子</dt>
          <dd className="font-mono-numeric text-sm text-ink">{data.path_seed}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-muted">是否成功</dt>
          <dd className="text-ink">{data.succeeded ? "是" : "否"}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-muted">期末资产（{caliberLabel}）</dt>
          <dd className="text-ink">{formatMoneyWan(terminalWealth)}</dd>
        </div>
        <div>
          <dt className="text-sm text-ink-muted">全路径最大回撤</dt>
          <dd className="text-ink">{formatPercent(maxDrawdown)}</dd>
        </div>
        {data.failure_month != null && (
          <div>
            <dt className="text-sm text-ink-muted">失败月份</dt>
            <dd className="text-ink">{data.failure_month}</dd>
          </div>
        )}
        {data.failure_reason && (
          <div className="sm:col-span-2">
            <dt className="text-sm text-ink-muted">失败状态</dt>
            <dd className="text-ink">{failureStatusLabel(data.failure_reason)}</dd>
          </div>
        )}
      </dl>

      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant={view === "monthly" ? "primary" : "secondary"}
          className="px-3 py-1"
          onClick={() => setView("monthly")}
        >
          月度 ({data.monthly.length})
        </Button>
        <Button
          variant={view === "yearly" ? "primary" : "secondary"}
          className="px-3 py-1"
          onClick={() => setView("yearly")}
        >
          年度 ({data.yearly.length})
        </Button>
        <span className="ml-2 text-sm text-ink-muted">金额口径</span>
        <div role="group" aria-label="金额口径" className="flex gap-1">
          {(["nominal", "real"] as const).map((opt) => (
            <button
              key={opt}
              type="button"
              aria-pressed={caliber === opt}
              onClick={() => setCaliber(opt)}
              className={
                caliber === opt
                  ? "rounded border border-brand bg-brand/10 px-2 py-1 text-sm font-medium text-brand-strong"
                  : "rounded border border-line px-2 py-1 text-sm text-ink-muted hover:text-ink"
              }
            >
              {opt === "real" ? "起点购买力" : "名义金额"}
            </button>
          ))}
        </div>
      </div>

      {view === "monthly" ? (
        <ScrollTable
          rows={data.monthly}
          columns={monthCols}
          height={480}
          emptyLabel="暂无月度路径数据"
          rowKey={(m) => m.month_offset}
        />
      ) : (
        <ScrollTable
          rows={data.yearly}
          columns={yearCols}
          height={400}
          emptyLabel="暂无年度路径数据"
          rowKey={(y) => y.year}
        />
      )}
    </div>
  );
}
