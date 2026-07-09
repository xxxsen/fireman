"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import {
  createCollection,
  deleteCollection,
  listCollections,
  listRecentRuns,
  updateCollection,
  type ResearchCollectionListItem,
  type ResearchRunView,
} from "@/lib/api/research";
import { parseCollectionJSON } from "@/lib/research/collection-json";
import { queryErrorMessage } from "@/lib/query-error";
import { formatDateTimeFromMs, formatNullablePercent, formatPercent } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { CopyFromPlanDialog } from "@/components/research/CopyFromPlanDialog";
import { runStatusBadge } from "@/components/research/runStatus";

function weightBadge(item: ResearchCollectionListItem) {
  if (item.enabled_assets === 0) {
    return <Badge variant="neutral">无启用资产</Badge>;
  }
  if (item.weight_valid) {
    return <Badge variant="positive">权重 100%</Badge>;
  }
  return <Badge variant="warning">权重 {formatPercent(item.weight_sum)}</Badge>;
}

function latestRunCell(item: ResearchCollectionListItem) {
  const run = item.latest_run;
  if (!run) return <span className="text-xs text-ink-muted">尚未回测</span>;
  const summary = item.latest_run_summary;
  if (run.status === "succeeded" && summary) {
    return (
      <div className="space-y-0.5 text-xs">
        <div>
          CAGR <span className="font-medium text-ink">{formatPercent(summary.cagr)}</span>
          <span className="mx-1 text-ink-muted">·</span>
          回撤 <span className="font-medium text-ink">{formatPercent(summary.max_drawdown)}</span>
          <span className="mx-1 text-ink-muted">·</span>
          波动 <span className="font-medium text-ink">{formatNullablePercent(summary.annual_volatility)}</span>
        </div>
        <div className="text-ink-muted">{formatDateTimeFromMs(run.created_at)}</div>
      </div>
    );
  }
  return (
    <div className="space-y-0.5 text-xs">
      {runStatusBadge(run.status)}
      <div className="text-ink-muted">{formatDateTimeFromMs(run.created_at)}</div>
    </div>
  );
}

function runDetailHref(run: ResearchRunView): string {
  return `/research/collections/${run.collection_id}/runs/${run.id}`;
}

function RecentRunsAside({
  runs,
  syncingCollections,
  attentionCollections,
}: {
  runs: ResearchRunView[];
  syncingCollections: ResearchCollectionListItem[];
  attentionCollections: ResearchCollectionListItem[];
}) {
  return (
    <aside className="space-y-6 lg:w-80 lg:shrink-0" data-testid="research-aside">
      <section data-testid="recent-runs">
        <h2 className="mb-2 text-sm font-semibold text-ink">最近运行</h2>
        {runs.length === 0 ? (
          <p className="rounded-md border border-dashed border-line px-3 py-4 text-xs text-ink-muted">
            暂无回测运行记录。
          </p>
        ) : (
          <ul className="space-y-2">
            {runs.map((run) => (
              <li key={run.id} className="rounded-md border border-line bg-surface px-3 py-2">
                <div className="flex items-center justify-between gap-2">
                  <Link
                    href={runDetailHref(run)}
                    className="truncate text-sm font-medium text-brand underline-offset-2 hover:underline"
                  >
                    {run.window_start} ~ {run.window_end}
                  </Link>
                  {runStatusBadge(run.status)}
                </div>
                <div className="mt-1 flex items-center justify-between text-xs text-ink-muted">
                  <span>
                    {run.status === "succeeded" && run.summary
                      ? `CAGR ${formatPercent(run.summary.cagr)} · 回撤 ${formatPercent(run.summary.max_drawdown)}`
                      : run.rebalance_policy}
                  </span>
                  <span>{formatDateTimeFromMs(run.created_at)}</span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      {syncingCollections.length > 0 && (
        <section data-testid="syncing-collections">
          <h2 className="mb-2 text-sm font-semibold text-ink">回测计算中</h2>
          <ul className="space-y-1.5">
            {syncingCollections.map((item) => (
              <li key={item.id} className="flex items-center justify-between gap-2 text-sm">
                <Link
                  href={`/research/collections/${item.id}`}
                  className="truncate text-brand underline-offset-2 hover:underline"
                >
                  {item.name}
                </Link>
                {runStatusBadge(item.latest_run?.status ?? "running")}
              </li>
            ))}
          </ul>
        </section>
      )}

      {attentionCollections.length > 0 && (
        <section data-testid="attention-collections">
          <h2 className="mb-2 text-sm font-semibold text-ink">需要处理</h2>
          <ul className="space-y-1.5">
            {attentionCollections.map((item) => (
              <li key={item.id} className="flex items-center justify-between gap-2 text-sm">
                <Link
                  href={`/research/collections/${item.id}`}
                  className="truncate text-brand underline-offset-2 hover:underline"
                >
                  {item.name}
                </Link>
                {item.latest_run?.status === "failed" ? (
                  <Badge variant="danger">回测失败</Badge>
                ) : (
                  <Badge variant="warning">权重未配平</Badge>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
    </aside>
  );
}

export default function ResearchHomePage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [copyDialogOpen, setCopyDialogOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<ResearchCollectionListItem | null>(null);
  const [pendingHardDelete, setPendingHardDelete] =
    useState<ResearchCollectionListItem | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [importError, setImportError] = useState<string | null>(null);
  const importInputRef = useRef<HTMLInputElement>(null);

  const collectionsQuery = useQuery({
    queryKey: ["research", "collections"],
    queryFn: () => listCollections(),
  });
  const recentRunsQuery = useQuery({
    queryKey: ["research", "recent-runs"],
    queryFn: () => listRecentRuns(8),
  });

  const invalidateCollections = () =>
    queryClient.invalidateQueries({ queryKey: ["research", "collections"] });

  const archiveMutation = useMutation({
    mutationFn: (id: string) => deleteCollection(id, false),
    onSuccess: () => void invalidateCollections(),
  });

  const restoreMutation = useMutation({
    mutationFn: (id: string) => updateCollection(id, { status: "active" }),
    onSuccess: () => void invalidateCollections(),
  });

  const hardDeleteMutation = useMutation({
    mutationFn: (id: string) => deleteCollection(id, true),
    onSuccess: () => void invalidateCollections(),
  });

  const cloneMutation = useMutation({
    mutationFn: (item: ResearchCollectionListItem) =>
      createCollection({ name: `${item.name} 副本`, from_collection_id: item.id }),
    onSuccess: (detail) => {
      void invalidateCollections();
      router.push(`/research/collections/${detail.id}`);
    },
  });

  const importMutation = useMutation({
    mutationFn: async (file: File) => {
      const input = parseCollectionJSON(await file.text());
      return createCollection(input);
    },
    onSuccess: (detail) => {
      setImportError(null);
      void invalidateCollections();
      router.push(`/research/collections/${detail.id}`);
    },
    onError: (err) => setImportError(queryErrorMessage(err)),
  });

  const header = (
    <PageHeader
      title="组合研究"
      description="构建研究集合、运行历史回测，验证组合表现后再落地到 FIRE 计划。"
      primaryAction={{ label: "新建集合", href: "/research/collections/new" }}
      secondaryActions={
        <>
          <Button
            variant="secondary"
            onClick={() => setCopyDialogOpen(true)}
            data-testid="copy-from-plan-entry"
          >
            从计划复制
          </Button>
          <Button
            variant="secondary"
            pending={importMutation.isPending}
            onClick={() => importInputRef.current?.click()}
            data-testid="import-json-entry"
          >
            导入 JSON
          </Button>
          <input
            ref={importInputRef}
            type="file"
            accept="application/json,.json"
            className="hidden"
            data-testid="import-json-input"
            onChange={(e) => {
              const file = e.target.files?.[0];
              e.target.value = "";
              if (file) importMutation.mutate(file);
            }}
          />
        </>
      }
    />
  );

  if (collectionsQuery.isLoading) {
    return (
      <div className="content-enter">
        {header}
        <LoadingState label="加载集合列表…" />
      </div>
    );
  }

  if (collectionsQuery.isError) {
    return (
      <div className="content-enter">
        {header}
        <ErrorState
          message="加载研究集合失败。"
          onRetry={() => void collectionsQuery.refetch()}
          technicalDetail={queryErrorMessage(collectionsQuery.error)}
        />
      </div>
    );
  }

  const allCollections = collectionsQuery.data?.collections ?? [];
  const collections = allCollections.filter((c) => c.status === "active");
  const archived = allCollections.filter((c) => c.status === "archived");
  const runs = recentRunsQuery.data?.runs ?? [];
  const syncingCollections = collections.filter(
    (c) => c.latest_run && (c.latest_run.status === "queued" || c.latest_run.status === "running"),
  );
  const attentionCollections = collections.filter(
    (c) =>
      c.latest_run?.status === "failed" || (c.enabled_assets > 0 && !c.weight_valid),
  );

  return (
    <div className="content-enter">
      {header}

      <div className="flex flex-col gap-8 lg:flex-row">
        <div className="min-w-0 flex-1">
          {collections.length === 0 ? (
            <EmptyState
              title="还没有研究集合"
              description="新建研究集合，或从现有 FIRE 计划复制持仓开始研究。"
              action={{ label: "新建集合", href: "/research/collections/new" }}
            />
          ) : (
            <div
              className="overflow-x-auto rounded-lg border border-line bg-surface"
              data-testid="collection-table"
            >
              <table className="w-full min-w-[720px] text-sm">
                <thead>
                  <tr className="border-b border-line text-left text-xs text-ink-muted">
                    <th className="px-4 py-2.5 font-medium">名称</th>
                    <th className="px-4 py-2.5 font-medium">资产数</th>
                    <th className="px-4 py-2.5 font-medium">权重状态</th>
                    <th className="px-4 py-2.5 font-medium">最近回测</th>
                    <th className="px-4 py-2.5 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {collections.map((item) => (
                    <tr
                      key={item.id}
                      className="border-b border-line/60 last:border-0 hover:bg-surface-muted/50"
                    >
                      <td className="px-4 py-3">
                        <Link
                          href={`/research/collections/${item.id}`}
                          className="font-medium text-ink underline-offset-2 hover:text-brand hover:underline"
                        >
                          {item.name}
                        </Link>
                        {item.description && (
                          <p className="mt-0.5 max-w-xs truncate text-xs text-ink-muted">
                            {item.description}
                          </p>
                        )}
                      </td>
                      <td className="px-4 py-3">
                        {item.enabled_assets}
                        <span className="text-xs text-ink-muted"> / {item.total_assets}</span>
                      </td>
                      <td className="px-4 py-3">{weightBadge(item)}</td>
                      <td className="px-4 py-3">{latestRunCell(item)}</td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" href={`/research/collections/${item.id}`}>
                            打开
                          </Button>
                          <Button
                            variant="ghost"
                            onClick={() => cloneMutation.mutate(item)}
                            disabled={cloneMutation.isPending}
                            data-testid={`clone-${item.id}`}
                          >
                            复制
                          </Button>
                          <Button
                            variant="ghost"
                            onClick={() => setPendingDelete(item)}
                            data-testid={`archive-${item.id}`}
                          >
                            归档
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {cloneMutation.isError && (
            <p className="mt-2 text-sm text-danger" role="alert">
              复制集合失败：{queryErrorMessage(cloneMutation.error)}
            </p>
          )}
          {importError && (
            <p className="mt-2 text-sm text-danger" role="alert">
              导入失败：{importError}
            </p>
          )}

          {archived.length > 0 && (
            <section className="mt-6" data-testid="archived-section">
              <button
                type="button"
                onClick={() => setShowArchived(!showArchived)}
                className="mb-2 text-sm font-medium text-ink-muted underline-offset-2 hover:text-ink hover:underline"
                data-testid="archived-toggle"
              >
                已归档（{archived.length}）{showArchived ? " 收起" : " 展开"}
              </button>
              {showArchived && (
                <ul className="space-y-1.5">
                  {archived.map((item) => (
                    <li
                      key={item.id}
                      className="flex items-center justify-between gap-3 rounded-md border border-line bg-surface px-4 py-2 text-sm"
                      data-testid={`archived-${item.id}`}
                    >
                      <Link
                        href={`/research/collections/${item.id}`}
                        className="min-w-0 truncate font-medium text-ink underline-offset-2 hover:text-brand hover:underline"
                      >
                        {item.name}
                      </Link>
                      <span className="flex shrink-0 gap-1">
                        <Button
                          variant="ghost"
                          onClick={() => restoreMutation.mutate(item.id)}
                          disabled={restoreMutation.isPending}
                          data-testid={`restore-${item.id}`}
                        >
                          恢复
                        </Button>
                        <Button
                          variant="ghost"
                          onClick={() => setPendingHardDelete(item)}
                          data-testid={`hard-delete-${item.id}`}
                        >
                          彻底删除
                        </Button>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}
        </div>

        <RecentRunsAside
          runs={runs}
          syncingCollections={syncingCollections}
          attentionCollections={attentionCollections}
        />
      </div>

      <CopyFromPlanDialog
        open={copyDialogOpen}
        onClose={() => setCopyDialogOpen(false)}
        onCreated={(collectionId) => {
          setCopyDialogOpen(false);
          void queryClient.invalidateQueries({ queryKey: ["research", "collections"] });
          router.push(`/research/collections/${collectionId}`);
        }}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        title="归档集合"
        description={`确认归档「${pendingDelete?.name ?? ""}」？归档后集合移入「已归档」区域，历史回测记录保留。`}
        confirmLabel="归档"
        pending={archiveMutation.isPending}
        onConfirm={() => {
          if (!pendingDelete) return;
          archiveMutation.mutate(pendingDelete.id, {
            onSettled: () => setPendingDelete(null),
          });
        }}
        onClose={() => setPendingDelete(null)}
      />

      <ConfirmDialog
        open={pendingHardDelete !== null}
        title="彻底删除集合"
        description={`确认彻底删除「${pendingHardDelete?.name ?? ""}」？集合内资产、权重与全部回测记录将被删除，且无法恢复。`}
        confirmLabel="彻底删除"
        pending={hardDeleteMutation.isPending}
        onConfirm={() => {
          if (!pendingHardDelete) return;
          hardDeleteMutation.mutate(pendingHardDelete.id, {
            onSettled: () => setPendingHardDelete(null),
          });
        }}
        onClose={() => setPendingHardDelete(null)}
      />
    </div>
  );
}
