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
import { assetClassLabel, formatPercent } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import { queryErrorMessage } from "@/lib/query-error";
import {
  buildRegionTargetsPayload,
  defaultWizardRegionTargets,
} from "@/lib/wizard-allocation";
import type { AllocationScenario, RegionTarget } from "@/types/api";

function CopyIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  );
}

function EditIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4Z" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path d="M3 6h18" />
      <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <path d="M6 6v14a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2V6" />
    </svg>
  );
}

function defaultScenarioRegionTargets(): RegionTarget[] {
  return buildRegionTargetsPayload(defaultWizardRegionTargets());
}

function ensureRegionTargets(targets: RegionTarget[] | undefined): RegionTarget[] {
  return targets && targets.length > 0 ? targets : defaultScenarioRegionTargets();
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
    saveMut.mutate(editing);
  };

  const header = (
    <PageHeader
      title="配置模板"
      description="管理跨计划复用的资产配置模板（大类目标权重）；在计划设置中切换当前计划使用的模板。"
      status={
        isFetching && !isLoading ? <LoadingState label="刷新中…" className="text-xs" /> : undefined
      }
      primaryAction={{
        label: "新建配置模板",
        onClick: () => {
          setFormError(null);
          setEditing({
            id: "new_" + Date.now(),
            name: "新配置模板",
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
        <LoadingState label="加载配置模板…" />
      </div>
    );
  }

  if (isError && !data) {
    return (
      <div className="content-enter">
        {header}
        <ErrorState
          message="无法加载配置模板。请确认后端服务可用后重试。"
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
          title="暂无配置模板"
          description="新建配置模板后，可在计划设置中为各计划选用。"
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
                    <h3 className="flex flex-wrap items-center gap-2 font-medium text-ink">
                      <span className="truncate">{scn.name}</span>
                      {scn.is_builtin && (
                        <span className="shrink-0 rounded bg-surface-muted px-2 py-0.5 text-xs font-normal text-ink-muted">
                          内置
                        </span>
                      )}
                    </h3>
                    {scn.description && (
                      <p className="text-sm text-ink-muted">{scn.description}</p>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      variant="ghost"
                      className="px-2 py-1"
                      aria-label="复制配置模板"
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
                      <CopyIcon />
                    </Button>
                    {!scn.is_builtin && (
                      <Button
                        variant="ghost"
                        className="px-2 py-1"
                        aria-label="编辑配置模板"
                        onClick={() => {
                          setFormError(null);
                          setEditing({
                            ...scn,
                            weights: [...scn.weights],
                            region_targets: ensureRegionTargets(scn.region_targets).map(
                              (target) => ({ ...target }),
                            ),
                          });
                        }}
                      >
                        <EditIcon />
                      </Button>
                    )}
                    {!scn.is_builtin && scn.plan_count === 0 && (
                      <Button
                        variant="danger"
                        className="px-2 py-1"
                        aria-label="删除配置模板"
                        onClick={() => {
                          setDeleteError(null);
                          setDeleteTarget(scn);
                        }}
                      >
                        <TrashIcon />
                      </Button>
                    )}
                  </div>
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
                {!ok && (
                  <p className="mt-2 text-xs text-danger" role="alert">
                    权重合计 {formatPercent(total)}，请调整到 100%
                  </p>
                )}
                {scn.plan_count > 0 && (
                  <p className="mt-2 text-xs text-ink-muted">{scn.plan_count} 个计划使用</p>
                )}
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
        title="编辑配置模板"
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
              保存配置模板
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
            <p className="mt-3 text-xs text-ink-muted">
              配置模板只决定大类目标权重；国内/国外配比在各计划的设置中维护。
            </p>
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
        title="删除配置模板"
        description={
          deleteTarget
            ? `确定删除配置模板「${deleteTarget.name}」？此操作不可撤销。`
            : undefined
        }
        confirmLabel="删除配置模板"
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
