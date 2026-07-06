"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import {
  createCollection,
  type ResearchRebalancePolicy,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";

export default function NewResearchCollectionPage() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [baseCurrency, setBaseCurrency] = useState("CNY");
  const [initialAmount, setInitialAmount] = useState("1000000");
  const [rebalancePolicy, setRebalancePolicy] = useState<ResearchRebalancePolicy>("monthly");

  const createMutation = useMutation({
    mutationFn: () =>
      createCollection({
        name: name.trim(),
        description: description.trim() || undefined,
        base_currency: baseCurrency,
        initial_amount_minor: Math.round(Number(initialAmount) * 100),
        rebalance_policy: rebalancePolicy,
      }),
    onSuccess: (detail) => {
      router.push(`/research/collections/${detail.id}`);
    },
  });

  const amountValid = Number.isFinite(Number(initialAmount)) && Number(initialAmount) > 0;

  return (
    <div className="content-enter mx-auto max-w-xl">
      <PageHeader
        backHref="/research"
        backLabel="组合研究"
        title="新建研究集合"
        description="创建后可在集合编辑页添加资产、配置权重并运行回测。"
      />

      <div className="space-y-4 rounded-lg border border-line bg-surface p-5">
        <label className="block">
          <span className="mb-1 block text-sm font-medium text-ink">名称</span>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="如 中美宽基 60/40"
            className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
            data-testid="new-collection-name"
          />
        </label>

        <label className="block">
          <span className="mb-1 block text-sm font-medium text-ink">描述（可选）</span>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={2}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
          />
        </label>

        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <span className="mb-1 block text-sm font-medium text-ink">基准币种</span>
            <select
              value={baseCurrency}
              onChange={(e) => setBaseCurrency(e.target.value)}
              className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
            >
              <option value="CNY">CNY</option>
              <option value="USD">USD</option>
              <option value="HKD">HKD</option>
            </select>
          </label>

          <label className="block">
            <span className="mb-1 block text-sm font-medium text-ink">初始资金</span>
            <input
              type="number"
              value={initialAmount}
              onChange={(e) => setInitialAmount(e.target.value)}
              className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
              data-testid="new-collection-amount"
            />
          </label>
        </div>

        <label className="block">
          <span className="mb-1 block text-sm font-medium text-ink">再平衡规则</span>
          <select
            value={rebalancePolicy}
            onChange={(e) => setRebalancePolicy(e.target.value as ResearchRebalancePolicy)}
            className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
          >
            {Object.entries(REBALANCE_POLICY_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>

        {createMutation.isError && (
          <p className="text-sm text-danger" role="alert">
            创建失败：{queryErrorMessage(createMutation.error)}
          </p>
        )}

        <div className="flex justify-end gap-2">
          <Button variant="secondary" href="/research">
            取消
          </Button>
          <Button
            disabled={!name.trim() || !amountValid}
            pending={createMutation.isPending}
            onClick={() => createMutation.mutate()}
            data-testid="create-collection"
          >
            创建集合
          </Button>
        </div>
      </div>
    </div>
  );
}
