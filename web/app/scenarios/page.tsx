"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { EmptyState } from "@/components/ui/EmptyState";
import { PercentInput } from "@/components/ui/PercentInput";
import { MetricHelp } from "@/components/ui/MetricHelp";
import {
  createScenario,
  deleteScenario,
  listScenarios,
  updateScenario,
} from "@/lib/api/allocation";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import { queryErrorMessage } from "@/lib/query-error";
import {
  buildRegionTargetsPayload,
  defaultWizardRegionTargets,
} from "@/lib/wizard-allocation";
import type { AllocationScenario, RegionTarget } from "@/types/api";

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
  const [formError, setFormError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AllocationScenario | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ["scenarios"],
    queryFn: listScenarios,
  });

  const saveMut = useMutation({
    mutationFn: async (scn: AllocationScenario) => {
      const regionTargets = ensureRegionTargets(scn.region_targets);
      if (scn.id.startsWith("new_")) {
        return createScenario({
          name: scn.name,
          description: scn.description,
          weights: scn.weights,
          region_targets: regionTargets,
        });
      }
      return updateScenario(scn.id, {
        name: scn.name,
        description: scn.description,
        weights: scn.weights,
        region_targets: regionTargets,
      });
    },
    onSuccess: () => {
      setEditing(null);
      setFormError(null);
      void qc.invalidateQueries({ queryKey: ["scenarios"] });
    },
    onError: (e) => setFormError(queryErrorMessage(e, "保存失败")),
  });

  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteScenario(id),
    onSuccess: () => {
      setDeleteTarget(null);
      setDeleteError(null);
      void qc.invalidateQueries({ queryKey: ["scenarios"] });
    },
    onError: (e) => setDeleteError(queryErrorMessage(e, "删除失败")),
  });

  const saveScenario = () => {
    if (!editing) return;
    const weightCheck = validatePercentSum(
      editing.weights.map((w) => ({ label: w.asset_class, value: w.weight })),
    );
    if (!weightCheck.passed) {
      setFormError(weightCheck.message);
      return;
    }
    const regionError = validateScenarioRegionTargets(
      ensureRegionTargets(editing.region_targets),
    );
    if (regionError) {
      setFormError(regionError);
      return;
    }
    saveMut.mutate(editing);
  };

  const header = (
    <PageHeader
      title="场景配置"
      description="管理跨计划复用的场景模板；在计划设置中切换当前计划使用的模板。"
      status={
        isFetching && !isLoading ? <LoadingState label="刷新中…" className="text-xs" /> : undefined
      }
      primaryAction={{
        label: "新建场景",
        onClick: () => {
          setFormError(null);
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
        },
      }}
    />
  );

  if (isLoading && !data) {
    return (
      <div className="content-enter">
        {header}
        <LoadingState label="加载场景…" />
      </div>
    );
  }

  if (isError && !data) {
    return (
      <div className="content-enter">
        {header}
        <ErrorState
          message="无法加载场景模板。请确认后端服务可用后重试。"
          onRetry={() => void refetch()}
          backHref="/"
          technicalDetail={queryErrorMessage(error)}
        />
      </div>
    );
  }

  const scenarios = data?.scenarios ?? [];

  return (
    <div className="content-enter">
      {header}

      {!scenarios.length ? (
        <EmptyState
          title="暂无场景模板"
          description="新建场景模板后，可在计划设置中为各计划选用。"
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {scenarios.map((scn) => {
            const total = scn.weights.reduce((s, w) => s + w.weight, 0);
            const ok = Math.abs(total - 1) <= 0.0001;
            return (
              <article key={scn.id} className="rounded-lg border border-line bg-surface p-4">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <h3 className="font-medium text-ink">{scn.name}</h3>
                    <p className="text-sm text-ink-muted">{scn.description}</p>
                  </div>
                  {scn.is_builtin && (
                    <span className="shrink-0 rounded bg-surface-muted px-2 py-0.5 text-xs text-ink-muted">
                      内置
                    </span>
                  )}
                </div>
                <table className="mt-3 w-full text-sm">
                  <tbody>
                    {scn.weights.map((w) => (
                      <tr key={w.asset_class}>
                        <td className="text-ink">{assetClassLabel(w.asset_class)}</td>
                        <td className="text-right font-mono-numeric text-ink">
                          {formatPercent(w.weight)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p className={`mt-2 text-xs ${ok ? "text-positive" : "text-danger"}`}>
                  合计 {formatPercent(total)} {ok ? "，通过" : "，未通过"}
                </p>
                <p className="text-xs text-ink-muted">{scn.plan_count} 个计划使用</p>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Button
                    variant="ghost"
                    className="px-2 py-1"
                    onClick={() => {
                      setFormError(null);
                      setEditing({
                        ...scn,
                        weights: [...scn.weights],
                        region_targets: ensureRegionTargets(scn.region_targets).map((target) => ({
                          ...target,
                        })),
                      });
                    }}
                  >
                    编辑
                  </Button>
                  <Button
                    variant="ghost"
                    className="px-2 py-1"
                    onClick={() => {
                      setFormError(null);
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
                  </Button>
                  {!scn.is_builtin && scn.plan_count === 0 && (
                    <Button
                      variant="danger"
                      className="px-2 py-1"
                      onClick={() => {
                        setDeleteError(null);
                        setDeleteTarget(scn);
                      }}
                    >
                      删除
                    </Button>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      )}

      <Dialog
        open={editing !== null}
        onClose={() => {
          if (saveMut.isPending) return;
          setEditing(null);
        }}
        title="编辑场景"
        footer={
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="secondary"
              disabled={saveMut.isPending}
              onClick={() => setEditing(null)}
            >
              取消
            </Button>
            <Button pending={saveMut.isPending} onClick={saveScenario}>
              保存场景
            </Button>
          </div>
        }
      >
        {editing && (
          <div>
            <label className="block text-sm text-ink">
              名称
              <input
                className="input-base mt-1"
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
              />
            </label>
            <p className="mt-4 flex items-center text-sm font-medium text-ink">
              大类权重
              <MetricHelp termKey="portfolio_weight" />
            </p>
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
            <p className="mt-4 flex items-center text-sm font-medium text-ink">
              地区组内权重
              <MetricHelp termKey="target_weight_within_asset_class" />
            </p>
            {EDITABLE_REGION_CLASSES.map((assetClass) => {
              const regions = ensureRegionTargets(editing.region_targets).filter(
                (target) => target.asset_class === assetClass,
              );
              return (
                <div key={assetClass} className="mt-3 rounded-md border border-line p-3">
                  <p className="text-sm font-medium text-ink">{assetClassLabel(assetClass)}</p>
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
            {formError && (
              <p className="mt-4 text-sm text-danger" role="alert">
                {formError}
              </p>
            )}
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={deleteTarget !== null}
        title="删除场景"
        description={
          deleteTarget
            ? `确定删除场景「${deleteTarget.name}」？此操作不可撤销。`
            : undefined
        }
        confirmLabel="删除场景"
        variant="danger"
        pending={deleteMut.isPending}
        error={deleteError}
        onConfirm={() => {
          if (deleteTarget) deleteMut.mutate(deleteTarget.id);
        }}
        onClose={() => {
          setDeleteTarget(null);
          setDeleteError(null);
        }}
      />
    </div>
  );
}

export default function ScenariosPage() {
  return <ScenariosPageContent />;
}
