"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createRebalanceExecution,
  listRebalanceExecutions,
} from "@/lib/api/rebalance-executions";
import { formatMoney } from "@/lib/format";
import { ApiError } from "@/lib/api/client";

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

  if (list.isLoading) return <p className="text-slate-600">加载调仓执行列表…</p>;
  if (list.error) {
    return (
      <p className="text-red-600">
        加载失败：{list.error instanceof Error ? list.error.message : "未知错误"}
      </p>
    );
  }

  const rows = list.data ?? [];
  const active = rows.find((row) => row.status === "draft" || row.status === "in_progress");

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">调仓执行</h1>
          <p className="mt-1 text-sm text-slate-600">查看历史任务或继续未完成的调仓执行。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Link
            href={`/plans/${planId}/rebalance`}
            className="inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm"
          >
            返回持仓预览
          </Link>
          {!active && (
            <button
              type="button"
              className="inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
              disabled={create.isPending}
              data-testid="create-rebalance-execution"
              onClick={() => create.mutate()}
            >
              新建调仓执行
            </button>
          )}
        </div>
      </div>

      {create.error && (
        <p className="text-sm text-red-600" role="alert">
          {create.error instanceof ApiError ? create.error.message : "创建失败"}
        </p>
      )}

      {active && (
        <div className="rounded-lg border border-sky-200 bg-sky-50 px-4 py-3 text-sm text-sky-900">
          当前有进行中的调仓执行。
          <Link
            href={`/plans/${planId}/rebalance/executions/${active.id}`}
            className="ml-2 font-medium underline"
          >
            继续调仓执行
          </Link>
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border border-slate-200">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left">
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
                <td colSpan={7} className="px-3 py-8 text-center text-slate-500">
                  尚无调仓执行记录。
                </td>
              </tr>
            ) : (
              rows.map((row) => (
                <tr key={row.id} className="border-t">
                  <td className="px-3 py-2">{formatDate(row.created_at)}</td>
                  <td className="px-3 py-2">{statusLabel(row.status)}</td>
                  <td className="px-3 py-2 text-right">{row.line_count}</td>
                  <td className="px-3 py-2 text-right">{row.done_line_count}</td>
                  <td className="px-3 py-2 text-right">{formatMoney(row.cash_pool_minor)}</td>
                  <td className="px-3 py-2">
                    {formatDate(row.last_event_at || row.updated_at)}
                  </td>
                  <td className="px-3 py-2">
                    <Link
                      href={`/plans/${planId}/rebalance/executions/${row.id}`}
                      className="font-medium underline"
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
