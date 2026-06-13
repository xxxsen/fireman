"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { SaveBar } from "@/components/ui/SaveBar";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { applyScenario, getAllocation, listScenarios } from "@/lib/api/allocation";
import { getParameters, getPlan } from "@/lib/api/plans";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";

const ASSET_CLASSES = ["equity", "bond", "cash"] as const;

export function PlanTargetsContent() {
  const planId = useParams().id as string;
  const queryClient = useQueryClient();
  const { markDirty, markClean } = usePlanEdit();
  const [selectedScenarioId, setSelectedScenarioId] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  const plan = useQuery({
    queryKey: ["plan", planId],
    queryFn: () => getPlan(planId),
  });
  const parameters = useQuery({
    queryKey: ["parameters", planId],
    queryFn: () => getParameters(planId),
  });
  const allocation = useQuery({
    queryKey: ["allocation", planId],
    queryFn: () => getAllocation(planId),
  });
  const scenarios = useQuery({
    queryKey: ["scenarios"],
    queryFn: listScenarios,
  });

  const initialScenarioId = parameters.data?.parameters.selected_scenario_id ?? "";
  const currentScenarioId = selectedScenarioId ?? initialScenarioId;
  const dirty = currentScenarioId !== initialScenarioId;

  useEffect(() => {
    if (dirty) markDirty();
    else markClean();
  }, [dirty, markDirty, markClean]);

  const previewScenario = useMemo(
    () => scenarios.data?.scenarios.find((scenario) => scenario.id === currentScenarioId),
    [scenarios.data, currentScenarioId],
  );

  const assetTargets = previewScenario?.weights ?? allocation.data?.asset_class_targets ?? [];
  const regionTargets = allocation.data?.region_targets ?? [];

  const save = useMutation({
    mutationFn: async () => {
      if (!plan.data || !currentScenarioId) throw new Error("计划尚未加载");
      return applyScenario(planId, {
        scenario_id: currentScenarioId,
        config_version: plan.data.config_version,
        dry_run: false,
      });
    },
    onSuccess: () => {
      setSelectedScenarioId(null);
      setSaveError(null);
      markClean();
      for (const key of ["plan", "parameters", "allocation", "targets", "dashboard"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
    },
    onError: (error) =>
      setSaveError(error instanceof ApiError ? error.message : "保存失败"),
  });

  if (!allocation.data || !parameters.data) {
    return <p className="text-slate-600">加载目标配置…</p>;
  }

  return (
    <section className="space-y-5 rounded-lg border border-slate-200 p-4 pb-20">
      <div>
        <h2 className="text-lg font-medium">当前计划目标配置</h2>
        <p className="mt-1 text-sm text-slate-600">
          切换当前计划使用的场景模板；模板内的大类与地区权重仅可查看。
        </p>
        <label className="mt-3 block text-sm">
          场景模板
          <select
            className="mt-1 w-full rounded-md border px-3 py-2"
            value={currentScenarioId}
            onChange={(event) => setSelectedScenarioId(event.target.value)}
            data-testid="plan-targets-scenario-select"
          >
            <option value="">—</option>
            {scenarios.data?.scenarios.map((scenario) => (
              <option key={scenario.id} value={scenario.id}>
                {scenario.name}
              </option>
            ))}
          </select>
        </label>
        {dirty && (
          <p className="mt-2 text-sm text-amber-800" data-testid="plan-targets-preview-note">
            大类权重随所选场景预览；地区组内权重沿用当前计划配置，保存场景切换后生效。
          </p>
        )}
        <p className="mt-3 text-sm text-slate-600">
          要修改场景结构，请前往「场景配置」
        </p>
        <Link
          href="/scenarios"
          className="mt-2 inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm font-medium"
        >
          前往场景配置
        </Link>
      </div>

      <div>
        <h3 className="text-sm font-medium">大类目标权重（只读）</h3>
        <dl className="mt-2 grid gap-2 sm:grid-cols-3">
          {assetTargets.map((target) => (
            <div key={target.asset_class} className="rounded-md bg-slate-50 px-3 py-2 text-sm">
              <dt className="text-slate-500">{assetClassLabel(target.asset_class)}</dt>
              <dd className="font-medium tabular-nums">{formatPercent(target.weight)}</dd>
            </div>
          ))}
        </dl>
      </div>

      {ASSET_CLASSES.map((assetClass) => {
        const regions = regionTargets.filter((target) => target.asset_class === assetClass);
        if (regions.length === 0) return null;
        return (
          <div key={assetClass}>
            <h3 className="text-sm font-medium">
              {assetClassLabel(assetClass)} · 地区组内权重（只读）
            </h3>
            <dl className="mt-2 grid gap-2 sm:grid-cols-2">
              {regions.map((target) => (
                <div
                  key={`${target.asset_class}:${target.region}`}
                  className="rounded-md bg-slate-50 px-3 py-2 text-sm"
                >
                  <dt className="text-slate-500">{regionLabel(target.region)}</dt>
                  <dd className="font-medium tabular-nums">
                    {formatPercent(target.weight_within_class)}
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        );
      })}

      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={saveError}
        onSave={() => {
          if (!currentScenarioId) {
            setSaveError("请选择一个场景模板。");
            return;
          }
          save.mutate();
        }}
      />
    </section>
  );
}

/** @deprecated Use PlanTargetsContent */
export { PlanTargetsContent as AllocationSettings };
