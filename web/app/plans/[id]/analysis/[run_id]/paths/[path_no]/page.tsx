"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { getPathDetail } from "@/lib/api/simulations";
import { failureReasonLabel, formatMoney, formatPercent } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type { PathMonthRecord, PathYearRecord } from "@/types/api";

const ROW_HEIGHT = 36;

function VirtualTable<T>({
  rows,
  columns,
  height = 480,
}: {
  rows: T[];
  columns: { key: string; header: string; render: (row: T) => ReactNode; align?: "left" | "right" }[];
  height?: number;
}) {
  const [scrollTop, setScrollTop] = useState(0);
  const visible = Math.ceil(height / ROW_HEIGHT) + 2;
  const start = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT));
  const slice = rows.slice(start, start + visible);
  const offsetY = start * ROW_HEIGHT;

  return (
    <div
      className="overflow-auto rounded-lg border border-line"
      style={{ height }}
      onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
    >
      <table className="min-w-full text-sm">
        <thead className="sticky top-0 z-10 bg-surface-muted">
          <tr>
            {columns.map((c) => (
              <th
                key={c.key}
                className={`px-3 py-2 font-medium text-ink-muted ${c.align === "right" ? "text-right" : "text-left"}`}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody style={{ height: rows.length * ROW_HEIGHT }}>
          <tr style={{ height: offsetY }} aria-hidden />
          {slice.map((row, i) => (
            <tr key={start + i} className="border-t border-line" style={{ height: ROW_HEIGHT }}>
              {columns.map((c) => (
                <td key={c.key} className={`px-3 py-2 text-ink ${c.align === "right" ? "text-right" : ""}`}>
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

export default function PathDetailPage() {
  const params = useParams();
  const planId = params.id as string;
  const runId = params.run_id as string;
  const pathNo = Number(params.path_no);
  const [view, setView] = useState<"monthly" | "yearly">("monthly");

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

  const terminalWealth =
    data.monthly.length > 0 ? data.monthly[data.monthly.length - 1].total_wealth_minor : 0;

  const monthCols = [
    { key: "m", header: "月份", render: (m: PathMonthRecord) => m.month_offset },
    { key: "w", header: "资产", align: "right" as const, render: (m: PathMonthRecord) => formatMoney(m.total_wealth_minor) },
    { key: "i", header: "收入", align: "right" as const, render: (m: PathMonthRecord) => formatMoney(m.income_minor) },
    { key: "s", header: "支出", align: "right" as const, render: (m: PathMonthRecord) => formatMoney(m.spending_minor) },
    { key: "t", header: "税费", align: "right" as const, render: (m: PathMonthRecord) => formatMoney(m.tax_minor) },
    { key: "c", header: "交易成本", align: "right" as const, render: (m: PathMonthRecord) => formatMoney(m.transaction_cost) },
    { key: "d", header: "回撤", align: "right" as const, render: (m: PathMonthRecord) => formatPercent(m.drawdown) },
    { key: "r", header: "调仓", render: (m: PathMonthRecord) => (m.rebalanced ? "是" : "否") },
  ];

  const yearCols = [
    { key: "y", header: "年份", render: (y: PathYearRecord) => y.year },
    { key: "sw", header: "年初资产", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.start_wealth_minor) },
    { key: "i", header: "收入", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.income_minor) },
    { key: "s", header: "支出", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.spending_minor) },
    { key: "t", header: "税费", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.tax_minor) },
    { key: "c", header: "交易成本", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.transaction_cost) },
    { key: "g", header: "投资损益", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.investment_gain_loss) },
    { key: "ew", header: "期末资产", align: "right" as const, render: (y: PathYearRecord) => formatMoney(y.end_wealth_minor) },
    { key: "ydd", header: "年末回撤", align: "right" as const, render: (y: PathYearRecord) => formatPercent(y.year_end_drawdown) },
    { key: "idd", header: "年内最大回撤", align: "right" as const, render: (y: PathYearRecord) => formatPercent(y.max_intra_year_dd) },
    { key: "r", header: "调仓", render: (y: PathYearRecord) => (y.rebalanced ? "是" : "否") },
    {
      key: "wt",
      header: "年末权重",
      render: (y: PathYearRecord) =>
        y.asset_weights
          ? Object.entries(y.asset_weights)
              .map(([k, v]) => `${k}: ${formatPercent(v)}`)
              .join(" · ")
          : "—",
    },
  ];

  return (
    <div className="space-y-4">
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
          <dt className="text-sm text-ink-muted">期末资产</dt>
          <dd className="text-ink">{formatMoney(terminalWealth)}</dd>
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
            <dt className="text-sm text-ink-muted">失败原因</dt>
            <dd className="text-ink">{failureReasonLabel(data.failure_reason)}</dd>
          </div>
        )}
      </dl>

      <div className="flex gap-2">
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
      </div>

      {view === "monthly" ? (
        <VirtualTable rows={data.monthly} columns={monthCols} />
      ) : (
        <VirtualTable rows={data.yearly} columns={yearCols} height={400} />
      )}
    </div>
  );
}
