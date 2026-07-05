"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { CurrentWeightCell, TargetWeightCell } from "@/components/plans/TargetWeightCell";
import { InlineTooltip } from "@/components/ui/InlineTooltip";
import type { RebalanceWorkspaceRow } from "@/lib/allocation-summary";
import { buildRebalanceWorkspaceRows } from "@/lib/allocation-summary";
import { getRebalance, getTargets } from "@/lib/api/holdings";
import {
  createRebalanceExecution,
  getActiveRebalanceExecution,
} from "@/lib/api/rebalance-executions";
import {
  createRebalanceDraft,
  getActiveRebalanceDraft,
} from "@/lib/api/rebalance-drafts";
import { assetClassLabel, formatMoney, regionLabel } from "@/lib/format";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { queryErrorMessage } from "@/lib/query-error";

function lineStatusHint(status: string, remainingMinor: number): string | null {
  switch (status) {
    case "partial":
      return `执行中 · 剩余 ${formatMoney(Math.abs(remainingMinor))}`;
    case "done":
      return "已完成";
    case "not_started":
      return remainingMinor !== 0 ? `剩余 ${formatMoney(Math.abs(remainingMinor))}` : null;
    default:
      return null;
  }
}

type PendingCreate = "draft" | "execution" | null;

export default function RebalancePage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const searchParams = useSearchParams();
  const assetRefreshed = searchParams.get("asset_refreshed") === "1";
  const executionCompleted = searchParams.get("execution_completed") === "1";
  const [pendingCreate, setPendingCreate] = useState<PendingCreate>(null);

  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });
  const rebalance = useQuery({
    queryKey: ["rebalance", planId],
    queryFn: () => getRebalance(planId, "full"),
  });
  const activeExecution = useQuery({
    queryKey: ["rebalance-execution-active", planId],
    queryFn: () => getActiveRebalanceExecution(planId),
  });
  const activeDraft = useQuery({
    queryKey: ["rebalance-draft-active", planId],
    queryFn: () => getActiveRebalanceDraft(planId),
  });

  const createExecution = useMutation({
    mutationFn: () => createRebalanceExecution(planId),
    onSuccess: (detail) => {
      setPendingCreate(null);
      void queryClient.invalidateQueries({ queryKey: ["rebalance-execution-active", planId] });
      router.push(`/plans/${planId}/rebalance/executions/${detail.execution.id}`);
    },
  });

  const createDraft = useMutation({
    mutationFn: () => createRebalanceDraft(planId),
    onSuccess: (detail) => {
      setPendingCreate(null);
      void queryClient.invalidateQueries({ queryKey: ["rebalance-draft-active", planId] });
      router.push(`/plans/${planId}/rebalance/plan/${detail.draft.id}`);
    },
  });

  const summary = rebalance.data?.summary;
  const hasEnabledHoldings = (summary?.holdings_total_minor ?? 0) > 0;
  const active = activeExecution.data;
  const executionInProgress = !!active?.execution;
  const draft = activeDraft.data;
  const draftInProgress = !!draft?.draft;

  const executionLineByAsset = useMemo(() => {
    const map = new Map<string, { status: string; remaining_delta_minor: number }>();
    for (const line of active?.lines ?? []) {
      map.set(line.asset_key, {
        status: line.execution_status,
        remaining_delta_minor: line.remaining_delta_minor,
      });
    }
    return map;
  }, [active?.lines]);

  const workspaceRows = useMemo(() => {
    if (!targets.data || !rebalance.data) return [];
    return buildRebalanceWorkspaceRows(targets.data, rebalance.data.lines);
  }, [targets.data, rebalance.data]);

  if (
    ((targets.isError || rebalance.isError) && (!targets.data || !rebalance.data)) ||
    (activeExecution.isError && active == null) ||
    (activeDraft.isError && draft == null)
  ) {
    return (
      <ErrorState
        message="无法加载调仓工作台。请确认后端服务可用后重试。"
        onRetry={() => {
          if (targets.isError) void targets.refetch();
          if (rebalance.isError) void rebalance.refetch();
          if (activeExecution.isError) void activeExecution.refetch();
          if (activeDraft.isError) void activeDraft.refetch();
        }}
        backHref={`/plans/${planId}/overview`}
        backLabel="返回总览"
        technicalDetail={queryErrorMessage(
          targets.error ?? rebalance.error ?? activeExecution.error ?? activeDraft.error,
        )}
      />
    );
  }

  if (
    targets.isLoading ||
    rebalance.isLoading ||
    activeExecution.isLoading ||
    activeDraft.isLoading ||
    !targets.data ||
    !rebalance.data
  ) {
    return <PageSkeleton label="加载调仓工作台…" />;
  }

  const dimensionLabel = (row: RebalanceWorkspaceRow) => {
    if (row.level === "asset_class") return assetClassLabel(row.asset_class);
    if (row.level === "region") return regionLabel(row.region ?? "");
    return row.label;
  };

  const dimensionClass = (row: RebalanceWorkspaceRow) => {
    if (row.level === "asset_class") return "font-medium text-ink";
    if (row.level === "region") return "pl-8 text-ink-muted";
    return "pl-14 text-ink";
  };

  const summaryAmountPlaceholder = (
    row: RebalanceWorkspaceRow,
    kind: "target" | "current",
  ) => {
    const amount =
      kind === "target" ? row.target_amount_minor : row.current_amount_minor;
    const label = kind === "target" ? "合计目标金额" : "合计当前金额";
    return (
      <InlineTooltip content={`${label}：${formatMoney(amount)}`}>
        <span className="text-ink-muted">—</span>
      </InlineTooltip>
    );
  };

  const gapAmountLabel = (row: RebalanceWorkspaceRow) =>
    row.gap_amount_minor >= 0
      ? `待投入 ${formatMoney(row.gap_amount_minor)}`
      : `待减配 ${formatMoney(Math.abs(row.gap_amount_minor))}`;

  const gapAmountCell = (row: RebalanceWorkspaceRow) => {
    if (row.gap_amount_minor === 0) {
      return <span className="text-ink-muted">—</span>;
    }

    const formatted = gapAmountLabel(row);
    const content = (
      <span
        className={`font-medium ${
          row.gap_amount_minor >= 0 ? "text-positive" : "text-danger"
        }`}
      >
        {formatted}
      </span>
    );

    if (row.level === "holding") return content;

    return (
      <InlineTooltip content={formatted}>
        <span className="text-ink-muted" aria-label={formatted}>
          —
        </span>
      </InlineTooltip>
    );
  };

  const executionHref = executionInProgress
    ? `/plans/${planId}/rebalance/executions/${active!.execution.id}`
    : `/plans/${planId}/rebalance/executions`;

  const holdingCardRow = (row: RebalanceWorkspaceRow) => {
    const execLine =
      row.level === "holding" && row.asset_key
        ? executionLineByAsset.get(row.asset_key)
        : undefined;
    const execHint = execLine
      ? lineStatusHint(execLine.status, execLine.remaining_delta_minor)
      : null;
    return (
      <div key={row.key} className="px-4 py-3">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            {row.asset_key ? (
              <Link
                href={`/assets/market/${encodeURIComponent(row.asset_key)}`}
                className="font-medium text-brand underline-offset-2 hover:underline"
              >
                {row.label}
              </Link>
            ) : (
              <span className="font-medium text-ink">{row.label}</span>
            )}
            {row.instrument_code && (
              <span className="block text-xs text-ink-muted">{row.instrument_code}</span>
            )}
            {execHint && (
              <span className="mt-1 block text-xs text-info" data-testid="execution-line-hint">
                {execHint}
              </span>
            )}
          </div>
          <div className="shrink-0 text-right text-sm">{gapAmountCell(row)}</div>
        </div>
        <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <dt className="text-ink-muted">目标金额</dt>
          <dd className="text-right text-ink">{formatMoney(row.target_amount_minor)}</dd>
          <dt className="text-ink-muted">当前金额</dt>
          <dd className="text-right text-ink">{formatMoney(row.current_amount_minor)}</dd>
        </dl>
      </div>
    );
  };

  // Mobile cards: one card per asset class; region rows become sub-headers and
  // holding rows render as compact key-value blocks (same data as the table).
  const mobileCards = () => {
    const cards: React.ReactNode[] = [];
    let currentCard: { header: RebalanceWorkspaceRow; children: React.ReactNode[] } | null = null;
    const flush = () => {
      if (!currentCard) return;
      const header = currentCard.header;
      cards.push(
        <article key={header.key} className="rounded-lg border border-line bg-surface">
          <div className="flex items-center justify-between gap-2 border-b border-line bg-surface-muted px-4 py-2.5">
            <span className="font-medium text-ink">{assetClassLabel(header.asset_class)}</span>
            <span className="text-xs text-ink-muted">
              {header.gap_amount_minor === 0 ? "无偏差" : gapAmountLabel(header)}
            </span>
          </div>
          <div className="divide-y divide-line">{currentCard.children}</div>
        </article>,
      );
      currentCard = null;
    };
    for (const row of workspaceRows) {
      if (row.level === "asset_class") {
        flush();
        currentCard = { header: row, children: [] };
        continue;
      }
      if (!currentCard) continue;
      if (row.level === "region") {
        currentCard.children.push(
          <p key={row.key} className="bg-surface-muted/60 px-4 py-1.5 text-xs text-ink-muted">
            {regionLabel(row.region ?? "")}
          </p>,
        );
        continue;
      }
      currentCard.children.push(holdingCardRow(row));
    }
    flush();
    return cards;
  };

  return (
    <div className="content-enter space-y-6">
      {assetRefreshed && (
        <Alert variant="success">持仓校正已提交，调仓工作台已更新。</Alert>
      )}
      {executionCompleted && (
        <Alert variant="success">调仓执行已完成，持仓已同步更新。</Alert>
      )}

      {!targets.data.weight_checks.passed && (
        <Alert variant="danger">
          {targets.data.weight_checks.checks
            .filter((check) => !check.passed)
            .map((check) => check.message)
            .join("；")}
          <Link
            href={`/plans/${planId}/settings?section=plan-targets`}
            className="ml-2 font-medium underline"
          >
            检查目标配置
          </Link>
        </Alert>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-ink">调仓工作台</h1>
          <p className="mt-1 text-sm text-ink-muted">
            对比当前持仓与目标结构；可在此更新真实持仓（持仓校正）、生成调仓计划或登记调仓执行。
          </p>
          {executionInProgress && (
            <p className="mt-2 text-sm text-warning" data-testid="execution-blocking-hint">
              当前有进行中的调仓执行。请先完成或放弃调仓，再进行持仓校正或创建调仓计划。
            </p>
          )}
          {executionInProgress && active && (
            <p className="mt-1 text-sm text-ink-muted">
              进行中 · 已完成 {active.stats.done_line_count}/{active.stats.line_count} 个资产
              {active.stats.skipped_line_count ? ` · 跳过 ${active.stats.skipped_line_count} 个` : ""} · 现金池{" "}
              {formatMoney(active.execution.cash_pool_minor)}
            </p>
          )}
          {!executionInProgress && draftInProgress && draft && (
            <p className="mt-2 text-sm text-info" data-testid="draft-in-progress-hint">
              有进行中的调仓计划（创建于{" "}
              {new Date(draft.draft.created_at).toLocaleDateString("zh-CN")}
              ），可继续编辑或在计划内放弃。
            </p>
          )}
        </div>
        {hasEnabledHoldings && (
          <div className="flex flex-wrap gap-2">
            {executionInProgress ? (
              <span
                className="inline-flex min-h-10 cursor-not-allowed items-center rounded-md border border-line bg-surface-muted px-4 text-sm font-medium text-ink-muted"
                data-testid="asset-refresh-primary-disabled"
                aria-disabled="true"
              >
                持仓校正
              </span>
            ) : (
              <Button
                href={`/plans/${planId}/asset-refresh`}
                variant="secondary"
                data-testid="asset-refresh-primary"
              >
                持仓校正
              </Button>
            )}
            {executionInProgress ? (
              <span
                className="inline-flex min-h-10 cursor-not-allowed items-center rounded-md border border-line bg-surface-muted px-4 text-sm font-medium text-ink-muted"
                data-testid="create-rebalance-plan-disabled"
                aria-disabled="true"
              >
                创建调仓计划
              </span>
            ) : draftInProgress && draft ? (
              <Button
                href={`/plans/${planId}/rebalance/plan/${draft.draft.id}`}
                data-testid="continue-rebalance-plan"
              >
                继续调仓计划
              </Button>
            ) : (
              <Button
                variant="secondary"
                data-testid="create-rebalance-plan"
                onClick={() => setPendingCreate("draft")}
              >
                创建调仓计划
              </Button>
            )}
            {executionInProgress ? (
              <Button
                href={executionHref}
                data-testid="continue-rebalance-execution"
              >
                继续调仓执行
              </Button>
            ) : (
              <Button
                variant={draftInProgress ? "secondary" : "primary"}
                data-testid="start-rebalance-execution"
                onClick={() => setPendingCreate("execution")}
              >
                调仓执行
              </Button>
            )}
          </div>
        )}
      </div>

      {!hasEnabledHoldings ? (
        <section className="rounded-lg border border-dashed border-line p-8 text-center">
          <h2 className="font-medium text-ink">尚未录入持仓</h2>
          <p className="mt-2 text-sm text-ink-muted">请先通过持仓校正录入当前真实持仓。</p>
          <Button
            href={`/plans/${planId}/asset-refresh`}
            className="mt-4"
            data-testid="asset-refresh-primary"
          >
            持仓校正
          </Button>
        </section>
      ) : (
        <section>
          <h2 className="flex items-center font-medium text-ink">
            结构偏差汇总
            <MetricHelp termKey="gap_color_semantics" />
          </h2>

          {/* Desktop table */}
          <div className="mt-3 hidden overflow-x-auto rounded-lg border border-line md:block">
            <table className="min-w-full text-sm">
              <thead className="bg-surface-muted text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-left font-medium">维度</th>
                  <th className="px-3 py-2 text-right font-medium">目标占比</th>
                  <th className="px-3 py-2 text-right font-medium">现状占比</th>
                  <th className="px-3 py-2 text-right font-medium">目标金额</th>
                  <th className="px-3 py-2 text-right font-medium">当前金额</th>
                  <th className="px-3 py-2 text-right font-medium">待投入 / 偏差</th>
                </tr>
              </thead>
              <tbody>
                {workspaceRows.map((row) => {
                  const execLine =
                    row.level === "holding" && row.asset_key
                      ? executionLineByAsset.get(row.asset_key)
                      : undefined;
                  const execHint = execLine
                    ? lineStatusHint(execLine.status, execLine.remaining_delta_minor)
                    : null;
                  return (
                    <tr
                      key={row.key}
                      className={`border-t border-line ${
                        row.level === "holding" ? "bg-surface hover:bg-surface-muted" : "bg-surface-muted/60"
                      }`}
                    >
                      <td className={`px-3 py-2 ${dimensionClass(row)}`}>
                        {row.level === "holding" && row.asset_key ? (
                          <Link
                            href={`/assets/market/${encodeURIComponent(row.asset_key)}`}
                            className="font-medium text-brand underline-offset-2 hover:underline"
                          >
                            {dimensionLabel(row)}
                          </Link>
                        ) : (
                          dimensionLabel(row)
                        )}
                        {row.level === "holding" && row.instrument_code && (
                          <span className="block text-xs font-normal text-ink-muted">
                            {row.instrument_code}
                          </span>
                        )}
                        {execHint && (
                          <span
                            className="mt-1 block text-xs text-info"
                            data-testid="execution-line-hint"
                          >
                            {execHint}
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-right">
                        <TargetWeightCell row={row} />
                      </td>
                      <td className="px-3 py-2 text-right">
                        <CurrentWeightCell row={row} />
                      </td>
                      <td className="px-3 py-2 text-right">
                        {row.level === "holding"
                          ? formatMoney(row.target_amount_minor)
                          : summaryAmountPlaceholder(row, "target")}
                      </td>
                      <td className="px-3 py-2 text-right">
                        {row.level === "holding"
                          ? formatMoney(row.current_amount_minor)
                          : summaryAmountPlaceholder(row, "current")}
                      </td>
                      <td className="px-3 py-2 text-right">{gapAmountCell(row)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Mobile cards */}
          <div className="mt-3 space-y-3 md:hidden" data-testid="rebalance-summary-cards">
            {mobileCards()}
          </div>
        </section>
      )}

      <ConfirmDialog
        open={pendingCreate === "draft"}
        title="创建调仓计划"
        description="将基于当前持仓与目标结构生成参考调仓方案（草稿）。草稿不会直接修改持仓，提交前可随时放弃。"
        confirmLabel="创建调仓计划"
        pending={createDraft.isPending}
        error={
          createDraft.error
            ? queryErrorMessage(createDraft.error, "创建调仓计划失败")
            : null
        }
        onConfirm={() => createDraft.mutate()}
        onClose={() => {
          setPendingCreate(null);
          createDraft.reset();
        }}
      />

      <ConfirmDialog
        open={pendingCreate === "execution"}
        title="创建调仓执行"
        description="将创建一笔调仓执行单，用于分多日登记真实的卖出与买入。执行进行中将暂时无法进行持仓校正，完成或放弃后恢复。"
        confirmLabel="创建调仓执行"
        pending={createExecution.isPending}
        error={
          createExecution.error
            ? queryErrorMessage(createExecution.error, "创建调仓执行失败")
            : null
        }
        onConfirm={() => createExecution.mutate()}
        onClose={() => {
          setPendingCreate(null);
          createExecution.reset();
        }}
      />
    </div>
  );
}
