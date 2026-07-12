"use client";

import { useCallback, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  getOptimizationReadiness,
  type ResearchTailRiskSpec,
} from "@/lib/api/research";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { MetricHelp } from "@/components/ui/MetricHelp";

const WEIGHT_STEP_OPTIONS = [
  { value: 0.01, label: "1%" },
  { value: 0.025, label: "2.5%" },
  { value: 0.05, label: "5%" },
  { value: 0.1, label: "10%" },
];

export interface OptimizationSubmitConfig {
  weight_step: number;
  top_k: number;
  tail_risk: ResearchTailRiskSpec;
  minimum_cagr?: number;
}

export interface OptimizationConfigDialogProps {
  open: boolean;
  onClose: () => void;
  pending: boolean;
  onSubmit: (config: OptimizationSubmitConfig) => void;
  collectionId: string;
  defaultConfidence: ResearchTailRiskSpec["confidence"];
  defaultHorizonDays: ResearchTailRiskSpec["horizon_days"];
}

function Segmented<T extends number>({
  values,
  value,
  labels,
  onChange,
  testId,
}: {
  values: readonly T[];
  value: T;
  labels: Record<T, string>;
  onChange: (value: T) => void;
  testId: string;
}) {
  return (
    <div className="inline-flex overflow-hidden rounded-md border border-line" data-testid={testId}>
      {values.map((option) => (
        <button
          key={option}
          type="button"
          aria-pressed={value === option}
          onClick={() => onChange(option)}
          className={`min-h-9 px-3 text-sm ${value === option ? "bg-brand text-white" : "bg-surface text-ink hover:bg-surface-muted"}`}
        >
          {labels[option]}
        </button>
      ))}
    </div>
  );
}

export function OptimizationConfigDialog({
  open,
  onClose,
  pending,
  onSubmit,
  collectionId,
  defaultConfidence,
  defaultHorizonDays,
}: OptimizationConfigDialogProps) {
  const [weightStep, setWeightStep] = useState(0.05);
  const [confidence, setConfidence] = useState(defaultConfidence);
  const [horizonDays, setHorizonDays] = useState(defaultHorizonDays);
  const [limitCAGR, setLimitCAGR] = useState(false);
  const [minimumCAGR, setMinimumCAGR] = useState("3");
  const [topK, setTopK] = useState(20);

  const readinessQuery = useQuery({
    queryKey: ["research", "optimization-readiness", collectionId, weightStep, confidence, horizonDays],
    queryFn: () =>
      getOptimizationReadiness(collectionId, { weightStep, confidence, horizonDays }),
    enabled: open,
    placeholderData: keepPreviousData,
  });

  const optReadiness = readinessQuery.data;
  const minimumCAGRValue = Number(minimumCAGR);
  const minimumCAGRValid = !limitCAGR || (
    minimumCAGR.trim() !== "" && Number.isFinite(minimumCAGRValue) &&
    minimumCAGRValue >= -95 && minimumCAGRValue <= 200
  );

  const handleSubmit = useCallback(() => {
    const config: OptimizationSubmitConfig = {
      weight_step: weightStep,
      top_k: topK,
      tail_risk: { confidence, horizon_days: horizonDays },
    };
    if (limitCAGR) config.minimum_cagr = minimumCAGRValue / 100;
    onSubmit(config);
  }, [confidence, horizonDays, limitCAGR, minimumCAGRValue, onSubmit, topK, weightStep]);

  const canSubmit =
    optReadiness?.ready && !pending && minimumCAGRValid && topK >= 1 && topK <= 100 &&
    (optReadiness?.candidate_count ?? 0) > 0;

  return (
    <Dialog open={open} onClose={onClose} title="寻找最优组合" data-testid="optimization-config-dialog">
      <div className="space-y-4">
        <div>
          <label className="mb-1 block text-sm font-medium text-ink">权重步长</label>
          <select
            value={weightStep}
            onChange={(event) => setWeightStep(Number(event.target.value))}
            className="rounded-md border border-line bg-surface px-3 py-1.5 text-sm text-ink"
            data-testid="weight-step-select"
          >
            {WEIGHT_STEP_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <span className="mb-1 flex items-center text-sm font-medium text-ink">
              CVaR 置信度
              <MetricHelp text="95% CVaR 表示历史最差 5% 持有期场景中的平均损失。" />
            </span>
            <Segmented
              values={[0.9, 0.95, 0.99] as const}
              value={confidence}
              labels={{ 0.9: "90%", 0.95: "95%", 0.99: "99%" }}
              onChange={setConfidence}
              testId="optimization-confidence"
            />
          </div>
          <div>
            <span className="mb-1 flex items-center text-sm font-medium text-ink">
              CVaR 持有期
              <MetricHelp text="20 日按有效收益日滚动复合，相邻场景会共享部分交易日。" />
            </span>
            <Segmented
              values={[1, 20] as const}
              value={horizonDays}
              labels={{ 1: "1 日", 20: "20 日" }}
              onChange={setHorizonDays}
              testId="optimization-horizon"
            />
          </div>
        </div>

        <label className="flex items-center gap-2 text-sm text-ink">
          <input type="checkbox" checked={limitCAGR} onChange={(event) => setLimitCAGR(event.target.checked)} />
          限制最低历史年化收益
        </label>
        {limitCAGR && (
          <label className="block text-sm font-medium text-ink">
            最低 CAGR（%）
            <input
              type="text"
              inputMode="decimal"
              value={minimumCAGR}
              onChange={(event) => setMinimumCAGR(event.target.value)}
              className="mt-1 block w-28 rounded-md border border-line bg-surface px-3 py-1.5 text-sm text-ink"
              aria-invalid={!minimumCAGRValid}
              data-testid="minimum-cagr-input"
            />
          </label>
        )}

        <label className="block text-sm font-medium text-ink">
          每组保留数量 (Top K)
          <input
            type="number"
            min={1}
            max={100}
            value={topK}
            onChange={(event) => setTopK(Number(event.target.value))}
            className="mt-1 block w-24 rounded-md border border-line bg-surface px-3 py-1.5 text-sm text-ink"
            data-testid="top-k-input"
          />
        </label>

        <dl className="rounded-md bg-surface-muted/60 px-4 py-3 text-xs">
          <div className="flex justify-between gap-3"><dt className="text-ink-muted">有效收益日</dt><dd className="text-ink">{optReadiness?.tail_risk?.effective_return_count ?? "—"}{readinessQuery.isFetching && <span className="ml-1 text-ink-muted">更新中…</span>}</dd></div>
          <div className="mt-1 flex justify-between gap-3"><dt className="text-ink-muted">CVaR 场景</dt><dd className="text-ink">{optReadiness?.tail_risk ? `${optReadiness.tail_risk.scenario_count} / 最少 ${optReadiness.tail_risk.minimum_scenario_count}` : "—"}</dd></div>
          <div className="mt-1 flex justify-between gap-3"><dt className="text-ink-muted">候选数量</dt><dd className="font-medium text-ink" data-testid="candidate-count">{optReadiness ? optReadiness.candidate_count.toLocaleString() : "—"}</dd></div>
        </dl>

        {optReadiness && !optReadiness.ready && (
          <div className="rounded-md border border-danger/25 bg-danger/5 px-3 py-2 text-xs text-danger">
            {optReadiness.blocking_reasons.map((reason, index) => <p key={`${reason.reason}-${reason.asset_key ?? ""}-${index}`}>{reason.message}</p>)}
          </div>
        )}
        {optReadiness?.warnings?.map((warning, index) => <p key={`${warning.reason}-${index}`} className="text-xs text-warning">{warning.message}</p>)}

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="secondary" onClick={onClose}>取消</Button>
          <Button disabled={!canSubmit} pending={pending} onClick={handleSubmit} data-testid="start-optimization">开始调优</Button>
        </div>
      </div>
    </Dialog>
  );
}
