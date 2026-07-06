"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import { getCollection, listRuns } from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import {
  formatDateTimeFromMs,
  formatNullablePercent,
  formatPercent,
} from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { runStatusBadge } from "@/components/research/runStatus";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";
import type { ResearchRebalancePolicy } from "@/lib/api/research";

export default function ResearchRunsPage() {
  const id = useParams().id as string;

  const collectionQuery = useQuery({
    queryKey: ["research", "collection", id],
    queryFn: () => getCollection(id),
  });
  const runsQuery = useQuery({
    queryKey: ["research", "runs", id, "full"],
    queryFn: () => listRuns(id, 100),
  });

  if (runsQuery.isLoading || collectionQuery.isLoading) {
    return (
      <div className="content-enter">
        <LoadingState label="加载运行记录…" />
      </div>
    );
  }

  if (runsQuery.isError) {
    return (
      <div className="content-enter">
        <ErrorState
          message="加载运行记录失败。"
          onRetry={() => void runsQuery.refetch()}
          backHref={`/research/collections/${id}`}
          technicalDetail={queryErrorMessage(runsQuery.error)}
        />
      </div>
    );
  }

  const runs = runsQuery.data?.runs ?? [];
  const collectionName = collectionQuery.data?.name ?? "研究集合";

  return (
    <div className="content-enter">
      <PageHeader
        backHref={`/research/collections/${id}`}
        backLabel={collectionName}
        title="运行记录"
        description="每次回测生成不可变结果，与运行时的权重、参数和数据 source hash 绑定。"
      />

      {runs.length === 0 ? (
        <EmptyState
          title="还没有回测运行"
          description="回到集合页，数据就绪后点击「运行回测」。"
          action={{ label: "返回集合", href: `/research/collections/${id}` }}
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className="w-full min-w-[880px] text-sm" data-testid="runs-table">
            <thead>
              <tr className="border-b border-line text-left text-xs text-ink-muted">
                <th className="px-4 py-2.5 font-medium">区间</th>
                <th className="px-4 py-2.5 font-medium">状态</th>
                <th className="px-4 py-2.5 font-medium">再平衡</th>
                <th className="px-4 py-2.5 font-medium">CAGR</th>
                <th className="px-4 py-2.5 font-medium">最大回撤</th>
                <th className="px-4 py-2.5 font-medium">波动率</th>
                <th className="px-4 py-2.5 font-medium">Sharpe</th>
                <th className="px-4 py-2.5 font-medium">创建时间</th>
                <th className="px-4 py-2.5 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id} className="border-b border-line/60 last:border-0 hover:bg-surface-muted/40">
                  <td className="px-4 py-2.5 font-mono-numeric text-xs">
                    {run.window_start} ~ {run.window_end}
                  </td>
                  <td className="px-4 py-2.5">{runStatusBadge(run.status)}</td>
                  <td className="px-4 py-2.5 text-xs">
                    {REBALANCE_POLICY_LABELS[run.rebalance_policy as ResearchRebalancePolicy] ??
                      run.rebalance_policy}
                  </td>
                  <td className="px-4 py-2.5 font-mono-numeric text-xs">
                    {run.summary ? formatPercent(run.summary.cagr) : "—"}
                  </td>
                  <td className="px-4 py-2.5 font-mono-numeric text-xs text-danger">
                    {run.summary ? formatPercent(run.summary.max_drawdown) : "—"}
                  </td>
                  <td className="px-4 py-2.5 font-mono-numeric text-xs">
                    {run.summary ? formatNullablePercent(run.summary.annual_volatility) : "—"}
                  </td>
                  <td className="px-4 py-2.5 font-mono-numeric text-xs">
                    {run.summary?.sharpe != null ? run.summary.sharpe.toFixed(2) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-xs text-ink-muted">
                    {formatDateTimeFromMs(run.created_at)}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <Link
                      href={`/research/collections/${id}/runs/${run.id}`}
                      className="text-sm text-brand underline-offset-2 hover:underline"
                    >
                      查看
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
