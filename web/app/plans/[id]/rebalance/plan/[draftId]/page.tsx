"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { RebalanceFundPoolBar } from "@/components/plans/RebalanceFundPoolBar";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { Dialog } from "@/components/ui/Dialog";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { LoadingState } from "@/components/ui/LoadingState";
import { ErrorState } from "@/components/ui/ErrorState";
import { PlanPageHeader } from "@/components/layout/PlanPageHeader";
import { getHoldings } from "@/lib/api/holdings";
import {
  cancelRebalanceDraft,
  commitRebalanceDraft,
  getRebalanceDraft,
  patchRebalanceDraftLines,
  undoRebalanceDraft,
} from "@/lib/api/rebalance-drafts";
import { getPlan } from "@/lib/api/plans";
import {
  formatMoney,
  formatPercent,
  rebalanceActionLabel,
} from "@/lib/format";
import {
  applyRecommendedOneLine,
  buildReferencePackageItems,
  computeFundPool,
  countStagedChanges,
  findCashSweepHolding,
  formatPackageDeltaLabel,
  hasReferencePackage,
  isFundPoolBalanced,
  recommendedPlannedMinor,
} from "@/lib/rebalance-plan";
import { ApiError } from "@/lib/api/client";
import { queryErrorMessage } from "@/lib/query-error";
import type { RebalanceDraftEvent, RebalanceDraftLine } from "@/types/api";

function parseEventSummary(event: RebalanceDraftEvent): string {
  try {
    const payload = JSON.parse(event.payload_json) as { summary?: string };
    if (payload.summary) return payload.summary;
  } catch {
    /* ignore */
  }
  return event.event_type === "undo" ? "撤销上一步" : event.event_type;
}

function packageDeltaClass(deltaMinor: number): string {
  if (deltaMinor > 0) return "text-positive";
  if (deltaMinor < 0) return "text-danger";
  return "text-ink-muted";
}

export default function RebalancePlanPage() {
  const planId = useParams().id as string;
  const draftId = useParams().draftId as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [edits, setEdits] = useState<Record<string, number>>({});
  const [previewOpen, setPreviewOpen] = useState(false);
  const [recordSnapshot, setRecordSnapshot] = useState(false);
  const [sweepToCash, setSweepToCash] = useState(true);
  const [acceptScaleShrink, setAcceptScaleShrink] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [applyTarget, setApplyTarget] = useState<RebalanceDraftLine | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [overshootOpen, setOvershootOpen] = useState(false);

  const plan = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const holdings = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });
  const draft = useQuery({
    queryKey: ["rebalance-draft", planId, draftId],
    queryFn: () => getRebalanceDraft(planId, draftId),
  });

  const lines = useMemo(() => draft.data?.lines ?? [], [draft.data?.lines]);
  const events = draft.data?.events ?? [];
  const fundPool = useMemo(
    () =>
      computeFundPool(
        lines.map((line) => ({
          baseline_current_minor: line.baseline_current_minor,
          planned_current_minor: edits[line.id] ?? line.planned_current_minor,
        })),
      ),
    [lines, edits],
  );
  const stagedCount = countStagedChanges(
    lines.map((line) => ({
      baseline_current_minor: line.baseline_current_minor,
      planned_current_minor: edits[line.id] ?? line.planned_current_minor,
    })),
  );
  const packageItems = useMemo(
    () => buildReferencePackageItems(lines),
    [lines],
  );
  const cashHolding = useMemo(
    () => findCashSweepHolding(holdings.data?.holdings ?? []),
    [holdings.data?.holdings],
  );
  const needsSweepChoice = !isFundPoolBalanced(fundPool.netMinor) && fundPool.netMinor > 0;

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["rebalance-draft", planId, draftId] });
    void queryClient.invalidateQueries({ queryKey: ["rebalance-draft-active", planId] });
  };

  const stage = useMutation({
    mutationFn: async (lineIds: string[]) => {
      const payload = lineIds.map((lineId) => ({
        line_id: lineId,
        planned_current_minor: edits[lineId] ?? lines.find((l) => l.id === lineId)!.planned_current_minor,
      }));
      return patchRebalanceDraftLines(planId, draftId, { stage: true, lines: payload });
    },
    onSuccess: () => {
      setEdits({});
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "暂存失败"),
  });

  const applyRecommended = useMutation({
    mutationFn: async (lineId: string) => {
      const line = lines.find((l) => l.id === lineId);
      if (!line || line.recommended_package_delta_minor === 0) {
        throw new Error("本行无推荐变动");
      }
      return patchRebalanceDraftLines(planId, draftId, {
        stage: true,
        lines: [applyRecommendedOneLine(line)],
      });
    },
    onSuccess: (_data, lineId) => {
      const line = lines.find((l) => l.id === lineId);
      const label = line?.instrument_name ?? line?.instrument_code ?? "标的";
      setToast(`已应用 ${label} 的推荐金额`);
      setTimeout(() => setToast(null), 3000);
      setEdits({});
      invalidate();
    },
    onError: (err) => {
      setError(err instanceof ApiError ? err.message : "应用推荐金额失败");
    },
  });

  const undo = useMutation({
    mutationFn: () => undoRebalanceDraft(planId, draftId),
    onSuccess: () => {
      setEdits({});
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "撤销失败"),
  });

  const commit = useMutation({
    mutationFn: () => {
      if (!plan.data || !draft.data) throw new Error("数据尚未加载");
      const imbalanced = !isFundPoolBalanced(fundPool.netMinor);
      if (imbalanced && fundPool.netMinor > 0) {
        if (cashHolding && sweepToCash) {
          /* sweep on commit */
        } else if (!acceptScaleShrink) {
          throw new Error("请选择未分配资金处理方式");
        }
      }
      for (const line of lines) {
        const planned = edits[line.id] ?? line.planned_current_minor;
        if (planned < 0) throw new Error("计划金额不能为负");
      }
      return commitRebalanceDraft(planId, draftId, {
        config_version: plan.data.config_version,
        confirm_imbalanced: imbalanced,
        sweep_unallocated_to_cash: Boolean(cashHolding && sweepToCash && fundPool.netMinor > 0),
        accept_scale_shrink: acceptScaleShrink && fundPool.netMinor > 0,
        record_snapshot: recordSnapshot,
      });
    },
    onSuccess: () => {
      for (const key of ["holdings", "targets", "rebalance", "dashboard", "plan"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
      router.push(`/plans/${planId}/rebalance`);
    },
    onError: (err) => setError(queryErrorMessage(err, "提交失败")),
  });

  const cancel = useMutation({
    mutationFn: () => cancelRebalanceDraft(planId, draftId),
    onSuccess: () => router.push(`/plans/${planId}/rebalance`),
    onError: (err) => setError(queryErrorMessage(err, "放弃调仓计划失败")),
  });

  const submitCommit = () => {
    const imbalanced = !isFundPoolBalanced(fundPool.netMinor);
    if (imbalanced && fundPool.netMinor < 0) {
      setOvershootOpen(true);
      return;
    }
    commit.mutate();
  };

  const openPreview = () => {
    setSweepToCash(Boolean(cashHolding));
    setAcceptScaleShrink(false);
    setPreviewOpen(true);
  };

  if (
    (plan.isError || draft.isError || holdings.isError) &&
    (!plan.data || !draft.data || !holdings.data)
  ) {
    return (
      <ErrorState
        message="无法加载调仓计划。请确认后端服务可用后重试。"
        onRetry={() => {
          if (plan.isError) void plan.refetch();
          if (draft.isError) void draft.refetch();
          if (holdings.isError) void holdings.refetch();
        }}
        backHref={`/plans/${planId}/rebalance`}
        backLabel="返回调仓工作台"
        technicalDetail={queryErrorMessage(plan.error ?? draft.error ?? holdings.error)}
      />
    );
  }

  if (
    plan.isLoading ||
    draft.isLoading ||
    holdings.isLoading ||
    !plan.data ||
    !draft.data ||
    !holdings.data
  ) {
    return <LoadingState label="加载调仓计划…" />;
  }

  if (draft.data.draft.status !== "draft") {
    return (
      <div className="space-y-4">
        <p className="text-ink-muted">
          此调仓计划已{draft.data.draft.status === "committed" ? "提交" : "放弃"}。
        </p>
        <Link
          href={`/plans/${planId}/rebalance`}
          className="text-brand underline-offset-2 hover:underline"
        >
          返回调仓工作台
        </Link>
      </div>
    );
  }

  const actionableLines = lines.filter(
    (line) => line.frozen_action === "increase" || line.frozen_action === "decrease",
  );
  const planTarget = plan.data;

  return (
    <div className="content-enter space-y-6">
      <PlanPageHeader
        title="调仓计划"
        description={
          <>
            状态：进行中 · 基准持仓合计{" "}
            {formatMoney(draft.data.draft.baseline_holdings_total_minor, planTarget.base_currency)}
            · {actionableLines.length} 个标的待调整
            <MetricHelp termKey="rebalance_plan_draft" />
          </>
        }
        actions={
          <Button variant="danger" onClick={() => setCancelOpen(true)}>
            放弃计划
          </Button>
        }
      />

      {error && <Alert variant="danger">{error}</Alert>}
      {toast && <Alert variant="success">{toast}</Alert>}

      {hasReferencePackage(lines) && (
        <section className="rounded-lg border border-line bg-surface-muted px-4 py-3 text-sm">
          <p className="font-medium text-ink">
            参考调仓方案（结构对齐，含未达阈值的微调）
            <MetricHelp termKey="rebalance_reference_package" />
          </p>
          <p className="mt-1 text-ink">{packageItems.join("   ")}</p>
          <p className="mt-2 text-xs text-ink-muted">
            行内「不动」表示未超调仓阈值；方案为完整对齐参考，请逐行应用或手工调整。
          </p>
        </section>
      )}

      <RebalanceFundPoolBar
        releasedMinor={fundPool.releasedMinor}
        usedMinor={fundPool.usedMinor}
        netMinor={fundPool.netMinor}
        currency={planTarget.base_currency}
      />

      {/* Desktop table */}
      <section
        className="hidden overflow-x-auto rounded-lg border border-line md:block"
        data-testid="rebalance-line-table"
      >
        <table className="min-w-full text-sm">
          <thead className="bg-surface-muted text-left text-ink-muted">
            <tr>
              <th className="px-3 py-2 font-medium">标的</th>
              <th className="px-3 py-2 text-right font-medium">基准当前</th>
              <th className="px-3 py-2 text-right font-medium">
                冻结结构目标
                <MetricHelp termKey="frozen_structural_gap" />
              </th>
              <th className="px-3 py-2 text-right font-medium">冻结结构还差</th>
              <th className="px-3 py-2 font-medium">参考建议</th>
              <th className="px-3 py-2 text-right font-medium">
                方案变动
                <MetricHelp termKey="rebalance_reference_package" />
              </th>
              <th className="px-3 py-2 text-right font-medium">计划当前金额</th>
              <th className="px-3 py-2 text-right font-medium">相对基准变动</th>
              <th className="px-3 py-2 font-medium">状态</th>
              <th className="px-3 py-2 font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((line) => {
              const planned = edits[line.id] ?? line.planned_current_minor;
              const delta = planned - line.baseline_current_minor;
              const status =
                planned === line.baseline_current_minor
                  ? "未改"
                  : line.last_saved_at && planned === line.planned_current_minor
                    ? "已暂存"
                    : "编辑中";
              const hasPackageDelta = line.recommended_package_delta_minor !== 0;
              return (
                <tr key={line.id} className="border-t border-line">
                  <td className="px-3 py-2">
                    <span className="font-medium text-ink">
                      {line.instrument_name ?? line.instrument_code}
                    </span>
                    <span className="block text-xs text-ink-muted">{line.instrument_code}</span>
                  </td>
                  <td className="px-3 py-2 text-right text-ink">
                    {formatMoney(line.baseline_current_minor, planTarget.base_currency)}
                  </td>
                  <td className="px-3 py-2 text-right text-ink">
                    {formatMoney(line.frozen_target_minor, planTarget.base_currency)}
                  </td>
                  <td className="px-3 py-2 text-right text-ink">
                    {formatMoney(line.frozen_gap_minor, planTarget.base_currency)}
                    <span className="block text-xs text-ink-muted">
                      {formatPercent(line.frozen_gap_weight)}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-ink">
                    {rebalanceActionLabel(line.frozen_action)}
                    {line.frozen_suggested_trade_minor !== 0 && (
                      <span className="block text-xs text-ink-muted">
                        {formatMoney(Math.abs(line.frozen_suggested_trade_minor), planTarget.base_currency)}
                      </span>
                    )}
                  </td>
                  <td className={`px-3 py-2 text-right font-medium ${packageDeltaClass(line.recommended_package_delta_minor)}`}>
                    {formatPackageDeltaLabel(line.recommended_package_delta_minor)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <MoneyInput
                      valueMinor={planned}
                      onChange={(value) =>
                        setEdits((prev) => ({ ...prev, [line.id]: value }))
                      }
                    />
                  </td>
                  <td className={`px-3 py-2 text-right font-medium ${delta >= 0 ? "text-positive" : "text-danger"}`}>
                    {delta >= 0 ? "+" : ""}
                    {formatMoney(delta, planTarget.base_currency)}
                  </td>
                  <td className="px-3 py-2 text-xs text-ink-muted">{status}</td>
                  <td className="px-3 py-2">
                    {hasPackageDelta ? (
                      <span className="inline-flex items-center gap-1">
                        <Button
                          variant="ghost"
                          className="px-2 py-1 text-xs"
                          disabled={applyRecommended.isPending || Object.keys(edits).length > 0}
                          title={Object.keys(edits).length > 0 ? "请先暂存或放弃未保存编辑" : undefined}
                          onClick={() => setApplyTarget(line)}
                        >
                          应用推荐金额
                        </Button>
                        <MetricHelp termKey="apply_recommended_one_line" />
                      </span>
                    ) : (
                      <span className="text-xs text-ink-muted">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </section>

      {/* Mobile cards */}
      <section className="space-y-3 md:hidden" data-testid="rebalance-line-cards">
        {lines.map((line) => {
          const planned = edits[line.id] ?? line.planned_current_minor;
          const delta = planned - line.baseline_current_minor;
          const status =
            planned === line.baseline_current_minor
              ? "未改"
              : line.last_saved_at && planned === line.planned_current_minor
                ? "已暂存"
                : "编辑中";
          const hasPackageDelta = line.recommended_package_delta_minor !== 0;
          return (
            <article key={line.id} className="rounded-lg border border-line bg-surface p-4 text-sm">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="font-medium text-ink">{line.instrument_name ?? line.instrument_code}</p>
                  <p className="text-xs text-ink-muted">{line.instrument_code}</p>
                </div>
                <span className="shrink-0 text-xs text-ink-muted">{status}</span>
              </div>
              <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                <dt className="text-ink-muted">基准当前</dt>
                <dd className="text-right text-ink">
                  {formatMoney(line.baseline_current_minor, planTarget.base_currency)}
                </dd>
                <dt className="text-ink-muted">冻结结构还差</dt>
                <dd className="text-right text-ink">
                  {formatMoney(line.frozen_gap_minor, planTarget.base_currency)}
                </dd>
                <dt className="text-ink-muted">参考建议</dt>
                <dd className="text-right text-ink">{rebalanceActionLabel(line.frozen_action)}</dd>
                <dt className="text-ink-muted">方案变动</dt>
                <dd className={`text-right font-medium ${packageDeltaClass(line.recommended_package_delta_minor)}`}>
                  {formatPackageDeltaLabel(line.recommended_package_delta_minor)}
                </dd>
              </dl>
              <div className="mt-3">
                <label className="block text-xs text-ink-muted">计划当前金额</label>
                <div className="mt-1">
                  <MoneyInput
                    valueMinor={planned}
                    onChange={(value) => setEdits((prev) => ({ ...prev, [line.id]: value }))}
                  />
                </div>
                <p className={`mt-1 text-xs font-medium ${delta >= 0 ? "text-positive" : "text-danger"}`}>
                  相对基准变动 {delta >= 0 ? "+" : ""}
                  {formatMoney(delta, planTarget.base_currency)}
                </p>
              </div>
              {hasPackageDelta && (
                <div className="mt-3 flex items-center gap-1">
                  <Button
                    variant="ghost"
                    className="px-2 py-1 text-xs"
                    disabled={applyRecommended.isPending || Object.keys(edits).length > 0}
                    title={Object.keys(edits).length > 0 ? "请先暂存或放弃未保存编辑" : undefined}
                    onClick={() => setApplyTarget(line)}
                  >
                    应用推荐金额
                  </Button>
                  <MetricHelp termKey="apply_recommended_one_line" />
                </div>
              )}
            </article>
          );
        })}
      </section>

      <section className="rounded-lg border border-line bg-surface p-4">
        <h2 className="font-medium text-ink">变更时间线</h2>
        {events.length === 0 ? (
          <p className="mt-2 text-sm text-ink-muted">暂无暂存记录。</p>
        ) : (
          <ol className="mt-2 space-y-2 text-sm text-ink">
            {events
              .filter((e) => e.event_type === "stage" || e.event_type === "undo")
              .map((event) => (
                <li key={event.id} className="flex justify-between gap-2">
                  <span>
                    {event.event_type === "undo" ? "↩ " : "• "}
                    {parseEventSummary(event)}
                  </span>
                </li>
              ))}
          </ol>
        )}
        <Button
          variant="ghost"
          className="mt-3 px-2 py-1"
          disabled={undo.isPending || !events.some((e) => e.event_type === "stage")}
          onClick={() => undo.mutate()}
        >
          撤销上一步
        </Button>
      </section>

      <div
        className="sticky bottom-0 z-10 border-t border-line bg-surface/95 px-4 py-3 backdrop-blur"
        style={{ paddingBottom: "calc(0.75rem + var(--safe-area-bottom))" }}
      >
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="secondary"
            disabled={stage.isPending || Object.keys(edits).length === 0}
            onClick={() => stage.mutate(Object.keys(edits))}
          >
            暂存本步变更
          </Button>
          <Button variant="secondary" onClick={openPreview}>
            预览最终持仓
          </Button>
          <Button
            disabled={commit.isPending || stagedCount === 0}
            onClick={() => {
              if (Object.keys(edits).length > 0) {
                setError("请先暂存未保存的编辑");
                return;
              }
              openPreview();
            }}
          >
            完成并更新持仓
          </Button>
          {stagedCount > 0 && (
            <span className="text-xs text-ink-muted">{stagedCount} 项待提交变更</span>
          )}
        </div>
      </div>

      <Dialog
        open={previewOpen}
        onClose={() => setPreviewOpen(false)}
        title="预览最终持仓"
        footer={
          <div className="flex flex-wrap justify-end gap-3">
            <Button variant="secondary" onClick={() => setPreviewOpen(false)}>
              返回分配
            </Button>
            <Button
              pending={commit.isPending}
              disabled={
                (needsSweepChoice && !cashHolding && !acceptScaleShrink) ||
                (needsSweepChoice && Boolean(cashHolding) && !sweepToCash && !acceptScaleShrink)
              }
              onClick={submitCommit}
            >
              确认提交
            </Button>
          </div>
        }
      >
        <ul className="divide-y divide-line text-sm">
          {lines.map((line) => {
            const planned = edits[line.id] ?? line.planned_current_minor;
            const delta = planned - line.baseline_current_minor;
            if (delta === 0) return null;
            return (
              <li key={line.id} className="flex justify-between py-2 text-ink">
                <span>{line.instrument_name ?? line.instrument_code}</span>
                <span>
                  {formatMoney(line.baseline_current_minor)} → {formatMoney(planned)}
                </span>
              </li>
            );
          })}
          {cashHolding && sweepToCash && !acceptScaleShrink && fundPool.netMinor > 0 && (
            <li key="cash-sweep" className="flex justify-between py-2 text-ink">
              <span>{cashHolding.instrument_name ?? "现金"}</span>
              <span>
                {formatMoney(cashHolding.current_amount_minor)} →{" "}
                {formatMoney(cashHolding.current_amount_minor + fundPool.netMinor)}
              </span>
            </li>
          )}
        </ul>

        {needsSweepChoice && (
          <div className="mt-4 space-y-3 rounded-md border border-info/25 bg-info/5 p-3 text-sm text-ink">
            <p>
              尚有 {formatMoney(fundPool.netMinor, planTarget.base_currency)} 未在标的间分配。
            </p>
            {cashHolding ? (
              <label className="flex items-start gap-2">
                <input
                  type="radio"
                  name="sweep_choice"
                  checked={sweepToCash && !acceptScaleShrink}
                  onChange={() => {
                    setSweepToCash(true);
                    setAcceptScaleShrink(false);
                  }}
                />
                <span>
                  未分配资金将计入「{cashHolding.instrument_name ?? "现金"}」持仓（
                  {formatMoney(cashHolding.current_amount_minor)} →{" "}
                  {formatMoney(cashHolding.current_amount_minor + fundPool.netMinor)}）
                  <MetricHelp termKey="unallocated_sweep_to_cash" />
                </span>
              </label>
            ) : (
              <p className="text-warning">
                计划中尚无现金持仓。请先到{" "}
                <Link href={`/plans/${planId}/rebalance`} className="underline">
                  调仓工作台
                </Link>{" "}
                通过持仓校正添加 CNY 现金，或选择接受组合规模下降。
              </p>
            )}
            <label className="flex items-start gap-2">
              <input
                type="radio"
                name="sweep_choice"
                checked={acceptScaleShrink}
                onChange={() => {
                  setSweepToCash(false);
                  setAcceptScaleShrink(true);
                }}
              />
              <span>
                接受组合规模下降（不增加现金，总市值减少{" "}
                {formatMoney(fundPool.netMinor, planTarget.base_currency)}）
              </span>
            </label>
          </div>
        )}

        <label className="mt-4 flex items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={recordSnapshot}
            onChange={(e) => setRecordSnapshot(e.target.checked)}
          />
          记录调仓后快照
        </label>
      </Dialog>

      <ConfirmDialog
        open={applyTarget !== null}
        title="应用推荐金额"
        description={
          applyTarget
            ? `将「${applyTarget.instrument_name ?? applyTarget.instrument_code}」计划金额 ${formatMoney(applyTarget.planned_current_minor)} → ${formatMoney(recommendedPlannedMinor(applyTarget.baseline_current_minor, applyTarget.recommended_package_delta_minor))}（推荐 ${formatPackageDeltaLabel(applyTarget.recommended_package_delta_minor)}）？`
            : undefined
        }
        confirmLabel="应用推荐金额"
        pending={applyRecommended.isPending}
        onConfirm={() => {
          if (applyTarget) applyRecommended.mutate(applyTarget.id);
          setApplyTarget(null);
        }}
        onClose={() => setApplyTarget(null)}
      />

      <ConfirmDialog
        open={overshootOpen}
        title="增配超出减配释放"
        description="本次增配金额超过减配释放的资金，提交后将更新持仓并可能动用额外资金。仍要提交吗？"
        confirmLabel="仍要提交"
        onConfirm={() => {
          setOvershootOpen(false);
          commit.mutate();
        }}
        onClose={() => setOvershootOpen(false)}
      />

      <ConfirmDialog
        open={cancelOpen}
        title="放弃调仓计划"
        description="确定放弃此调仓计划？正式持仓不会变更，已暂存的编辑将一并丢弃。"
        confirmLabel="放弃计划"
        variant="danger"
        pending={cancel.isPending}
        onConfirm={() => cancel.mutate()}
        onClose={() => setCancelOpen(false)}
      />
    </div>
  );
}
