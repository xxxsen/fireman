"use client";

import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { PercentInput } from "@/components/ui/PercentInput";
import { SaveBar } from "@/components/ui/SaveBar";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import {
  getAllocation,
  listScenarios,
  updateAllocation,
} from "@/lib/api/allocation";
import { getParameters, getPlan, updateParameters } from "@/lib/api/plans";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import type { AssetClassTarget, RegionTarget } from "@/types/api";

const ASSET_CLASSES = ["equity", "bond", "cash"] as const;

export function AllocationSettings() {
  const planId = useParams().id as string;
  const router = useRouter();
  const searchParams = useSearchParams();
  const returnTo = searchParams.get("return");
  const queryClient = useQueryClient();
  const { markDirty, markClean } = usePlanEdit();
  const [assetTargets, setAssetTargets] = useState<AssetClassTarget[]>([]);
  const [regionTargets, setRegionTargets] = useState<RegionTarget[]>([]);
  const [selectedScenarioId, setSelectedScenarioId] = useState("");
  const [allocationDirty, setAllocationDirty] = useState(false);
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

  useEffect(() => {
    if (!allocation.data || !parameters.data || allocationDirty) return;
    setAssetTargets(allocation.data.asset_class_targets);
    setRegionTargets(allocation.data.region_targets);
    setSelectedScenarioId(parameters.data.parameters.selected_scenario_id ?? "");
  }, [allocation.data, parameters.data, allocationDirty]);

  const assetCheck = validatePercentSum(
    assetTargets.map((target) => ({
      label: assetClassLabel(target.asset_class),
      value: target.weight,
    })),
  );
  const regionChecks = ASSET_CLASSES.map((assetClass) => ({
    assetClass,
    ...validatePercentSum(
      regionTargets
        .filter((target) => target.asset_class === assetClass)
        .map((target) => ({
          label: regionLabel(target.region),
          value: target.weight_within_class,
        })),
    ),
  }));

  const save = useMutation({
    mutationFn: async () => {
      if (!plan.data || !parameters.data) throw new Error("计划尚未加载");
      await updateAllocation(planId, {
        config_version: plan.data.config_version,
        asset_class_targets: assetTargets,
        region_targets: regionTargets,
      });
      return updateParameters(planId, {
        config_version: plan.data.config_version + 1,
        parameters: {
          ...parameters.data.parameters,
          selected_scenario_id: selectedScenarioId || null,
        },
        cash_flows: parameters.data.cash_flows,
      });
    },
    onSuccess: () => {
      markClean();
      setAllocationDirty(false);
      setSaveError(null);
      for (const key of ["plan", "parameters", "allocation", "targets", "dashboard"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
      if (returnTo === "asset-refresh") {
        router.push(`/plans/${planId}/asset-refresh`);
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
        <label className="mt-3 block text-sm">
          场景选择
          <select
            className="mt-1 w-full rounded-md border px-3 py-2"
            value={selectedScenarioId}
            onChange={(event) => {
              setSelectedScenarioId(event.target.value);
              setAllocationDirty(true);
              markDirty();
            }}
          >
            <option value="">—</option>
            {scenarios.data?.scenarios.map((scenario) => (
              <option key={scenario.id} value={scenario.id}>
                {scenario.name}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div>
        <h3 className="text-sm font-medium">大类目标权重</h3>
        <div className="mt-2 grid gap-3 sm:grid-cols-3">
          {assetTargets.map((target, index) => (
            <PercentInput
              key={target.asset_class}
              label={assetClassLabel(target.asset_class)}
              value={target.weight}
              onChange={(value) => {
                const next = [...assetTargets];
                next[index] = { ...target, weight: value };
                setAssetTargets(next);
                setAllocationDirty(true);
                markDirty();
              }}
            />
          ))}
        </div>
        {!assetCheck.passed && (
          <p className="mt-1 text-sm text-red-600">{assetCheck.message}</p>
        )}
      </div>

      {ASSET_CLASSES.map((assetClass) => {
        const check = regionChecks.find((item) => item.assetClass === assetClass);
        return (
          <div key={assetClass}>
            <h3 className="text-sm font-medium">
              {assetClassLabel(assetClass)} · 地区组内权重
            </h3>
            <div className="mt-2 grid gap-3 sm:grid-cols-2">
              {regionTargets
                .filter((target) => target.asset_class === assetClass)
                .map((target) => {
                  const index = regionTargets.indexOf(target);
                  return (
                    <PercentInput
                      key={`${target.asset_class}:${target.region}`}
                      label={regionLabel(target.region)}
                      value={target.weight_within_class}
                      onChange={(value) => {
                        const next = [...regionTargets];
                        next[index] = { ...target, weight_within_class: value };
                        setRegionTargets(next);
                        setAllocationDirty(true);
                        markDirty();
                      }}
                    />
                  );
                })}
            </div>
            {check && !check.passed && (
              <p className="mt-1 text-sm text-red-600">{check.message}</p>
            )}
          </div>
        );
      })}

      <SaveBar
        dirty={allocationDirty}
        saving={save.isPending}
        error={saveError}
        onSave={() => {
          if (!assetCheck.passed || regionChecks.some((check) => !check.passed)) {
            setSaveError("资产配置权重未通过校验。");
            return;
          }
          save.mutate();
        }}
      />
    </section>
  );
}
