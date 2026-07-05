"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getSimulationReadiness,
  syncMissingAssetHistory,
  type SimulationReadiness,
} from "@/lib/api/simulations";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import Link from "next/link";

const POLL_INTERVAL_MS = 4000;

const REASON_LABELS: Record<string, string> = {
  history_missing: "未同步历史数据",
  insufficient_history: "历史数据不足",
};

/**
 * Shared readiness query for the simulation entry. The panel and the run
 * button both consume this hook; react-query dedupes by key. While history
 * sync tasks are active the query polls until the backend reports ready.
 */
export function useSimulationReadiness(planId: string) {
  return useQuery({
    queryKey: ["simulation-readiness", planId],
    queryFn: () => getSimulationReadiness(planId),
    refetchInterval: (query) => {
      const data = query.state.data as SimulationReadiness | undefined;
      if (data && !data.ready && data.active_tasks.length > 0) return POLL_INTERVAL_MS;
      return false;
    },
  });
}

/**
 * Simulation readiness gate: lists plan holdings whose market asset history is
 * missing, offers one-click sync of every missing asset, and polls until the
 * readiness check passes.
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
      const created = res.created.length;
      const existing = res.existing.length;
      const parts: string[] = [];
      if (created > 0) parts.push(`已创建 ${created} 个同步任务`);
      if (existing > 0) parts.push(`复用 ${existing} 个进行中的任务`);
      setSyncMessage(parts.length ? parts.join("，") : "所有资产历史已就绪，正在重新检查…");
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
      <p className="font-medium">以下持仓的市场资产还没有可用的历史数据，暂时无法创建模拟：</p>
      <ul className="mt-2 space-y-1 text-sm" data-testid="missing-history-list">
        {data.missing_history.map((item) => (
          <li key={item.holding_id} className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{item.name || item.symbol || item.asset_key}</span>
            {item.symbol && <span className="text-ink-muted">{item.symbol}</span>}
            <span>{REASON_LABELS[item.reason] ?? item.reason}</span>
            <Link
              href={`/assets/market/${encodeURIComponent(item.asset_key)}`}
              className="text-brand underline-offset-2 hover:underline"
            >
              资产详情
            </Link>
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
        {syncMessage && <span className="text-sm">{syncMessage}</span>}
      </div>
      </Alert>
    </div>
  );
}
