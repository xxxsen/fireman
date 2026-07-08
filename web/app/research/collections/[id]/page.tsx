"use client";

import { useCallback, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import {
  addCollectionItem,
  deleteCollectionItem,
  getCollection,
  getReadiness,
  listRuns,
  normalizeWeights,
  updateCollection,
  updateCollectionItem,
  type ResearchAssetView,
  type ResearchCollectionDetail,
  type ResearchItemUpdate,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { collectionToJSON } from "@/lib/research/collection-json";
import { PageHeader } from "@/components/ui/PageHeader";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { CollectionParamsForm } from "@/components/research/CollectionParamsForm";
import { WeightEditor, type WeightUpdates } from "@/components/research/WeightEditor";
import { DataStatusPanel } from "@/components/research/DataStatusPanel";
import { BacktestPanel } from "@/components/research/BacktestPanel";
import { AddAssetDialog } from "@/components/research/AddAssetDialog";
import { CopyToPlanDialog } from "@/components/research/CopyToPlanDialog";

export default function ResearchCollectionPage() {
  const id = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [addAssetOpen, setAddAssetOpen] = useState(false);
  const [copyToPlanOpen, setCopyToPlanOpen] = useState(false);
  const [itemError, setItemError] = useState<string | null>(null);

  const detailQuery = useQuery({
    queryKey: ["research", "collection", id],
    queryFn: () => getCollection(id),
  });

  const readinessQuery = useQuery({
    queryKey: ["research", "readiness", id],
    queryFn: () => getReadiness(id),
    enabled: detailQuery.isSuccess,
  });

  const runsQuery = useQuery({
    queryKey: ["research", "runs", id],
    queryFn: () => listRuns(id, 5),
    enabled: detailQuery.isSuccess,
  });

  const applyDetail = useCallback(
    (detail: ResearchCollectionDetail) => {
      queryClient.setQueryData(["research", "collection", id], detail);
      void queryClient.invalidateQueries({ queryKey: ["research", "readiness", id] });
      void queryClient.invalidateQueries({
        queryKey: ["research", "optimization-readiness", id],
      });
    },
    [queryClient, id],
  );

  const updateItemMutation = useMutation({
    mutationFn: ({ itemId, patch }: { itemId: string; patch: ResearchItemUpdate }) =>
      updateCollectionItem(id, itemId, patch),
    onSuccess: (detail) => {
      setItemError(null);
      applyDetail(detail);
    },
    onError: (err) => setItemError(queryErrorMessage(err)),
  });

  const deleteItemMutation = useMutation({
    mutationFn: (itemId: string) => deleteCollectionItem(id, itemId),
    onSuccess: (detail) => {
      setItemError(null);
      applyDetail(detail);
    },
    onError: (err) => setItemError(queryErrorMessage(err)),
  });

  const batchWeightsMutation = useMutation({
    mutationFn: async (updates: WeightUpdates) => {
      let latest: ResearchCollectionDetail | null = null;
      for (const { itemId, weight } of updates) {
        latest = await updateCollectionItem(id, itemId, { weight });
      }
      return latest;
    },
    onSuccess: (detail) => {
      setItemError(null);
      if (detail) applyDetail(detail);
    },
    onError: (err) => setItemError(queryErrorMessage(err)),
  });

  const normalizeMutation = useMutation({
    mutationFn: () => normalizeWeights(id),
    onSuccess: (detail) => {
      setItemError(null);
      applyDetail(detail);
    },
    onError: (err) => setItemError(queryErrorMessage(err)),
  });

  const reorderMutation = useMutation({
    mutationFn: async (orderedItemIds: string[]) => {
      let latest: ResearchCollectionDetail | null = null;
      const current = detailQuery.data?.items ?? [];
      const currentOrder = new Map(current.map((it) => [it.id, it.sort_order]));
      for (let i = 0; i < orderedItemIds.length; i++) {
        const itemId = orderedItemIds[i]!;
        if (currentOrder.get(itemId) === i) continue;
        latest = await updateCollectionItem(id, itemId, { sort_order: i });
      }
      return latest;
    },
    onSuccess: (detail) => {
      setItemError(null);
      if (detail) applyDetail(detail);
    },
    onError: (err) => setItemError(queryErrorMessage(err)),
  });

  const addItemMutation = useMutation({
    mutationFn: (asset: ResearchAssetView) =>
      addCollectionItem(id, {
        asset_key: asset.asset_key,
        weight: 0,
        enabled: true,
        adjust_policy: asset.adjust_policy,
        point_type: asset.point_type,
      }),
    onSuccess: (detail) => {
      setItemError(null);
      applyDetail(detail);
    },
    onError: (err) => setItemError(queryErrorMessage(err)),
  });

  const restoreMutation = useMutation({
    mutationFn: () => updateCollection(id, { status: "active" }),
    onSuccess: applyDetail,
  });

  const exportJSON = useCallback(() => {
    const detail = detailQuery.data;
    if (!detail) return;
    const blob = new Blob([JSON.stringify(collectionToJSON(detail), null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `research-collection-${detail.id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }, [detailQuery.data]);

  if (detailQuery.isLoading) {
    return (
      <div className="content-enter">
        <LoadingState label="加载集合…" />
      </div>
    );
  }

  if (detailQuery.isError) {
    return (
      <div className="content-enter">
        <ErrorState
          message="加载研究集合失败。"
          onRetry={() => void detailQuery.refetch()}
          backHref="/research"
          technicalDetail={queryErrorMessage(detailQuery.error)}
        />
      </div>
    );
  }

  const detail = detailQuery.data!;
  const itemsPending =
    updateItemMutation.isPending ||
    deleteItemMutation.isPending ||
    batchWeightsMutation.isPending ||
    normalizeMutation.isPending ||
    reorderMutation.isPending ||
    addItemMutation.isPending;

  return (
    <div className="content-enter">
      <PageHeader
        backHref="/research"
        backLabel="组合研究"
        title={detail.name}
        status={
          detail.status === "archived" ? <Badge variant="neutral">已归档</Badge> : undefined
        }
        description={detail.description || undefined}
        secondaryActions={
          <>
            {detail.status === "archived" && (
              <Button
                variant="secondary"
                pending={restoreMutation.isPending}
                onClick={() => restoreMutation.mutate()}
                data-testid="restore-collection"
              >
                恢复集合
              </Button>
            )}
            <Button
              variant="secondary"
              href={`/research/screener?collection=${encodeURIComponent(id)}`}
              data-testid="screener-add-entry"
            >
              从筛选器添加
            </Button>
            <Button
              variant="secondary"
              onClick={() => setCopyToPlanOpen(true)}
              data-testid="copy-to-plan-entry"
            >
              复制到计划
            </Button>
            <Button variant="secondary" onClick={exportJSON} data-testid="export-collection-json">
              导出 JSON
            </Button>
            <Button
              variant="secondary"
              onClick={() => router.push(`/research/collections/${id}/runs`)}
            >
              运行记录
            </Button>
          </>
        }
      />

      <div className="space-y-6">
        {/* Key remounts the form whenever the server copy changes so local
            drafts reset to the persisted values. */}
        <CollectionParamsForm
          key={`${detail.id}:${detail.updated_at}`}
          detail={detail}
          onSaved={applyDetail}
        />

        {itemError && (
          <p className="rounded-md border border-danger/25 bg-danger/5 px-3 py-2 text-sm text-danger" role="alert">
            {itemError}
          </p>
        )}

        <WeightEditor
          detail={detail}
          readiness={readinessQuery.data}
          pending={itemsPending}
          onUpdateItem={(itemId, patch) => updateItemMutation.mutate({ itemId, patch })}
          onDeleteItem={(itemId) => deleteItemMutation.mutate(itemId)}
          onApplyWeights={(updates) => batchWeightsMutation.mutate(updates)}
          onNormalize={() => normalizeMutation.mutate()}
          onReorder={(ids) => reorderMutation.mutate(ids)}
          onAddAsset={() => setAddAssetOpen(true)}
        />

        <DataStatusPanel
          collectionId={id}
          readiness={readinessQuery.data}
          readinessLoading={readinessQuery.isFetching}
          onReadinessRefresh={() => {
            void queryClient.invalidateQueries({ queryKey: ["research", "readiness", id] });
            void queryClient.invalidateQueries({ queryKey: ["research", "collection", id] });
          }}
        />

        <BacktestPanel
          detail={detail}
          readiness={readinessQuery.data}
          latestRuns={runsQuery.data?.runs ?? []}
        />
      </div>

      <AddAssetDialog
        open={addAssetOpen}
        onClose={() => setAddAssetOpen(false)}
        existingAssetKeys={new Set(detail.items.map((it) => it.asset_key))}
        onAdd={(asset) => addItemMutation.mutate(asset)}
        addPending={addItemMutation.isPending}
      />

      <CopyToPlanDialog
        open={copyToPlanOpen}
        onClose={() => setCopyToPlanOpen(false)}
        detail={detail}
      />
    </div>
  );
}
