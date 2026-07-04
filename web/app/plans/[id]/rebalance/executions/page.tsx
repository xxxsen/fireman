"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { PlanPageHeader } from "@/components/layout/PlanPageHeader";
import {
  createRebalanceExecution,
  listRebalanceExecutions,
} from "@/lib/api/rebalance-executions";
import { formatMoney } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";

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

function formatDate(ms: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleDateString("zh-CN");
}

export default function RebalanceExecutionsListPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [confirmOpen, setConfirmOpen] = useState(false);

  const list = useQuery({
    queryKey: ["rebalance-executions", planId],
    queryFn: () => listRebalanceExecutions(planId),
  });

  const create = useMutation({
    mutationFn: () => createRebalanceExecution(planId),
    onSuccess: (detail) => {
      setConfirmOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["rebalance-executions", planId] });
      void queryClient.invalidateQueries({ queryKey: ["rebalance-execution-active", planId] });
      router.push(`/plans/${planId}/rebalance/executions/${detail.execution.id}`);
    },
  });

  if (list.isLoading && !list.data) return <LoadingState label="加载调仓执行列表…" />;
  if (list.isError && !list.data) {
    return (
      <ErrorState
        message="无法加载调仓执行列表。请确认后端服务可用后重试。"
        onRetry={() => void list.refetch()}
        backHref={`/plans/${planId}/rebalance`}
        backLabel="返回调仓工作台"
        technicalDetail={queryErrorMessage(list.error)}
      />
    );
  }

  const rows = list.data ?? [];
  const active = rows.find((row) => row.status === "draft" || row.status === "in_progress");

  return (
    <div className="content-enter space-y-6">
      <PlanPageHeader
        title="调仓执行"
        description="查看历史任务或继续未完成的调仓执行。"
        actions={
          <>
            <Button href={`/plans/${planId}/rebalance`} variant="secondary">
              返回调仓工作台
            </Button>
            {!active && (
              <Button
                data-testid="create-rebalance-execution"
                onClick={() => setConfirmOpen(true)}
              >
                新建调仓执行
              </Button>
            )}
          </>
        }
      />

      {active && (
        <Alert variant="info">
          当前有进行中的调仓执行。
          <Link
            href={`/plans/${planId}/rebalance/executions/${active.id}`}
            className="ml-2 font-medium underline"
          >
            继续调仓执行
          </Link>
        </Alert>
      )}

      {/* Desktop table */}
      <div className="hidden overflow-x-auto rounded-lg border border-line md:block">
        <table className="min-w-full text-sm">
          <thead className="bg-surface-muted text-left text-ink-muted">
            <tr>
              <th className="px-3 py-2 font-medium">创建时间</th>
              <th className="px-3 py-2 font-medium">状态</th>
              <th className="px-3 py-2 font-medium text-right">目标资产数</th>
              <th className="px-3 py-2 font-medium text-right">已完成</th>
              <th className="px-3 py-2 font-medium text-right">现金池</th>
              <th className="px-3 py-2 font-medium">最后更新</th>
              <th className="px-3 py-2 font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-3 py-8 text-center text-ink-muted">
                  尚无调仓执行记录。
                </td>
              </tr>
            ) : (
              rows.map((row) => (
                <tr key={row.id} className="border-t border-line">
                  <td className="px-3 py-2 text-ink">{formatDate(row.created_at)}</td>
                  <td className="px-3 py-2 text-ink">{statusLabel(row.status)}</td>
                  <td className="px-3 py-2 text-right text-ink">{row.line_count}</td>
                  <td className="px-3 py-2 text-right text-ink">{row.done_line_count}</td>
                  <td className="px-3 py-2 text-right text-ink">{formatMoney(row.cash_pool_minor)}</td>
                  <td className="px-3 py-2 text-ink">
                    {formatDate(row.last_event_at || row.updated_at)}
                  </td>
                  <td className="px-3 py-2">
                    <Link
                      href={`/plans/${planId}/rebalance/executions/${row.id}`}
                      className="font-medium text-brand underline-offset-2 hover:underline"
                    >
                      {row.status === "draft" || row.status === "in_progress" ? "继续" : "查看详情"}
                    </Link>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Mobile cards */}
      <div className="space-y-3 md:hidden" data-testid="execution-list-cards">
        {rows.length === 0 ? (
          <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-ink-muted">
            尚无调仓执行记录。
          </p>
        ) : (
          rows.map((row) => (
            <article key={row.id} className="rounded-lg border border-line bg-surface p-4">
              <div className="flex items-center justify-between gap-2">
                <span className="font-medium text-ink">{formatDate(row.created_at)}</span>
                <span className="text-sm text-ink-muted">{statusLabel(row.status)}</span>
              </div>
              <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                <dt className="text-ink-muted">进度</dt>
                <dd className="text-right text-ink">
                  {row.done_line_count}/{row.line_count} 个资产
                </dd>
                <dt className="text-ink-muted">现金池</dt>
                <dd className="text-right text-ink">{formatMoney(row.cash_pool_minor)}</dd>
                <dt className="text-ink-muted">最后更新</dt>
                <dd className="text-right text-ink">
                  {formatDate(row.last_event_at || row.updated_at)}
                </dd>
              </dl>
              <div className="mt-3">
                <Link
                  href={`/plans/${planId}/rebalance/executions/${row.id}`}
                  className="text-sm font-medium text-brand underline-offset-2 hover:underline"
                >
                  {row.status === "draft" || row.status === "in_progress" ? "继续" : "查看详情"}
                </Link>
              </div>
            </article>
          ))
        )}
      </div>

      <ConfirmDialog
        open={confirmOpen}
        title="创建调仓执行"
        description="将创建一笔调仓执行单，用于分多日登记真实的卖出与买入。执行进行中将暂时无法进行持仓校正，完成或放弃后恢复。"
        confirmLabel="创建调仓执行"
        pending={create.isPending}
        error={create.error ? queryErrorMessage(create.error, "创建失败") : null}
        onConfirm={() => create.mutate()}
        onClose={() => {
          setConfirmOpen(false);
          create.reset();
        }}
      />
    </div>
  );
}
