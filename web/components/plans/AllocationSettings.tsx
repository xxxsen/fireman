"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { PercentInput } from "@/components/ui/PercentInput";
import { SaveBar } from "@/components/ui/SaveBar";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import {
  applyScenario,
  getAllocation,
  listScenarios,
  updateAllocation,
} from "@/lib/api/allocation";
import { getParameters, getPlan } from "@/lib/api/plans";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import type { RegionTarget } from "@/types/api";
import { HelpLabel } from "@/components/ui/HelpLabel";

const ASSET_CLASSES = ["equity", "bond", "cash"] as const;

export function PlanTargetsContent() {
  const planId = useParams().id as string;
  const queryClient = useQueryClient();
  const { markDirty, markClean } = usePlanEdit();
  const [selectedScenarioId, setSelectedScenarioId] = useState<string | null>(null);
  const [localRegionTargets, setLocalRegionTargets] = useState<RegionTarget[] | null>(null);
  const [regionDirty, setRegionDirty] = useState(false);
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
  const scenarioDirty = currentScenarioId !== initialScenarioId;
  const dirty = scenarioDirty || regionDirty;

  useEffect(() => {
    if (dirty) markDirty();
    else markClean();
  }, [dirty, markDirty, markClean]);

  const previewScenario = useMemo(
    () => scenarios.data?.scenarios.find((scenario) => scenario.id === currentScenarioId),
    [scenarios.data, currentScenarioId],
  );

  // Scenario templates only carry asset-class structure; the domestic/foreign
  // split belongs to the plan and is never overwritten by switching templates.
  const assetTargets = scenarioDirty
    ? (previewScenario?.weights ?? allocation.data?.asset_class_targets ?? [])
    : (allocation.data?.asset_class_targets ?? []);
  const regionTargets =
    regionDirty && localRegionTargets
      ? localRegionTargets
      : (allocation.data?.region_targets ?? []);

  const regionChecks = ASSET_CLASSES.map((ac) => {
    const items = regionTargets
      .filter((r) => r.asset_class === ac)
      .map((r) => ({ label: regionLabel(r.region), value: r.weight_within_class }));
    return { ac, ...validatePercentSum(items) };
  });

  const save = useMutation({
    mutationFn: async () => {
      if (!plan.data) throw new Error("计划尚未加载");
      let version = plan.data.config_version;
      if (scenarioDirty) {
        if (!currentScenarioId) throw new Error("请选择一个配置模板");
        const res = await applyScenario(planId, {
          scenario_id: currentScenarioId,
          config_version: version,
          dry_run: false,
        });
        version = res.config_version ?? version + 1;
      }
      if (regionDirty) {
        await updateAllocation(planId, {
          config_version: version,
          asset_class_targets: assetTargets,
          region_targets: regionTargets,
        });
      }
    },
    onSuccess: () => {
      setSelectedScenarioId(null);
      setLocalRegionTargets(null);
      setRegionDirty(false);
      setSaveError(null);
      markClean();
      for (const key of ["plan", "parameters", "allocation", "targets", "dashboard"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
    },
    onError: (error) => setSaveError(error instanceof ApiError ? error.message : "保存失败"),
  });

  if (!allocation.data || !parameters.data) {
    return <p className="text-ink-muted">加载目标配置…</p>;
  }

  return (
    <section className="space-y-5 rounded-lg border border-line p-4 pb-20">
      <div>
        <h2 className="text-lg font-medium">目标配置</h2>
        <p className="mt-1 text-sm text-ink-muted">
          配置模板决定大类目标权重；国内/国外配比是本计划自己的设置，切换模板不会改变它。年龄、储蓄等
          FIRE 假设请在「FIRE 参数」分区调整。
        </p>
        <label className="mt-3 block text-sm">
          <HelpLabel label="配置模板" termKey="config_template" />
          <select
            className="input-base mt-1"
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
        {scenarioDirty && (
          <p className="mt-2 text-sm text-warning" data-testid="plan-targets-preview-note">
            大类目标权重随所选配置模板预览；保存后将应用新模板的大类权重，地区配比保持不变。
          </p>
        )}
        <p className="mt-3 text-sm text-ink-muted">
          要修改模板结构，请前往{" "}
          <Link
            href="/scenarios"
            className="font-medium text-brand underline underline-offset-2"
            data-testid="plan-targets-scenarios-link"
          >
            配置模板
          </Link>
        </p>
      </div>

      <div>
        <h3 className="text-sm font-medium">
          <HelpLabel label="大类目标权重（只读）" termKey="target_weight_portfolio" />
        </h3>
        <dl className="mt-2 grid gap-2 sm:grid-cols-3">
          {assetTargets.map((target) => (
            <div key={target.asset_class} className="rounded-md bg-surface-muted px-3 py-2 text-sm">
              <dt className="text-ink-muted">{assetClassLabel(target.asset_class)}</dt>
              <dd className="font-medium tabular-nums">{formatPercent(target.weight)}</dd>
            </div>
          ))}
        </dl>
      </div>

      <div className="space-y-4">
        <h3 className="text-sm font-medium">
          <HelpLabel
            label="本计划国内/国外配比"
            termKey="target_weight_within_asset_class"
          />
        </h3>
        {ASSET_CLASSES.map((assetClass) => {
          const regions = regionTargets.filter((target) => target.asset_class === assetClass);
          if (regions.length === 0) return null;
          const check = regionChecks.find((c) => c.ac === assetClass);
          return (
            <div key={assetClass}>
              <h4 className="text-sm text-ink-muted">{assetClassLabel(assetClass)}</h4>
              <div className="mt-2 grid gap-3 sm:grid-cols-2">
                {regions.map((target) => {
                  const idx = regionTargets.indexOf(target);
                  return (
                    <PercentInput
                      key={`${target.asset_class}:${target.region}`}
                      label={
                        <HelpLabel
                          label={regionLabel(target.region)}
                          termKey="target_weight_within_asset_class"
                        />
                      }
                      value={target.weight_within_class}
                      onChange={(v) => {
                        const next = [...regionTargets];
                        next[idx] = { ...target, weight_within_class: v };
                        setLocalRegionTargets(next);
                        setRegionDirty(true);
                      }}
                    />
                  );
                })}
              </div>
              {check && !check.passed && (
                <p className="mt-1 text-sm text-danger">{check.message}</p>
              )}
            </div>
          );
        })}
      </div>

      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={saveError}
        onSave={() => {
          if (scenarioDirty && !currentScenarioId) {
            setSaveError("请选择一个配置模板。");
            return;
          }
          if (regionDirty && regionChecks.some((c) => !c.passed)) {
            setSaveError("各大类国内与国外配比须合计 100%。");
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
