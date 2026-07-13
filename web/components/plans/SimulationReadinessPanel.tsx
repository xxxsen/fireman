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
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import Link from "next/link";
import { HelpLabel } from "@/components/ui/HelpLabel";
import { MetricHelp } from "@/components/ui/MetricHelp";

const POLL_INTERVAL_MS = 4000;

const REASON_LABELS: Record<string, string> = {
  history_missing: "未同步历史数据",
  history_sync_running: "历史同步中",
  simulation_insufficient_history: "历史已同步，但完整年度不足，暂不可模拟",
  provider_data_anomaly: "历史已同步，但数据质量异常，暂不可模拟",
  foreign_cash_not_supported: "外币现金暂不支持 FIRE 模拟",
};

/**
 * Polling only follows in-flight history sync tasks: terminal blocked states
 * (data anomaly, short history) do not poll because waiting cannot resolve
 * them. Exported for tests.
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

/** Readiness diagnoses the selected asset's own data; inspect it in place. */
function BlockingItemActions({ item }: { item: BlockingAsset }) {
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
  // Local lock covering the window between the sync mutation resolving (tasks
  // created/reused) and the readiness query refetching active_tasks. Without
  // it the button briefly re-enables and can be double-clicked.
  const [syncLocked, setSyncLocked] = useState(false);

  const data = readinessQ.data;
  const wasBlocked = useRef(false);

  // When readiness flips back to ready (after a sync), refresh holdings so
  // snapshot-derived warnings and dashboards pick up the new data.
  useEffect(() => {
    if (!data) return;
    if (!data.ready) {
      wasBlocked.current = true;
    } else if (wasBlocked.current) {
      wasBlocked.current = false;
      setSyncMessage(null);
      void qc.invalidateQueries({ queryKey: ["holdings", planId] });
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
    }
  }, [data, planId, qc]);

  // Release the local lock once readiness reflects reality again: either
  // the tasks now show up as active_tasks (activeCount disables the button)
  // or everything became ready. Render-time state adjustment instead of an
  // effect so the release happens in the same pass.
  if (syncLocked && data && (data.ready || data.active_tasks.length > 0)) {
    setSyncLocked(false);
  }

  const syncMut = useMutation({
    mutationFn: () => syncMissingAssetHistory(planId),
    onSuccess: (res) => {
      const hasTask = res.created.length > 0 || res.existing.length > 0;
      setSyncLocked(hasTask);
      setSyncMessage(buildSyncResultMessage(res));
      void qc.invalidateQueries({ queryKey: ["simulation-readiness", planId] });
    },
    onError: (e) => {
      setSyncLocked(false);
      setSyncMessage(e instanceof Error ? `同步任务创建失败：${e.message}` : "同步任务创建失败");
    },
  });

  if (readinessQ.isLoading || readinessQ.isFetching) {
    return (
      <p className="mt-2 text-sm text-ink-muted" role="status">
        正在检查模拟就绪状态…
      </p>
    );
  }

  if (readinessQ.isError) {
    return (
      <Alert variant="danger" className="mt-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>模拟就绪状态检查失败，请重试。</span>
          <Button
            variant="secondary"
            className="min-h-8 px-3 py-1 text-sm"
            onClick={() => void readinessQ.refetch()}
          >
            重试就绪检查
          </Button>
        </div>
      </Alert>
    );
  }

  if (!data || data.ready) return null;

  const activeCount = data.active_tasks.length;

  return (
    <div data-testid="simulation-readiness-panel">
      <Alert variant="warning" className="mt-2">
      <p className="font-medium">
        <HelpLabel label="以下持仓暂时无法创建模拟：" termKey="readiness_status" />
        <MetricHelp termKey="simulation_snapshot_sync" />
      </p>
      <ul className="mt-2 space-y-2 text-sm" data-testid="blocking-assets-list">
        {data.blocking_assets.map((item) => (
          <li key={item.holding_id} data-testid="blocking-asset-item">
            <span className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{item.name || item.symbol || item.asset_key}</span>
              {item.symbol && <span className="text-ink-muted">{item.symbol}</span>}
              <span>{REASON_LABELS[item.reason] ?? item.reason}</span>
              <BlockingItemActions item={item} />
            </span>
            {/* history_missing / history_sync_running are fully covered by
                their labels and the active-task line; only richer diagnoses
                (such as anomaly details) need the backend message. */}
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
          pending={syncMut.isPending || syncLocked}
          disabled={syncMut.isPending || syncLocked || activeCount > 0}
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
      {data.active_tasks.length > 0 && (
        <ul className="mt-3 space-y-1.5">
          {data.active_tasks.map((task) => (
            <li
              key={task.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-line px-3 py-2 text-xs"
            >
              <span className="min-w-0 truncate font-mono text-ink-muted">
                {task.scope_id || task.id}
              </span>
              <TaskCancelButton
                task={task}
                shared
                className="min-h-7 px-2 py-0.5 text-xs"
                onCanceled={() => readinessQ.refetch().then(() => undefined)}
              />
            </li>
          ))}
        </ul>
      )}
      </Alert>
    </div>
  );
}
