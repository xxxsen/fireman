"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Dialog } from "@/components/ui/Dialog";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { getPlan } from "@/lib/api/plans";
import {
  buyRebalanceExecution,
  cancelRebalanceExecution,
  completeRebalanceExecution,
  getRebalanceExecution,
  noteRebalanceExecution,
  sellRebalanceExecution,
  skipRebalanceExecutionLine,
} from "@/lib/api/rebalance-executions";
import { formatMoney, rebalanceActionLabel } from "@/lib/format";
import { ApiError } from "@/lib/api/client";
import type { RebalanceExecutionEvent, RebalanceExecutionLine } from "@/types/api";

type ModalKind = "sell" | "buy" | "note" | null;

function statusLabel(status: string): string {
  switch (status) {
    case "draft":
    case "in_progress":
      return "进行中";
    case "completed":
      return "已完成";
    case "canceled":
    case "cancelled":
      return "已放弃";
    default:
      return status;
  }
}

function lineStatusLabel(status: string): string {
  switch (status) {
    case "not_started":
      return "未开始";
    case "partial":
      return "执行中";
    case "done":
      return "已完成";
    case "skipped":
      return "跳过";
    default:
      return status;
  }
}

function formatSignedDelta(minor: number): string {
  if (minor === 0) return formatMoney(0);
  const prefix = minor > 0 ? "+" : "-";
  return `${prefix}${formatMoney(Math.abs(minor))}`;
}

function parseEventSummary(event: RebalanceExecutionEvent): string {
  try {
    const payload = JSON.parse(event.payload_json) as { summary?: string; note?: string };
    if (payload.summary) return payload.summary;
    if (payload.note) return payload.note;
  } catch {
    /* ignore */
  }
  if (event.event_type === "complete") return "标记本次调仓执行完成";
  return event.event_type;
}

export default function RebalanceExecutionWorkspacePage() {
  const planId = useParams().id as string;
  const executionId = useParams().executionId as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [modal, setModal] = useState<ModalKind>(null);
  const [selectedLine, setSelectedLine] = useState<RebalanceExecutionLine | null>(null);
  const [amountMinor, setAmountMinor] = useState(0);
  const [noteText, setNoteText] = useState("");
  const [error, setError] = useState<string | null>(null);

  const plan = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const detail = useQuery({
    queryKey: ["rebalance-execution", planId, executionId],
    queryFn: () => getRebalanceExecution(planId, executionId),
  });

  const readonly =
    detail.data?.execution.status === "completed" ||
    detail.data?.execution.status === "canceled" ||
    detail.data?.execution.status === "cancelled";

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["rebalance-execution", planId, executionId] });
    void queryClient.invalidateQueries({ queryKey: ["rebalance-executions", planId] });
    void queryClient.invalidateQueries({ queryKey: ["rebalance-execution-active", planId] });
    void queryClient.invalidateQueries({ queryKey: ["dashboard", planId] });
    void queryClient.invalidateQueries({ queryKey: ["rebalance", planId] });
  };

  const tradeMut = useMutation({
    mutationFn: async () => {
      if (!selectedLine) throw new Error("未选择资产");
      const body = { line_id: selectedLine.id, amount_minor: amountMinor, note: noteText };
      if (modal === "sell") return sellRebalanceExecution(planId, executionId, body);
      if (modal === "buy") return buyRebalanceExecution(planId, executionId, body);
      throw new Error("invalid modal");
    },
    onSuccess: () => {
      setModal(null);
      setSelectedLine(null);
      setAmountMinor(0);
      setNoteText("");
      setError(null);
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "操作失败"),
  });

  const noteMut = useMutation({
    mutationFn: () => noteRebalanceExecution(planId, executionId, { note: noteText }),
    onSuccess: () => {
      setModal(null);
      setNoteText("");
      setError(null);
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "记录备注失败"),
  });

  const completeMut = useMutation({
    mutationFn: () => {
      const version = plan.data?.config_version ?? detail.data?.execution.baseline_config_version ?? 0;
      return completeRebalanceExecution(planId, executionId, { config_version: version });
    },
    onSuccess: () => {
      invalidate();
      router.push(`/plans/${planId}/rebalance?execution_completed=1`);
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "完成失败"),
  });

  const cancelMut = useMutation({
    mutationFn: () => cancelRebalanceExecution(planId, executionId),
    onSuccess: () => {
      invalidate();
      router.push(`/plans/${planId}/rebalance`);
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "放弃失败"),
  });

  const skipMut = useMutation({
    mutationFn: (lineId: string) => skipRebalanceExecutionLine(planId, executionId, { line_id: lineId }),
    onSuccess: () => {
      setError(null);
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "标记跳过失败"),
  });

  const visibleLines = useMemo(
    () => (detail.data?.lines ?? []).filter((line) => line.action_direction !== "hold"),
    [detail.data?.lines],
  );

  const firstSellLine = useMemo(
    () =>
      visibleLines.find(
        (line) =>
          line.action_direction === "decrease" &&
          line.execution_status !== "done" &&
          line.execution_status !== "skipped",
      ) ?? null,
    [visibleLines],
  );

  const firstBuyLine = useMemo(
    () =>
      visibleLines.find(
        (line) =>
          line.action_direction === "increase" &&
          line.execution_status !== "done" &&
          line.execution_status !== "skipped",
      ) ?? null,
    [visibleLines],
  );

  const openTradeModal = (kind: "sell" | "buy", line: RebalanceExecutionLine) => {
    setSelectedLine(line);
    setAmountMinor(Math.abs(line.remaining_delta_minor));
    setNoteText("");
    setError(null);
    setModal(kind);
  };

  if (detail.isLoading || !detail.data) {
    return <p className="text-slate-600">加载调仓执行工作区…</p>;
  }

  const { execution, events, stats } = detail.data;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">
            {plan.data?.name ?? "计划"} · 调仓执行
          </h1>
          <p className="mt-1 text-sm text-slate-600">
            {statusLabel(execution.status)} · {new Date(execution.created_at).toLocaleDateString("zh-CN")} 创建 ·
            已完成 {stats.done_line_count}/{stats.line_count} 个资产
            {stats.skipped_line_count ? ` · 跳过 ${stats.skipped_line_count} 个` : ""} · 现金池 {formatMoney(execution.cash_pool_minor)}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Link
            href={`/plans/${planId}/rebalance`}
            className="inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm"
          >
            返回持仓预览
          </Link>
          {!readonly && (
            <>
              <button
                type="button"
                className="inline-flex min-h-11 items-center rounded-md bg-emerald-700 px-4 text-sm text-white disabled:opacity-50"
                disabled={completeMut.isPending}
                data-testid="complete-execution"
                onClick={() => {
                  if (window.confirm("确认将执行结果写回正式持仓？此操作不可撤销。")) {
                    completeMut.mutate();
                  }
                }}
              >
                完成执行并写回持仓
              </button>
              <button
                type="button"
                className="inline-flex min-h-11 items-center rounded-md border border-red-300 px-4 text-sm text-red-700 disabled:opacity-50"
                disabled={cancelMut.isPending}
                data-testid="cancel-execution"
                onClick={() => {
                  if (window.confirm("确认放弃本次调仓执行？已登记的动作不会写回持仓。")) {
                    cancelMut.mutate();
                  }
                }}
              >
                放弃调仓
              </button>
            </>
          )}
        </div>
      </div>

      {error && (
        <p className="text-sm text-red-600" role="alert">
          {error}
        </p>
      )}

      <section className="grid gap-4 md:grid-cols-2">
        <div className="rounded-lg border border-slate-200 p-4">
          <h2 className="font-medium">调仓现金池</h2>
          <dl className="mt-3 space-y-2 text-sm">
            <div className="flex justify-between">
              <dt className="text-slate-600">当前余额</dt>
              <dd className="font-semibold" data-testid="cash-pool-balance">
                {formatMoney(execution.cash_pool_minor)}
              </dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-slate-600">已卖出累计</dt>
              <dd>{formatMoney(stats.sold_total_minor)}</dd>
            </div>
            <div className="flex justify-between">
              <dt className="text-slate-600">已买入累计</dt>
              <dd>{formatMoney(stats.bought_total_minor)}</dd>
            </div>
          </dl>
        </div>
        {!readonly && (
          <div className="rounded-lg border border-slate-200 p-4">
            <h2 className="font-medium">今日执行动作</h2>
            <div className="mt-3 flex flex-wrap gap-2">
              <button
                type="button"
                className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
                disabled={!firstSellLine}
                data-testid="quick-sell"
                onClick={() => firstSellLine && openTradeModal("sell", firstSellLine)}
              >
                登记卖出到现金池
              </button>
              <button
                type="button"
                className="rounded-md border border-slate-300 px-4 py-2 text-sm disabled:opacity-50"
                disabled={!firstBuyLine || execution.cash_pool_minor <= 0}
                data-testid="quick-buy"
                onClick={() => firstBuyLine && openTradeModal("buy", firstBuyLine)}
              >
                从现金池登记买入
              </button>
              <button
                type="button"
                className="rounded-md border border-slate-300 px-4 py-2 text-sm"
                onClick={() => {
                  setSelectedLine(null);
                  setNoteText("");
                  setError(null);
                  setModal("note");
                }}
              >
                记录备注
              </button>
            </div>
            <p className="mt-2 text-xs text-slate-500">
              快捷按钮针对首个待执行资产；也可在下方资产清单中针对单个资产登记卖出、买入或跳过。
            </p>
          </div>
        )}
      </section>

      <section className="rounded-lg border border-slate-200">
        <h2 className="border-b px-4 py-3 font-medium">资产执行清单</h2>
        <div className="overflow-x-auto">
          <table className="min-w-full text-sm">
            <thead className="bg-slate-50 text-left">
              <tr>
                <th className="px-3 py-2 font-medium">资产</th>
                <th className="px-3 py-2 font-medium">方向</th>
                <th className="px-3 py-2 font-medium text-right">应调金额</th>
                <th className="px-3 py-2 font-medium text-right">已执行</th>
                <th className="px-3 py-2 font-medium text-right">剩余待执行</th>
                <th className="px-3 py-2 font-medium">状态</th>
                {!readonly && <th className="px-3 py-2 font-medium">操作</th>}
              </tr>
            </thead>
            <tbody>
              {visibleLines.map((line) => (
                <tr key={line.id} className="border-t">
                  <td className="px-3 py-2">
                    <Link href={`/assets/${line.instrument_id}`} className="font-medium underline-offset-2 hover:underline">
                      {line.instrument_name ?? line.instrument_code}
                    </Link>
                  </td>
                  <td className="px-3 py-2">{rebalanceActionLabel(line.action_direction)}</td>
                  <td className="px-3 py-2 text-right">{formatSignedDelta(line.target_delta_minor)}</td>
                  <td className="px-3 py-2 text-right">{formatSignedDelta(line.executed_delta_minor)}</td>
                  <td className="px-3 py-2 text-right font-medium">
                    {formatSignedDelta(line.remaining_delta_minor)}
                  </td>
                  <td className="px-3 py-2">{lineStatusLabel(line.execution_status)}</td>
                  {!readonly && (
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap gap-x-3 gap-y-1">
                        {line.action_direction === "decrease" &&
                          line.execution_status !== "done" &&
                          line.execution_status !== "skipped" && (
                            <button
                              type="button"
                              className="text-sm font-medium underline"
                              onClick={() => openTradeModal("sell", line)}
                            >
                              卖出到现金池
                            </button>
                          )}
                        {line.action_direction === "increase" &&
                          line.execution_status !== "done" &&
                          line.execution_status !== "skipped" && (
                            <button
                              type="button"
                              className="text-sm font-medium underline"
                              onClick={() => openTradeModal("buy", line)}
                            >
                              从现金池买入
                            </button>
                          )}
                        {line.execution_status !== "done" &&
                          line.execution_status !== "skipped" && (
                            <button
                              type="button"
                              className="text-sm text-slate-600 underline disabled:opacity-50"
                              disabled={skipMut.isPending}
                              data-testid={`skip-line-${line.id}`}
                              onClick={() => {
                                const label = line.instrument_name ?? line.instrument_code;
                                if (window.confirm(`确认跳过 ${label}？剩余待执行金额将不再登记。`)) {
                                  skipMut.mutate(line.id);
                                }
                              }}
                            >
                              标记跳过
                            </button>
                          )}
                      </div>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="font-medium">执行时间线</h2>
        {events.length === 0 ? (
          <p className="mt-3 text-sm text-slate-600">尚无执行记录。</p>
        ) : (
          <ul className="mt-3 space-y-4">
            {[...events].reverse().map((event) => (
              <li key={event.id} className="border-l-2 border-slate-200 pl-4">
                <p className="text-xs text-slate-500">
                  {new Date(event.created_at).toLocaleString("zh-CN")}
                </p>
                <p className="text-sm">{parseEventSummary(event)}</p>
                {event.amount_minor > 0 && (
                  <p className="text-xs text-slate-600">
                    现金池：{formatMoney(event.cash_pool_after_minor - (event.event_type === "sell_to_cash" ? event.amount_minor : event.event_type === "buy_from_cash" ? -event.amount_minor : 0))} → {formatMoney(event.cash_pool_after_minor)}
                  </p>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <Dialog
        open={modal !== null}
        onClose={() => setModal(null)}
        title={
          modal === "sell"
            ? "登记卖出到现金池"
            : modal === "buy"
              ? "从现金池登记买入"
              : "记录备注"
        }
      >
        {(modal === "sell" || modal === "buy") && selectedLine && (
          <div className="space-y-4">
            <p className="text-sm text-slate-600">
              {selectedLine.instrument_name ?? selectedLine.instrument_code} · 剩余{" "}
              {formatMoney(Math.abs(selectedLine.remaining_delta_minor))}
            </p>
            <MoneyInput
              label="本次金额"
              valueMinor={amountMinor}
              onChange={setAmountMinor}
              plain
            />
            <label className="block text-sm">
              备注
              <textarea
                className="mt-1 w-full rounded-md border px-3 py-2"
                rows={2}
                value={noteText}
                onChange={(e) => setNoteText(e.target.value)}
              />
            </label>
            <div className="flex justify-end gap-2">
              <button type="button" className="rounded-md border px-4 py-2 text-sm" onClick={() => setModal(null)}>
                取消
              </button>
              <button
                type="button"
                className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
                disabled={tradeMut.isPending || amountMinor <= 0}
                onClick={() => tradeMut.mutate()}
              >
                确认
              </button>
            </div>
          </div>
        )}
        {modal === "note" && (
          <div className="space-y-4">
            <label className="block text-sm">
              备注内容
              <textarea
                className="mt-1 w-full rounded-md border px-3 py-2"
                rows={3}
                value={noteText}
                onChange={(e) => setNoteText(e.target.value)}
              />
            </label>
            <div className="flex justify-end gap-2">
              <button type="button" className="rounded-md border px-4 py-2 text-sm" onClick={() => setModal(null)}>
                取消
              </button>
              <button
                type="button"
                className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
                disabled={noteMut.isPending || !noteText.trim()}
                onClick={() => noteMut.mutate()}
              >
                保存
              </button>
            </div>
          </div>
        )}
      </Dialog>
    </div>
  );
}
