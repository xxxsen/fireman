"use client";

import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { listPlans } from "@/lib/api/plans";
import { createCollection } from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { LoadingState } from "@/components/ui/LoadingState";

export interface CopyFromPlanDialogProps {
  open: boolean;
  onClose: () => void;
  onCreated: (collectionId: string) => void;
}

/**
 * Pick an existing FIRE plan and create a research collection from its
 * enabled holdings (backend converts current amounts into weights).
 */
export function CopyFromPlanDialog({ open, onClose, onCreated }: CopyFromPlanDialogProps) {
  const [selectedPlanId, setSelectedPlanId] = useState<string>("");
  const [name, setName] = useState("");

  const plansQuery = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
    enabled: open,
  });

  const createMutation = useMutation({
    mutationFn: () =>
      createCollection({
        name: name.trim() || defaultName(),
        from_plan_id: selectedPlanId,
      }),
    onSuccess: (detail) => {
      onCreated(detail.id);
    },
  });

  const plans = plansQuery.data ?? [];
  const selectedPlan = plans.find((p) => p.id === selectedPlanId);

  function defaultName(): string {
    return selectedPlan ? `${selectedPlan.name} · 研究` : "计划复制集合";
  }

  return (
    <Dialog
      open={open}
      onClose={() => {
        if (createMutation.isPending) return;
        onClose();
      }}
      title="从计划复制"
      footer={
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" disabled={createMutation.isPending} onClick={onClose}>
            取消
          </Button>
          <Button
            disabled={!selectedPlanId}
            pending={createMutation.isPending}
            onClick={() => createMutation.mutate()}
            data-testid="copy-from-plan-confirm"
          >
            创建研究集合
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-ink-muted">
          选择一个 FIRE 计划，将其启用持仓的当前金额换算为权重，创建研究集合。
        </p>

        {plansQuery.isLoading && <LoadingState label="加载计划列表…" />}
        {plansQuery.isError && (
          <p className="text-sm text-danger" role="alert">
            加载计划失败：{queryErrorMessage(plansQuery.error)}
          </p>
        )}

        {!plansQuery.isLoading && !plansQuery.isError && plans.length === 0 && (
          <p className="text-sm text-ink-muted">暂无可用计划，请先创建 FIRE 计划。</p>
        )}

        {plans.length > 0 && (
          <div className="space-y-2" role="radiogroup" aria-label="选择计划">
            {plans.map((plan) => (
              <label
                key={plan.id}
                className="flex cursor-pointer items-center gap-3 rounded-md border border-line px-3 py-2 text-sm hover:bg-surface-muted has-[:checked]:border-brand has-[:checked]:bg-brand/5"
              >
                <input
                  type="radio"
                  name="copy-from-plan"
                  value={plan.id}
                  checked={selectedPlanId === plan.id}
                  onChange={() => setSelectedPlanId(plan.id)}
                />
                <span className="flex-1">
                  <span className="font-medium text-ink">{plan.name}</span>
                  <span className="ml-2 text-xs text-ink-muted">
                    {plan.base_currency} · 估值日 {plan.valuation_date}
                  </span>
                </span>
              </label>
            ))}
          </div>
        )}

        <div>
          <label className="mb-1 block text-sm font-medium text-ink" htmlFor="copy-plan-name">
            集合名称（可选）
          </label>
          <input
            id="copy-plan-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={selectedPlan ? `${selectedPlan.name} · 研究` : "默认使用计划名"}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
          />
        </div>

        {createMutation.isError && (
          <p className="text-sm text-danger" role="alert">
            创建失败：{queryErrorMessage(createMutation.error)}
          </p>
        )}
      </div>
    </Dialog>
  );
}
