"use client";

import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import type { PlanCashFlow } from "@/types/api";

export function CashFlowEditor({
  planId,
  flows,
  onChange,
}: {
  planId: string;
  flows: PlanCashFlow[];
  onChange: (flows: PlanCashFlow[]) => void;
}) {
  const addFlow = () => {
    onChange([
      ...flows,
      {
        id: `cf_${Date.now()}`,
        plan_id: planId,
        name: "新现金流",
        kind: "expense",
        amount_minor: 0,
        start_month_offset: 0,
        end_month_offset: 0,
        recurrence: "once",
        inflation_linked: false,
        annual_growth_rate: 0,
        enabled: true,
        note: "",
      },
    ]);
  };

  const update = (idx: number, patch: Partial<PlanCashFlow>) => {
    const next = [...flows];
    next[idx] = { ...next[idx], ...patch };
    onChange(next);
  };

  const remove = (idx: number) => {
    onChange(flows.filter((_, i) => i !== idx));
  };

  return (
    <div className="space-y-3">
      {flows.length === 0 && (
        <p className="text-sm text-slate-500">暂无额外现金流事件。可添加一次性或年度收入/支出。</p>
      )}
      {flows.map((f, i) => (
        <div key={f.id} className="rounded-md border border-slate-200 p-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="text-sm">
              名称
              <input
                className="mt-1 w-full rounded-md border px-2 py-1.5"
                value={f.name}
                onChange={(e) => update(i, { name: e.target.value })}
              />
            </label>
            <label className="text-sm">
              类型
              <select
                className="mt-1 w-full rounded-md border px-2 py-1.5"
                value={f.kind}
                onChange={(e) => update(i, { kind: e.target.value as PlanCashFlow["kind"] })}
              >
                <option value="income">收入</option>
                <option value="expense">支出</option>
              </select>
            </label>
            <MoneyInput
              label="金额"
              valueMinor={f.amount_minor}
              onChange={(v) => update(i, { amount_minor: v })}
            />
            <label className="text-sm">
              重复方式
              <select
                className="mt-1 w-full rounded-md border px-2 py-1.5"
                value={f.recurrence}
                onChange={(e) =>
                  update(i, { recurrence: e.target.value as PlanCashFlow["recurrence"] })
                }
              >
                <option value="once">一次性</option>
                <option value="monthly">月度</option>
                <option value="annual">年度</option>
              </select>
            </label>
            <label className="text-sm">
              起始月（相对规划起点）
              <input
                type="number"
                min={0}
                className="mt-1 w-full rounded-md border px-2 py-1.5"
                value={f.start_month_offset}
                onChange={(e) => update(i, { start_month_offset: Number(e.target.value) })}
              />
            </label>
            <label className="text-sm">
              结束月
              <input
                type="number"
                min={0}
                className="mt-1 w-full rounded-md border px-2 py-1.5"
                value={f.end_month_offset}
                onChange={(e) => update(i, { end_month_offset: Number(e.target.value) })}
              />
            </label>
            <PercentInput
              label="年增长率"
              value={f.annual_growth_rate}
              onChange={(v) => update(i, { annual_growth_rate: v })}
            />
            <label className="flex items-center gap-2 self-end text-sm">
              <input
                type="checkbox"
                checked={f.inflation_linked}
                onChange={(e) => update(i, { inflation_linked: e.target.checked })}
              />
              随通胀调整
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={f.enabled}
                onChange={(e) => update(i, { enabled: e.target.checked })}
              />
              启用
            </label>
          </div>
          <button
            type="button"
            className="mt-2 text-sm text-red-600 underline"
            onClick={() => remove(i)}
          >
            删除
          </button>
        </div>
      ))}
      <button type="button" className="text-sm underline" onClick={addFlow}>
        + 添加现金流事件
      </button>
    </div>
  );
}
