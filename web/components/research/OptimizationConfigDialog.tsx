"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getOptimizationReadiness,
  type ResearchOptimizationReadiness,
} from "@/lib/api/research";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";

const WEIGHT_STEP_OPTIONS = [
  { value: 0.01, label: "1%" },
  { value: 0.025, label: "2.5%" },
  { value: 0.05, label: "5%" },
  { value: 0.1, label: "10%" },
];

export interface OptimizationConfigDialogProps {
  open: boolean;
  onClose: () => void;
  optReadiness?: ResearchOptimizationReadiness;
  pending: boolean;
  onSubmit: (config: { weight_step: number; top_k: number }) => void;
  onWeightStepChange: (step: number) => void;
  collectionId: string;
}

export function OptimizationConfigDialog({
  open,
  onClose,
  pending,
  onSubmit,
  collectionId,
}: OptimizationConfigDialogProps) {
  const [weightStep, setWeightStep] = useState(0.05);
  const [topK, setTopK] = useState(20);

  const readinessQuery = useQuery({
    queryKey: ["research", "optimization-readiness", collectionId, weightStep],
    queryFn: () => getOptimizationReadiness(collectionId, weightStep),
    enabled: open,
  });

  const optReadiness = readinessQuery.data;

  useEffect(() => {
    if (!open) {
      setWeightStep(0.05);
      setTopK(20);
    }
  }, [open]);

  const handleSubmit = useCallback(() => {
    onSubmit({ weight_step: weightStep, top_k: topK });
  }, [onSubmit, weightStep, topK]);

  const canSubmit =
    optReadiness?.ready && !pending && (optReadiness?.candidate_count ?? 0) > 0;

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="寻找最优组合"
      data-testid="optimization-config-dialog"
    >
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">
          自动调优会保持锁定资产权重不变，并在未锁定资产之间分配剩余权重。
          权重为 0 且启用的资产会参与调优。一期最多支持 10 个启用资产。
        </p>

        <div>
          <label className="mb-1 block text-sm font-medium text-ink">权重步长</label>
          <select
            value={weightStep}
            onChange={(e) => setWeightStep(Number(e.target.value))}
            className="rounded-md border border-line bg-surface px-3 py-1.5 text-sm text-ink"
            data-testid="weight-step-select"
          >
            {WEIGHT_STEP_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="mb-1 block text-sm font-medium text-ink">
            每组保留数量 (Top K)
          </label>
          <input
            type="number"
            min={1}
            max={100}
            value={topK}
            onChange={(e) => setTopK(Math.max(1, Number(e.target.value) || 1))}
            className="w-24 rounded-md border border-line bg-surface px-3 py-1.5 text-sm text-ink"
            data-testid="top-k-input"
          />
        </div>

        <dl className="rounded-md bg-surface-muted/60 px-4 py-3 text-xs">
          <div className="flex justify-between gap-3">
            <dt className="text-ink-muted">启用资产</dt>
            <dd className="text-ink">{optReadiness?.enabled_count ?? "—"}</dd>
          </div>
          <div className="mt-1 flex justify-between gap-3">
            <dt className="text-ink-muted">锁定 / 可调</dt>
            <dd className="text-ink">
              {optReadiness
                ? `${optReadiness.locked_count} / ${optReadiness.tunable_count}`
                : "—"}
            </dd>
          </div>
          <div className="mt-1 flex justify-between gap-3">
            <dt className="text-ink-muted">候选数量预估</dt>
            <dd className="font-medium text-ink" data-testid="candidate-count">
              {readinessQuery.isFetching
                ? "计算中…"
                : optReadiness
                  ? optReadiness.candidate_count.toLocaleString()
                  : "—"}
            </dd>
          </div>
        </dl>

        {optReadiness && !optReadiness.ready && (
          <div className="rounded-md border border-danger/25 bg-danger/5 px-3 py-2 text-xs text-danger">
            {optReadiness.blocking_reasons.map((r, i) => (
              <p key={i}>{r.message}</p>
            ))}
          </div>
        )}

        {optReadiness?.warnings?.map((w, i) => (
          <p key={i} className="text-xs text-warning">
            {w.message}
          </p>
        ))}

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            disabled={!canSubmit}
            pending={pending}
            onClick={handleSubmit}
            data-testid="start-optimization"
          >
            开始调优
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
