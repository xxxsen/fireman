"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { PercentInput } from "@/components/ui/PercentInput";
import {
  createScenario,
  deleteScenario,
  listScenarios,
  updateScenario,
} from "@/lib/api/allocation";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import {
  buildRegionTargetsPayload,
  defaultWizardRegionTargets,
} from "@/lib/wizard-allocation";
import type { AllocationScenario, RegionTarget } from "@/types/api";
import { ApiError } from "@/lib/api/client";

const EDITABLE_REGION_CLASSES = ["equity", "bond", "cash"] as const;

function defaultScenarioRegionTargets(): RegionTarget[] {
  return buildRegionTargetsPayload(defaultWizardRegionTargets());
}

function ensureRegionTargets(targets: RegionTarget[] | undefined): RegionTarget[] {
  return targets && targets.length > 0 ? targets : defaultScenarioRegionTargets();
}

function updateRegionTarget(
  targets: RegionTarget[],
  assetClass: string,
  region: string,
  weight: number,
): RegionTarget[] {
  return targets.map((target) =>
    target.asset_class === assetClass && target.region === region
      ? { ...target, weight_within_class: weight }
      : target,
  );
}

function validateScenarioRegionTargets(targets: RegionTarget[]): string | null {
  for (const assetClass of ["equity", "bond", "cash"] as const) {
    const group = targets.filter((target) => target.asset_class === assetClass);
    const check = validatePercentSum(
      group.map((target) => ({ label: target.region, value: target.weight_within_class })),
    );
    if (!check.passed) {
      return `${assetClassLabel(assetClass)} 地区组内权重：${check.message}`;
    }
  }
  return null;
}

export function ScenariosPageContent() {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<AllocationScenario | null>(null);
  const [error, setError] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["scenarios"],
    queryFn: listScenarios,
  });

  const saveScenario = async () => {
    if (!editing) return;
    const weightCheck = validatePercentSum(
      editing.weights.map((w) => ({ label: w.asset_class, value: w.weight })),
    );
    if (!weightCheck.passed) {
      setError(weightCheck.message);
      return;
    }
    const regionTargets = ensureRegionTargets(editing.region_targets);
    const regionError = validateScenarioRegionTargets(regionTargets);
    if (regionError) {
      setError(regionError);
      return;
    }
    try {
      if (editing.id.startsWith("new_")) {
        await createScenario({
          name: editing.name,
          description: editing.description,
          weights: editing.weights,
          region_targets: regionTargets,
        });
      } else {
        await updateScenario(editing.id, {
          name: editing.name,
          description: editing.description,
          weights: editing.weights,
          region_targets: regionTargets,
        });
      }
      setEditing(null);
      setError(null);
      void qc.invalidateQueries({ queryKey: ["scenarios"] });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "保存失败");
    }
  };

  if (isLoading) return <p>加载场景…</p>;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">场景配置</h1>
          <p className="mt-1 text-sm text-slate-600">
            管理跨计划复用的场景模板；在计划设置中切换当前计划使用的模板。
          </p>
        </div>
        <button
          type="button"
          className="min-h-11 rounded-md border px-4 text-sm"
          onClick={() => {
            setEditing({
              id: "new_" + Date.now(),
              name: "新场景",
              description: "",
              is_builtin: false,
              plan_count: 0,
              weights: [
                { asset_class: "equity", weight: 0.7 },
                { asset_class: "bond", weight: 0.3 },
                { asset_class: "cash", weight: 0 },
              ],
              region_targets: defaultScenarioRegionTargets(),
              created_at: 0,
              updated_at: 0,
            });
          }}
        >
          新建场景
        </button>
      </div>

      {error && <p className="text-sm text-red-600">{error}</p>}

      <div className="grid gap-4 md:grid-cols-2">
        {data?.scenarios.map((scn) => {
          const total = scn.weights.reduce((s, w) => s + w.weight, 0);
          const ok = Math.abs(total - 1) <= 0.0001;
          return (
            <article key={scn.id} className="rounded-lg border border-slate-200 p-4">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-medium">{scn.name}</h3>
                  <p className="text-sm text-slate-500">{scn.description}</p>
                </div>
                {scn.is_builtin && (
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs">内置</span>
                )}
              </div>
              <table className="mt-3 w-full text-sm">
                <tbody>
                  {scn.weights.map((w) => (
                    <tr key={w.asset_class}>
                      <td>{assetClassLabel(w.asset_class)}</td>
                      <td className="text-right">{formatPercent(w.weight)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className={`mt-2 text-xs ${ok ? "text-emerald-700" : "text-red-700"}`}>
                合计 {formatPercent(total)} {ok ? "，通过" : "，未通过"}
              </p>
              <p className="text-xs text-slate-500">{scn.plan_count} 个计划使用</p>
              <div className="mt-3 flex flex-wrap gap-2">
                <button
                  type="button"
                  className="text-sm underline"
                  onClick={() =>
                    setEditing({
                      ...scn,
                      weights: [...scn.weights],
                      region_targets: ensureRegionTargets(scn.region_targets).map((target) => ({
                        ...target,
                      })),
                    })
                  }
                >
                  编辑
                </button>
                <button
                  type="button"
                  className="text-sm underline"
                  onClick={() => {
                    setEditing({
                      ...scn,
                      id: "new_" + Date.now(),
                      name: scn.name + " 副本",
                      is_builtin: false,
                      weights: [...scn.weights],
                      region_targets: ensureRegionTargets(scn.region_targets).map((target) => ({
                        ...target,
                      })),
                    });
                  }}
                >
                  复制
                </button>
                {!scn.is_builtin && scn.plan_count === 0 && (
                  <button
                    type="button"
                    className="text-sm text-red-600 underline"
                    onClick={async () => {
                      if (window.confirm("确定删除此场景？")) {
                        await deleteScenario(scn.id);
                        void qc.invalidateQueries({ queryKey: ["scenarios"] });
                      }
                    }}
                  >
                    删除
                  </button>
                )}
              </div>
            </article>
          );
        })}
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4">
          <div className="max-h-[90vh] w-full max-w-lg overflow-auto rounded-lg bg-white p-6 shadow-xl">
            <h3 className="text-lg font-medium">编辑场景</h3>
            <label className="mt-4 block text-sm">
              名称
              <input
                className="mt-1 w-full rounded-md border px-3 py-2"
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
              />
            </label>
            <p className="mt-4 text-sm font-medium">大类权重</p>
            {editing.weights.map((w, i) => (
              <div key={w.asset_class} className="mt-3">
                <PercentInput
                  label={assetClassLabel(w.asset_class)}
                  value={w.weight}
                  onChange={(v) => {
                    const weights = [...editing.weights];
                    weights[i] = { ...w, weight: v };
                    setEditing({ ...editing, weights });
                  }}
                />
              </div>
            ))}
            <p className="mt-4 text-sm font-medium">地区组内权重</p>
            {EDITABLE_REGION_CLASSES.map((assetClass) => {
              const regions = ensureRegionTargets(editing.region_targets).filter(
                (target) => target.asset_class === assetClass,
              );
              return (
                <div key={assetClass} className="mt-3 rounded-md border border-slate-200 p-3">
                  <p className="text-sm font-medium">{assetClassLabel(assetClass)}</p>
                  {regions.map((target) => (
                    <div key={`${target.asset_class}:${target.region}`} className="mt-2">
                      <PercentInput
                        label={regionLabel(target.region)}
                        value={target.weight_within_class}
                        onChange={(value) =>
                          setEditing({
                            ...editing,
                            region_targets: updateRegionTarget(
                              ensureRegionTargets(editing.region_targets),
                              target.asset_class,
                              target.region,
                              value,
                            ),
                          })
                        }
                      />
                    </div>
                  ))}
                </div>
              );
            })}
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="px-3 py-2 text-sm" onClick={() => setEditing(null)}>
                取消
              </button>
              <button
                type="button"
                className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white"
                onClick={() => void saveScenario()}
              >
                保存场景
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default function ScenariosPage() {
  return <ScenariosPageContent />;
}
