"use client";

import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { PercentInput } from "@/components/ui/PercentInput";
import { usePlanEdit } from "../layout";
import {
  applyScenario,
  createScenario,
  deleteScenario,
  listScenarios,
  updateScenario,
} from "@/lib/api/allocation";
import { getPlan } from "@/lib/api/plans";
import { assetClassLabel, formatPercent } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import type { AllocationScenario } from "@/types/api";
import { ApiError } from "@/lib/api/client";

export function ScenariosContent() {
  const planId = useParams().id as string;
  const qc = useQueryClient();
  const { markDirty, markClean } = usePlanEdit();
  const [editing, setEditing] = useState<AllocationScenario | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [applyPreview, setApplyPreview] = useState<{
    scenarioId: string;
    scenarioName: string;
    before: { asset_class: string; weight: number }[];
    after: { asset_class: string; weight: number }[];
  } | null>(null);

  const planQ = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const { data, isLoading } = useQuery({
    queryKey: ["scenarios"],
    queryFn: listScenarios,
  });

  const previewMut = useMutation({
    mutationFn: async (scenarioId: string) => {
      if (!planQ.data) throw new Error("plan");
      const scn = data?.scenarios.find((s) => s.id === scenarioId);
      const res = await applyScenario(planId, {
        scenario_id: scenarioId,
        config_version: planQ.data.config_version,
        dry_run: true,
      });
      return {
        scenarioId,
        scenarioName: scn?.name ?? scenarioId,
        before: res.before,
        after: res.after,
      };
    },
    onSuccess: (res) => {
      setApplyPreview({
        scenarioId: res.scenarioId,
        scenarioName: res.scenarioName,
        before: res.before,
        after: res.after,
      });
      setError(null);
    },
    onError: (e) => setError(e instanceof ApiError ? e.message : "预览失败"),
  });

  const applyMut = useMutation({
    mutationFn: (scenarioId: string) => {
      if (!planQ.data) throw new Error("plan");
      return applyScenario(planId, {
        scenario_id: scenarioId,
        config_version: planQ.data.config_version,
        dry_run: false,
      });
    },
    onSuccess: () => {
      setApplyPreview(null);
      void qc.invalidateQueries({ queryKey: ["plan", planId] });
      void qc.invalidateQueries({ queryKey: ["allocation", planId] });
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
    },
    onError: (e) => setError(e instanceof ApiError ? e.message : "应用失败"),
  });

  const saveScenario = async () => {
    if (!editing) return;
    const check = validatePercentSum(
      editing.weights.map((w) => ({ label: w.asset_class, value: w.weight })),
    );
    if (!check.passed) {
      setError(check.message);
      return;
    }
    try {
      if (editing.id.startsWith("new_")) {
        await createScenario({
          name: editing.name,
          description: editing.description,
          weights: editing.weights,
        });
      } else {
        await updateScenario(editing.id, {
          name: editing.name,
          description: editing.description,
          weights: editing.weights,
        });
      }
      setEditing(null);
      markClean();
      void qc.invalidateQueries({ queryKey: ["scenarios"] });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "保存失败");
    }
  };

  if (isLoading) return <p>加载场景…</p>;

  return (
    <div className="space-y-4">
      <div className="flex justify-between">
        <h2 className="text-lg font-medium">场景配置</h2>
        <button
          type="button"
          className="rounded-md border px-3 py-1.5 text-sm"
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
              created_at: 0,
              updated_at: 0,
            });
            markDirty();
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
                  onClick={() => {
                    setEditing({ ...scn, weights: [...scn.weights] });
                    markDirty();
                  }}
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
                    });
                    markDirty();
                  }}
                >
                  复制
                </button>
                <button
                  type="button"
                  className="text-sm font-medium underline"
                  onClick={() => previewMut.mutate(scn.id)}
                  disabled={previewMut.isPending}
                >
                  应用到当前计划
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

      {applyPreview && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4">
          <div className="max-h-[90vh] w-full max-w-lg overflow-auto rounded-lg bg-white p-6 shadow-xl">
            <h3 className="text-lg font-medium">应用场景：{applyPreview.scenarioName}</h3>
            <p className="mt-2 text-sm text-slate-600">仅更新当前计划的大类权重，地区权重不变。</p>
            <div className="mt-4 grid gap-4 sm:grid-cols-2">
              <div>
                <h4 className="text-sm font-medium text-slate-500">变更前</h4>
                <ul className="mt-2 text-sm">
                  {applyPreview.before.map((w) => (
                    <li key={w.asset_class}>
                      {assetClassLabel(w.asset_class)} {formatPercent(w.weight)}
                    </li>
                  ))}
                </ul>
              </div>
              <div>
                <h4 className="text-sm font-medium text-slate-500">变更后</h4>
                <ul className="mt-2 text-sm">
                  {applyPreview.after.map((w) => (
                    <li key={w.asset_class}>
                      {assetClassLabel(w.asset_class)} {formatPercent(w.weight)}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button type="button" className="px-3 py-2 text-sm" onClick={() => setApplyPreview(null)}>
                取消
              </button>
              <button
                type="button"
                className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white"
                disabled={applyMut.isPending}
                onClick={() => applyMut.mutate(applyPreview.scenarioId)}
              >
                确认应用
              </button>
            </div>
          </div>
        </div>
      )}

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
            <div className="mt-6 flex justify-end gap-2">
              <button
                type="button"
                className="px-3 py-2 text-sm"
                onClick={() => {
                  setEditing(null);
                  markClean();
                }}
              >
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
  const planId = useParams().id as string;
  const router = useRouter();
  useEffect(() => {
    router.replace(`/plans/${planId}/settings?section=scenarios`);
  }, [planId, router]);
  return <p className="text-slate-600">正在前往计划设置…</p>;
}
