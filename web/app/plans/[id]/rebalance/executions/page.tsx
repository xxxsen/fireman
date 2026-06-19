"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
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

  const list = useQuery({
    queryKey: ["rebalance-executions", planId],
    queryFn: () => listRebalanceExecutions(planId),
  });

  const create = useMutation({
    mutationFn: () => createRebalanceExecution(planId),
    onSuccess: (detail) => {
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
        backLabel="返回持仓预览"
        technicalDetail={queryErrorMessage(list.error)}
      />
    );
  }

  const rows = list.data ?? [];
  const active = rows.find((row) => row.status === "draft" || row.status === "in_progress");

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-ink">调仓执行</h1>
          <p className="mt-1 text-sm text-ink-muted">查看历史任务或继续未完成的调仓执行。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button href={`/plans/${planId}/rebalance`} variant="secondary">
            返回持仓预览
          </Button>
          {!active && (
            <Button
              disabled={create.isPending}
              data-testid="create-rebalance-execution"
              onClick={() => create.mutate()}
            >
              新建调仓执行
            </Button>
          )}
        </div>
      </div>

      {create.error && (
        <Alert variant="danger">{queryErrorMessage(create.error, "创建失败")}</Alert>
      )}

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

      <div className="overflow-x-auto rounded-lg border border-line">
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
    </div>
  );
}
