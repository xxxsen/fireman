"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getSimulationReadiness,
  syncMissingAssetHistory,
  type BlockingAsset,
  type SimulationReadiness,
  type SyncMissingHistoryResult,
} from "@/lib/api/simulations";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import Link from "next/link";

const POLL_INTERVAL_MS = 4000;

const REASON_LABELS: Record<string, string> = {
  history_missing: "未同步历史数据",
  history_sync_running: "历史同步中",
  simulation_insufficient_history: "历史已同步，但完整年度不足，暂不可模拟",
  provider_data_anomaly: "历史已同步，但数据质量异常，暂不可模拟",
  asset_identity_conflict: "资产身份可能选错，当前历史不可用于模拟",
};

/**
 * Polling only follows in-flight history sync tasks: terminal blocked states
 * (identity conflict, data anomaly, short history) do not poll — they need
 * user action, not waiting. Exported for tests.
 */
export function readinessPollInterval(
  data: SimulationReadiness | undefined,
): number | false {
  if (data && !data.ready && data.active_tasks.length > 0) return POLL_INTERVAL_MS;
  return false;
}

/**
 * Shared readiness query for the simulation entry. The panel and the run
 * button both consume this hook; react-query dedupes by key.
 */
export function useSimulationReadiness(planId: string) {
  return useQuery({
    queryKey: ["simulation-readiness", planId],
    queryFn: () => getSimulationReadiness(planId),
    refetchInterval: (query) =>
      readinessPollInterval(query.state.data as SimulationReadiness | undefined),
  });
}

/** User-facing summary for a one-click sync result. Exported for tests. */
export function buildSyncResultMessage(res: SyncMissingHistoryResult): string {
  const parts: string[] = [];
  if (res.created.length > 0) parts.push(`已创建 ${res.created.length} 个同步任务`);
  if (res.existing.length > 0) parts.push(`复用 ${res.existing.length} 个进行中的任务`);
  if (res.blocked.length > 0) {
    if (parts.length === 0) {
      return "没有可创建的同步任务；部分资产历史已同步但不可用于模拟，请按提示处理。";
    }
    parts.push(`${res.blocked.length} 个资产历史已同步但不可用于模拟，请按提示处理`);
    return parts.join("，");
  }
  return parts.length ? parts.join("，") : "所有资产历史已就绪，正在重新检查…";
}

/** Per-reason action links: fix-in-place for conflicts, inspect for anomalies. */
function BlockingItemActions({ planId, item }: { planId: string; item: BlockingAsset }) {
  if (item.reason === "asset_identity_conflict") {
    return (
      <Link
        href={`/plans/${planId}/asset-refresh`}
        className="text-brand underline-offset-2 hover:underline"
        data-testid="readiness-go-asset-refresh"
      >
        去持仓校正
      </Link>
    );
  }
  return (
    <Link
      href={`/assets/market/${encodeURIComponent(item.asset_key)}`}
      className="text-brand underline-offset-2 hover:underline"
    >
      查看资产详情
    </Link>
  );
}

/**
 * Simulation readiness gate: lists plan holdings whose market asset cannot
 * build a simulation snapshot yet, offers one-click sync for the ones that
 * are actually missing history, and polls while sync tasks run.
 */
export function SimulationReadinessPanel({ planId }: { planId: string }) {
  const qc = useQueryClient();
  const readinessQ = useSimulationReadiness(planId);
  const [syncMessage, setSyncMessage] = useState<string | null>(null);

  const data = readinessQ.data;
  const wasBlocked = useRef(false);

  // When readiness flips back to ready (after a sync), refresh holdings so
  // snapshot-derived warnings and dashboards pick up the new data.
  useEffect(() => {
    if (!data) return;
    if (!data.ready) {
      wasBlocked.current = true;
      return;
    }
    if (wasBlocked.current) {
      wasBlocked.current = false;
      setSyncMessage(null);
      void qc.invalidateQueries({ queryKey: ["holdings", planId] });
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
    }
  }, [data, planId, qc]);

  const syncMut = useMutation({
    mutationFn: () => syncMissingAssetHistory(planId),
    onSuccess: (res) => {
      setSyncMessage(buildSyncResultMessage(res));
      void qc.invalidateQueries({ queryKey: ["simulation-readiness", planId] });
    },
    onError: (e) =>
      setSyncMessage(e instanceof Error ? `同步任务创建失败：${e.message}` : "同步任务创建失败"),
  });

  if (!data || data.ready) return null;

  const activeCount = data.active_tasks.length;

  return (
    <div data-testid="simulation-readiness-panel">
      <Alert variant="warning" className="mt-2">
      <p className="font-medium">以下持仓暂时无法创建模拟：</p>
      <ul className="mt-2 space-y-2 text-sm" data-testid="blocking-assets-list">
        {data.blocking_assets.map((item) => (
          <li key={item.holding_id} data-testid="blocking-asset-item">
            <span className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{item.name || item.symbol || item.asset_key}</span>
              {item.symbol && <span className="text-ink-muted">{item.symbol}</span>}
              <span>{REASON_LABELS[item.reason] ?? item.reason}</span>
              <BlockingItemActions planId={planId} item={item} />
            </span>
            {/* history_missing / history_sync_running are fully covered by
                their labels and the active-task line; only richer diagnoses
                (conflict advice, anomaly detail) need the backend message. */}
            {item.message &&
              item.reason !== "history_missing" &&
              item.reason !== "history_sync_running" && (
                <span className="mt-0.5 block text-xs text-ink-muted">{item.message}</span>
              )}
          </li>
        ))}
      </ul>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <Button
          variant="secondary"
          className="min-h-8 px-3 py-1 text-sm"
          disabled={syncMut.isPending}
          onClick={() => syncMut.mutate()}
          data-testid="sync-missing-history-button"
        >
          一键同步缺失历史数据
        </Button>
        {activeCount > 0 && (
          <span className="text-sm" data-testid="readiness-active-tasks">
            {activeCount} 个同步任务进行中，完成后将自动重新检查…
          </span>
        )}
        {syncMessage && (
          <span className="text-sm" data-testid="readiness-sync-message">
            {syncMessage}
          </span>
        )}
      </div>
      </Alert>
    </div>
  );
}
